package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestConversationCombinesPaginatedCommentsAndSubmittedReviews(t *testing.T) {
	var requests []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("missing bounded page size: %s", r.URL)
		}
		switch r.URL.Path + "?page=" + r.URL.Query().Get("page") {
		case "/repos/acme/app/issues/42/comments?page=1":
			w.Header().Set("Link", `<https://api.github.com/repos/acme/app/issues/42/comments?page=2>; rel="next"`)
			fmt.Fprint(w, `[{"id":7,"body":"Later question","created_at":"2026-11-01T15:04:00Z","user":{"login":"author"},"html_url":"https://github.com/acme/app/pull/42#issuecomment-7"}]`)
		case "/repos/acme/app/issues/42/comments?page=2":
			fmt.Fprint(w, `[{"id":7,"body":"Later question","created_at":"2026-11-01T15:04:00Z"},{"id":8,"body":"First question","created_at":"2026-11-01T15:01:00Z"}]`)
		case "/repos/acme/app/pulls/42/reviews?page=1":
			w.Header().Set("Link", `<https://api.github.com/repos/acme/app/pulls/42/reviews?page=2>; rel="next"`)
			fmt.Fprint(w, `[{"id":7,"body":"","state":"APPROVED","submitted_at":"2026-11-01T15:02:00Z","user":{"login":"reviewer"}},{"id":9,"body":"Private draft","state":"PENDING","submitted_at":null}]`)
		case "/repos/acme/app/pulls/42/reviews?page=2":
			fmt.Fprint(w, `[{"id":10,"body":"Please address this","state":"CHANGES_REQUESTED","submitted_at":"2026-11-01T15:03:00Z"}]`)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	})
	entries, err := c.Conversation(context.Background(), "acme", "app", 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 4 || len(entries) != 4 {
		t.Fatalf("requests %v; entries %+v", requests, entries)
	}
	want := []struct {
		id   int64
		kind string
	}{{8, "comment"}, {7, "review"}, {10, "review"}, {7, "comment"}}
	for i, entry := range entries {
		if entry.ID != want[i].id || entry.Kind != want[i].kind {
			t.Errorf("entry %d: %+v", i, entry)
		}
	}
	if entries[1].State != "APPROVED" || entries[1].Body != "" || entries[1].Author != "reviewer" {
		t.Errorf("empty review decision lost: %+v", entries[1])
	}
	if entries[3].Author != "author" || entries[3].URL != "https://github.com/acme/app/pull/42#issuecomment-7" {
		t.Errorf("comment metadata lost: %+v", entries[3])
	}
}

func TestConversationFailsWithoutReturningPartialDiscussion(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/reviews") {
			http.Error(w, "private customer content", http.StatusForbidden)
			return
		}
		fmt.Fprint(w, `[{"id":1,"body":"Visible","created_at":"2026-11-01T15:00:00Z"}]`)
	})
	entries, err := c.Conversation(context.Background(), "acme", "app", 42)
	if err == nil || entries != nil || strings.Contains(err.Error(), "private customer content") {
		t.Fatalf("partial response or unsafe error: %+v, %v", entries, err)
	}
}

func TestConversationBoundsPaginationEvenForPendingReviews(t *testing.T) {
	requests := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if strings.Contains(r.URL.Path, "/issues/") {
			fmt.Fprint(w, `[]`)
			return
		}
		w.Header().Set("Link", `<https://api.github.com/repos/acme/app/pulls/42/reviews?page=999>; rel="next"`)
		fmt.Fprint(w, `[{"id":9,"state":"PENDING","submitted_at":null}]`)
	})
	entries, err := c.Conversation(context.Background(), "acme", "app", 42)
	if err == nil || entries != nil || requests != 1+maxItems/pageSize {
		t.Fatalf("unbounded pages: requests %d, entries %+v, err %v", requests, entries, err)
	}
}

func TestConversationBoundsAggregateBodies(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		entries := []map[string]any{{"id": 1, "body": strings.Repeat("x", maxResponse/2+1)}}
		if strings.HasSuffix(r.URL.Path, "/reviews") {
			entries[0]["state"] = "COMMENTED"
			entries[0]["submitted_at"] = "2026-11-01T15:01:00Z"
		} else {
			entries[0]["created_at"] = "2026-11-01T15:00:00Z"
		}
		if err := json.NewEncoder(w).Encode(entries); err != nil {
			t.Error(err)
		}
	})
	entries, err := c.Conversation(context.Background(), "acme", "app", 42)
	if err == nil || entries != nil || !strings.Contains(err.Error(), "supported limit") {
		t.Fatalf("unbounded bodies: %+v, %v", entries, err)
	}
}

func TestCommentsPreserveThreadAnchorsAndOutdatedState(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"id":1,"line":8,"start_line":6,"side":"LEFT","start_side":"LEFT","original_line":10,"original_start_line":8,"commit_id":"current","original_commit_id":"original","created_at":"2026-11-01T15:00:00Z"},
			{"id":2,"line":8,"side":"LEFT","in_reply_to_id":1},
			{"id":3,"line":null,"original_line":9,"side":"RIGHT","subject_type":"line"},
			{"id":4,"line":null,"subject_type":"file"}
		]`)
	})
	comments, err := c.Comments(context.Background(), "acme", "app", 42)
	if err != nil || len(comments) != 4 {
		t.Fatalf("comments: %+v, %v", comments, err)
	}
	root := comments[0]
	if root.Line != 8 || root.StartLine != 6 || root.Side != "LEFT" || root.StartSide != "LEFT" || root.OriginalLine != 10 || root.OriginalStartLine != 8 || root.CommitID != "current" || root.OriginalCommitID != "original" || root.CreatedAt == "" || root.Outdated {
		t.Errorf("root metadata lost: %+v", root)
	}
	if comments[1].InReplyToID != 1 || !comments[2].Outdated || comments[2].Line != 0 || comments[2].OriginalLine != 9 || comments[3].Outdated || comments[3].SubjectType != "file" {
		t.Errorf("thread metadata lost: %+v", comments)
	}
}
