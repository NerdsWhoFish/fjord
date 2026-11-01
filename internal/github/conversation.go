package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Conversation includes issue comments and review decisions, which GitHub
// serves separately from comments anchored to diff lines.
func (c *Client) Conversation(ctx context.Context, owner, repo string, number int) ([]ConversationEntry, error) {
	if err := validatePull(owner, repo, number); err != nil {
		return nil, err
	}
	result := make([]ConversationEntry, 0)
	bodyBytes, itemCount := 0, 0
	for _, source := range []struct{ kind, path string }{
		{"comment", "/repos/" + owner + "/" + repo + "/issues/" + strconv.Itoa(number) + "/comments"},
		{"review", pullPath(owner, repo, number) + "/reviews"},
	} {
		seen := make(map[int64]bool)
		for page := 1; page <= maxItems/pageSize; page++ {
			var entries []struct {
				ID          int64     `json:"id"`
				Body        string    `json:"body"`
				URL         string    `json:"html_url"`
				CreatedAt   time.Time `json:"created_at"`
				SubmittedAt time.Time `json:"submitted_at"`
				State       string    `json:"state"`
				User        struct {
					Login string `json:"login"`
				} `json:"user"`
			}
			headers, err := c.json(ctx, http.MethodGet, source.path+fmt.Sprintf("?per_page=%d&page=%d", pageSize, page), nil, &entries)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				itemCount++
				bodyBytes += len(entry.Body)
				if itemCount > maxItems || bodyBytes > maxResponse {
					return nil, errors.New("PR conversation exceeds the supported limit; read the remaining discussion on GitHub")
				}
				if seen[entry.ID] {
					continue
				}
				seen[entry.ID] = true
				at := entry.CreatedAt
				if source.kind == "review" {
					if entry.State == "PENDING" || entry.SubmittedAt.IsZero() {
						continue
					}
					at = entry.SubmittedAt
				}
				result = append(result, ConversationEntry{
					ID: entry.ID, Kind: source.kind, Body: entry.Body, Author: entry.User.Login,
					URL: entry.URL, CreatedAt: at.UTC().Format(time.RFC3339Nano), State: entry.State,
				})
			}
			if !hasNext(headers) {
				break
			}
			if page == maxItems/pageSize {
				return nil, errors.New("PR conversation exceeds the supported limit; read the remaining discussion on GitHub")
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, result[i].CreatedAt)
		right, _ := time.Parse(time.RFC3339Nano, result[j].CreatedAt)
		if !left.Equal(right) {
			return left.Before(right)
		}
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}
