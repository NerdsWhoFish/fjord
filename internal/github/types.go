// Package github implements the GitHub operations used by a Fjord review.
package github

import (
	"bytes"
	"encoding/json"
)

type PR struct {
	Owner    string `json:"owner"`
	Repo     string `json:"repo"`
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	Author   string `json:"author"`
	Reviewer string `json:"reviewer,omitempty"`
	State    string `json:"state"`
	// BaseSHA is the merge base used by the diff, not the target branch tip.
	BaseSHA string `json:"baseSHA"`
	HeadSHA string `json:"headSHA"`
	BaseRef string `json:"baseRef"`
	HeadRef string `json:"headRef"`
	Files   []File `json:"files"`
}

type File struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath,omitempty"`
	Status       string `json:"status"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Patch        string `json:"patch"`
	Reviewable   *bool  `json:"reviewable,omitempty"`
}

type Review struct {
	HeadSHA  string    `json:"headSHA"`
	BaseSHA  string    `json:"baseSHA"`
	Event    string    `json:"event"`
	Body     string    `json:"body"`
	Comments []Comment `json:"comments"`
}

type Comment struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	Body      string `json:"body"`
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`

	startLineSet bool
	startSideSet bool
}

func (c *Comment) UnmarshalJSON(data []byte) error {
	type plainComment Comment
	var decoded plainComment
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*c = Comment(decoded)
	// Explicit zero/null range fields must not silently become a single-line
	// comment. Preserve their presence until review validation.
	_, c.startLineSet = fields["start_line"]
	_, c.startSideSet = fields["start_side"]
	return nil
}

type ThreadComment struct {
	ID                int64  `json:"id"`
	Body              string `json:"body"`
	Path              string `json:"path"`
	Line              int    `json:"line"`
	Side              string `json:"side"`
	StartLine         int    `json:"startLine,omitempty"`
	StartSide         string `json:"startSide,omitempty"`
	Author            string `json:"author"`
	URL               string `json:"url"`
	InReplyToID       int64  `json:"inReplyToId,omitempty"`
	OriginalLine      int    `json:"originalLine,omitempty"`
	OriginalStartLine int    `json:"originalStartLine,omitempty"`
	CommitID          string `json:"commitId,omitempty"`
	OriginalCommitID  string `json:"originalCommitId,omitempty"`
	CreatedAt         string `json:"createdAt,omitempty"`
	SubjectType       string `json:"subjectType,omitempty"`
	Outdated          bool   `json:"outdated"`
}

type ConversationEntry struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
	State     string `json:"state,omitempty"`
}

// APIError deliberately excludes GitHub response bodies, which can contain
// private repository data. Status is available for HTTP/UI error mapping.
type APIError struct {
	Status int
}

// UncertainWriteError means the request may have reached GitHub. Callers must
// not retry automatically, because GitHub review writes have no idempotency key.
type UncertainWriteError struct{}

func (*UncertainWriteError) Error() string {
	return "GitHub submission outcome is unknown; check the pull request on GitHub before retrying"
}

func (e *APIError) Error() string {
	switch e.Status {
	case 401:
		return "GitHub authentication failed; run gh auth login"
	case 403:
		return "GitHub denied the request; check repository permissions and rate limits"
	case 404:
		return "GitHub resource was not found or is not accessible"
	case 409:
		return "GitHub reported a conflict; reload the pull request"
	case 422:
		return "GitHub rejected the review; check comment locations and review permissions"
	case 429:
		return "GitHub rate limit reached; try again later"
	default:
		return "GitHub request failed; try again later"
	}
}
