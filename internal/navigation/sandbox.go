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

func startLanguageServer(ctx context.Context, dir, root, language string) (*rpcClient, map[string]bool, error) {
	config, err := configureLanguage(ctx, dir, root, language)
	if err != nil {
		return nil, nil, err
	}
	cmd, stop, err := sandboxCommand(dir, root, config.executable, config.runtimeRoot, config.env, "serve")
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stop()
		return nil, nil, errors.New("cannot open language-server output")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		stop()
		return nil, nil, errors.New("cannot open language-server input")
	}
	// Language-server diagnostics may contain source, paths and module names.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		stop()
		return nil, nil, fmt.Errorf("cannot start isolated %s language server: %w", config.name, ErrSandbox)
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
	var initialized struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	err = client.call(initCtx, "initialize", map[string]any{
		"processId": nil, "rootUri": uri,
		"workspaceFolders": []map[string]string{{"uri": uri, "name": "revision"}},
		"capabilities": map[string]any{
			"general":      map[string]any{"positionEncodings": []string{"utf-16"}},
			"textDocument": map[string]any{"hover": map[string]any{"contentFormat": []string{"plaintext"}}},
		},
		"initializationOptions": config.options,
	}, &initialized)
	if err != nil {
		client.close()
		return nil, nil, fmt.Errorf("isolated %s initialization failed: %w", config.name, err)
	}
	if err = client.notify("initialized", map[string]any{}); err != nil {
		client.close()
		return nil, nil, err
	}
	capabilities := make(map[string]bool)
	for _, method := range []string{"definition", "implementation", "references", "hover"} {
		value := bytes.TrimSpace(initialized.Capabilities[method+"Provider"])
		capabilities[method] = bytes.Equal(value, []byte("true")) || (len(value) > 0 && value[0] == '{')
	}
	return client, capabilities, nil
}

type runtimeConfiguration struct {
	executable  string
	runtimeRoot string
	name        string
	env         []string
	options     map[string]any
}

func configureLanguage(ctx context.Context, dir, root, language string) (runtimeConfiguration, error) {
	config := runtimeConfiguration{runtimeRoot: dir}
	for _, name := range []string{"bin", "home", "cache", "tmp", "go", "gopls-cache"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			return config, errors.New("cannot create isolated language-server cache")
		}
	}
	config.env = []string{
		"HOME=" + filepath.Join(dir, "home"), "TMPDIR=" + filepath.Join(dir, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(dir, "cache"),
		"GOTELEMETRY=off", "GOMAXPROCS=2", "GOMEMLIMIT=512MiB",
	}
	if language == "terraform" {
		executable, err := exec.LookPath("tofu-ls")
		if err != nil {
			return config, errors.New("OpenTofu/Terraform navigation requires tofu-ls on the Fjord server's PATH")
		}
		config.executable, err = filepath.EvalSymlinks(executable)
		if err != nil {
			return config, errors.New("cannot resolve tofu-ls executable")
		}
		config.name = "tofu-ls"
		// An empty private executable directory prevents discovery of tofu or
		// terraform. Static LSP navigation needs neither CLI nor provider execution.
		config.env = append(config.env, "PATH="+filepath.Join(dir, "bin"), "TF_IN_AUTOMATION=1", "CHECKPOINT_DISABLE=1")
		config.options = map[string]any{
			"experimentalFeatures": map[string]bool{"validateOnSave": false},
			"validation":           map[string]bool{"enableEnhancedValidation": false},
		}
		return config, nil
	}
	if language != "go" {
		return config, errors.New("unsupported language-server runtime")
	}
	executable, err := exec.LookPath("gopls")
	if err != nil {
		return config, errors.New("Go navigation requires gopls on the Fjord server's PATH")
	}
	config.executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return config, errors.New("cannot resolve gopls executable")
	}
	config.name = "gopls"
	goBin, err := exec.LookPath("go")
	if err != nil {
		return config, errors.New("Go navigation requires an installed Go toolchain")
	}
	goBin, err = filepath.EvalSymlinks(goBin)
	if err != nil {
		return config, errors.New("cannot resolve Go executable")
	}
	config.env = append(config.env,
		"PATH="+filepath.Dir(goBin)+":/usr/bin:/bin", "GOTMPDIR="+filepath.Join(dir, "tmp"),
		"GOCACHE="+filepath.Join(dir, "cache"), "GOPATH="+filepath.Join(dir, "go"), "GOPLSCACHE="+filepath.Join(dir, "gopls-cache"),
		"GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOVCS=*:off", "CGO_ENABLED=0",
	)
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(probeCtx, goBin, "env", "GOROOT")
	probe.Env, probe.Dir = config.env, dir
	out, err := probe.Output()
	if err != nil {
		return config, errors.New("cannot locate Go toolchain")
	}
	config.runtimeRoot, err = filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return config, errors.New("cannot resolve Go toolchain")
	}
	if err := validatePackages(ctx, dir, root, goBin, config.runtimeRoot, config.env); err != nil {
		return config, err
	}
	config.options = map[string]any{"staticcheck": false, "checkBugReports": false, "telemetryPrompt": false}
	return config, nil
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
		return errors.New("isolated Go package loading failed; check the sandbox and this file's Go module")
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
		return errors.New("no Go packages found in this file's module")
	}
	return nil
}
