package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestLoadReportsAuthenticatedReviewer(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			fmt.Fprint(w, `{"login":"Reviewer"}`)
			return
		}
		fixture(w, r)
	})
	pr, err := c.Load(context.Background(), "acme", "app", 42)
	if err != nil || pr.Reviewer != "Reviewer" || pr.Author != "reviewer" {
		t.Fatalf("incorrect identities: author=%q reviewer=%q err=%v", pr.Author, pr.Reviewer, err)
	}
}

func TestLoadReviewerLookupFailures(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/user" {
					w.WriteHeader(status)
					return
				}
				fixture(w, r)
			})
			pr, err := c.Load(context.Background(), "acme", "app", 42)
			unavailable := status == 403 || status == 404
			if (err == nil) != unavailable || pr.Reviewer != "" {
				t.Fatalf("status %d: reviewer=%q err=%v", status, pr.Reviewer, err)
			}
		})
	}
}

func TestSelfReviewDecisionsNeverPostButCommentsRemainAllowed(t *testing.T) {
	for _, event := range []string{"APPROVE", "REQUEST_CHANGES", "COMMENT"} {
		t.Run(event, func(t *testing.T) {
			posts := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/user" {
					fmt.Fprint(w, `{"login":"REVIEWER"}`)
					return
				}
				if r.Method == http.MethodPost {
					posts++
					fmt.Fprint(w, `{"html_url":"https://github.com/acme/app/pull/42#review"}`)
					return
				}
				fixture(w, r)
			})
			_, err := c.Review(context.Background(), "acme", "app", 42, Review{
				HeadSHA: headSHA, BaseSHA: mergeSHA, Event: event, Body: "Review summary",
			})
			if event == "COMMENT" {
				if err != nil || posts != 1 {
					t.Fatalf("self comment rejected: posts=%d err=%v", posts, err)
				}
			} else if posts != 0 || err == nil || !strings.Contains(err.Error(), "own pull request") {
				t.Fatalf("self review reached GitHub: posts=%d err=%v", posts, err)
			}
		})
	}
}
