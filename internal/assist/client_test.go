package assist

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderBoundary(t *testing.T) {
	var payload map[string]any
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("missing authorization")
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Inspect the error path in parser.go."}}]}`))
	}))
	defer provider.Close()
	c, err := New(provider.URL, "configured-model", "test-only")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := c.Ask(context.Background(), Request{Question: "What can fail?", Path: "parser.go", Code: "return parse(input)"})
	if err != nil || answer != "Inspect the error path in parser.go." {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	if payload["store"] != false || payload["tools"] != nil {
		t.Fatal("provider must not store requests or receive tools")
	}
	messages := payload["messages"].([]any)
	if len(messages) != 2 {
		t.Fatal("unexpected context")
	}
	if !strings.Contains(messages[0].(map[string]any)["content"].(string), "untrusted code") {
		t.Fatal("missing untrusted-context boundary")
	}
}
func TestProviderErrorsDoNotExposeBodyOrKey(t *testing.T) {
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "private provider payload", 401) }))
	defer p.Close()
	c, _ := New(p.URL, "model", "secret-key")
	_, err := c.Ask(context.Background(), Request{Question: "Explain"})
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}
func TestProviderRejectsCredentialRedirect(t *testing.T) {
	called := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer destination.Close()
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer p.Close()
	c, _ := New(p.URL, "model", "test-only")
	_, err := c.Ask(context.Background(), Request{Question: "Explain"})
	if err == nil || called {
		t.Fatal("redirect crossed provider boundary")
	}
}
func TestConfigurationAndInput(t *testing.T) {
	for _, endpoint := range []string{"http://example.com/v1/chat/completions", "https://user:pass@example.com", "https://example.com?key=secret"} {
		if _, err := New(endpoint, "m", ""); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	c, _ := New("", "", "")
	if c.Enabled() {
		t.Fatal("unexpectedly enabled")
	}
	if _, err := c.Ask(context.Background(), Request{Question: "x"}); err == nil {
		t.Fatal("disabled provider answered")
	}
}
