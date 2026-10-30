package navigation

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSandboxGoProbe(t *testing.T) {
	if os.Getenv("FJORD_TEST_GOPLS") != "1" {
		t.Skip("opt-in OS sandbox test")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"home", "cache", "tmp", "go", "gopls-cache", "source"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dir, "source")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/probe\n\ngo 1.24.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "probe.go"), []byte("package probe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goBin, err = filepath.EvalSymlinks(goBin)
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + filepath.Dir(goBin) + ":/usr/bin:/bin", "HOME=" + filepath.Join(dir, "home"), "TMPDIR=" + filepath.Join(dir, "tmp"), "GOCACHE=" + filepath.Join(dir, "cache"), "GOPATH=" + filepath.Join(dir, "go"), "GOENV=off", "GOWORK=off", "CGO_ENABLED=0", "GOPROXY=off", "GOTOOLCHAIN=local", "GOTELEMETRY=off"}
	cmd, cancel, err := sandboxCommand(dir, root, goBin, runtime.GOROOT(), env, "list", "-deps", "-e", "-json", "./...")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture Go probe: %v\n%s", err, output)
	}
}

func TestSandboxIsolation(t *testing.T) {
	if os.Getenv("FJORD_TEST_GOPLS") != "1" {
		t.Skip("opt-in OS sandbox test")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"home", "cache", "tmp", "go", "gopls-cache", "source"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dir, "source")
	inside := filepath.Join(root, "readable.txt")
	if err := os.WriteFile(inside, []byte("inside fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private-sentinel.txt")
	if err := os.WriteFile(outside, []byte("outside fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	runtimeDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeFile := filepath.Join(runtimeDir, "runtime-fixture.txt")
	if err := os.WriteFile(runtimeFile, []byte("runtime fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"FJORD_SANDBOX_CANARY=1", "FJORD_CANARY_OUTSIDE=" + outside, "FJORD_CANARY_INSIDE=" + inside, "FJORD_CANARY_WRITE=" + filepath.Join(dir, "home", "allowed.txt"), "FJORD_CANARY_ADDRESS=" + listener.Addr().String(), "FJORD_CANARY_RUNTIME=" + runtimeFile}
	cmd, cancel, err := sandboxWithPaths(dir, root, executable, []string{runtime.GOROOT(), runtimeDir}, env, "-test.run=^TestSandboxCanaryHelper$")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox canary failed: %v\n%s", err, output)
	}
}

func TestSandboxCanaryHelper(t *testing.T) {
	if os.Getenv("FJORD_SANDBOX_CANARY") != "1" {
		t.Skip("sandbox subprocess only")
	}
	if _, err := os.ReadFile(os.Getenv("FJORD_CANARY_INSIDE")); err != nil {
		t.Fatal("sandbox cannot read its own source fixture")
	}
	if _, err := os.ReadFile(os.Getenv("FJORD_CANARY_OUTSIDE")); err == nil {
		t.Fatal("sandbox read outside workspace")
	}
	if _, err := os.ReadFile(os.Getenv("FJORD_CANARY_RUNTIME")); err != nil {
		t.Fatal("sandbox cannot read configured runtime path")
	}
	if err := os.WriteFile(os.Getenv("FJORD_CANARY_RUNTIME"), []byte("changed"), 0600); err == nil {
		t.Fatal("sandbox modified readonly runtime path")
	}
	if err := os.WriteFile(os.Getenv("FJORD_CANARY_INSIDE"), []byte("changed"), 0600); err == nil {
		t.Fatal("sandbox modified source fixture")
	}
	if err := os.WriteFile(os.Getenv("FJORD_CANARY_WRITE"), []byte("cache fixture"), 0600); err != nil {
		t.Fatal("sandbox cannot write its own cache")
	}
	connection, err := net.DialTimeout("tcp", os.Getenv("FJORD_CANARY_ADDRESS"), time.Second)
	if err == nil {
		connection.Close()
		t.Fatal("sandbox connected to host network")
	}
}
