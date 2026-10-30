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
var hunkPattern = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

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
	if !validSHA(review.HeadSHA) || !validSHA(review.BaseSHA) {
		return errors.New("review must identify the full PR head and merge-base commit SHAs")
	}
	if review.Event != "COMMENT" && review.Event != "APPROVE" && review.Event != "REQUEST_CHANGES" {
		return errors.New("review event must be COMMENT, APPROVE, or REQUEST_CHANGES")
	}
	if !validBody(review.Body) || len(review.Comments) > 100 {
		return errors.New("review is too large; use at most 100 comments and 65536 bytes per body")
	}
	if review.Event == "REQUEST_CHANGES" && strings.TrimSpace(review.Body) == "" {
		return errors.New("requesting changes requires a review summary")
	}
	if review.Event == "COMMENT" && strings.TrimSpace(review.Body) == "" && len(review.Comments) == 0 {
		return errors.New("a comment review requires a summary or inline comment")
	}
	for _, comment := range review.Comments {
		if !validPath(comment.Path) || comment.Line < 1 || (comment.Side != "LEFT" && comment.Side != "RIGHT") || !validBody(comment.Body) || strings.TrimSpace(comment.Body) == "" {
			return errors.New("inline comments require a file, positive line, LEFT or RIGHT side, and nonempty body")
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
	for _, comment := range review.Comments {
		found := false
		for _, file := range pr.Files {
			if file.Path == comment.Path && containsLine(file.Patch, comment.Side, comment.Line) {
				found = true
				break
			}
		}
		if !found {
			return "", errors.New("an inline comment is outside the current diff; reload and choose a visible diff line")
		}
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
	left, right := 0, 0
	inHunk := false
	for _, line := range strings.Split(patch, "\n") {
		if match := hunkPattern.FindStringSubmatch(line); match != nil {
			left, _ = strconv.Atoi(match[1])
			right, _ = strconv.Atoi(match[2])
			inHunk = true
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		switch line[0] {
		case '-':
			if side == "LEFT" && left == target {
				return true
			}
			left++
		case '+':
			if side == "RIGHT" && right == target {
				return true
			}
			right++
		case ' ':
			if (side == "LEFT" && left == target) || (side == "RIGHT" && right == target) {
				return true
			}
			left++
			right++
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
			ID   int64  `json:"id"`
			Body string `json:"body"`
			Path string `json:"path"`
			Line int    `json:"line"`
			Side string `json:"side"`
			URL  string `json:"html_url"`
			User struct {
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
			comments = append(comments, ThreadComment{ID: entry.ID, Body: entry.Body, Path: entry.Path, Line: entry.Line, Side: entry.Side, Author: entry.User.Login, URL: entry.URL})
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
