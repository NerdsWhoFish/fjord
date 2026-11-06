package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NerdsWhoFish/fjord/internal/github"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const publicTokenA = "test_credential_alpha_123"
const publicTokenB = "test_credential_beta_1234"
const publicSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type browserGitHub struct {
	fakeGitHub
	mu           sync.Mutex
	authStatus   int
	reviewStatus int
	repoStatus   int
	authCalls    int
	repoCalls    int
}

func (g *browserGitHub) Authenticate(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.authCalls++
	token, err := browserCredential(ctx)
	if err != nil {
		return "", err
	}
	if g.authStatus != 0 {
		return "", &github.APIError{Status: g.authStatus}
	}
	if token == publicTokenA {
		return "first-reviewer", nil
	}
	return "other-reviewer", nil
}

func (g *browserGitHub) AuthorizeRevision(_ context.Context, _, _, sha string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.repoCalls++
	if sha != publicSHA && sha != sessionPR().BaseSHA && sha != sessionPR().HeadSHA {
		return fmt.Errorf("unexpected test revision")
	}
	if g.repoStatus != 0 {
		return &github.APIError{Status: g.repoStatus}
	}
	return nil
}

func (g *browserGitHub) AuthorizeReview(ctx context.Context, _, _ string, _ int) error {
	if _, err := browserCredential(ctx); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.reviewStatus != 0 {
		return &github.APIError{Status: g.reviewStatus}
	}
	return nil
}

func (g *browserGitHub) Load(ctx context.Context, _, _ string, _ int) (github.PR, error) {
	pr := sessionPR()
	token, err := browserCredential(ctx)
	if err != nil {
		return pr, err
	}
	if token == publicTokenA {
		pr.Title = "first private review"
	} else {
		pr.Title = "second private review"
	}
	return pr, nil
}

func (g *browserGitHub) Archive(ctx context.Context, _, _, _ string) (io.ReadCloser, error) {
	token, err := browserCredential(ctx)
	if err != nil {
		return nil, err
	}
	content := "second private source"
	if token == publicTokenA {
		content = "first private source"
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

type publicTestNavigation struct {
	fakeNavigation
	content string
	closed  atomic.Int32
	reads   atomic.Int32
	entered chan struct{}
	proceed chan struct{}
}

func (n *publicTestNavigation) Prepare(_ context.Context, _, _, _ string, archive io.Reader) (string, error) {
	data, err := io.ReadAll(archive)
	n.content = string(data)
	return "same-inner-id", err
}

func (n *publicTestNavigation) File(context.Context, string, string) (string, error) {
	n.reads.Add(1)
	if n.entered != nil {
		close(n.entered)
		<-n.proceed
	}
	return n.content, nil
}

func (n *publicTestNavigation) Close() error { n.closed.Add(1); return nil }

func publicFixture(t *testing.T) (*publicGateway, *browserGitHub, *[]*publicTestNavigation) {
	t.Helper()
	client := &browserGitHub{}
	var managers []*publicTestNavigation
	gateway := newPublicGateway(Config{
		Host: "fjord.example", Origin: "https://fjord.example", PublicMode: true,
		// Neither of these shared dependencies may be used.
		GitHub: &fakeGitHub{}, Navigation: fakeNavigation{},
		PublicNavigation: func() ManagedNavigation {
			nav := &publicTestNavigation{}
			managers = append(managers, nav)
			return nav
		},
	}, client)
	t.Cleanup(func() { _ = gateway.Close() })
	return gateway, client, &managers
}

func publicRequest(handler http.Handler, path, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "https://fjord.example"+path, strings.NewReader(body))
	request.Header.Set("Origin", "https://fjord.example")
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func preparePublic(t *testing.T, handler http.Handler, token string) string {
	t.Helper()
	response := publicRequest(handler, "/api/workspace", token, `{"owner":"acme","repo":"private","sha":"`+publicSHA+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("prepare failed: %d %s", response.Code, response.Body.String())
	}
	var value struct{ ID string }
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil || value.ID == "" {
		t.Fatal("missing workspace handle")
	}
	return value.ID
}

func TestPublicModeAuthenticationBoundary(t *testing.T) {
	gateway, client, managers := publicFixture(t)
	for _, path := range []string{"/api/config", "/api/pr", "/api/workspace", "/api/workspace/file", "/api/review", "/api/reply", "/api/auth", "/mcp", "/api/session/open", "/api/session/list", "/api/session/sync", "/api/session/remove"} {
		if response := publicRequest(gateway, path, "", "{}"); response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s returned %d", path, response.Code)
		}
	}
	for _, path := range []string{"/mcp/"} {
		if response := publicRequest(gateway, path, publicTokenA, "{}"); response.Code != http.StatusNotFound {
			t.Fatalf("public session endpoint %s returned %d", path, response.Code)
		}
	}
	for _, path := range []string{"/healthz", "/config.js"} {
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://fjord.example"+path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("public bootstrap %s requires authentication", path)
		}
		if path == "/config.js" && !strings.Contains(response.Body.String(), `"publicMode":true`) {
			t.Fatal("bootstrap did not enable public mode")
		}
	}
	response := publicRequest(gateway, "/api/auth", publicTokenA, "{}")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"login":"first-reviewer"`) {
		t.Fatal("credential validation failed")
	}
	if len(*managers) != 0 || client.authCalls != 1 {
		t.Fatal("bootstrap allocated a tenant or skipped authentication")
	}
}

func TestPublicOriginAndCredentialParsing(t *testing.T) {
	gateway, client, _ := publicFixture(t)
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Host = "attacker.example" },
		func(r *http.Request) { r.Header.Set("Origin", "https://attacker.example") },
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+publicTokenB) },
		func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+publicTokenA+" ") },
		func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 257)) },
	} {
		req := httptest.NewRequest(http.MethodPost, "https://fjord.example/api/auth", strings.NewReader("{}"))
		req.Header.Set("Origin", "https://fjord.example")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+publicTokenA)
		mutate(req)
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, req)
		if response.Code < 400 {
			t.Fatal("accepted unsafe request")
		}
	}
	if client.authCalls != 0 {
		t.Fatal("unsafe request reached GitHub")
	}
}

func TestPublicWorkspaceIsolationAndReauthorization(t *testing.T) {
	gateway, client, managers := publicFixture(t)
	first := preparePublic(t, gateway, publicTokenA)
	second := preparePublic(t, gateway, publicTokenB)
	if first == second || len(*managers) != 2 {
		t.Fatal("tenants shared a workspace handle or navigation manager")
	}
	for _, path := range []string{"/api/workspace/file", "/api/navigation/capabilities", "/api/navigate"} {
		response := publicRequest(gateway, path, publicTokenB, `{"id":"`+first+`","path":"main.go"}`)
		if response.Code == http.StatusOK {
			t.Fatalf("cross-tenant workspace accessible through %s", path)
		}
	}
	for _, test := range []struct{ token, id, source string }{
		{publicTokenA, first, "first private source"},
		{publicTokenB, second, "second private source"},
	} {
		response := publicRequest(gateway, "/api/workspace/file", test.token, `{"id":"`+test.id+`","path":"main.go"}`)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.source) {
			t.Fatal("tenant did not receive its own source")
		}
	}
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		client.repoStatus = status
		for _, path := range []string{"/api/workspace/file", "/api/navigation/capabilities", "/api/navigate"} {
			response := publicRequest(gateway, path, publicTokenA, `{"id":"`+first+`","path":"main.go"}`)
			if response.Code == http.StatusOK || strings.Contains(response.Body.String(), "private source") {
				t.Fatalf("revoked Contents permission returned cached data via %s", path)
			}
		}
	}
	if (*managers)[0].reads.Load() != 1 || client.repoCalls != 8 {
		t.Fatal("cached access bypassed repository authorization")
	}
	client.authStatus = http.StatusUnauthorized
	response := publicRequest(gateway, "/api/workspace/file", publicTokenA, `{"id":"`+first+`","path":"main.go"}`)
	if response.Code != http.StatusUnauthorized || (*managers)[0].closed.Load() != 1 {
		t.Fatal("revoked token retained accessible workspace")
	}
}

func TestPublicLogoutWaitsForActiveNavigationAndForgetsHandles(t *testing.T) {
	gateway, _, managers := publicFixture(t)
	id := preparePublic(t, gateway, publicTokenA)
	nav := (*managers)[0]
	nav.entered, nav.proceed = make(chan struct{}), make(chan struct{})
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- publicRequest(gateway, "/api/workspace/file", publicTokenA, `{"id":"`+id+`","path":"main.go"}`)
	}()
	<-nav.entered
	response := publicRequest(gateway, "/api/auth/logout", publicTokenA, "{}")
	if response.Code != http.StatusOK || nav.closed.Load() != 0 {
		t.Fatal("logout interrupted a manager before its active request released it")
	}
	close(nav.proceed)
	<-result
	if nav.closed.Load() != 1 {
		t.Fatal("logout did not clean up the released manager")
	}
	response = publicRequest(gateway, "/api/workspace/file", publicTokenA, `{"id":"`+id+`","path":"main.go"}`)
	if response.Code == http.StatusOK {
		t.Fatal("logout retained an old workspace handle")
	}
}

func TestPublicCapacityAndIdleExpiry(t *testing.T) {
	gateway, _, managers := publicFixture(t)
	for i := 0; i < maxPublicTenants; i++ {
		token := fmt.Sprintf("test_credential_%020d", i)
		preparePublic(t, gateway, token)
	}
	response := publicRequest(gateway, "/api/workspace", publicTokenA, `{"owner":"acme","repo":"private","sha":"`+publicSHA+`"}`)
	if response.Code != http.StatusTooManyRequests || len(*managers) != maxPublicTenants {
		t.Fatal("public tenant capacity was not enforced")
	}
	response = publicRequest(gateway, "/api/config", publicTokenA, "{}")
	if response.Code != http.StatusOK || len(*managers) != maxPublicTenants {
		t.Fatal("stateless API consumed or required a sandbox slot")
	}
	key := sha256.Sum256([]byte(fmt.Sprintf("test_credential_%020d", 0)))
	gateway.expire(key, gateway.tenants[key])
	if (*managers)[0].closed.Load() != 0 {
		t.Fatal("a stale timer callback expired a freshly used tenant")
	}
	gateway.tenants[key].idleAt = time.Now().Add(-publicIdleTTL)
	gateway.expire(key, gateway.tenants[key])
	if (*managers)[0].closed.Load() != 1 {
		t.Fatal("idle tenant did not release its sandbox")
	}
	preparePublic(t, gateway, publicTokenA)
	for i := 0; i < cap(gateway.slots); i++ {
		gateway.slots <- struct{}{}
	}
	response = publicRequest(gateway, "/api/auth", publicTokenA, "{}")
	if response.Code != http.StatusTooManyRequests {
		t.Fatal("global request capacity was not enforced")
	}
	for i := 0; i < cap(gateway.slots); i++ {
		<-gateway.slots
	}
}

func TestPublicAuthenticationLogsOmitIdentityAndCredentials(t *testing.T) {
	gateway, client, _ := publicFixture(t)
	var logs bytes.Buffer
	previous := slog.Default()
	previousTracer := otel.GetTracerProvider()
	spans := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous); otel.SetTracerProvider(previousTracer) })
	publicRequest(gateway, "/api/auth", publicTokenA, "{}")
	client.authStatus = http.StatusUnauthorized
	publicRequest(gateway, "/api/auth", publicTokenA, "{}")
	publicRequest(gateway, "/api/auth/logout", publicTokenA, "{}")
	var spanData strings.Builder
	for _, span := range spans.Ended() {
		fmt.Fprintf(&spanData, "%s %+v %+v %+v %+v", span.Name(), span.Attributes(), span.Events(), span.Resource().Attributes(), span.Status())
	}
	for _, forbidden := range []string{publicTokenA, "first-reviewer", "Authorization", "Bearer"} {
		if strings.Contains(logs.String(), forbidden) || strings.Contains(spanData.String(), forbidden) {
			t.Fatal("public authentication telemetry contained sensitive data")
		}
	}
	if !strings.Contains(logs.String(), "public logout completed") || len(spans.Ended()) < 3 {
		t.Fatal("public authentication/logout lacked correlated operation telemetry")
	}
}

var _ Navigation = (*authorizedNavigation)(nil)

type publicMCPTransport struct {
	handler http.Handler
	token   string
	origin  string
}

func (transport publicMCPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request.Header.Set("Authorization", "Bearer "+transport.token)
	if transport.origin != "" {
		request.Header.Set("Origin", transport.origin)
	}
	response := httptest.NewRecorder()
	transport.handler.ServeHTTP(response, request)
	return response.Result(), nil
}

func publicMCP(t *testing.T, handler http.Handler, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	connected, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: "https://fjord.example/mcp", HTTPClient: &http.Client{Transport: publicMCPTransport{handler: handler, token: token}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connected.Close() })
	return connected
}

func publicBrowserSession(t *testing.T, handler http.Handler, token string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"url": sessionPR().URL, "headSHA": sessionPR().HeadSHA, "baseSHA": sessionPR().BaseSHA, "draft": emptySessionDraft(),
	})
	response := publicRequest(handler, "/api/session/open", token, string(body))
	var opened struct{ ID string }
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &opened) != nil || opened.ID == "" {
		t.Fatalf("public session open failed: %d %s", response.Code, response.Body.String())
	}
	return opened.ID
}

func TestPublicMCPUsesOnlyItsCredentialSessions(t *testing.T) {
	gateway, _, managers := publicFixture(t)
	firstMCP := publicMCP(t, gateway, publicTokenA)
	empty := callMCP(t, firstMCP, "list_sessions", map[string]any{}, false)
	if string(empty["sessions"]) != "[]" || len(*managers) != 0 {
		t.Fatal("MCP initialization allocated a sandbox or exposed another session")
	}
	first := publicBrowserSession(t, gateway, publicTokenA)
	second := publicBrowserSession(t, gateway, publicTokenB)
	secondMCP := publicMCP(t, gateway, publicTokenB)
	for _, test := range []struct {
		client *mcp.ClientSession
		own    string
		other  string
	}{{firstMCP, first, second}, {secondMCP, second, first}} {
		list := callMCP(t, test.client, "list_sessions", map[string]any{}, false)
		if !strings.Contains(string(list["sessions"]), test.own) || strings.Contains(string(list["sessions"]), test.other) {
			t.Fatal("MCP session list crossed credential boundary")
		}
		callMCP(t, test.client, "get_review", map[string]any{"sessionId": test.other}, true)
		callMCP(t, test.client, "add_comment", map[string]any{
			"sessionId": test.other, "requestId": "cross-account", "path": "main.go", "sha": sessionPR().HeadSHA, "line": 3, "side": "RIGHT", "body": "must not appear",
		}, true)
	}
	callMCP(t, firstMCP, "add_comment", map[string]any{
		"sessionId": first, "requestId": "own-account", "path": "main.go", "sha": sessionPR().HeadSHA, "line": 3, "side": "RIGHT", "body": "review this carefully",
	}, false)
	body, _ := json.Marshal(map[string]any{"id": first, "draft": emptySessionDraft(), "acknowledgements": []any{}})
	response := publicRequest(gateway, "/api/session/sync", publicTokenA, string(body))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "review this carefully") {
		t.Fatal("MCP action did not reach its own browser")
	}
	response = publicRequest(gateway, "/api/session/remove", publicTokenB, `{"id":"`+first+`"}`)
	if response.Code == http.StatusOK {
		t.Fatal("another token removed a browser session")
	}
	callMCP(t, firstMCP, "get_review", map[string]any{"sessionId": first}, false)
}

func TestPublicSessionsRecheckPermissionsAndPreserveDraftsOnOutage(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusBadGateway} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			gateway, client, _ := publicFixture(t)
			id := publicBrowserSession(t, gateway, publicTokenA)
			native := publicMCP(t, gateway, publicTokenA)
			client.repoStatus = status
			callMCP(t, native, "get_review", map[string]any{"sessionId": id}, true)
			response := publicRequest(gateway, "/api/session/list", publicTokenA, "{}")
			if strings.Contains(response.Body.String(), "first private review") {
				t.Fatal("revoked or unavailable repository returned cached metadata")
			}
			client.repoStatus = 0
			review := callMCP(t, native, "get_review", map[string]any{"sessionId": id}, status != http.StatusBadGateway)
			if status == http.StatusBadGateway && !strings.Contains(string(review["session"]), "first private review") {
				t.Fatal("transient GitHub outage destroyed the review snapshot")
			}
		})
	}
}

func TestPublicMCPRejectsForeignOrigin(t *testing.T) {
	gateway, _, _ := publicFixture(t)
	request := httptest.NewRequest(http.MethodPost, "https://fjord.example/mcp", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer "+publicTokenA)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatal("native MCP exception allowed a foreign browser origin")
	}
}

func TestPublicSessionSyncNeverReturnsRevokedQueuedContent(t *testing.T) {
	gateway, client, _ := publicFixture(t)
	id := publicBrowserSession(t, gateway, publicTokenA)
	native := publicMCP(t, gateway, publicTokenA)
	callMCP(t, native, "add_comment", map[string]any{
		"sessionId": id, "requestId": "queued-before-revoke", "path": "main.go", "sha": sessionPR().HeadSHA, "line": 3, "side": "RIGHT", "body": "private queued suggestion",
	}, false)
	client.repoStatus = http.StatusForbidden
	body, _ := json.Marshal(map[string]any{"id": id, "draft": emptySessionDraft(), "acknowledgements": []any{}})
	response := publicRequest(gateway, "/api/session/sync", publicTokenA, string(body))
	if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "private queued suggestion") {
		t.Fatal("session sync exposed a queued action after Contents access was revoked")
	}
	client.repoStatus = 0
	id = publicBrowserSession(t, gateway, publicTokenA)
	client.reviewStatus = http.StatusForbidden
	list := callMCP(t, native, "list_sessions", map[string]any{}, false)
	if string(list["sessions"]) != "[]" {
		t.Fatal("MCP exposed metadata after Pull requests permission was revoked")
	}
	client.reviewStatus = 0
	publicBrowserSession(t, gateway, publicTokenA)
	client.authStatus = http.StatusUnauthorized
	if _, err := native.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_sessions", Arguments: map[string]any{}}); err == nil {
		t.Fatal("an initialized MCP connection bypassed revoked bearer authentication")
	}
}
