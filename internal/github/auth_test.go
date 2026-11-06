package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestBrowserAuthenticationRequiresUser(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/user" {
					t.Fatalf("unexpected authentication endpoint")
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"login":"reviewer"}`)
			})
			login, err := client.Authenticate(context.Background())
			if status == http.StatusOK {
				if err != nil || login != "reviewer" {
					t.Fatalf("authentication failed: %v", err)
				}
			} else if err == nil || login != "" {
				t.Fatal("accepted a credential without authenticated user access")
			}
		})
	}
}

func TestAuthorizeRevisionRequiresCurrentContentsPermission(t *testing.T) {
	status := http.StatusOK
	requests := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/repos/acme/app/git/commits/"+headSHA {
			t.Fatalf("wrong revision authorization endpoint")
		}
		w.WriteHeader(status)
		fmt.Fprint(w, `{}`)
	})
	if err := client.AuthorizeRevision(context.Background(), "acme", "app", headSHA); err != nil {
		t.Fatal(err)
	}
	for _, denied := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		status = denied
		err := client.AuthorizeRevision(context.Background(), "acme", "app", headSHA)
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.Status != status {
			t.Fatalf("authorization did not propagate status %d", status)
		}
	}
	if err := client.AuthorizeRevision(context.Background(), "acme", "app", "main"); err == nil || requests != 4 {
		t.Fatal("mutable revision reached GitHub")
	}
}
