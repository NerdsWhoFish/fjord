package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NerdsWhoFish/fjord/internal/github"
	"github.com/NerdsWhoFish/fjord/internal/lspconfig"
	"github.com/NerdsWhoFish/fjord/internal/navigation"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
)

const (
	maxPublicTenants = 4
	publicIdleTTL    = 10 * time.Minute
)

type ManagedNavigation interface {
	Navigation
	Close() error
}

type publicGitHub interface {
	GitHub
	Authenticate(context.Context) (string, error)
	AuthorizeRevision(context.Context, string, string, string) error
	AuthorizeReview(context.Context, string, string, int) error
}

type credentialContextKey struct{}

func browserCredential(ctx context.Context) (string, error) {
	token, _ := ctx.Value(credentialContextKey{}).(string)
	if token == "" {
		return "", errors.New("a browser GitHub token is required")
	}
	return token, nil
}

type publicTenant struct {
	handler http.Handler
	nav     ManagedNavigation
	active  int
	closed  bool
	timer   *time.Timer
	idleAt  time.Time
}

type publicGateway struct {
	config  Config
	github  publicGitHub
	base    http.Handler
	handler http.Handler
	slots   chan struct{}
	mu      sync.Mutex
	tenants map[[32]byte]*publicTenant
	total   int
	closed  bool
}

func newPublicHandler(c Config) *publicGateway {
	return newPublicGateway(c, github.New(browserCredential))
}

func newPublicGateway(c Config, client publicGitHub) *publicGateway {
	if c.Origin == "" {
		c.Origin = "http://" + c.Host
	}
	if c.PublicNavigation == nil {
		c.PublicNavigation = func() ManagedNavigation {
			return navigation.NewWithLimit(lspconfig.Defaults(), 2)
		}
	}
	// Shared-server credentials and navigation are never inherited by a tenant.
	c.GitHub, c.Navigation = client, nil
	g := &publicGateway{
		config: c, github: client, base: newHandler(c),
		slots: make(chan struct{}, 4), tenants: make(map[[32]byte]*publicTenant),
	}
	g.handler = secureHandler(c, http.HandlerFunc(g.serve))
	return g
}

func (g *publicGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.handler.ServeHTTP(w, r)
}

func (g *publicGateway) serve(w http.ResponseWriter, r *http.Request) {
	isMCP := r.URL.Path == "/mcp"
	if strings.HasPrefix(r.URL.Path, "/mcp/") {
		http.NotFound(w, r)
		return
	}
	if (!isMCP && !strings.HasPrefix(r.URL.Path, "/api/")) || r.URL.Path == "/api/telemetry" {
		g.base.ServeHTTP(w, r)
		return
	}
	if r.Method != http.MethodPost && !(isMCP && (r.Method == http.MethodGet || r.Method == http.MethodDelete)) {
		jsonError(w, "POST is required", http.StatusMethodNotAllowed)
		return
	}
	if (r.Header.Get("Origin") != g.config.Origin && !(isMCP && r.Header.Get("Origin") == "")) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		jsonError(w, "request origin is not allowed", http.StatusForbidden)
		return
	}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" && r.Method == http.MethodPost {
		jsonError(w, "application/json is required", http.StatusUnsupportedMediaType)
		return
	}
	token := bearerToken(r)
	if token == "" {
		jsonError(w, "connect a GitHub token to continue", http.StatusUnauthorized)
		return
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		jsonError(w, "Fjord is busy; try again shortly", http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))
	ctx = context.WithValue(ctx, credentialContextKey{}, token)
	r = r.WithContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	key := sha256.Sum256([]byte(token))
	if r.URL.Path == "/api/auth/logout" {
		var input struct{}
		if err := decode(r, &input); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, span := otel.Tracer("fjord/server").Start(ctx, "public.logout")
		defer span.End()
		g.remove(key)
		slog.InfoContext(ctx, "public logout completed")
		jsonReply(w, map[string]any{})
		return
	}
	authCtx, span := otel.Tracer("fjord/server").Start(ctx, "public.authenticate")
	login, err := g.github.Authenticate(authCtx)
	if err != nil {
		span.SetStatus(codes.Error, "authentication_failed")
		slog.WarnContext(authCtx, "public authentication failed")
		span.End()
		var apiError *github.APIError
		if errors.As(err, &apiError) && apiError.Status == http.StatusUnauthorized {
			g.remove(key)
			jsonError(w, "GitHub rejected this token; connect a valid token", http.StatusUnauthorized)
		} else {
			jsonError(w, "could not validate the token with GitHub; check its permissions and try again", http.StatusBadGateway)
		}
		return
	}
	slog.InfoContext(authCtx, "public authentication completed")
	span.End()
	if r.URL.Path == "/api/auth" {
		var input struct{}
		if err := decode(r, &input); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		jsonReply(w, map[string]any{"authenticated": true, "login": login})
		return
	}
	create := false
	switch r.URL.Path {
	case "/api/config", "/api/pr", "/api/content", "/api/diff", "/api/comments", "/api/review", "/api/reply":
		g.base.ServeHTTP(w, r)
		return
	case "/api/workspace", "/api/session/open":
		create = true
	case "/mcp", "/api/workspace/file", "/api/navigate", "/api/navigation/capabilities",
		"/api/session/list", "/api/session/sync", "/api/session/close", "/api/session/remove":
	default:
		http.NotFound(w, r)
		return
	}
	tenant := g.acquire(key, create)
	if tenant == nil {
		if create {
			slog.WarnContext(ctx, "public tenant capacity reached")
			jsonError(w, "public Fjord is at capacity; try again after an active review closes", http.StatusTooManyRequests)
		} else if isMCP || strings.HasPrefix(r.URL.Path, "/api/session/") {
			// This handler never stores sessions. A native client may initialize
			// before its browser opens a review without consuming sandbox capacity.
			g.base.ServeHTTP(w, r)
		} else {
			jsonError(w, navigation.ErrNotFound.Error(), http.StatusNotFound)
		}
		return
	}
	defer g.release(tenant)
	tenant.handler.ServeHTTP(w, r)
}

func bearerToken(r *http.Request) string {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return ""
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	if len(token) < 20 || len(token) > 256 {
		return ""
	}
	for _, c := range token {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return ""
		}
	}
	return token
}

func (g *publicGateway) acquire(key [32]byte, create bool) *publicTenant {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	tenant := g.tenants[key]
	if tenant == nil {
		if !create || g.total >= maxPublicTenants {
			return nil
		}
		nav := g.config.PublicNavigation()
		c := g.config
		c.GitHub = g.github
		c.Navigation = &authorizedNavigation{inner: nav, github: g.github, workspaces: make(map[string]publicWorkspace)}
		tenant = &publicTenant{handler: newHandler(c), nav: nav, idleAt: time.Now()}
		g.tenants[key] = tenant
		g.total++
		tenant.timer = time.AfterFunc(publicIdleTTL, func() { g.expire(key, tenant) })
	}
	tenant.active++
	tenant.timer.Stop()
	return tenant
}

func (g *publicGateway) expire(key [32]byte, tenant *publicTenant) {
	g.mu.Lock()
	if g.tenants[key] != tenant || tenant.active != 0 {
		g.mu.Unlock()
		return
	}
	// Stop can race with a timer callback already waiting on this lock. A new
	// lease must receive its full idle lifetime after that callback wakes.
	if remaining := publicIdleTTL - time.Since(tenant.idleAt); remaining > 0 {
		tenant.timer.Reset(remaining)
		g.mu.Unlock()
		return
	}
	delete(g.tenants, key)
	tenant.closed = true
	g.mu.Unlock()
	g.closeTenant(tenant)
}

func (g *publicGateway) release(tenant *publicTenant) {
	g.mu.Lock()
	tenant.active--
	closeNow := tenant.closed && tenant.active == 0
	if !tenant.closed && tenant.active == 0 {
		tenant.idleAt = time.Now()
		tenant.timer.Reset(publicIdleTTL)
	}
	g.mu.Unlock()
	if closeNow {
		g.closeTenant(tenant)
	}
}

func (g *publicGateway) remove(key [32]byte) {
	g.mu.Lock()
	tenant := g.tenants[key]
	closeNow := false
	if tenant != nil {
		delete(g.tenants, key)
		tenant.closed = true
		tenant.timer.Stop()
		closeNow = tenant.active == 0
	}
	g.mu.Unlock()
	if closeNow {
		g.closeTenant(tenant)
	}
}

func (g *publicGateway) closeTenant(tenant *publicTenant) {
	ctx, span := otel.Tracer("fjord/server").Start(context.Background(), "public.tenant.cleanup")
	defer span.End()
	if err := tenant.nav.Close(); err != nil {
		span.SetStatus(codes.Error, "cleanup_failed")
		slog.ErrorContext(ctx, "public tenant cleanup failed")
	} else {
		slog.InfoContext(ctx, "public tenant cleanup completed")
	}
	g.mu.Lock()
	g.total--
	g.mu.Unlock()
}

func (g *publicGateway) Close() error {
	g.mu.Lock()
	g.closed = true
	var idle []*publicTenant
	for key, tenant := range g.tenants {
		delete(g.tenants, key)
		tenant.closed = true
		tenant.timer.Stop()
		if tenant.active == 0 {
			idle = append(idle, tenant)
		}
	}
	g.mu.Unlock()
	for _, tenant := range idle {
		g.closeTenant(tenant)
	}
	return nil
}

type publicWorkspace struct {
	target
	innerID string
	created time.Time
}

type authorizedNavigation struct {
	inner      Navigation
	github     publicGitHub
	mu         sync.Mutex
	workspaces map[string]publicWorkspace
}

func (n *authorizedNavigation) Prepare(ctx context.Context, owner, repo, sha string, archive io.Reader) (string, error) {
	id, err := n.inner.Prepare(ctx, owner, repo, sha, archive)
	if err != nil {
		return "", err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for key, workspace := range n.workspaces {
		if time.Since(workspace.created) >= 20*time.Minute {
			delete(n.workspaces, key)
		} else if workspace.innerID == id {
			return key, nil
		}
	}
	// Navigation itself retains at most two trees. Bound stale handles too.
	if len(n.workspaces) >= 16 {
		var oldest string
		for key, value := range n.workspaces {
			if oldest == "" || value.created.Before(n.workspaces[oldest].created) {
				oldest = key
			}
		}
		delete(n.workspaces, oldest)
	}
	key := randomID()
	n.workspaces[key] = publicWorkspace{target: target{Owner: owner, Repo: repo, SHA: sha}, innerID: id, created: time.Now()}
	return key, nil
}

func (n *authorizedNavigation) authorize(ctx context.Context, id string) (string, error) {
	n.mu.Lock()
	workspace, ok := n.workspaces[id]
	n.mu.Unlock()
	if !ok {
		return "", navigation.ErrNotFound
	}
	if err := n.github.AuthorizeRevision(ctx, workspace.Owner, workspace.Repo, workspace.SHA); err != nil {
		return "", errors.New("GitHub no longer permits access to this revision; check your token and reload the PR")
	}
	return workspace.innerID, nil
}

func (n *authorizedNavigation) File(ctx context.Context, id, path string) (string, error) {
	innerID, err := n.authorize(ctx, id)
	if err != nil {
		return "", err
	}
	return n.inner.File(ctx, innerID, path)
}

func (n *authorizedNavigation) Capabilities(ctx context.Context, id, path string) (navigation.Capabilities, error) {
	innerID, err := n.authorize(ctx, id)
	if err != nil {
		return navigation.Capabilities{}, err
	}
	return n.inner.Capabilities(ctx, innerID, path)
}

func (n *authorizedNavigation) Query(ctx context.Context, id, path, method string, line, character int) (navigation.Result, error) {
	return n.Inspect(ctx, id, path, method, line, character, navigation.QueryOptions{})
}

func (n *authorizedNavigation) Inspect(ctx context.Context, id, path, method string, line, character int, options navigation.QueryOptions) (navigation.Result, error) {
	innerID, err := n.authorize(ctx, id)
	if err != nil {
		return navigation.Result{}, err
	}
	return n.inner.Inspect(ctx, innerID, path, method, line, character, options)
}
