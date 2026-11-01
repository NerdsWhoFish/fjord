package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var hunkPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func validSHA(value string) bool { return shaPattern.MatchString(value) }

func validatePull(owner, repo string, number int) error {
	if !ownerPattern.MatchString(owner) || !repoPattern.MatchString(repo) || repo == "." || repo == ".." || number < 1 {
		return errors.New("a valid GitHub owner, repository, and positive pull request number are required")
	}
	return nil
}

func validPath(path string) bool {
	if path == "" || len(path) > 4096 || !utf8.ValidString(path) || strings.Contains(path, "\\") {
		return false
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validBody(body string) bool {
	return utf8.ValidString(body) && len(body) <= 65536 && !strings.ContainsRune(body, 0)
}

func validateReview(review Review) error {
	return validateReviewFields(review, true)
}

func validateReviewFields(review Review, submitting bool) error {
	if !validSHA(review.HeadSHA) || !validSHA(review.BaseSHA) {
		return errors.New("review must identify the full PR head and merge-base commit SHAs")
	}
	if review.Event != "COMMENT" && review.Event != "APPROVE" && review.Event != "REQUEST_CHANGES" {
		return errors.New("review event must be COMMENT, APPROVE, or REQUEST_CHANGES")
	}
	if !validBody(review.Body) || len(review.Comments) > 100 {
		return errors.New("review is too large; use at most 100 comments and 65536 bytes per body")
	}
	if submitting && review.Event == "REQUEST_CHANGES" && strings.TrimSpace(review.Body) == "" {
		return errors.New("requesting changes requires a review summary")
	}
	if submitting && review.Event == "COMMENT" && strings.TrimSpace(review.Body) == "" && len(review.Comments) == 0 {
		return errors.New("a comment review requires a summary or inline comment")
	}
	for _, comment := range review.Comments {
		if !validPath(comment.Path) || comment.Line < 1 || (comment.Side != "LEFT" && comment.Side != "RIGHT") || !validBody(comment.Body) || strings.TrimSpace(comment.Body) == "" {
			return errors.New("inline comments require a file, positive line, LEFT or RIGHT side, and nonempty body")
		}
		if comment.StartLine != 0 || comment.StartSide != "" || comment.startLineSet || comment.startSideSet {
			if comment.StartLine < 1 || comment.StartLine >= comment.Line || comment.StartSide != comment.Side {
				return errors.New("multiline comments require start_line before line and matching LEFT or RIGHT start_side and side")
			}
		}
	}
	return nil
}

// ValidateDraft checks immutable revision and diff anchors without submitting.
func ValidateDraft(pr PR, review Review) error {
	if review.HeadSHA != pr.HeadSHA || review.BaseSHA != pr.BaseSHA {
		return errors.New("draft must match this exact PR head and merge base")
	}
	if err := validateReviewFields(review, false); err != nil {
		return err
	}
	return validateCommentRanges(pr, review.Comments)
}

func validateCommentRanges(pr PR, comments []Comment) error {
	for _, comment := range comments {
		found := false
		start := comment.StartLine
		if start == 0 {
			start = comment.Line
		}
		for _, file := range pr.Files {
			if file.Path == comment.Path && containsRange(file.Patch, comment.Side, start, comment.Line) {
				found = true
				break
			}
		}
		if !found {
			return errors.New("an inline comment is outside the current diff; choose visible lines on one side within a single diff hunk")
		}
	}
	return nil
}

func (c *Client) Review(ctx context.Context, owner, repo string, number int, review Review) (string, error) {
	if err := validatePull(owner, repo, number); err != nil {
		return "", err
	}
	if err := validateReview(review); err != nil {
		return "", err
	}
	pr, err := c.Load(ctx, owner, repo, number)
	if err != nil {
		return "", err
	}
	if pr.HeadSHA != review.HeadSHA {
		return "", errors.New("PR head changed since this draft was written; reload and review the new changes before submitting")
	}
	if pr.BaseSHA != review.BaseSHA {
		return "", errors.New("PR merge base changed since this draft was written; reload and review the updated diff before submitting")
	}
	if pr.State != "open" {
		return "", errors.New("only open pull requests can receive a review")
	}
	if err := validateCommentRanges(pr, review.Comments); err != nil {
		return "", err
	}
	input := struct {
		CommitID string    `json:"commit_id"`
		Event    string    `json:"event"`
		Body     string    `json:"body"`
		Comments []Comment `json:"comments,omitempty"`
	}{review.HeadSHA, review.Event, review.Body, review.Comments}
	var result struct {
		URL string `json:"html_url"`
	}
	_, err = c.json(ctx, http.MethodPost, pullPath(owner, repo, number)+"/reviews", input, &result)
	if err != nil {
		return "", err
	}
	if result.URL == "" {
		// The write may already have succeeded. Do not encourage a retry.
		return pr.URL, nil
	}
	return result.URL, nil
}

func containsLine(patch, side string, target int) bool {
	return containsRange(patch, side, target, target)
}

func hunkRange(startText, countText string) (int, int, bool) {
	start, err := strconv.Atoi(startText)
	if err != nil {
		return 0, 0, false
	}
	count := 1
	if countText != "" {
		count, err = strconv.Atoi(countText)
	}
	if err != nil || (start == 0 && count != 0) || count > int(^uint(0)>>1)-start {
		return 0, 0, false
	}
	return start, count, true
}

func containsRange(patch, side string, start, end int) bool {
	if start < 1 || start > end || (side != "LEFT" && side != "RIGHT") {
		return false
	}
	left, right, leftRemaining, rightRemaining := 0, 0, 0, 0
	next := start
	inHunk := false
	for _, line := range strings.Split(patch, "\n") {
		if match := hunkPattern.FindStringSubmatch(line); match != nil {
			var leftOK, rightOK bool
			left, leftRemaining, leftOK = hunkRange(match[1], match[2])
			right, rightRemaining, rightOK = hunkRange(match[3], match[4])
			inHunk = leftOK && rightOK
			// A new hunk cannot complete a range started in an earlier hunk,
			// even when the two visible endpoints have adjacent line numbers.
			next = start
			continue
		}
		if !inHunk {
			continue
		}
		if line == "\\ No newline at end of file" {
			continue
		}
		if line == "" {
			inHunk = false
			continue
		}
		selectedLine := 0
		switch line[0] {
		case '-':
			if leftRemaining < 1 {
				inHunk = false
				continue
			}
			if side == "LEFT" {
				selectedLine = left
			}
			left++
			leftRemaining--
		case '+':
			if rightRemaining < 1 {
				inHunk = false
				continue
			}
			if side == "RIGHT" {
				selectedLine = right
			}
			right++
			rightRemaining--
		case ' ':
			if leftRemaining < 1 || rightRemaining < 1 {
				inHunk = false
				continue
			}
			selectedLine = left
			if side == "RIGHT" {
				selectedLine = right
			}
			left++
			right++
			leftRemaining--
			rightRemaining--
		default:
			inHunk = false
		}
		if selectedLine == next {
			if next == end {
				return true
			}
			next++
		}
	}
	return false
}

func (c *Client) Comments(ctx context.Context, owner, repo string, number int) ([]ThreadComment, error) {
	if err := validatePull(owner, repo, number); err != nil {
		return nil, err
	}
	comments := make([]ThreadComment, 0)
	bodyBytes := 0
	for page := 1; page <= maxItems/pageSize; page++ {
		var entries []struct {
			ID                int64  `json:"id"`
			Body              string `json:"body"`
			Path              string `json:"path"`
			Line              int    `json:"line"`
			Side              string `json:"side"`
			StartLine         int    `json:"start_line"`
			StartSide         string `json:"start_side"`
			URL               string `json:"html_url"`
			InReplyToID       int64  `json:"in_reply_to_id"`
			OriginalLine      int    `json:"original_line"`
			OriginalStartLine int    `json:"original_start_line"`
			CommitID          string `json:"commit_id"`
			OriginalCommitID  string `json:"original_commit_id"`
			CreatedAt         string `json:"created_at"`
			SubjectType       string `json:"subject_type"`
			User              struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		headers, err := c.json(ctx, http.MethodGet, pullPath(owner, repo, number)+fmt.Sprintf("/comments?per_page=%d&page=%d", pageSize, page), nil, &entries)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			bodyBytes += len(entry.Body)
			if bodyBytes > maxResponse {
				return nil, errors.New("review discussion exceeds the 16 MiB limit; read it on GitHub")
			}
			comments = append(comments, ThreadComment{
				ID: entry.ID, Body: entry.Body, Path: entry.Path, Line: entry.Line, Side: entry.Side,
				StartLine: entry.StartLine, StartSide: entry.StartSide, Author: entry.User.Login, URL: entry.URL,
				InReplyToID: entry.InReplyToID, OriginalLine: entry.OriginalLine, OriginalStartLine: entry.OriginalStartLine,
				CommitID: entry.CommitID, OriginalCommitID: entry.OriginalCommitID, CreatedAt: entry.CreatedAt,
				SubjectType: entry.SubjectType, Outdated: entry.Line == 0 && entry.SubjectType != "file",
			})
		}
		if len(comments) > maxItems || (hasNext(headers) && page == maxItems/pageSize) {
			return nil, errors.New("review comments exceed the supported limit; read the remaining discussion on GitHub")
		}
		if !hasNext(headers) {
			return comments, nil
		}
	}
	return comments, nil
}

func (c *Client) Reply(ctx context.Context, owner, repo string, number int, commentID int64, body string) (string, error) {
	if err := validatePull(owner, repo, number); err != nil {
		return "", err
	}
	if commentID < 1 || !validBody(body) || strings.TrimSpace(body) == "" {
		return "", errors.New("a reply requires a positive comment ID and nonempty body of at most 65536 bytes")
	}
	// GitHub accepts replies only against the top-level review comment. The UI
	// also displays replies, so resolve their parent before submitting.
	var comment struct {
		ID             int64  `json:"id"`
		InReplyTo      int64  `json:"in_reply_to_id"`
		PullRequestURL string `json:"pull_request_url"`
	}
	_, err := c.json(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/pulls/comments/"+strconv.FormatInt(commentID, 10), nil, &comment)
	if err != nil {
		return "", err
	}
	if comment.ID != commentID || comment.InReplyTo < 0 || comment.PullRequestURL != "https://api.github.com"+pullPath(owner, repo, number) {
		return "", errors.New("the review comment does not belong to this pull request")
	}
	if comment.InReplyTo > 0 {
		commentID = comment.InReplyTo
	}
	var result struct {
		URL string `json:"html_url"`
	}
	_, err = c.json(ctx, http.MethodPost, pullPath(owner, repo, number)+"/comments/"+strconv.FormatInt(commentID, 10)+"/replies", map[string]string{"body": body}, &result)
	if err != nil {
		return "", err
	}
	if result.URL == "" {
		return "https://github.com/" + owner + "/" + repo + "/pull/" + strconv.Itoa(number), nil
	}
	return result.URL, nil
}
