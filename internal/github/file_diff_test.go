package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func diffRequest() DiffRequest {
	return DiffRequest{Owner: "acme", Repo: "app", BaseSHA: mergeSHA, HeadSHA: headSHA, Path: "new.go", PreviousPath: "old.go", Status: "renamed"}
}

func TestReconstructRenamedFileUsesPinnedPaths(t *testing.T) {
	var reads []string
	result, err := ReconstructDiff(context.Background(), diffRequest(), func(_ context.Context, owner, repo, sha, path string) (string, error) {
		reads = append(reads, sha+":"+path)
		if owner != "acme" || repo != "app" {
			t.Fatal("wrong repository")
		}
		return "package main\n\nfunc main() {}\n", nil
	})
	if err != nil || result.Status != "unchanged" || !strings.Contains(result.Reason, "Renamed without text changes") {
		t.Fatalf("unexpected result: %#v, %v", result, err)
	}
	if strings.Join(reads, ",") != mergeSHA+":old.go,"+headSHA+":new.go" {
		t.Fatalf("wrong pinned sources: %v", reads)
	}
	if result.Patch != "@@ -1,3 +1,3 @@\n package main\n \n func main() {}" {
		t.Fatalf("wrong read-only context: %q", result.Patch)
	}
}

func TestReconstructTextEditsAndEmptySides(t *testing.T) {
	for _, tc := range []struct {
		name, status, base, head, want string
	}{
		{"modified", "modified", "one\nold\nlast\n", "one\nnew\nlast\n", "@@ -1,3 +1,3 @@\n one\n-old\n+new\n last"},
		{"added", "added", "", "new\n", "@@ -0,0 +1,1 @@\n+new"},
		{"removed", "removed", "old\n", "", "@@ -1,1 +0,0 @@\n-old"},
		{"missing-newline", "modified", "old", "new", "@@ -1,1 +1,1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file"},
		{"empty", "modified", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := diffRequest()
			request.Status = tc.status
			result, err := ReconstructDiff(context.Background(), request, func(_ context.Context, _, _, sha, _ string) (string, error) {
				if sha == mergeSHA {
					if tc.status == "added" {
						t.Fatal("read absent base")
					}
					return tc.base, nil
				}
				if tc.status == "removed" {
					t.Fatal("read absent head")
				}
				return tc.head, nil
			})
			if err != nil || result.Patch != tc.want {
				t.Fatalf("patch %q, want %q, error %v", result.Patch, tc.want, err)
			}
		})
	}
}

func TestReconstructFailureReasonsAreBoundedAndSafe(t *testing.T) {
	for _, tc := range []struct {
		name, source, status string
		err                  error
	}{
		{name: "binary", status: "binary", err: ErrBinarySource},
		{name: "API size", status: "too_large", err: ErrResponseTooLarge},
		{name: "bytes", status: "too_large", source: strings.Repeat("x", maxFallbackBytes+1)},
		{name: "lines", status: "too_large", source: strings.Repeat("x\n", maxFallbackLines+1)},
		{name: "upstream failure", status: "unavailable", err: errors.New("secret upstream response")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ReconstructDiff(context.Background(), diffRequest(), func(context.Context, string, string, string, string) (string, error) {
				return tc.source, tc.err
			})
			if err != nil || result.Status != tc.status || result.Patch != "" || strings.Contains(result.Reason, "secret") {
				t.Fatalf("unexpected failure: %#v %v", result, err)
			}
		})
	}
}

func TestReconstructionRejectsUnpinnedOrUnsafeInputsBeforeReading(t *testing.T) {
	for _, change := range []func(*DiffRequest){
		func(in *DiffRequest) { in.BaseSHA = "main" },
		func(in *DiffRequest) { in.HeadSHA = "main" },
		func(in *DiffRequest) { in.Path = "../file" },
		func(in *DiffRequest) { in.PreviousPath = "/private/file" },
		func(in *DiffRequest) { in.Status = "unknown" },
	} {
		in := diffRequest()
		change(&in)
		_, err := ReconstructDiff(context.Background(), in, func(context.Context, string, string, string, string) (string, error) {
			t.Fatal("read invalid request")
			return "", nil
		})
		if err == nil {
			t.Fatal("invalid diff request accepted")
		}
	}
}

func TestMissingPatchIsReadOnlyAndDoesNotFetchContentDuringLoad(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/files") {
			fmt.Fprint(w, `[{"filename":"new.go","previous_filename":"old.go","status":"renamed","additions":0,"deletions":0}]`)
		} else if !fixture(w, r) {
			t.Fatalf("unexpected eager content read %s", r.URL.Path)
		}
	})
	pr, err := c.Load(context.Background(), "acme", "app", 42)
	if err != nil || pr.Files[0].Reviewable == nil || *pr.Files[0].Reviewable {
		t.Fatalf("missing patch not marked read-only: %#v %v", pr, err)
	}
	pr.Files[0].Patch = "@@ -1,1 +1,1 @@\n unchanged"
	err = ValidateDraft(pr, Review{HeadSHA: headSHA, BaseSHA: mergeSHA, Event: "COMMENT", Comments: []Comment{{Path: "new.go", Side: "RIGHT", Line: 1, Body: "feedback"}}})
	if err == nil {
		t.Fatal("reconstructed context accepted as a GitHub comment anchor")
	}
}
