package navigation

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

func TestRequiredGoModules(t *testing.T) {
	var metadata goModuleFile
	metadata.Require = []goModule{{"example.com/a", "v1.0.0"}, {"example.com/b", "v2.0.0"}, {"example.com/a", "v1.0.0"}}
	metadata.Replace = append(metadata.Replace, struct{ Old, New goModule }{
		Old: goModule{Path: "example.com/a"}, New: goModule{"example.com/replacement", "v1.1.0"},
	})
	metadata.Replace = append(metadata.Replace, struct{ Old, New goModule }{
		Old: goModule{Path: "example.com/b"}, New: goModule{Path: "../local"},
	})
	modules, err := requiredGoModules(metadata, "/revision/source", "/revision/source/app")
	if err != nil || len(modules) != 1 || modules[0].Path != "example.com/replacement" {
		t.Fatalf("replacement selection: %v %v", modules, err)
	}
	metadata.Replace[1].New.Path = "../../secret"
	if _, err := requiredGoModules(metadata, "/revision/source", "/revision/source/app"); err == nil {
		t.Fatal("accepted escaping replacement")
	}
	metadata.Replace[1].New.Path = "/etc"
	if _, err := requiredGoModules(metadata, "/revision/source", "/revision/source/app"); err == nil {
		t.Fatal("accepted absolute outside replacement")
	}
}

func TestGoExactReplacementPrecedesWildcard(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		var metadata goModuleFile
		metadata.Require = []goModule{{"example.com/dependency", "v1.0.0"}}
		metadata.Replace = append(metadata.Replace,
			struct{ Old, New goModule }{goModule{Path: "example.com/dependency"}, goModule{"example.com/wildcard", "v1.1.0"}},
			struct{ Old, New goModule }{goModule{"example.com/dependency", "v1.0.0"}, goModule{"example.com/exact", "v1.2.0"}},
		)
		if reverse {
			metadata.Replace[0], metadata.Replace[1] = metadata.Replace[1], metadata.Replace[0]
		}
		modules, err := requiredGoModules(metadata, "/revision/source", "/revision/source")
		if err != nil || len(modules) != 1 || modules[0].Path != "example.com/exact" {
			t.Fatalf("exact replacement lost (reverse=%v): %v %v", reverse, modules, err)
		}
	}
}

type countedGoReader struct {
	reader io.Reader
	read   *atomic.Int64
}

func (r countedGoReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read.Add(int64(n))
	return n, err
}

func TestGoDownloadBudgetIsSharedWhileReading(t *testing.T) {
	const remaining = 10000
	budget := goArtifactFetcher{transferred: maxGoDownloadBytes - remaining}
	var read atomic.Int64
	var group sync.WaitGroup
	for range 3 {
		group.Go(func() {
			reader := countedGoReader{reader: strings.NewReader(strings.Repeat("x", remaining*2)), read: &read}
			_, err := io.ReadAll(goBudgetReader{reader: reader, budget: &budget})
			if !errors.Is(err, errGoDownloadLimit) {
				t.Errorf("expected shared budget error, got %v", err)
			}
		})
	}
	group.Wait()
	if read.Load() != remaining || budget.transferred != maxGoDownloadBytes {
		t.Fatalf("read %d bytes beyond baseline with budget %d", read.Load(), budget.transferred)
	}
}

func TestGoLegacyGraphErrors(t *testing.T) {
	for _, version := range []string{"", "1.16", "1.17", "1.27.3"} {
		root := t.TempDir()
		body := "module example.com/fixture\n"
		if version != "" {
			body += "\ngo " + version + "\n"
		}
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		err := goGraphError(root, errGoDependenciesUnavailable)
		wantLegacy := version == "" || version == "1.16"
		if strings.Contains(err.Error(), "legacy Go module graph") != wantLegacy {
			t.Fatalf("go %q incorrectly classified: %v", version, err)
		}
		toolchain := goPackageError("requires go >= 1.99")
		if goGraphError(root, toolchain) != toolchain {
			t.Fatal("legacy graph replaced specific toolchain error")
		}
		if goGraphError(root, nil) != nil {
			t.Fatal("successful legacy module was rejected")
		}
	}
}

func TestGoDependencyPinsAndPaths(t *testing.T) {
	checksum := "h1:" + strings.Repeat("a", 43) + "="
	pins := pinnedGoModules("example.com/a v1.0.0 " + checksum + "\nexample.com/b v1.0.0/go.mod " + checksum + "\n")
	if pins["example.com/a@v1.0.0"] == "" || pins["example.com/b@v1.0.0"] != "" || pins["example.com/b@v1.0.0/go.mod"] == "" {
		t.Fatalf("zip pins must exclude go.mod-only entries: %v", pins)
	}
	for _, path := range []string{"../example.com/a", "example.com/../a", "example.com/a?secret", "-flag.com/a", "example.com/a%2fb", "example.com//a"} {
		if safeGoModule(goModule{path, "v1.0.0"}) {
			t.Errorf("accepted path %q", path)
		}
	}
	if got := escapeGoModule("github.com/Azure/SDK"); got != "github.com/!azure/!s!d!k" {
		t.Fatalf("incorrect module path encoding: %s", got)
	}
}

type goArtifactTransport func(*http.Request) (*http.Response, error)

func (f goArtifactTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGoArtifactsBoundedAndPrivateCacheOffline(t *testing.T) {
	client := &http.Client{Transport: goArtifactTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "proxy.golang.org" || req.Header.Get("Authorization") != "" {
			t.Fatal("unexpected upstream or credentials")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("module example.com/a\n")), Header: make(http.Header)}, nil
	})}
	fetcher := goArtifactFetcher{client: client}
	if _, err := fetcher.fetch(context.Background(), "example.com/a/@v/v1.0.0.mod", false); err != nil {
		t.Fatal(err)
	}
	fetcher.transferred = maxGoDownloadBytes
	if _, err := fetcher.fetch(context.Background(), "example.com/a/@v/v1.0.0.mod", false); err == nil {
		t.Fatal("ignored cumulative download bound")
	}
	cache := t.TempDir()
	fetcher = goArtifactFetcher{
		operatorCache: cache,
		client: &http.Client{Transport: goArtifactTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("private cache miss must not reveal module names to public proxy")
			return nil, errors.New("unexpected network")
		})},
	}
	if _, err := fetcher.fetch(context.Background(), "private.example/a/@v/v1.0.0.mod", false); err == nil {
		t.Fatal("missing operator artifact accepted")
	}
}

func TestGoArchiveExpansionBound(t *testing.T) {
	var body bytes.Buffer
	archive := zip.NewWriter(&body)
	file, err := archive.Create("example.com/a@v1.0.0/a.go")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("package a"))
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	fetcher := goArtifactFetcher{expanded: maxGoExpandedBytes, client: &http.Client{Transport: goArtifactTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body.Bytes())), Header: make(http.Header)}, nil
	})}}
	if _, err := fetcher.fetch(context.Background(), "example.com/a/@v/v1.0.0.zip", true); err == nil {
		t.Fatal("accepted archive beyond cumulative expanded limit")
	}
}

func TestGoProxyRejectsRedirects(t *testing.T) {
	client := goProxyClient()
	defer client.CloseIdleConnections()
	for _, target := range []string{"http://proxy.golang.org/x", "https://evil.example/x", "https://proxy.golang.org:443/x", "https://user:password@storage.googleapis.com/x"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.CheckRedirect(req, nil); err == nil {
			t.Errorf("accepted redirect %s", target)
		}
	}
}

func TestGoPackageErrorsAreActionableAndPrivate(t *testing.T) {
	for _, test := range []struct{ detail, expected string }{
		{"go: go.mod requires go >= 1.99.0 (running go 1.27)", "toolchain"},
		{"verifying private.example/a: checksum mismatch", "checksum"},
		{"missing go.sum entry for private.example/a", "go mod tidy"},
		{"no required module provides package private.example/a", "FJORD_GO_MODULE_CACHE"},
		{"inconsistent vendoring in /secret/repo", "go mod vendor"},
	} {
		message := goPackageError(test.detail).Error()
		if !strings.Contains(message, test.expected) || strings.Contains(message, "private.example") || strings.Contains(message, "/secret") {
			t.Errorf("classification %q: %s", test.detail, message)
		}
	}
}

func TestGoplsPreparedPublicDependency(t *testing.T) {
	if os.Getenv("FJORD_TEST_GOPLS") != "1" {
		t.Skip("opt-in gopls and sandbox test")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("unsupported sandbox")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	probe := exec.Command(goBin, "env", "GOMODCACHE")
	probe.Env = append(os.Environ(), "GOWORK=off")
	output, err := probe.Output()
	if err != nil {
		t.Fatal(err)
	}
	operatorCache := strings.TrimSpace(string(output))
	sum, err := os.ReadFile(filepath.Join(operatorCache, "cache", "download", "github.com", "google", "uuid", "@v", "v1.6.0.ziphash"))
	if err != nil {
		t.Fatal("the Fjord build dependency github.com/google/uuid must be cached:", err)
	}
	t.Setenv("FJORD_GO_MODULE_CACHE", operatorCache)
	if os.Getenv("FJORD_TEST_GO_PROXY") == "1" {
		t.Setenv("FJORD_GO_MODULE_CACHE", "")
	}
	m := New()
	defer m.Close()
	source := "package fixture\nimport \"github.com/google/uuid\"\nfunc Target() uuid.UUID { return uuid.Nil }\nfunc Call() uuid.UUID { return Target() }\n"
	archive := archiveBytes(t,
		archiveEntry{name: "repo/go.mod", body: "module example.com/fixture\n\ngo 1.24.0\n\nrequire github.com/google/uuid v1.6.0\n"},
		archiveEntry{name: "repo/go.sum", body: "github.com/google/uuid v1.6.0 " + strings.TrimSpace(string(sum)) + "\ngithub.com/google/uuid v1.6.0/go.mod h1:TIyPZe4MgqvfeYDBFedMoGGpEw/LqOeaOT+nhxU+yHo=\n"},
		archiveEntry{name: "repo/main.go", body: source},
	)
	id, err := m.Prepare(context.Background(), "example", "dependency", strings.Repeat("d", 40), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.Query(context.Background(), id, "main.go", "definition", 3, 34)
	if err != nil {
		t.Fatalf("dependency-backed local navigation failed: %v", err)
	}
	if len(result.Locations) != 1 || result.Locations[0].Path != "main.go" || result.Locations[0].Line != 2 {
		t.Fatalf("unexpected local definition: %+v", result)
	}
	w := m.workspaces[id]
	caches, err := filepath.Glob(filepath.Join(w.dir, "modules-*"))
	if err != nil || len(caches) != 1 {
		t.Fatalf("expected one prepared cache: %v %v", caches, err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	cmd, cancel, err := sandboxWithPaths(w.dir, w.root, executable, []string{runtime.GOROOT()}, []string{"FJORD_GO_READONLY_PROBE=" + caches[0]}, "-test.run=^TestGoReadOnlyCacheHelper$")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("read-only cache probe: %v %s", err, output)
	}
}

func TestGoReadOnlyCacheHelper(t *testing.T) {
	cache := os.Getenv("FJORD_GO_READONLY_PROBE")
	if cache == "" {
		t.Skip("sandbox subprocess only")
	}
	if err := os.WriteFile(filepath.Join(cache, "probe"), []byte("must fail"), 0600); err == nil {
		t.Fatal("language server can write prepared dependency cache")
	}
	if _, err := os.ReadFile(filepath.Join(cache, "github.com", "google", "uuid@v1.6.0", "uuid.go")); err != nil {
		t.Fatal("language server cannot read prepared dependency source")
	}
}

func TestGoDependencyPreparationCases(t *testing.T) {
	if os.Getenv("FJORD_TEST_GOPLS") != "1" {
		t.Skip("opt-in installed Go and sandbox test")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	probe := exec.Command(goBin, "env", "GOMODCACHE")
	probe.Env = append(os.Environ(), "GOWORK=off")
	output, err := probe.Output()
	if err != nil {
		t.Fatal(err)
	}
	upstream := filepath.Join(strings.TrimSpace(string(output)), "cache", "download", "github.com", "google", "uuid", "@v")
	const sums = "github.com/google/uuid v1.6.0 h1:NIvaJDMOsjHA8n1jAhLSgzrAzy1Hgr+hNrb57e+94F0=\ngithub.com/google/uuid v1.6.0/go.mod h1:TIyPZe4MgqvfeYDBFedMoGGpEw/LqOeaOT+nhxU+yHo=\n"
	for _, test := range []struct {
		name, want string
	}{
		{"metadata-only", ""},
		{"tampered-mod", "checksum"},
		{"tampered-zip", "checksum"},
	} {
		t.Run(test.name, func(t *testing.T) {
			operator := t.TempDir()
			artifactDir := filepath.Join(operator, "cache", "download", "github.com", "google", "uuid", "@v")
			if err := os.MkdirAll(artifactDir, 0700); err != nil {
				t.Fatal(err)
			}
			for _, extension := range []string{".info", ".mod", ".zip"} {
				if test.name == "metadata-only" && extension == ".zip" {
					continue
				}
				body, err := os.ReadFile(filepath.Join(upstream, "v1.6.0"+extension))
				if err != nil {
					t.Fatal(err)
				}
				if test.name == "tampered-mod" && extension == ".mod" {
					body = append(body, []byte("\n// changed\n")...)
				}
				if test.name == "tampered-zip" && extension == ".zip" {
					body = tamperGoZip(t, body)
				}
				if err := os.WriteFile(filepath.Join(artifactDir, "v1.6.0"+extension), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("FJORD_GO_MODULE_CACHE", operator)
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(dir, "source")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			sum := sums
			if test.name == "metadata-only" {
				_, sum, _ = strings.Cut(sums, "\n")
			}
			for name, body := range map[string]string{
				"go.mod":  "module example.com/fixture\n\ngo 1.24.0\n\nrequire github.com/google/uuid v1.6.0\n",
				"go.sum":  sum,
				"main.go": "package fixture\nfunc Target() int { return 1 }\n",
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err = configureLanguage(context.Background(), dir, root, "go")
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("wanted %s failure, got %v", test.want, err)
			}
		})
	}
}

func TestGoVendoredModulesBypassDownloadCountLimit(t *testing.T) {
	if os.Getenv("FJORD_TEST_GOPLS") != "1" {
		t.Skip("opt-in installed Go and sandbox test")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0700); err != nil {
		t.Fatal(err)
	}
	var manifest, vendor strings.Builder
	manifest.WriteString("module example.com/fixture\n\ngo 1.24.0\n\nrequire (\n")
	for i := range maxGoModules + 1 {
		fmt.Fprintf(&manifest, "example.com/dependency%d v1.0.0\n", i)
		fmt.Fprintf(&vendor, "# example.com/dependency%d v1.0.0\n## explicit; go 1.24.0\n", i)
	}
	manifest.WriteString(")\n")
	for name, body := range map[string]string{
		"go.mod": manifest.String(), "vendor/modules.txt": vendor.String(),
		"main.go": "package fixture\nfunc Target() int { return 1 }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := configureLanguage(context.Background(), dir, root, "go"); err != nil {
		t.Fatalf("vendored modules must not hit download module count: %v", err)
	}
}

func tamperGoZip(t *testing.T, body []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range reader.File {
		if err := writer.Copy(file); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.Create("github.com/google/uuid@v1.6.0/extra.go")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(file, "package uuid\n")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestGoOperatorCacheRejectsPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix FIFO test")
	}
	cache := t.TempDir()
	path := filepath.Join(cache, "cache", "download", "example.com", "a", "@v", "v1.0.0.mod")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	fetcher := goArtifactFetcher{operatorCache: cache}
	if _, err := fetcher.fetch(context.Background(), "example.com/a/@v/v1.0.0.mod", false); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("named pipe must fail before opening: %v", err)
	}
}
