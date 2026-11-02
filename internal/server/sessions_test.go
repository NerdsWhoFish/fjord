package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NerdsWhoFish/fjord/internal/github"
)

func TestSessionManagementRoutes(t *testing.T) {
	host := "localhost:8080"
	handler := New(Config{Host: host, GitHub: &sessionGitHub{}, Navigation: sessionNavigation{}})
	id := browserSession(t, handler, host)
	list := request(handler, "/api/session/list", `{}`, host, "http://"+host, "application/json")
	var result struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if list.Code != http.StatusOK || json.Unmarshal(list.Body.Bytes(), &result) != nil || len(result.Sessions) != 1 {
		t.Fatalf("list failed: %d %s", list.Code, list.Body)
	}
	if result.Sessions[0]["id"] != id || result.Sessions[0]["draftCommentCount"] != float64(0) {
		t.Fatalf("unexpected metadata: %v", result.Sessions)
	}
	if _, exposed := result.Sessions[0]["draft"]; exposed {
		t.Fatal("listing exposed draft contents")
	}
	body := fmt.Sprintf(`{"id":%q}`, id)
	for range 2 {
		removed := request(handler, "/api/session/remove", body, host, "http://"+host, "application/json")
		if removed.Code != http.StatusOK {
			t.Fatalf("remove failed: %d %s", removed.Code, removed.Body)
		}
	}
	// A late unload must not erase the deliberate-removal marker.
	request(handler, "/api/session/close", body, host, "http://"+host, "application/json")
	sync := request(handler, "/api/session/sync", body, host, "http://"+host, "application/json")
	if sync.Code != http.StatusGone || !strings.Contains(sync.Body.String(), `"session_removed"`) {
		t.Fatalf("removed session was not terminal: %d %s", sync.Code, sync.Body)
	}
	list = request(handler, "/api/session/list", `{}`, host, "http://"+host, "application/json")
	if !strings.Contains(list.Body.String(), `"sessions":[]`) {
		t.Fatalf("removed session still listed: %s", list.Body)
	}
	for _, path := range []string{"/api/session/list", "/api/session/remove"} {
		blocked := request(handler, path, body, host, "https://evil.example", "application/json")
		if blocked.Code != http.StatusForbidden {
			t.Fatalf("%s accepted cross-origin request: %d", path, blocked.Code)
		}
	}
}

func TestRemovedSessionRetentionIsBoundedAndSeparateFromCapacity(t *testing.T) {
	store := newSessionStore()
	now := time.Now()
	store.now = func() time.Time { return now }
	id, _ := store.open(sessionPR(), emptySessionDraft())
	if err := store.remove(id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.get(id); !errors.Is(err, errSessionRemoved) {
		t.Fatalf("get removed session: %v", err)
	}
	for len(store.removed) < maxRemovedSessions {
		store.removed[fmt.Sprint(len(store.removed))] = now.Add(removedSessionTTL)
	}
	next, err := store.open(sessionPR(), emptySessionDraft())
	if err != nil {
		t.Fatalf("tombstones consumed active session capacity: %v", err)
	}
	if err := store.remove(next); err == nil {
		t.Fatal("unbounded tombstone insertion")
	}
	if _, err := store.get(next); err != nil {
		t.Fatalf("failed removal deleted live session: %v", err)
	}
	now = now.Add(removedSessionTTL)
	store.list()
	if len(store.removed) != 0 {
		t.Fatal("expired tombstones were not pruned")
	}
}

func sessionPR() github.PR {
	return github.PR{
		Owner: "org", Repo: "repo", Number: 7, Title: "Review this change", State: "open",
		URL:     "https://github.com/org/repo/pull/7",
		HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40),
		Files: []github.File{{Path: "main.go", Status: "modified", Patch: "@@ -1,4 +1,4 @@\n package main\n func main() {\n- println(\"old\")\n+ println(\"hello\")\n }\n"}},
	}
}

func emptySessionDraft() sessionDraft {
	return sessionDraft{Event: "COMMENT", Comments: []sessionComment{}}
}

func TestSessionActionReplayAndAtomicAcknowledgement(t *testing.T) {
	store := newSessionStore()
	pr := sessionPR()
	id, err := store.open(pr, emptySessionDraft())
	if err != nil {
		t.Fatal(err)
	}
	comment := sessionComment{Path: "main.go", Line: 3, Side: "RIGHT", Body: "Please check this", Source: "ai"}
	action := browserAction{Kind: "draft", Comment: &comment}
	first, err := store.enqueue(id, "same-operation", "payload", action)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.enqueue(id, "same-operation", "payload", action)
	if err != nil || second.ID != first.ID {
		t.Fatalf("retry created a different action: %+v %v", second, err)
	}
	if _, err := store.enqueue(id, "same-operation", "changed payload", action); err == nil {
		t.Fatal("accepted idempotency collision")
	}
	session, _ := store.get(id)
	if len(session.Actions) != 1 {
		t.Fatalf("queued %d duplicate actions", len(session.Actions))
	}
	draft := emptySessionDraft()
	draft.Comments = []sessionComment{*session.Actions[0].Comment}
	session, err = store.sync(id, draft, nil, []actionReceipt{{ID: first.ID, Status: "applied"}})
	if err != nil || len(session.Actions) != 0 || len(session.Draft.Comments) != 1 || session.Receipts[0].Status != "applied" {
		t.Fatalf("ack and draft did not commit together: %+v %v", session, err)
	}
	replayed, err := store.enqueue(id, "same-operation", "payload", action)
	if err != nil || replayed.Status != "applied" || replayed.ID != first.ID {
		t.Fatalf("acknowledged action was reapplied: %+v %v", replayed, err)
	}
	draft.Body = "must not overwrite"
	if _, err := store.sync(id, draft, nil, []actionReceipt{{ID: "unknown", Status: "applied"}}); err == nil {
		t.Fatal("unknown acknowledgement accepted")
	}
	session, _ = store.get(id)
	if session.Draft.Body != "" {
		t.Fatal("invalid sync partially committed draft")
	}
}

func TestSessionConcurrentRetriesOnlyQueueOnce(t *testing.T) {
	store := newSessionStore()
	id, _ := store.open(sessionPR(), emptySessionDraft())
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.enqueue(id, "operation", "payload", browserAction{Kind: "source", Path: "main.go", Line: 1}); err != nil {
				t.Error(err)
			}
			_, _ = store.get(id)
		}()
	}
	wg.Wait()
	session, _ := store.get(id)
	if len(session.Actions) != 1 || len(session.Receipts) != 1 {
		t.Fatal("concurrent retries duplicated browser effects")
	}
}

func TestSessionExpiryAndBounds(t *testing.T) {
	now := time.Now()
	store := newSessionStore()
	store.now = func() time.Time { return now }
	ids := []string{}
	for range maxSessions {
		id, err := store.open(sessionPR(), emptySessionDraft())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := store.open(sessionPR(), emptySessionDraft()); err == nil {
		t.Fatal("unbounded active sessions")
	}
	now = now.Add(sessionTTL)
	if _, err := store.get(ids[0]); !errors.Is(err, errSessionGone) {
		t.Fatal("expired session remained accessible")
	}
	id, err := store.open(sessionPR(), emptySessionDraft())
	if err != nil || len(store.list()) != 1 {
		t.Fatalf("expired capacity not reclaimed: %v", err)
	}
	store.close(id)
	if _, err := store.get(id); !errors.Is(err, errSessionGone) {
		t.Fatal("closed session remained accessible")
	}
}

func TestSessionDraftAndPositionValidation(t *testing.T) {
	store := newSessionStore()
	pr := sessionPR()
	id, _ := store.open(pr, emptySessionDraft())
	invalid := []sessionDraft{
		{Event: "EXECUTE"},
		{Event: "COMMENT", Comments: []sessionComment{{ID: "x", Path: "main.go", Line: 900, Side: "RIGHT", Body: "outside"}}},
		{Event: "COMMENT", Comments: []sessionComment{{ID: "x", Path: "../main.go", Line: 3, Side: "RIGHT", Body: "outside"}}},
		{Event: "COMMENT", Comments: []sessionComment{{ID: "x", Path: "main.go", Line: 3, Side: "RIGHT", Body: "one"}, {ID: "x", Path: "main.go", Line: 3, Side: "RIGHT", Body: "two"}}},
	}
	for _, draft := range invalid {
		if _, err := store.sync(id, draft, nil, nil); err == nil {
			t.Fatalf("accepted invalid draft %+v", draft)
		}
	}
	for _, selected := range []sessionSelection{
		{Path: "main.go", SHA: "wrong", Line: 1},
		{Path: "../main.go", SHA: pr.HeadSHA, Line: 1},
		{Path: "main.go", SHA: pr.HeadSHA, Line: 0},
		{Path: "main.go", SHA: pr.HeadSHA, Line: 1, Character: -1},
	} {
		if _, err := store.sync(id, emptySessionDraft(), &selected, nil); err == nil {
			t.Fatalf("accepted invalid selection %+v", selected)
		}
	}
}

func TestSessionQueueBoundAndFailedReceipt(t *testing.T) {
	store := newSessionStore()
	id, _ := store.open(sessionPR(), emptySessionDraft())
	var first actionReceipt
	for i := range maxPendingActions {
		receipt, err := store.enqueue(id, strings.Repeat("x", i+1), i, browserAction{Kind: "source"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = receipt
		}
	}
	if _, err := store.enqueue(id, "over-limit", "payload", browserAction{Kind: "source"}); err == nil {
		t.Fatal("pending action count is unbounded")
	}
	if _, err := store.sync(id, emptySessionDraft(), nil, []actionReceipt{{ID: first.ID, Status: "failed", Message: "Cannot open source"}}); err != nil {
		t.Fatal(err)
	}
	retried, err := store.enqueue(id, "x", 0, browserAction{Kind: "source"})
	if err != nil || retried.Status != "failed" || retried.ID != first.ID {
		t.Fatal("failed browser action replayed automatically")
	}
}

func TestUTF16NavigationPosition(t *testing.T) {
	for _, tt := range []struct {
		column int
		valid  bool
	}{{0, true}, {1, true}, {2, false}, {3, true}, {4, true}, {5, false}} {
		if got := validUTF16Position("a😀b\r\n", 1, tt.column); got != tt.valid {
			t.Errorf("column %d: %v", tt.column, got)
		}
	}
}
