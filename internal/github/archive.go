package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const maxArchive = 64 << 20

// Archive returns an immutable GitHub source archive. The caller must close it.
// GitHub's codeload redirect carries a temporary signed URL; the API credential
// must never follow that cross-host redirect.
func (c *Client) Archive(ctx context.Context, owner, repo, sha string) (io.ReadCloser, error) {
	if err := validatePull(owner, repo, 1); err != nil {
		return nil, err
	}
	if !validSHA(sha) {
		return nil, errors.New("an archive requires a full commit SHA")
	}
	ctx, span := otel.Tracer("fjord/github").Start(ctx, "github.archive")
	fail := func(err error) (io.ReadCloser, error) {
		span.SetStatus(codes.Error, "github_archive_failed")
		span.End()
		return nil, err
	}
	token, err := c.token(ctx)
	if err != nil || token == "" || strings.ContainsAny(token, "\r\n") {
		return fail(errors.New("GitHub authentication unavailable; run gh auth login"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/tarball/"+sha, nil)
	if err != nil {
		return fail(errors.New("could not construct GitHub archive request"))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "fjord")
	client := *c.http
	client.Timeout = 60 * time.Second
	client.CheckRedirect = archiveRedirect
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		return fail(errors.New("could not download GitHub archive"))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fail(&APIError{Status: resp.StatusCode})
	}
	if resp.ContentLength > maxArchive {
		resp.Body.Close()
		return fail(errors.New("repository archive exceeds the 64 MiB compressed size limit"))
	}
	return &archiveReader{body: resp.Body, remaining: maxArchive, span: span}, nil
}

func archiveRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" {
		return errors.New("untrusted GitHub archive redirect")
	}
	if req.URL.Host != "api.github.com" && req.URL.Host != "codeload.github.com" {
		return errors.New("untrusted GitHub archive redirect")
	}
	if len(via) > 0 && req.URL.Host != via[0].URL.Host {
		req.Header.Del("Authorization")
	}
	return nil
}

type archiveReader struct {
	body      io.ReadCloser
	remaining int64
	span      trace.Span
	closed    bool
}

func (r *archiveReader) Read(p []byte) (int, error) {
	if r.remaining < 0 {
		return 0, errors.New("repository archive exceeds the 64 MiB compressed size limit")
	}
	if int64(len(p)) > r.remaining+1 {
		p = p[:r.remaining+1]
	}
	n, err := r.body.Read(p)
	r.remaining -= int64(n)
	if r.remaining < 0 {
		r.span.SetStatus(codes.Error, "archive_size_limit")
		return 0, errors.New("repository archive exceeds the 64 MiB compressed size limit")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		r.span.SetStatus(codes.Error, "archive_read_failed")
		return n, errors.New("GitHub archive download was interrupted")
	}
	return n, err
}

func (r *archiveReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	defer r.span.End()
	return r.body.Close()
}
