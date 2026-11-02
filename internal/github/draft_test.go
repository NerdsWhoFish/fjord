package github

import "testing"

func TestDraftValidationReusesReviewAnchorsWithoutSubmissionRequirements(t *testing.T) {
	pr := PR{HeadSHA: headSHA, BaseSHA: mergeSHA, Files: []File{{Path: "main.go", Patch: diff}}}
	draft := Review{HeadSHA: headSHA, BaseSHA: mergeSHA, Event: "REQUEST_CHANGES"}
	if err := ValidateDraft(pr, draft); err != nil {
		t.Fatalf("empty draft summary must be editable before submission: %v", err)
	}
	if err := validateReview(draft); err == nil {
		t.Fatal("submission unexpectedly accepted missing request-changes summary")
	}
	draft.Event = "COMMENT"
	draft.Comments = []Comment{{Path: "main.go", Line: 4, Side: "RIGHT", Body: "Review this"}}
	if err := ValidateDraft(pr, draft); err != nil {
		t.Fatalf("valid inline draft rejected: %v", err)
	}
	draft.Comments[0].Line = 999
	if err := ValidateDraft(pr, draft); err == nil {
		t.Fatal("draft accepted a line outside the original diff")
	}
	draft.Comments = nil
	draft.HeadSHA = mergeSHA
	if err := ValidateDraft(pr, draft); err == nil {
		t.Fatal("draft accepted a different head revision")
	}
}
