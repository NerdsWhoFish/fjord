package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/NerdsWhoFish/fjord/internal/github"
)

func TestFileDiffEndpointUsesImmutableSourceAndOriginGuard(t *testing.T) {
	handler := New(Config{Host: "127.0.0.1:4727", GitHub: &fakeGitHub{}, Navigation: fakeNavigation{}})
	body, _ := json.Marshal(github.DiffRequest{
		Owner: "acme", Repo: "app", BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40),
		Path: "new.go", PreviousPath: "old.go", Status: "renamed",
	})
	response := request(handler, "/api/diff", string(body), "127.0.0.1:4727", "http://127.0.0.1:4727", "application/json")
	var result github.FileDiff
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatalf("diff request failed: %d %s", response.Code, response.Body.String())
	}
	if result.Status != "unchanged" || !strings.Contains(result.Patch, " package review") {
		t.Fatalf("missing read-only source: %+v", result)
	}
	blocked := request(handler, "/api/diff", string(body), "127.0.0.1:4727", "https://other.example", "application/json")
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("cross-origin reconstruction accepted: %d", blocked.Code)
	}
}
