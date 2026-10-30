package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NerdsWhoFish/fjord/internal/assist"
	"github.com/NerdsWhoFish/fjord/internal/github"
	"github.com/NerdsWhoFish/fjord/internal/navigation"
)

type fakeGitHub struct {
	loads   int
	reviews int
	last    github.Review
}

func (f *fakeGitHub) Load(context.Context, string, string, int) (github.PR, error) {
	f.loads++
	return github.PR{Title: "A real change"}, nil
}
func (f *fakeGitHub) Content(context.Context, string, string, string, string) (string, error) {
	return "package review", nil
}
func (f *fakeGitHub) Review(_ context.Context, _ string, _ string, _ int, r github.Review) (string, error) {
	f.reviews++
	f.last = r
	return "https://github.com/o/r/pull/1#review", nil
}
func (f *fakeGitHub) Comments(context.Context, string, string, int) ([]github.ThreadComment, error) {
	return []github.ThreadComment{}, nil
}
func (f *fakeGitHub) Reply(context.Context, string, string, int, int64, string) (string, error) {
	return "", nil
}
func (f *fakeGitHub) Archive(context.Context, string, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("archive")), nil
}

type fakeNavigation struct{}

func (fakeNavigation) Prepare(context.Context, string, string, string, io.Reader) (string, error) {
	return "id", nil
}
func (fakeNavigation) Query(context.Context, string, string, string, int, int) (navigation.Result, error) {
	return navigation.Result{}, nil
}
func (fakeNavigation) File(context.Context, string, string) (string, error) { return "", nil }
func testServer() (http.Handler, *fakeGitHub) {
	g := &fakeGitHub{}
	ai, _ := assist.New("", "", "")
	return New(Config{Host: "127.0.0.1:4727", GitHub: g, Navigation: fakeNavigation{}, AI: ai}), g
}
func request(h http.Handler, path, body, host, origin, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://"+host+path, strings.NewReader(body))
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestHostOriginAndJSONBoundary(t *testing.T) {
	h, g := testServer()
	for _, test := range []struct {
		host, origin, kind string
		status             int
	}{
		{"evil.example:4727", "http://evil.example:4727", "application/json", 403},
		{"127.0.0.1:4727", "http://evil.example", "application/json", 403},
		{"127.0.0.1:4727", "", "application/json", 403},
		{"127.0.0.1:4727", "null", "application/json", 403},
		{"127.0.0.1:4727", "http://127.0.0.1:4727", "text/plain", 415},
	} {
		w := request(h, "/api/pr", `{"url":"https://github.com/o/r/pull/1"}`, test.host, test.origin, test.kind)
		if w.Code != test.status {
			t.Errorf("got %d want %d", w.Code, test.status)
		}
	}
	if g.loads != 0 {
		t.Fatal("untrusted request reached GitHub")
	}
}
func TestPRLoadAndMalformedInputs(t *testing.T) {
	h, g := testServer()
	w := request(h, "/api/pr", `{"url":"https://github.com/o/r/pull/1"}`, "127.0.0.1:4727", "http://127.0.0.1:4727", "application/json")
	if w.Code != 200 || g.loads != 1 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"url":"https://evil.example/o/r/pull/1"}`, `{"url":"https://github.com/o/r/pull/1","token":"x"}`, `{} {}`, `{"url":"https://github.com/o/r/pull/1?secret=x"}`} {
		w = request(h, "/api/pr", body, "127.0.0.1:4727", "http://127.0.0.1:4727", "application/json")
		if w.Code != 400 {
			t.Errorf("accepted %s", body)
		}
	}
	if g.loads != 1 {
		t.Fatal("invalid input reached GitHub")
	}
}
func TestLANHostOriginBoundary(t *testing.T) {
	ai, _ := assist.New("", "", "")
	h := New(Config{Host: "192.168.1.10:4727", AI: ai})
	for _, tt := range []struct {
		host, origin string
		want         int
	}{
		{"192.168.1.10:4727", "http://192.168.1.10:4727", 200},
		{"evil.example:4727", "http://evil.example:4727", 403},
		{"192.168.1.10:4727", "http://evil.example:4727", 403},
		{"192.168.1.10:4727", "", 403},
	} {
		w := request(h, "/api/config", `{}`, tt.host, tt.origin, "application/json")
		if w.Code != tt.want {
			t.Errorf("host %q origin %q: got %d want %d", tt.host, tt.origin, w.Code, tt.want)
		}
	}
}
func TestReviewPayloadPreserved(t *testing.T) {
	h, g := testServer()
	body := `{"owner":"o","repo":"r","number":1,"headSHA":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","baseSHA":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","event":"REQUEST_CHANGES","body":"Please handle errors.","comments":[{"path":"a.go","line":12,"side":"LEFT","body":"Deleted error handling"}]}`
	w := request(h, "/api/review", body, "127.0.0.1:4727", "http://127.0.0.1:4727", "application/json")
	if w.Code != 200 || g.reviews != 1 || g.last.Event != "REQUEST_CHANGES" || g.last.Comments[0].Side != "LEFT" {
		t.Fatalf("review not preserved: %s", w.Body)
	}
}
func TestFaroEnvelopeDropsPrivateContent(t *testing.T) {
	var raw map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"meta":{"user":{"email":"private@example.com"},"page":{"url":"https://private/repo?token=secret"}},"exceptions":[{"value":"secret code","stacktrace":[{"filename":"private.go"}]}],"events":[{"name":"navigate","attributes":{"code":"secret"}},{"name":"private.event"}],"measurements":[{"type":"web-vitals","values":{"LCP":25,"secret":30}}],"traces":{"secret":"raw"}}`), &raw)
	data, _ := json.Marshal(safeEnvelope(raw, "test"))
	text := string(data)
	for _, forbidden := range []string{"secret", "private", "email", "stacktrace", "traces"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("leaked %s in %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "navigate") || !strings.Contains(text, "LCP") {
		t.Fatal("dropped safe signals")
	}
}
