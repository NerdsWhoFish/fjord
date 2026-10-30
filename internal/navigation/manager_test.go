package navigation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type archiveEntry struct {
	name, body string
	kind       byte
	pax        map[string]string
}

func archiveBytes(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Mode: 0600, Typeflag: kind}
		if entry.pax != nil {
			header.PAXRecords = entry.pax
			header.Format = tar.FormatPAX
		}
		if kind == tar.TypeXGlobalHeader {
			header.Mode = 0
		}
		if kind == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if kind == tar.TypeSymlink || kind == tar.TypeLink {
			header.Linkname = "../../outside"
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := io.WriteString(tw, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestExtractRejectsUnsafeArchives(t *testing.T) {
	tests := map[string][]archiveEntry{
		"parent":                    {{name: "repo/../outside"}},
		"absolute":                  {{name: "/tmp/outside"}},
		"symlink":                   {{name: "repo/link", kind: tar.TypeSymlink}},
		"hardlink":                  {{name: "repo/link", kind: tar.TypeLink}},
		"multiple roots":            {{name: "first/file"}, {name: "second/file"}},
		"duplicate":                 {{name: "repo/file"}, {name: "repo/file"}},
		"metadata":                  {{name: "repo/.git/config"}},
		"case insensitive metadata": {{name: "repo/.GiT/config"}},
		"windows separator":         {{name: "repo/..\\outside"}},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			if err := extract(context.Background(), bytes.NewReader(archiveBytes(t, entries...)), t.TempDir()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestExtractAndRead(t *testing.T) {
	root := t.TempDir()
	err := extract(context.Background(), bytes.NewReader(archiveBytes(t, archiveEntry{name: "repo/nested/a.go", body: "package nested\n"})), root)
	if err != nil {
		t.Fatal(err)
	}
	content, err := readFile(root, "nested/a.go")
	if err != nil || content != "package nested\n" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	if _, err = readFile(root, "../outside"); err == nil {
		t.Fatal("traversal accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if extract(ctx, bytes.NewReader(archiveBytes(t, archiveEntry{name: "repo/a"})), t.TempDir()) == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestUTF16Positions(t *testing.T) {
	for _, tc := range []struct {
		line, char int
		valid      bool
	}{{0, 0, true}, {0, 1, true}, {0, 2, false}, {0, 3, true}, {0, 4, true}, {0, 5, false}, {1, 0, true}, {2, 0, false}, {-1, 0, false}} {
		if got := validPosition("a\U0001f600b\n", tc.line, tc.char); got != tc.valid {
			t.Errorf("%d:%d = %v", tc.line, tc.char, got)
		}
	}
}

func TestGitHubGlobalPAXHeader(t *testing.T) {
	root := t.TempDir()
	archive := archiveBytes(t,
		archiveEntry{name: "pax_global_header", kind: tar.TypeXGlobalHeader, pax: map[string]string{"comment": strings.Repeat("a", 40)}},
		archiveEntry{name: "repo/main.go", body: "package sample\n"})
	if err := extract(context.Background(), bytes.NewReader(archive), root); err != nil {
		t.Fatal(err)
	}
	if _, err := readFile(root, "main.go"); err != nil {
		t.Fatal(err)
	}
}

func TestPublicGitHubArchive(t *testing.T) {
	filename := os.Getenv("FJORD_TEST_PUBLIC_ARCHIVE")
	if filename == "" {
		t.Skip("set FJORD_TEST_PUBLIC_ARCHIVE for a downloaded public fixture")
	}
	f, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m := New()
	defer m.Close()
	_, err = m.Prepare(context.Background(), "spf13", "pflag", "5b8b51e675bb1a685b8b3a529b5ed3516413f24e", f)
	if err != nil {
		t.Fatal(err)
	}
}

func TestArchiveChecksumAndSourceSymlinks(t *testing.T) {
	b := archiveBytes(t, archiveEntry{name: "repo/main.go", body: "package sample\n"})
	b[len(b)-8] ^= 0xff
	if err := extract(context.Background(), bytes.NewReader(b), t.TempDir()); err == nil {
		t.Fatal("invalid gzip checksum accepted")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := readFile(root, "escape.go"); err == nil {
		t.Fatal("source symlink escaped workspace")
	}
}

func TestGoplsMissingDependencies(t *testing.T) {
	if os.Getenv("FJORD_TEST_GOPLS") != "1" {
		t.Skip("opt-in gopls test")
	}
	m := New()
	defer m.Close()
	archive := archiveBytes(t,
		archiveEntry{name: "repo/go.mod", body: "module example.test/missing\n\ngo 1.24.0\n"},
		archiveEntry{name: "repo/main.go", body: "package missing\nimport _ \"example.test/not-vendored\"\n"})
	id, err := m.Prepare(context.Background(), "example", "missing", strings.Repeat("b", 40), bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("preparation must not require Go dependencies: %v", err)
	}
	_, err = m.Query(context.Background(), id, "main.go", "definition", 0, 8)
	if err == nil || !strings.Contains(err.Error(), "vendored dependencies") {
		t.Fatalf("expected explicit dependency failure, got %v", err)
	}
	if len(m.workspaces) != 1 || m.workspaces[id].client != nil {
		t.Fatal("failed runtime retained or prepared revision discarded")
	}
}

func TestWorkspaceEvictionSkipsActiveReaders(t *testing.T) {
	m := New()
	older := &workspace{dir: t.TempDir(), touched: time.Now().Add(-time.Hour)}
	older.active.Add(1)
	idle := &workspace{dir: t.TempDir(), touched: time.Now()}
	m.workspaces["active"] = older
	m.workspaces["idle"] = idle
	if !m.evictIdle() {
		t.Fatal("idle workspace was not evicted")
	}
	if m.workspaces["active"] != older || m.workspaces["idle"] != nil {
		t.Fatal("eviction selected an active workspace")
	}
	if _, err := os.Stat(idle.dir); !os.IsNotExist(err) {
		t.Fatal("evicted source directory retained")
	}
	if m.evictIdle() {
		t.Fatal("evicted active workspace")
	}
	older.active.Add(-1)
	if !m.evictIdle() {
		t.Fatal("released workspace was not evicted")
	}
}

func TestResultLocationsCannotEscape(t *testing.T) {
	root := t.TempDir()
	inside := (&url.URL{Scheme: "file", Path: filepath.Join(root, "main.go")}).String()
	outside := (&url.URL{Scheme: "file", Path: filepath.Join(filepath.Dir(root), "secret")}).String()
	for _, uri := range []string{outside, "https://example.com/code", "file://evil/main.go"} {
		raw, _ := json.Marshal(map[string]any{"uri": uri, "range": sourceRange{}})
		if _, err := decodeResult(root, "definition", raw); err == nil {
			t.Fatalf("accepted %q", uri)
		}
	}
	raw, _ := json.Marshal([]map[string]any{{"targetUri": inside, "targetSelectionRange": sourceRange{Start: position{Line: 1, Character: 2}}}})
	result, err := decodeResult(root, "definition", raw)
	if err != nil || len(result.Locations) != 1 || result.Locations[0].Path != "main.go" || result.Locations[0].Line != 1 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestGoplsNavigation(t *testing.T) {
	if os.Getenv("FJORD_TEST_GOPLS") != "1" {
		t.Skip("set FJORD_TEST_GOPLS=1 to exercise installed gopls and OS sandbox")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("sandbox not supported")
	}
	var entries []archiveEntry
	for _, name := range []string{"go.mod", "main.go"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "navigation", name))
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, archiveEntry{name: "revision/" + name, body: string(b)})
	}
	m := New()
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	id, err := m.Prepare(ctx, "example", "navigation", strings.Repeat("a", 40), bytes.NewReader(archiveBytes(t, entries...)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method          string
		line, char, min int
	}{{"definition", 12, 31, 1}, {"implementation", 3, 2, 1}, {"references", 10, 6, 2}, {"hover", 12, 31, 0}} {
		t.Run(tc.method, func(t *testing.T) {
			result, err := m.Query(ctx, id, "main.go", tc.method, tc.line, tc.char)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Locations) < tc.min {
				t.Fatalf("expected >= %d locations: %+v", tc.min, result)
			}
			if tc.method == "hover" && result.Hover == "" {
				t.Fatal("hover is empty")
			}
			for _, location := range result.Locations {
				if _, err := m.File(ctx, id, location.Path); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	if _, err := m.Query(ctx, id, "main.go", "workspace/executeCommand", 0, 0); err == nil {
		t.Fatal("unsafe LSP method accepted")
	}
	if _, err := m.File(ctx, id, "../../outside"); err == nil {
		t.Fatal("source escape accepted")
	}
}
