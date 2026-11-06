package github

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Authenticate requires a user credential. Installation tokens deliberately do
// not qualify as browser identities for a public Fjord instance.
func (c *Client) Authenticate(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	if _, err := c.json(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return "", err
	}
	if strings.TrimSpace(user.Login) == "" {
		return "", errors.New("GitHub did not return a user identity")
	}
	return user.Login, nil
}

// AuthorizeRevision checks current Contents permission, including fine-grained
// token scope, before a cached source tree can be read.
func (c *Client) AuthorizeRevision(ctx context.Context, owner, repo, sha string) error {
	if err := validatePull(owner, repo, 1); err != nil {
		return err
	}
	if !validSHA(sha) {
		return errors.New("a full commit SHA is required")
	}
	_, _, err := c.request(ctx, http.MethodGet, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/git/commits/"+sha, nil, maxResponse, false)
	return err
}

func (c *Client) AuthorizeReview(ctx context.Context, owner, repo string, number int) error {
	if err := validatePull(owner, repo, number); err != nil {
		return err
	}
	_, err := c.pull(ctx, owner, repo, number)
	return err
}
