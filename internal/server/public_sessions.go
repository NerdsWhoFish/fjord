package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/NerdsWhoFish/fjord/internal/github"
)

var errSessionAuthorization = errors.New("GitHub no longer permits this review session; check your token and reload the PR")

func (s *Server) authorizedSession(ctx context.Context, id string) (reviewSession, error) {
	session, err := s.sessions.get(id)
	if err != nil {
		return reviewSession{}, err
	}
	if err := s.authorizeSession(ctx, session); err != nil {
		return reviewSession{}, err
	}
	return session, nil
}

func (s *Server) authorizeSession(ctx context.Context, session reviewSession) error {
	if !s.config.PublicMode {
		return nil
	}
	client, ok := s.config.GitHub.(publicGitHub)
	if !ok {
		return errSessionAuthorization
	}
	pr := session.PR
	err := client.AuthorizeReview(ctx, pr.Owner, pr.Repo, pr.Number)
	if err == nil {
		err = client.AuthorizeRevision(ctx, pr.Owner, pr.Repo, pr.HeadSHA)
	}
	if err == nil && pr.BaseSHA != pr.HeadSHA {
		err = client.AuthorizeRevision(ctx, pr.Owner, pr.Repo, pr.BaseSHA)
	}
	if err != nil {
		var apiError *github.APIError
		if errors.As(err, &apiError) && (apiError.Status == http.StatusUnauthorized || apiError.Status == http.StatusForbidden || apiError.Status == http.StatusNotFound) {
			s.sessions.close(session.ID)
			return errSessionAuthorization
		}
		// Preserve drafts during an upstream outage, but return no cached content.
		return errors.New("could not verify review access with GitHub; try again shortly")
	}
	return nil
}

func (s *Server) authorizedSessionList(ctx context.Context) ([]map[string]any, error) {
	list := s.sessions.list()
	if !s.config.PublicMode {
		return list, nil
	}
	type scope struct {
		owner, repo, head, base string
		number                  int
	}
	checked := make(map[scope]error)
	result := make([]map[string]any, 0, len(list))
	for _, item := range list {
		id, _ := item["id"].(string)
		session, err := s.sessions.get(id)
		if err != nil {
			continue
		}
		pr := session.PR
		key := scope{pr.Owner, pr.Repo, pr.HeadSHA, pr.BaseSHA, pr.Number}
		err, seen := checked[key]
		if !seen {
			err = s.authorizeSession(ctx, session)
			checked[key] = err
		}
		if errors.Is(err, errSessionAuthorization) {
			s.sessions.close(id)
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}
