package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func reviewWithComment(comment Comment) Review {
	return Review{HeadSHA: headSHA, BaseSHA: mergeSHA, Event: "COMMENT", Comments: []Comment{comment}}
}

func TestMultilineCommentValidation(t *testing.T) {
	for _, test := range []struct {
		name      string
		startLine int
		startSide string
		line      int
		side      string
		valid     bool
	}{
		{"single-line", 0, "", 4, "RIGHT", true},
		{"right-range", 2, "RIGHT", 4, "RIGHT", true},
		{"left-range", 2, "LEFT", 4, "LEFT", true},
		{"missing-start-side", 2, "", 4, "RIGHT", false},
		{"missing-start-line", 0, "RIGHT", 4, "RIGHT", false},
		{"negative-start", -1, "LEFT", 4, "LEFT", false},
		{"same-line", 4, "RIGHT", 4, "RIGHT", false},
		{"reversed", 5, "RIGHT", 4, "RIGHT", false},
		{"cross-side", 2, "LEFT", 4, "RIGHT", false},
		{"invalid-start-side", 2, "head", 4, "RIGHT", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			comment := Comment{Path: "main.go", Line: test.line, Side: test.side, StartLine: test.startLine, StartSide: test.startSide, Body: "Please explain this range."}
			if err := validateReview(reviewWithComment(comment)); (err == nil) != test.valid {
				t.Fatalf("validation: %v; wanted valid=%v", err, test.valid)
			}
		})
	}
}

func TestMultilineJSONDoesNotSilentlyBecomeSingleLine(t *testing.T) {
	for _, extra := range []string{
		`,"start_line":0`,
		`,"start_side":""`,
		`,"start_line":0,"start_side":""`,
		`,"start_line":null,"start_side":null`,
		`,"start_line":null,"start_side":"RIGHT"`,
		`,"start_line":2`,
		`,"start_side":"RIGHT"`,
		`,"startLine":2,"startSide":"RIGHT"`,
		`,"unexpected":true`,
	} {
		var comment Comment
		err := json.Unmarshal([]byte(`{"path":"main.go","line":4,"side":"RIGHT","body":"Concern"`+extra+`}`), &comment)
		if err == nil {
			err = validateReview(reviewWithComment(comment))
		}
		if err == nil {
			t.Errorf("accepted malformed fields: %s", extra)
		}
	}
	for _, extra := range []string{"", `,"start_line":2,"start_side":"RIGHT"`} {
		var comment Comment
		if err := json.Unmarshal([]byte(`{"path":"main.go","line":4,"side":"RIGHT","body":"Concern"`+extra+`}`), &comment); err != nil {
			t.Fatal(err)
		}
		if err := validateReview(reviewWithComment(comment)); err != nil {
			t.Fatalf("valid JSON rejected: %v", err)
		}
	}
}

func TestCommentRangeRequiresEveryLineInOneHunk(t *testing.T) {
	adjacentHunks := "@@ -1,2 +1,2 @@\n a\n b\n@@ -3,2 +3,2 @@\n c\n d"
	separatedHunks := "@@ -1,2 +1,2 @@\n a\n b\n@@ -10,2 +10,2 @@\n j\n k"
	for _, test := range []struct {
		name, patch, side string
		start, end        int
		valid             bool
	}{
		{"right-context-and-additions", diff, "RIGHT", 2, 5, true},
		{"left-context-and-deletion", diff, "LEFT", 2, 4, true},
		{"left-end-missing", diff, "LEFT", 2, 5, false},
		{"right-start-missing", diff, "RIGHT", 1, 3, false},
		{"adjacent-hunks", adjacentHunks, "RIGHT", 2, 3, false},
		{"adjacent-hunks-left", adjacentHunks, "LEFT", 2, 3, false},
		{"within-second-hunk", adjacentHunks, "RIGHT", 3, 4, true},
		{"gap-between-hunks", separatedHunks, "RIGHT", 2, 10, false},
		{"addition-only-right", "@@ -0,0 +1,2 @@\n+a\n+b", "RIGHT", 1, 2, true},
		{"addition-only-left", "@@ -0,0 +1,2 @@\n+a\n+b", "LEFT", 1, 2, false},
		{"deletion-only-left", "@@ -1,2 +0,0 @@\n-a\n-b", "LEFT", 1, 2, true},
		{"deletion-only-right", "@@ -1,2 +0,0 @@\n-a\n-b", "RIGHT", 1, 2, false},
		{"truncated-range", "@@ -1,3 +1,3 @@\n a\n b", "RIGHT", 1, 3, false},
		{"line-count-overrun", "@@ -1 +1 @@\n a\n b", "RIGHT", 1, 2, false},
		{"malformed-line", "@@ -1,3 +1,3 @@\n a\nmissing prefix\n c", "RIGHT", 1, 2, false},
		{"malformed-header", "@@ -huge,3 +1,3 @@\n a\n b", "RIGHT", 1, 2, false},
		{"integer-overflow", "@@ -999999999999999999999999,3 +1,3 @@\n a\n b", "RIGHT", 1, 2, false},
		{"newline-marker", "@@ -1,2 +1,2 @@\n a\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file", "RIGHT", 1, 2, true},
		{"nonpositive-start", diff, "RIGHT", 0, 3, false},
		{"reversed-range", diff, "RIGHT", 4, 3, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := containsRange(test.patch, test.side, test.start, test.end); got != test.valid {
				t.Fatalf("containsRange=%v, want %v", got, test.valid)
			}
		})
	}
}

func TestMultilineReviewSerializesGitHubFieldsForRenamedFile(t *testing.T) {
	for _, side := range []string{"LEFT", "RIGHT"} {
		t.Run(side, func(t *testing.T) {
			posts := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
					var input struct {
						CommitID string           `json:"commit_id"`
						Comments []map[string]any `json:"comments"`
					}
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
						return
					}
					if input.CommitID != headSHA || len(input.Comments) != 1 {
						t.Errorf("invalid review payload: %+v", input)
						return
					}
					comment := input.Comments[0]
					if comment["path"] != "new.go" || comment["start_line"] != float64(2) || comment["line"] != float64(4) || comment["start_side"] != side || comment["side"] != side || comment["startLine"] != nil || comment["startSide"] != nil {
						t.Errorf("invalid multiline payload: %+v", comment)
					}
					fmt.Fprint(w, `{"html_url":"https://github.com/acme/app/pull/42#review"}`)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/files") {
					_ = json.NewEncoder(w).Encode([]map[string]any{{"filename": "new.go", "previous_filename": "old.go", "status": "renamed", "patch": diff}})
					return
				}
				fixture(w, r)
			})
			comment := Comment{Path: "new.go", Line: 4, Side: side, StartLine: 2, StartSide: side, Body: "Check the changed block."}
			review := reviewWithComment(comment)
			if _, err := c.Review(context.Background(), "acme", "app", 42, review); err != nil {
				t.Fatal(err)
			}
			review.Comments[0].Path = "old.go"
			if _, err := c.Review(context.Background(), "acme", "app", 42, review); err == nil {
				t.Fatal("renamed-file review accepted old path")
			}
			review.Comments[0].Path = "new.go"
			review.BaseSHA = baseSHA
			if _, err := c.Review(context.Background(), "acme", "app", 42, review); err == nil {
				t.Fatal("multiline review accepted stale merge base")
			}
			if posts != 1 {
				t.Fatalf("invalid reviews reached GitHub: %d POSTs", posts)
			}
		})
	}
}

func TestMultilineReviewRejectsCrossHunkRangeBeforePosting(t *testing.T) {
	posts := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			return
		}
		if strings.HasSuffix(r.URL.Path, "/files") {
			_ = json.NewEncoder(w).Encode([]map[string]any{{"filename": "main.go", "patch": "@@ -1,2 +1,2 @@\n a\n b\n@@ -3,2 +3,2 @@\n c\n d"}})
			return
		}
		fixture(w, r)
	})
	review := reviewWithComment(Comment{Path: "main.go", Line: 3, Side: "RIGHT", StartLine: 2, StartSide: "RIGHT", Body: "Cross-hunk selection"})
	if _, err := c.Review(context.Background(), "acme", "app", 42, review); err == nil || posts != 0 {
		t.Fatalf("cross-hunk range: error=%v, POSTs=%d", err, posts)
	}
}

func TestSingleLineReviewOmitsRangeFields(t *testing.T) {
	data, err := json.Marshal(Comment{Path: "main.go", Line: 3, Side: "LEFT", Body: "One line"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "start_") {
		t.Fatalf("single-line comment gained range fields: %s", data)
	}
}

func TestExistingMultilineCommentsExposeBrowserFields(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"id":7,"body":"A range","path":"main.go","start_line":2,"start_side":"LEFT","line":4,"side":"LEFT","user":{"login":"reviewer"}},{"id":8,"body":"Outdated","path":"main.go","start_line":null,"start_side":null,"line":null,"side":"RIGHT"}]`)
	})
	comments, err := c.Comments(context.Background(), "acme", "app", 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || comments[0].StartLine != 2 || comments[0].StartSide != "LEFT" || comments[0].Line != 4 || comments[1].StartLine != 0 || comments[1].Line != 0 {
		t.Fatalf("incorrect review comments: %+v", comments)
	}
	data, err := json.Marshal(comments)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"startLine":2`) || !strings.Contains(string(data), `"startSide":"LEFT"`) || strings.Contains(string(data), "start_") {
		t.Fatalf("incorrect browser serialization: %s", data)
	}
}
