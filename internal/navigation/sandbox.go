package navigation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func startServer(ctx context.Context, dir, root string) (*rpcClient, error) {
	gopls, err := exec.LookPath("gopls")
	if err != nil {
		return nil, errors.New("gopls is not installed; install golang.org/x/tools/gopls")
	}
	gopls, err = filepath.EvalSymlinks(gopls)
	if err != nil {
		return nil, errors.New("cannot resolve gopls executable")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, errors.New("Go is not installed")
	}
	goBin, err = filepath.EvalSymlinks(goBin)
	if err != nil {
		return nil, errors.New("cannot resolve Go executable")
	}
	for _, name := range []string{"home", "cache", "tmp", "go", "gopls-cache"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			return nil, errors.New("cannot create isolated Go cache")
		}
	}
	env := []string{
		"PATH=" + filepath.Dir(goBin) + ":/usr/bin:/bin",
		"HOME=" + filepath.Join(dir, "home"),
		"TMPDIR=" + filepath.Join(dir, "tmp"),
		"GOTMPDIR=" + filepath.Join(dir, "tmp"),
		"GOCACHE=" + filepath.Join(dir, "cache"),
		"GOPATH=" + filepath.Join(dir, "go"),
		"GOPLSCACHE=" + filepath.Join(dir, "gopls-cache"),
		"GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOVCS=*:off", "CGO_ENABLED=0", "GOTELEMETRY=off", "GOMAXPROCS=2", "GOMEMLIMIT=512MiB",
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(probeCtx, goBin, "env", "GOROOT")
	probe.Env = env
	probe.Dir = dir
	out, err := probe.Output()
	if err != nil {
		return nil, errors.New("cannot locate Go toolchain")
	}
	goRoot, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, errors.New("cannot resolve Go toolchain")
	}
	if err := validatePackages(ctx, dir, root, goBin, goRoot, env); err != nil {
		return nil, err
	}
	cmd, stop, err := sandboxCommand(dir, root, gopls, goRoot, env, "serve")
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stop()
		return nil, errors.New("cannot open language-server output")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		stop()
		return nil, errors.New("cannot open language-server input")
	}
	// gopls diagnostics may contain source, file paths and module names.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		stop()
		return nil, fmt.Errorf("cannot start isolated language server: %w", ErrSandbox)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	client := newRPC(stdout, stdin, func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		stop()
		<-exited
	})
	initCtx, cancelInit := context.WithTimeout(ctx, 30*time.Second)
	defer cancelInit()
	uri := (&url.URL{Scheme: "file", Path: root}).String()
	var initialized json.RawMessage
	err = client.call(initCtx, "initialize", map[string]any{
		"processId": nil, "rootUri": uri,
		"workspaceFolders": []map[string]string{{"uri": uri, "name": "revision"}},
		"capabilities": map[string]any{
			"general":      map[string]any{"positionEncodings": []string{"utf-16"}},
			"textDocument": map[string]any{"hover": map[string]any{"contentFormat": []string{"plaintext"}}},
		},
		"initializationOptions": map[string]any{"staticcheck": false, "checkBugReports": false, "telemetryPrompt": false},
	}, &initialized)
	if err != nil {
		client.close()
		return nil, fmt.Errorf("isolated gopls initialization failed: %w", err)
	}
	if err = client.notify("initialized", map[string]any{}); err != nil {
		client.close()
		return nil, err
	}
	return client, nil
}

func sandboxCommand(dir, root, executable, goRoot string, env []string, commandArgs ...string) (*exec.Cmd, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), workspaceLifetime)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
			cancel()
			return nil, nil, ErrSandbox
		}
		readPaths := []string{"/System", "/usr/lib", "/usr/share", "/Library/Apple", goRoot, dir}
		var profile strings.Builder
		profile.WriteString("(version 1)\n(deny default)\n(allow process-exec process-fork sysctl-read)\n")
		profile.WriteString("(allow signal (target self))\n")
		// macOS dyld inspects the root directory while locating its shared cache.
		profile.WriteString("(allow file-read* (literal \"/\"))\n")
		for _, p := range readPaths {
			fmt.Fprintf(&profile, "(allow file-read* (subpath %s))\n", strconv.Quote(p))
		}
		fmt.Fprintf(&profile, "(allow file-read* (literal %s) (literal \"/dev/urandom\") (literal \"/dev/random\"))\n", strconv.Quote(executable))
		profile.WriteString("(allow file-read* file-write* (literal \"/dev/null\"))\n")
		for _, name := range []string{"home", "cache", "tmp", "go", "gopls-cache"} {
			fmt.Fprintf(&profile, "(allow file-write* (subpath %s))\n", strconv.Quote(filepath.Join(dir, name)))
		}
		args := append([]string{"-p", profile.String(), executable}, commandArgs...)
		cmd = exec.CommandContext(ctx, "/usr/bin/sandbox-exec", args...)
	case "linux":
		bwrap, err := exec.LookPath("bwrap")
		if err != nil {
			cancel()
			return nil, nil, ErrSandbox
		}
		args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp"}
		for _, p := range []string{"/usr/bin", "/usr/lib", "/usr/lib64", "/usr/share", "/bin", "/lib", "/lib64"} {
			if _, err := os.Stat(p); err == nil {
				args = append(args, "--ro-bind", p, p)
			}
		}
		args = append(args, "--ro-bind", goRoot, goRoot, "--ro-bind", executable, executable, "--ro-bind", dir, dir)
		for _, name := range []string{"home", "cache", "tmp", "go", "gopls-cache"} {
			p := filepath.Join(dir, name)
			args = append(args, "--bind", p, p)
		}
		args = append(args, "--chdir", root, "--", executable)
		args = append(args, commandArgs...)
		cmd = exec.CommandContext(ctx, bwrap, args...)
	default:
		cancel()
		return nil, nil, ErrSandbox
	}
	cmd.Env = env
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd, cancel, nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxMessageBytes {
		return 0, errors.New("package metadata exceeds limit")
	}
	return b.Buffer.Write(p)
}

func validatePackages(ctx context.Context, dir, root, goBin, goRoot string, env []string) error {
	cmd, cancel, err := sandboxCommand(dir, root, goBin, goRoot, env, "list", "-deps", "-e", "-json", "./...")
	if err != nil {
		return err
	}
	defer cancel()
	timer := time.AfterFunc(30*time.Second, cancel)
	defer timer.Stop()
	stopOnCancel := context.AfterFunc(ctx, cancel)
	defer stopOnCancel()
	var output boundedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return errors.New("isolated Go package loading failed; ensure the sandbox works and the repository has a root go.mod")
	}
	decoder := json.NewDecoder(&output)
	count := 0
	for {
		var pkg struct {
			Error      *json.RawMessage
			DepsErrors []json.RawMessage
			Incomplete bool
		}
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("invalid Go package metadata")
		}
		count++
		if pkg.Error != nil || len(pkg.DepsErrors) > 0 || pkg.Incomplete {
			return errors.New("Go packages could not be fully loaded; navigation requires local or vendored dependencies and a compatible installed Go toolchain")
		}
	}
	if count == 0 {
		return errors.New("no Go packages found; navigation currently requires a Go module at the repository root")
	}
	return nil
}
