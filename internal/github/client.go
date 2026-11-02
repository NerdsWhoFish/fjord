package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

const maxResponse = 16 << 20
const maxFile = 2 << 20
const pageSize = 100
const maxItems = 3000

var ErrResponseTooLarge = errors.New("GitHub response exceeds the supported size limit")
var ErrBinarySource = errors.New("binary files cannot be displayed as source")

type Client struct {
	token   func(context.Context) (string, error)
	http    *http.Client
	baseURL string
}

func New(token func(context.Context) (string, error)) *Client {
	if token == nil {
		token = GHAuthToken
	}
	return &Client{
		token: token,
		http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("GitHub redirects are not allowed")
		}},
		baseURL: "https://api.github.com",
	}
}

// GHAuthToken reads the current CLI credential without putting it in argv,
// error messages, or browser responses.
func GHAuthToken(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com")
	cmd.Stderr = io.Discard
	value, err := cmd.Output()
	if err != nil {
		return "", errors.New("GitHub CLI authentication unavailable; run gh auth login")
	}
	token := strings.TrimSpace(string(value))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("GitHub CLI returned an invalid credential")
	}
	return token, nil
}

func (c *Client) request(ctx context.Context, method, endpoint string, input any, limit int64, raw bool) ([]byte, http.Header, error) {
	ctx, span := otel.Tracer("fjord/github").Start(ctx, "github.request")
	defer span.End()
	fail := func(err error) ([]byte, http.Header, error) {
		span.SetStatus(codes.Error, "github_request_failed")
		return nil, nil, err
	}
	token, err := c.token(ctx)
	if err != nil {
		return fail(errors.New("GitHub authentication unavailable; run gh auth login"))
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return fail(errors.New("GitHub authentication unavailable; run gh auth login"))
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fail(errors.New("could not encode GitHub request"))
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, body)
	if err != nil {
		return fail(errors.New("could not construct GitHub request"))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if raw {
		req.Header.Set("Accept", "application/vnd.github.raw+json")
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "fjord")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if method == http.MethodPost {
			return fail(&UncertainWriteError{})
		}
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		return fail(errors.New("could not reach GitHub"))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if method == http.MethodPost && resp.StatusCode >= 500 {
			return fail(&UncertainWriteError{})
		}
		return fail(&APIError{Status: resp.StatusCode})
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		if method == http.MethodPost {
			return fail(&UncertainWriteError{})
		}
		return fail(errors.New("could not read GitHub response"))
	}
	if int64(len(data)) > limit {
		if method == http.MethodPost {
			return fail(&UncertainWriteError{})
		}
		return fail(ErrResponseTooLarge)
	}
	return data, resp.Header, nil
}

func (c *Client) json(ctx context.Context, method, endpoint string, input, output any) (http.Header, error) {
	data, headers, err := c.request(ctx, method, endpoint, input, maxResponse, false)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, output); err != nil {
		if method == http.MethodPost {
			return nil, &UncertainWriteError{}
		}
		return nil, errors.New("GitHub returned an invalid response")
	}
	return headers, nil
}

func pullPath(owner, repo string, number int) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/pulls/" + strconv.Itoa(number)
}

type pullResponse struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	URL          string `json:"html_url"`
	State        string `json:"state"`
	ChangedFiles int    `json:"changed_files"`
	User         struct {
		Login string `json:"login"`
	} `json:"user"`
	Base struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
}

func (c *Client) pull(ctx context.Context, owner, repo string, number int) (pullResponse, error) {
	var result pullResponse
	_, err := c.json(ctx, http.MethodGet, pullPath(owner, repo, number), nil, &result)
	if err != nil {
		return result, err
	}
	if !validSHA(result.Head.SHA) || !validSHA(result.Base.SHA) || result.Number != number {
		return result, errors.New("GitHub returned invalid pull request revisions")
	}
	return result, nil
}

func (c *Client) Load(ctx context.Context, owner, repo string, number int) (PR, error) {
	if err := validatePull(owner, repo, number); err != nil {
		return PR{}, err
	}
	pull, err := c.pull(ctx, owner, repo, number)
	if err != nil {
		return PR{}, err
	}
	if pull.ChangedFiles > maxItems {
		return PR{}, errors.New("pull request exceeds GitHub's 3000-file API limit; review it on GitHub")
	}
	files := make([]File, 0)
	patchBytes := 0
	for page := 1; page <= maxItems/pageSize; page++ {
		var entries []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
			Status           string `json:"status"`
			Additions        int    `json:"additions"`
			Deletions        int    `json:"deletions"`
			Patch            string `json:"patch"`
		}
		headers, err := c.json(ctx, http.MethodGet, pullPath(owner, repo, number)+fmt.Sprintf("/files?per_page=%d&page=%d", pageSize, page), nil, &entries)
		if err != nil {
			return PR{}, err
		}
		for _, entry := range entries {
			if !validPath(entry.Filename) || (entry.PreviousFilename != "" && !validPath(entry.PreviousFilename)) {
				return PR{}, errors.New("pull request contains an unsupported file path")
			}
			patchBytes += len(entry.Patch)
			if patchBytes > maxResponse {
				return PR{}, errors.New("pull request diff exceeds the 16 MiB review limit; review it on GitHub")
			}
			file := File{Path: entry.Filename, PreviousPath: entry.PreviousFilename, Status: entry.Status, Additions: entry.Additions, Deletions: entry.Deletions, Patch: entry.Patch}
			if entry.Patch == "" {
				reviewable := false
				file.Reviewable = &reviewable
			}
			files = append(files, file)
		}
		if len(files) > maxItems {
			return PR{}, errors.New("pull request file list exceeds the supported limit")
		}
		if !hasNext(headers) {
			break
		}
		if page == maxItems/pageSize {
			return PR{}, errors.New("GitHub returned an incomplete pull request file list")
		}
	}
	if len(files) != pull.ChangedFiles {
		return PR{}, errors.New("pull request changed while loading or GitHub returned an incomplete file list; reload it")
	}
	var comparison struct {
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	_, err = c.json(ctx, http.MethodGet, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo)+"/compare/"+pull.Base.SHA+"..."+pull.Head.SHA+"?per_page=1", nil, &comparison)
	if err != nil {
		return PR{}, err
	}
	if !validSHA(comparison.MergeBase.SHA) {
		return PR{}, errors.New("GitHub did not return the merge base required for diff navigation")
	}
	// The files endpoint follows the live PR. Verify it still describes the
	// immutable commits that the browser and language server will navigate.
	current, err := c.pull(ctx, owner, repo, number)
	if err != nil {
		return PR{}, err
	}
	if current.Head.SHA != pull.Head.SHA || current.Base.SHA != pull.Base.SHA {
		return PR{}, errors.New("pull request changed while loading; reload it")
	}
	return PR{Owner: owner, Repo: repo, Number: number, Title: pull.Title, Body: pull.Body, URL: pull.URL, Author: pull.User.Login, State: pull.State, BaseSHA: comparison.MergeBase.SHA, HeadSHA: pull.Head.SHA, BaseRef: pull.Base.Ref, HeadRef: pull.Head.Ref, Files: files}, nil
}

func hasNext(headers http.Header) bool {
	return strings.Contains(headers.Get("Link"), `rel="next"`)
}

func (c *Client) Content(ctx context.Context, owner, repo, sha, path string) (string, error) {
	if err := validatePull(owner, repo, 1); err != nil {
		return "", err
	}
	if !validSHA(sha) || !validPath(path) {
		return "", errors.New("a full commit SHA and repository-relative file path are required")
	}
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	endpoint := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/contents/" + strings.Join(parts, "/") + "?ref=" + sha
	data, _, err := c.request(ctx, http.MethodGet, endpoint, nil, maxFile, true)
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", ErrBinarySource
	}
	return string(data), nil
}
