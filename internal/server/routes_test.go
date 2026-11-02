package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPublicAddress(t *testing.T) {
	for _, tt := range []struct{ input, origin, host string }{
		{"https://fjord.stout.zone", "https://fjord.stout.zone", "fjord.stout.zone"},
		{"https://FJORD.stout.zone:443/", "https://fjord.stout.zone", "fjord.stout.zone"},
		{"http://localhost:80", "http://localhost", "localhost"},
		{"http://192.168.1.10:4727", "http://192.168.1.10:4727", "192.168.1.10:4727"},
		{"https://[::1]:443", "https://[::1]", "[::1]"},
		{"https://fjord.stout.zone:8443", "https://fjord.stout.zone:8443", "fjord.stout.zone:8443"},
	} {
		origin, host, err := PublicAddress(tt.input)
		if err != nil || origin != tt.origin || host != tt.host {
			t.Errorf("PublicAddress(%q) = %q, %q, %v", tt.input, origin, host, err)
		}
	}
	for _, input := range []string{
		"", "fjord.stout.zone", "ftp://fjord.stout.zone", "https://user@fjord.stout.zone",
		"https://fjord.stout.zone/path", "https://fjord.stout.zone?secret=x",
		"https://fjord.stout.zone?", "https://fjord.stout.zone#", "https://fjord.stout.zone#fragment",
		"https://fjord.stout.zone:0", "https://fjord.stout.zone:65536", "https://fjord.stout.zone:",
		"https://fjord..zone", "https://-fjord.zone", "https://fjord.zone-", "https://*",
		"https://0.0.0.0", "http://[::]", "https://[fe80::1%25en0]", " https://fjord.stout.zone",
	} {
		if _, _, err := PublicAddress(input); err == nil {
			t.Errorf("accepted invalid public origin %q", input)
		}
	}
}

func TestHTTPSProxyBoundary(t *testing.T) {
	h := New(Config{Host: "fjord.stout.zone", Origin: "https://fjord.stout.zone"})
	for _, tt := range []struct {
		host, origin, forwarded string
		want                    int
	}{
		{"fjord.stout.zone", "https://fjord.stout.zone", "", 200},
		{"fjord.stout.zone", "http://fjord.stout.zone", "", 403},
		{"fjord.stout.zone", "https://evil.example", "https", 403},
		{"internal:4727", "https://fjord.stout.zone", "https", 403},
		{"fjord.stout.zone", "", "https", 403},
	} {
		r := httptest.NewRequest(http.MethodPost, "http://"+tt.host+"/api/config", strings.NewReader("{}"))
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-Proto", tt.forwarded)
		r.Header.Set("X-Forwarded-Host", "fjord.stout.zone")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Errorf("host %q origin %q: %d, want %d", tt.host, tt.origin, w.Code, tt.want)
		}
	}
	invalid := New(Config{Host: "fjord.stout.zone", Origin: "https://other.example"})
	w := request(invalid, "/api/config", "{}", "fjord.stout.zone", "https://other.example", "application/json")
	if w.Code != http.StatusForbidden {
		t.Fatal("invalid origin configuration did not fail closed")
	}
}

func TestPRDeepLinksServeOnlyTheAppRoutes(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":     {Data: []byte("<html>Fjord app</html>")},
		"assets/main.js": {Data: []byte("app()")},
	}
	h := New(Config{Host: "fjord.stout.zone", Origin: "https://fjord.stout.zone", Assets: assets})
	for _, tt := range []struct {
		path string
		want int
		body string
	}{
		{"/", 200, "Fjord app"},
		{"/spacelift-solutions/demo/229", 200, "Fjord app"},
		{"/o/r/001/", 200, "Fjord app"},
		{"/?owner=o&repo=r&number=1", 200, "Fjord app"},
		{"/assets/main.js", 200, "app()"},
		{"/assets/missing.js", 404, ""},
		{"/assets/missing/1", 404, ""},
		{"/api/missing/1", 404, ""},
		{"/api/missing", 404, ""},
		{"/mcp/missing/1", 404, ""},
		{"/o/r/0", 404, ""},
		{"/o/r/9007199254740992", 404, ""},
		{"/o/r/1/extra", 404, ""},
		{"/o/r%2Fother/1", 404, ""},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "https://fjord.stout.zone"+tt.path, nil))
		if w.Code != tt.want || (tt.body != "" && !strings.Contains(w.Body.String(), tt.body)) {
			t.Errorf("%s: got %d %q, want %d containing %q", tt.path, w.Code, w.Body.String(), tt.want, tt.body)
		}
	}
}

func TestGitHubChangesURLs(t *testing.T) {
	for _, suffix := range []string{"", "/", "/changes", "/files"} {
		target, err := parsePR("https://github.com/org/repo/pull/123" + suffix)
		if err != nil || target.Number != 123 {
			t.Errorf("rejected GitHub PR suffix %q: %v", suffix, err)
		}
	}
}
