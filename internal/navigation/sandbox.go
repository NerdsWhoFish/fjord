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

	"github.com/NerdsWhoFish/fjord/internal/lspconfig"
)

func startProfileServer(ctx context.Context, dir, root string, profile lspconfig.Profile) (*rpcClient, map[string]bool, error) {
	profile, cleanupSchemas, err := prepareProfileSchemas(dir, profile)
	if err != nil {
		return nil, nil, err
	}
	started := false
	defer func() {
		if !started {
			cleanupSchemas()
		}
	}()
	config, err := configureProfile(ctx, dir, root, profile)
	if err != nil {
		return nil, nil, err
	}
	cmd, stop, err := sandboxWithPaths(dir, root, config.executable, config.readPaths, config.env, config.args...)
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
	client := newRPCWithSettings(stdout, stdin, func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		stop()
		<-exited
		cleanupSchemas()
	}, profile.Settings)
	initCtx, cancelInit := context.WithTimeout(ctx, 30*time.Second)
	defer cancelInit()
	stopInitialization := context.AfterFunc(initCtx, client.close)
	defer stopInitialization()
	uri := (&url.URL{Scheme: "file", Path: root}).String()
	var initialized struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	err = client.call(initCtx, "initialize", map[string]any{
		"processId": nil, "rootUri": uri,
		"workspaceFolders": []map[string]string{{"uri": uri, "name": "revision"}},
		"capabilities": map[string]any{
			"general":   map[string]any{"positionEncodings": []string{"utf-16"}},
			"workspace": map[string]any{"configuration": true, "workspaceFolders": true},
			"textDocument": map[string]any{
				"hover":              map[string]any{"contentFormat": []string{"plaintext"}},
				"publishDiagnostics": map[string]any{"versionSupport": true, "relatedInformation": false},
				"diagnostic":         map[string]any{"dynamicRegistration": false},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"callHierarchy":      map[string]any{"dynamicRegistration": false},
				"typeHierarchy":      map[string]any{"dynamicRegistration": false},
				"inlayHint":          map[string]any{"dynamicRegistration": false},
				"codeAction":         map[string]any{"disabledSupport": true, "dataSupport": true, "resolveSupport": map[string]any{"properties": []string{"edit"}}, "codeActionLiteralSupport": map[string]any{"codeActionKind": map[string]any{"valueSet": []string{"", "quickfix", "refactor", "refactor.extract", "refactor.inline", "refactor.rewrite", "source", "source.organizeImports", "source.fixAll"}}}},
			},
		},
		"initializationOptions": config.options,
	}, &initialized)
	if err != nil {
		client.close()
		return nil, nil, fmt.Errorf("isolated %s initialization failed: %w", config.name, err)
	}
	if raw, ok := initialized.Capabilities["positionEncoding"]; ok {
		var encoding string
		if json.Unmarshal(raw, &encoding) != nil || encoding != "utf-16" {
			client.close()
			return nil, nil, errors.New("language server negotiated an unsupported position encoding; UTF-16 is required")
		}
	}
	if err = client.notify("initialized", map[string]any{}); err != nil {
		client.close()
		return nil, nil, err
	}
	if len(profile.Settings) > 0 {
		if err := client.notify("workspace/didChangeConfiguration", map[string]any{"settings": profile.Settings}); err != nil {
			client.close()
			return nil, nil, err
		}
	}
	capabilities := make(map[string]bool)
	for method, provider := range methodProviders {
		value := bytes.TrimSpace(initialized.Capabilities[provider])
		capabilities[method] = bytes.Equal(value, []byte("true")) || (len(value) > 0 && value[0] == '{')
	}
	capabilities["pullDiagnostics"] = capabilities["diagnostics"]
	var diagnostics struct {
		Identifier string `json:"identifier"`
	}
	_ = json.Unmarshal(initialized.Capabilities["diagnosticProvider"], &diagnostics)
	if len(diagnostics.Identifier) <= 256 {
		client.diagnosticIdentifier = diagnostics.Identifier
	}
	// Push diagnostics have no provider flag; silence must not imply clean code.
	capabilities["diagnostics"] = true
	var codeActions struct {
		ResolveProvider bool `json:"resolveProvider"`
	}
	_ = json.Unmarshal(initialized.Capabilities["codeActionProvider"], &codeActions)
	capabilities["resolveCodeAction"] = codeActions.ResolveProvider
	started = true
	return client, capabilities, nil
}

type runtimeConfiguration struct {
	executable  string
	runtimeRoot string
	name        string
	env         []string
	options     map[string]any
	args        []string
	readPaths   []string
}

func configureLanguage(ctx context.Context, dir, root, language string) (runtimeConfiguration, error) {
	for _, p := range lspconfig.Defaults() {
		if p.Name == language {
			return configureProfile(ctx, dir, root, p)
		}
	}
	return runtimeConfiguration{}, errors.New("unsupported language-server runtime")
}

func configureProfile(ctx context.Context, dir, root string, profile lspconfig.Profile) (runtimeConfiguration, error) {
	config := runtimeConfiguration{name: profile.Name, options: profile.InitializationOptions}
	if profile.Disabled {
		return config, errors.New("language-server profile is disabled")
	}
	if err := lspconfig.Validate([]lspconfig.Profile{profile}); err != nil {
		return config, errors.New("invalid language-server profile")
	}
	executable, err := exec.LookPath(profile.Command[0])
	if err != nil {
		return config, fmt.Errorf("%s navigation requires %s on the Fjord server's PATH", profile.Name, filepath.Base(profile.Command[0]))
	}
	config.executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return config, errors.New("cannot resolve language-server executable")
	}
	config.args = append([]string(nil), profile.Command[1:]...)
	config.readPaths, err = resolveRuntimePaths(profile.RuntimePaths)
	if err != nil {
		return config, err
	}
	// A script must explicitly name its interpreter; never expand filesystem
	// access implicitly from the executable's shebang.
	f, err := os.Open(config.executable)
	if err != nil {
		return config, errors.New("cannot read language-server executable")
	}
	var prefix [2]byte
	_, _ = f.Read(prefix[:])
	_ = f.Close()
	if string(prefix[:]) == "#!" {
		return config, errors.New("language-server scripts require an explicit interpreter in command")
	}
	for _, name := range []string{"bin", "home", "cache", "tmp", "go", "gopls-cache"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			return config, errors.New("cannot create isolated language-server cache")
		}
	}
	config.env = []string{
		"HOME=" + filepath.Join(dir, "home"), "TMPDIR=" + filepath.Join(dir, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(dir, "cache"),
		// Node subprocesses can replace execArgv, losing the explicit OpenSSL flag.
		"OPENSSL_CONF=/dev/null",
		"GOTELEMETRY=off", "GOMAXPROCS=2", "GOMEMLIMIT=512MiB",
	}
	for key, value := range profile.Environment {
		config.env = append(config.env, key+"="+value)
	}
	if profile.Adapter != "go" {
		config.env = append(config.env, "PATH="+filepath.Join(dir, "bin"))
		return config, nil
	}
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
		"GOPRIVATE=", "GONOSUMDB=", "GONOPROXY=", "GOFLAGS=-mod=readonly",
	)
	if info, err := os.Stat(filepath.Join(root, "vendor", "modules.txt")); err == nil && info.Mode().IsRegular() {
		config.env = replaceGoEnv(config.env, map[string]string{"GOFLAGS": "-mod=vendor"})
	}
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
	moduleCache, err := prepareGoDependencies(ctx, dir, root, goBin, config.runtimeRoot, config.env)
	if err != nil {
		return config, err
	}
	if moduleCache != "" {
		config.env = replaceGoEnv(config.env, map[string]string{"GOMODCACHE": moduleCache})
	}
	if err := validatePackages(ctx, dir, root, goBin, config.runtimeRoot, config.env); err != nil {
		return config, goGraphError(root, err)
	}
	config.readPaths = append(config.readPaths, config.runtimeRoot)
	return config, nil
}

func resolveRuntimePaths(paths []string) ([]string, error) {
	result := make([]string, 0, len(paths)*2)
	for _, p := range paths {
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			return nil, errors.New("language-server runtime path is unavailable")
		}
		// Preserve an explicit alias as well as its target for runtimes whose
		// loaders resolve symlinks themselves, including Homebrew dylibs.
		result = append(result, p)
		if real != p {
			result = append(result, real)
		}
	}
	return result, nil
}

func sandboxCommand(dir, root, executable, goRoot string, env []string, commandArgs ...string) (*exec.Cmd, context.CancelFunc, error) {
	return sandboxWithPaths(dir, root, executable, []string{goRoot}, env, commandArgs...)
}

func sandboxWithPaths(dir, root, executable string, runtimePaths, env []string, commandArgs ...string) (*exec.Cmd, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), workspaceLifetime)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
			cancel()
			return nil, nil, ErrSandbox
		}
		readPaths := append([]string{"/System", "/usr/lib", "/usr/share", "/Library/Apple", dir}, runtimePaths...)
		var profile strings.Builder
		profile.WriteString("(version 1)\n(deny default)\n(allow process-exec process-fork sysctl-read)\n")
		profile.WriteString("(allow signal (target self))\n")
		// macOS dyld inspects the root directory while locating its shared cache.
		profile.WriteString("(allow file-read* (literal \"/\"))\n")
		for _, p := range readPaths {
			kind := "subpath"
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				kind = "literal"
			}
			fmt.Fprintf(&profile, "(allow file-read* (%s %s))\n", kind, strconv.Quote(p))
		}
		// Dynamic loaders inspect ancestors and read package-manager symlinks.
		// Permit only those directory entries, never their sibling contents.
		ancestors := make(map[string]bool)
		for _, p := range append(readPaths, executable) {
			for parent := filepath.Dir(p); parent != "/"; parent = filepath.Dir(parent) {
				if ancestors[parent] {
					continue
				}
				ancestors[parent] = true
				operation := "file-read-metadata"
				if info, err := os.Lstat(parent); err == nil && info.Mode()&os.ModeSymlink != 0 {
					operation = "file-read*"
				}
				fmt.Fprintf(&profile, "(allow %s (literal %s))\n", operation, strconv.Quote(parent))
			}
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
		for _, p := range runtimePaths {
			args = append(args, "--ro-bind", p, p)
		}
		args = append(args, "--ro-bind", executable, executable, "--ro-bind", dir, dir)
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
	var output, stderr boundedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return errors.New("isolated Go package loading timed out")
		}
		return goPackageError(stderr.String())
	}
	decoder := json.NewDecoder(&output)
	count := 0
	for {
		var pkg struct {
			Error      *struct{ Err string }
			DepsErrors []struct{ Err string }
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
			if pkg.Error != nil {
				return goPackageError(pkg.Error.Err)
			}
			if len(pkg.DepsErrors) > 0 {
				return goPackageError(pkg.DepsErrors[0].Err)
			}
			return goPackageError("")
		}
	}
	if count == 0 {
		return errors.New("no Go packages found in this file's module")
	}
	return nil
}
