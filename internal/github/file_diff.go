package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
)

const maxFallbackBytes = 256 << 10
const maxFallbackLines = 2000

type DiffRequest struct {
	Owner        string `json:"owner"`
	Repo         string `json:"repo"`
	BaseSHA      string `json:"baseSHA"`
	HeadSHA      string `json:"headSHA"`
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath,omitempty"`
	Status       string `json:"status"`
}

type FileDiff struct {
	Patch  string `json:"patch"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type ContentReader func(context.Context, string, string, string, string) (string, error)

// Reconstructed lines are display-only because GitHub supplied no review anchors.
func ReconstructDiff(ctx context.Context, request DiffRequest, content ContentReader) (FileDiff, error) {
	if err := validatePull(request.Owner, request.Repo, 1); err != nil {
		return FileDiff{}, err
	}
	if !validSHA(request.BaseSHA) || !validSHA(request.HeadSHA) || !validPath(request.Path) || (request.PreviousPath != "" && !validPath(request.PreviousPath)) {
		return FileDiff{}, errors.New("diff reconstruction requires immutable revisions and repository-relative paths")
	}
	switch request.Status {
	case "added", "removed", "modified", "renamed", "copied", "changed", "unchanged":
	default:
		return FileDiff{}, errors.New("unsupported changed-file status")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	previous := request.Path
	if request.PreviousPath != "" {
		previous = request.PreviousPath
	}
	var base, head string
	read := func(sha, path string) (string, *FileDiff) {
		source, err := content(ctx, request.Owner, request.Repo, sha, path)
		if err != nil {
			switch {
			case errors.Is(err, ErrBinarySource):
				return "", &FileDiff{Status: "binary", Reason: "This file contains binary data; a text diff is not available."}
			case errors.Is(err, ErrResponseTooLarge):
				return "", &FileDiff{Status: "too_large", Reason: "This file exceeds the source size limit. Review it on GitHub."}
			default:
				return "", &FileDiff{Status: "unavailable", Reason: "Could not load the pinned file content. Retry or inspect it on GitHub."}
			}
		}
		if len(source) > maxFallbackBytes || len(sourceLines(source)) > maxFallbackLines {
			return "", &FileDiff{Status: "too_large", Reason: "This file exceeds the reconstructed view limit of 256 KiB or 2,000 lines. Open its source or review it on GitHub."}
		}
		return source, nil
	}
	var failure *FileDiff
	if request.Status != "added" {
		base, failure = read(request.BaseSHA, previous)
		if failure != nil {
			return *failure, nil
		}
	}
	if request.Status != "removed" {
		head, failure = read(request.HeadSHA, request.Path)
		if failure != nil {
			return *failure, nil
		}
	}
	status := "reconstructed"
	reason := "Locally reconstructed from the pinned revisions. GitHub provided no comment anchors; use the review summary for feedback."
	if base == head {
		status = "unchanged"
		reason = "No text content changed. This read-only view shows the file at the reviewed revision."
		if request.Status == "renamed" {
			reason = "Renamed without text changes. The file is shown read-only; use the review summary for feedback."
		}
	}
	return FileDiff{Patch: fullContextDiff(base, head), Status: status, Reason: reason}, nil
}

func sourceLines(source string) []string {
	if source == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(source, "\n"), "\n")
}

func fullContextDiff(base, head string) string {
	if base == "" && head == "" {
		return ""
	}
	engine := diffmatchpatch.New()
	engine.DiffTimeout = 250 * time.Millisecond
	oldChars, newChars, dictionary := engine.DiffLinesToChars(base, head)
	edits := engine.DiffCharsToLines(engine.DiffMain(oldChars, newChars, false), dictionary)
	oldCount, newCount := len(sourceLines(base)), len(sourceLines(head))
	oldStart, newStart := 1, 1
	if oldCount == 0 {
		oldStart = 0
	}
	if newCount == 0 {
		newStart = 0
	}
	var patch strings.Builder
	fmt.Fprintf(&patch, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
	for _, edit := range edits {
		marker := byte(' ')
		if edit.Type == diffmatchpatch.DiffDelete {
			marker = '-'
		} else if edit.Type == diffmatchpatch.DiffInsert {
			marker = '+'
		}
		for _, line := range sourceLines(edit.Text) {
			patch.WriteByte(marker)
			patch.WriteString(line)
			patch.WriteByte('\n')
		}
		if edit.Text != "" && !strings.HasSuffix(edit.Text, "\n") {
			patch.WriteString("\\ No newline at end of file\n")
		}
	}
	return strings.TrimSuffix(patch.String(), "\n")
}
