package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel"
)

const headSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const baseSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const mergeSHA = "cccccccccccccccccccccccccccccccccccccccc"
const diff = "@@ -2,3 +2,4 @@\n context\n-old\n+new\n+extra\n context"

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := New(func(context.Context) (string, error) { return "test-token", nil })
	c.baseURL = server.URL
	return c
}

func pullJSON(head string, files int) string {
	return fmt.Sprintf(`{"number":42,"title":"Change","state":"open","html_url":"https://github.com/acme/app/pull/42","changed_files":%d,"head":{"sha":%q,"ref":"feature"},"base":{"sha":%q,"ref":"main"},"user":{"login":"reviewer"}}`, files, head, baseSHA)
}

func fixture(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.URL.Path == "/repos/acme/app/pulls/42":
		fmt.Fprint(w, pullJSON(headSHA, 1))
	case strings.Contains(r.URL.Path, "/compare/"):
		fmt.Fprintf(w, `{"merge_base_commit":{"sha":%q}}`, mergeSHA)
	case r.URL.Path == "/repos/acme/app/pulls/42/files":
		json.NewEncoder(w).Encode([]map[string]any{{"filename": "main.go", "status": "modified", "patch": diff}})
	default:
		return false
	}
	return true
}

func TestLoadUsesMergeBaseAndEscapesPagination(t *testing.T) {
	var pages []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			t.Error("missing authentication or API version")
		}
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/42":
			fmt.Fprint(w, pullJSON(headSHA, 2))
		case strings.HasSuffix(r.URL.Path, "/files"):
			page := r.URL.Query().Get("page")
			pages = append(pages, page)
			if page == "1" {
				// Never follow an API-provided pagination URL with credentials.
				w.Header().Set("Link", `<https://evil.invalid/steal>; rel="next"`)
			}
			fmt.Fprintf(w, `[{"filename":"file%s.go","status":"added"}]`, page)
		default:
			if !fixture(w, r) {
				t.Errorf("unexpected request %s", r.URL.Path)
			}
		}
	})
	pr, err := c.Load(context.Background(), "acme", "app", 42)
	if err != nil {
		t.Fatal(err)
	}
	if pr.BaseSHA != mergeSHA || pr.HeadSHA != headSHA || len(pr.Files) != 2 || strings.Join(pages, ",") != "1,2" {
		t.Fatalf("incorrect immutable PR: %+v, pages %v", pr, pages)
	}
}

func TestLoadRejectsIncompleteAndMovingDiffs(t *testing.T) {
	for _, scenario := range []string{"over-limit", "missing-file", "moving-head"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/acme/app/pulls/42" {
					calls++
					files, head := 1, headSHA
					if scenario == "over-limit" {
						files = 3001
					}
					if scenario == "missing-file" {
						files = 2
					}
					if scenario == "moving-head" && calls > 1 {
						head = mergeSHA
					}
					fmt.Fprint(w, pullJSON(head, files))
					return
				}
				fixture(w, r)
			})
			if _, err := c.Load(context.Background(), "acme", "app", 42); err == nil {
				t.Fatal("accepted incomplete or moving diff")
			}
		})
	}
}

func TestReviewPinsCommitAndValidatesLocation(t *testing.T) {
	var posts atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if input["commit_id"] != headSHA || input["event"] != "COMMENT" {
				t.Errorf("review not pinned: %+v", input)
			}
			comments, ok := input["comments"].([]any)
			if !ok || len(comments) != 1 {
				t.Fatalf("inline comments missing: %+v", input)
			}
			comment := comments[0].(map[string]any)
			if comment["side"] != "RIGHT" || comment["line"] != float64(4) || comment["path"] != "main.go" {
				t.Errorf("incorrect GitHub inline location: %+v", comment)
			}
			fmt.Fprint(w, `{"html_url":"https://github.com/acme/app/pull/42#review"}`)
			return
		}
		fixture(w, r)
	})
	review := Review{HeadSHA: headSHA, BaseSHA: mergeSHA, Event: "COMMENT", Comments: []Comment{{Path: "main.go", Side: "RIGHT", Line: 4, Body: "Explain this change"}}}
	if _, err := c.Review(context.Background(), "acme", "app", 42, review); err != nil {
		t.Fatal(err)
	}
	review.HeadSHA = baseSHA
	if _, err := c.Review(context.Background(), "acme", "app", 42, review); err == nil {
		t.Fatal("stale review accepted")
	}
	review.HeadSHA = headSHA
	review.BaseSHA = baseSHA
	if _, err := c.Review(context.Background(), "acme", "app", 42, review); err == nil || !strings.Contains(err.Error(), "merge base changed") {
		t.Fatalf("same-head changed-base draft was not rejected: %v", err)
	}
	review.BaseSHA = mergeSHA
	review.Comments[0].Line = 100
	if _, err := c.Review(context.Background(), "acme", "app", 42, review); err == nil {
		t.Fatal("comment outside diff accepted")
	}
	if posts.Load() != 1 {
		t.Fatalf("invalid reviews sent: %d writes", posts.Load())
	}
}

func TestContainsLineRespectsDiffSides(t *testing.T) {
	for _, tc := range []struct {
		side string
		line int
		want bool
	}{{"LEFT", 2, true}, {"LEFT", 3, true}, {"RIGHT", 3, true}, {"RIGHT", 4, true}, {"LEFT", 5, false}, {"RIGHT", 5, true}, {"RIGHT", 1, false}} {
		if got := containsLine(diff, tc.side, tc.line); got != tc.want {
			t.Errorf("%s line %d: got %v want %v", tc.side, tc.line, got, tc.want)
		}
	}
}

func TestContentRequiresImmutableSafePathAndRejectsBinary(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("ref") != headSHA || r.Header.Get("Accept") != "application/vnd.github.raw+json" {
			t.Error("file request was not pinned raw content")
		}
		if strings.Contains(r.URL.Path, "binary") {
			w.Write([]byte{0, 1})
			return
		}
		fmt.Fprint(w, "package main\n")
	})
	if value, err := c.Content(context.Background(), "acme", "app", headSHA, "path with spaces/main.go"); err != nil || value != "package main\n" {
		t.Fatalf("content: %q, %v", value, err)
	}
	for _, invalid := range []struct{ sha, path string }{{"main", "main.go"}, {headSHA, "../secret"}, {headSHA, "/etc/passwd"}, {headSHA, "dir/../secret"}, {headSHA, "a\\b"}} {
		if _, err := c.Content(context.Background(), "acme", "app", invalid.sha, invalid.path); err == nil {
			t.Fatalf("accepted invalid request: %+v", invalid)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("invalid request reached API")
	}
	if _, err := c.Content(context.Background(), "acme", "app", headSHA, "binary"); err == nil {
		t.Fatal("binary source accepted")
	}
}

func TestAPIErrorDoesNotExposeBodyOrCredential(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "private-repo-content test-token")
	})
	_, err := c.Content(context.Background(), "acme", "app", headSHA, "main.go")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("expected typed API failure, got %v", err)
	}
	if strings.Contains(err.Error(), "private-repo-content") || strings.Contains(err.Error(), "test-token") {
		t.Fatal("sensitive error")
	}
}

func TestOrdinaryRequestsNeverFollowRedirects(t *testing.T) {
	var leaked atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer destination.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) })
	if _, err := c.Content(context.Background(), "acme", "app", headSHA, "main.go"); err == nil || leaked.Load() {
		t.Fatalf("redirect followed: %v", err)
	}
}

func TestArchiveRedirectStripsCredentialsAndRejectsUntrustedHosts(t *testing.T) {
	initial, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/acme/app/tarball/"+headSHA, nil)
	for _, target := range []string{"https://codeload.github.com/acme/app/tar.gz/" + headSHA, "http://codeload.github.com/x", "https://evil.invalid/x", "https://api.github.com:443/x", "https://user@api.github.com/x"} {
		req, _ := http.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer test-token")
		err := archiveRedirect(req, []*http.Request{initial})
		if strings.HasPrefix(target, "https://codeload.github.com/") {
			if err != nil || req.Header.Get("Authorization") != "" {
				t.Fatalf("codeload redirect leaked credentials: %v", err)
			}
		} else if err == nil {
			t.Fatalf("untrusted redirect accepted: %s", target)
		}
	}
}

func TestArchiveReadEnforcesStreamingLimit(t *testing.T) {
	_, span := otel.Tracer("test").Start(context.Background(), "archive")
	reader := &archiveReader{body: io.NopCloser(strings.NewReader("123456")), remaining: 5, span: span}
	defer reader.Close()
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("archive exceeded streaming limit")
	}
}

func TestCommentsAndReply(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/app/pulls/comments/7" {
			fmt.Fprint(w, `{"id":7,"pull_request_url":"https://api.github.com/repos/acme/app/pulls/42"}`)
			return
		}
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `[{"id":7,"body":"A concern","path":"main.go","line":null,"side":"RIGHT","user":{"login":"reviewer"},"html_url":"https://github.com/acme/app/pull/42#comment"}]`)
			return
		}
		if r.URL.Path != "/repos/acme/app/pulls/42/comments/7/replies" {
			t.Errorf("unexpected reply path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"html_url":"https://github.com/acme/app/pull/42#reply"}`)
	})
	comments, err := c.Comments(context.Background(), "acme", "app", 42)
	if err != nil || len(comments) != 1 || comments[0].Line != 0 || comments[0].Author != "reviewer" {
		t.Fatalf("comments: %+v, %v", comments, err)
	}
	link, err := c.Reply(context.Background(), "acme", "app", 42, 7, "Addressed")
	if err != nil {
		t.Fatal(err)
	}
	if parsed, err := url.Parse(link); err != nil || parsed.Fragment != "reply" {
		t.Fatalf("reply link %s", link)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestArchiveRedirectEndToEndKeepsCredentialOnAPIHost(t *testing.T) {
	c := New(func(context.Context) (string, error) { return "test-token", nil })
	calls := 0
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host == "api.github.com" {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("API request missing credential")
			}
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://codeload.github.com/acme/app/tar.gz/" + headSHA + "?token=signed-link"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		if r.URL.Host != "codeload.github.com" || r.Header.Get("Authorization") != "" {
			t.Error("archive credential crossed host boundary")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("archive bytes")), Request: r}, nil
	})
	body, err := c.Archive(context.Background(), "acme", "app", headSHA)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil || string(data) != "archive bytes" || calls != 2 {
		t.Fatalf("archive: %q, %v; calls %d", data, err, calls)
	}
}

func TestFileSizeLimit(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", maxFile+1))
	})
	if _, err := c.Content(context.Background(), "acme", "app", headSHA, "huge.go"); err == nil {
		t.Fatal("oversized file accepted")
	}
}

func TestReviewRejectsMalformedDraftsBeforeNetwork(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid draft reached network")
	})
	for _, review := range []Review{
		{HeadSHA: "main", BaseSHA: mergeSHA, Event: "APPROVE"},
		{HeadSHA: headSHA, Event: "APPROVE"},
		{HeadSHA: headSHA, BaseSHA: "main", Event: "APPROVE"},
		{HeadSHA: headSHA, BaseSHA: mergeSHA, Event: "MERGE"},
		{HeadSHA: headSHA, BaseSHA: mergeSHA, Event: "COMMENT"},
		{HeadSHA: headSHA, BaseSHA: mergeSHA, Event: "REQUEST_CHANGES"},
		{HeadSHA: headSHA, BaseSHA: mergeSHA, Event: "COMMENT", Body: strings.Repeat("x", 65537)},
		{HeadSHA: headSHA, BaseSHA: mergeSHA, Event: "COMMENT", Comments: []Comment{{Path: "main.go", Line: 1, Side: "INVALID", Body: "Comment"}}},
	} {
		if _, err := c.Review(context.Background(), "acme", "app", 42, review); err == nil {
			t.Fatalf("invalid review accepted: %+v", review)
		}
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenReader) Close() error             { return nil }

func TestWriteOutcomeIsUncertainAfterTransportReadAndDecodeFailure(t *testing.T) {
	for _, scenario := range []string{"transport", "read", "decode", "server-error"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			c := New(func(context.Context) (string, error) { return "test-token", nil })
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/repos/acme/app/pulls/comments/7" {
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"id":7,"pull_request_url":"https://api.github.com/repos/acme/app/pulls/42"}`)), Request: r}, nil
				}
				calls++
				if scenario == "transport" {
					return nil, io.ErrUnexpectedEOF
				}
				resp := &http.Response{StatusCode: http.StatusCreated, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("not json")), Request: r}
				if scenario == "read" {
					resp.Body = brokenReader{}
				}
				if scenario == "server-error" {
					resp.StatusCode = http.StatusInternalServerError
				}
				return resp, nil
			})
			_, err := c.Reply(context.Background(), "acme", "app", 42, 7, "Reviewed")
			var uncertain *UncertainWriteError
			if !errors.As(err, &uncertain) || !strings.Contains(err.Error(), "before retrying") || calls != 1 {
				t.Fatalf("write outcome: %v; requests %d", err, calls)
			}
			var result any
			_, err = c.json(context.Background(), http.MethodGet, "/test", nil, &result)
			if err == nil || errors.As(err, &uncertain) {
				t.Fatalf("GET incorrectly reported write uncertainty: %v", err)
			}
		})
	}
}

func TestReplyResolvesParentAndRejectsDifferentPR(t *testing.T) {
	posts := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if strings.HasSuffix(r.URL.Path, "/8") {
				fmt.Fprint(w, `{"id":8,"in_reply_to_id":7,"pull_request_url":"https://api.github.com/repos/acme/app/pulls/42"}`)
			} else {
				fmt.Fprint(w, `{"id":9,"pull_request_url":"https://api.github.com/repos/acme/app/pulls/99"}`)
			}
			return
		}
		posts++
		if !strings.HasSuffix(r.URL.Path, "/comments/7/replies") {
			t.Errorf("reply targeted child rather than parent: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"html_url":"https://github.com/acme/app/pull/42#reply"}`)
	})
	if _, err := c.Reply(context.Background(), "acme", "app", 42, 8, "Follow-up"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reply(context.Background(), "acme", "app", 42, 9, "Wrong PR"); err == nil {
		t.Fatal("cross-PR comment reply accepted")
	}
	if posts != 1 {
		t.Fatalf("wrong number of writes: %d", posts)
	}
}
