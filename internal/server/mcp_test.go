package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/NerdsWhoFish/fjord/internal/github"
	"github.com/NerdsWhoFish/fjord/internal/navigation"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type sessionGitHub struct{ fakeGitHub }

func (*sessionGitHub) Load(context.Context, string, string, int) (github.PR, error) {
	return sessionPR(), nil
}

type pagedSessionGitHub struct{ sessionGitHub }

func (*pagedSessionGitHub) Comments(context.Context, string, string, int) ([]github.ThreadComment, error) {
	return []github.ThreadComment{{ID: 1, Body: "First"}, {ID: 2, Body: "Second"}, {ID: 3, Body: "Third"}}, nil
}

type sessionNavigation struct{ fakeNavigation }

type handlerTransport struct{ handler http.Handler }

func (transport handlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response := httptest.NewRecorder()
	transport.handler.ServeHTTP(response, request)
	return response.Result(), nil
}

func (sessionNavigation) File(_ context.Context, _, path string) (string, error) {
	if path != "main.go" && path != "unchanged.go" {
		return "", errors.New("file not found")
	}
	return "package main\nfunc main() {\n println(\"hello\")\n}\n", nil
}
func (sessionNavigation) Capabilities(context.Context, string, string) (navigation.Capabilities, error) {
	return navigation.Capabilities{Methods: []string{"definition", "references", "hover"}}, nil
}
func (sessionNavigation) Inspect(_ context.Context, _, _, method string, _, _ int, _ navigation.QueryOptions) (navigation.Result, error) {
	if method == "command" {
		return navigation.Result{}, errors.New("unsupported navigation method")
	}
	return navigation.Result{Locations: []navigation.Location{{Path: "main.go", Line: 1, Character: 5, EndLine: 1, EndCharacter: 9}}}, nil
}

func browserSession(t *testing.T, handler http.Handler, host string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"url": sessionPR().URL, "headSHA": sessionPR().HeadSHA, "baseSHA": sessionPR().BaseSHA, "draft": emptySessionDraft()})
	response := request(handler, "/api/session/open", string(body), host, "http://"+host, "application/json")
	var opened struct {
		ID string `json:"id"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &opened) != nil || opened.ID == "" {
		t.Fatalf("session open failed: %d %s", response.Code, response.Body)
	}
	return opened.ID
}

func callMCP(t *testing.T, client *mcp.ClientSession, name string, args any, wantError bool) map[string]json.RawMessage {
	t.Helper()
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError != wantError {
		t.Fatalf("%s error=%v, want %v: %+v", name, result.IsError, wantError, result.Content)
	}
	if wantError {
		return nil
	}
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		t.Fatalf("tool returned invalid structured content: %s", body)
	}
	return fields
}

func TestMCPHTTPReviewWorkflow(t *testing.T) {
	g := &sessionGitHub{}
	const host = "127.0.0.1:4727"
	handler := New(Config{Host: host, GitHub: g, Navigation: sessionNavigation{}, Version: "test"})
	id := browserSession(t, handler, host)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	connected, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   "http://" + host + "/mcp",
		HTTPClient: &http.Client{Transport: handlerTransport{handler}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connected.Close()
	list, err := connected.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !reflect.DeepEqual(names, []string{"add_comment", "get_review", "list_sessions", "navigate", "read_file", "show_source"}) {
		t.Fatalf("unexpected mutation surface: %v", names)
	}
	unknown, unknownErr := connected.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "add_comment", Arguments: map[string]any{"sessionId": id, "command": "execute"},
	})
	if unknownErr == nil && !unknown.IsError {
		t.Fatal("executable or unknown arguments accepted")
	}
	sessions := callMCP(t, connected, "list_sessions", map[string]any{}, false)
	if !strings.Contains(string(sessions["sessions"]), id) {
		t.Fatal("browser review session absent from MCP")
	}
	file := callMCP(t, connected, "read_file", map[string]any{"sessionId": id, "path": "unchanged.go", "sha": sessionPR().HeadSHA, "line": 2, "count": 2}, false)
	if !strings.Contains(string(file["content"]), "func main") {
		t.Fatal("unchanged source unavailable")
	}
	callMCP(t, connected, "read_file", map[string]any{"sessionId": id, "path": "../main.go", "sha": sessionPR().HeadSHA}, true)
	callMCP(t, connected, "read_file", map[string]any{"sessionId": id, "path": "main.go", "sha": strings.Repeat("c", 40)}, true)
	nav := callMCP(t, connected, "navigate", map[string]any{
		"sessionId": id, "path": "main.go", "sha": sessionPR().HeadSHA, "line": 2, "character": 5,
		"method": "goTo", "reveal": true, "requestId": "navigation-1",
	}, false)
	if !strings.Contains(string(nav["result"]), "definitions") || !strings.Contains(string(nav["result"]), "references") {
		t.Fatal("combined navigation lost definition or references")
	}
	commentInput := map[string]any{
		"sessionId": id, "requestId": "draft-1", "path": "main.go", "sha": sessionPR().HeadSHA,
		"line": 3, "side": "RIGHT", "body": "Consider this suggestion.", "replacement": " println(\"reviewed\")",
	}
	first := callMCP(t, connected, "add_comment", commentInput, false)
	second := callMCP(t, connected, "add_comment", commentInput, false)
	if string(first["id"]) != string(second["id"]) {
		t.Fatal("retry created duplicate draft action")
	}
	invalid := map[string]any{
		"sessionId": id, "requestId": "invalid", "path": "main.go", "sha": sessionPR().HeadSHA,
		"line": 50, "side": "RIGHT", "body": "outside",
	}
	callMCP(t, connected, "add_comment", invalid, true)
	syncBody, _ := json.Marshal(map[string]any{"id": id, "draft": emptySessionDraft(), "acknowledgements": []any{}})
	synced := request(handler, "/api/session/sync", string(syncBody), host, "http://"+host, "application/json")
	var events struct {
		Actions []browserAction `json:"actions"`
	}
	if synced.Code != 200 || json.Unmarshal(synced.Body.Bytes(), &events) != nil || len(events.Actions) != 2 {
		t.Fatalf("browser events unavailable: %d %s", synced.Code, synced.Body)
	}
	if events.Actions[0].Kind != "navigation" || events.Actions[0].Result == nil ||
		events.Actions[1].Comment.Source != "ai" || !strings.Contains(events.Actions[1].Comment.Body, "```suggestion") {
		t.Fatal("browser action lost navigation, attribution, or suggestion")
	}
	draft := emptySessionDraft()
	draft.Comments = []sessionComment{*events.Actions[1].Comment}
	acks := []actionReceipt{{ID: events.Actions[0].ID, Status: "applied"}, {ID: events.Actions[1].ID, Status: "applied"}}
	syncBody, _ = json.Marshal(map[string]any{"id": id, "draft": draft, "acknowledgements": acks})
	synced = request(handler, "/api/session/sync", string(syncBody), host, "http://"+host, "application/json")
	if synced.Code != 200 {
		t.Fatalf("ack failed: %s", synced.Body)
	}
	review := callMCP(t, connected, "get_review", map[string]any{"sessionId": id}, false)
	if !strings.Contains(string(review["session"]), "AI-assisted draft") || !strings.Contains(string(review["session"]), `"status":"applied"`) {
		t.Fatal("MCP did not observe browser-applied draft")
	}
	if g.reviews != 0 {
		t.Fatal("MCP submitted a GitHub review")
	}
}

func TestMCPSameOriginBoundaryAndMissingSession(t *testing.T) {
	handler := New(Config{Host: "fjord.stout.zone", Origin: "https://fjord.stout.zone", GitHub: &sessionGitHub{}, Navigation: sessionNavigation{}})
	for _, tt := range []struct{ host, origin string }{
		{"fjord.stout.zone", "https://evil.example"},
		{"evil.example", "https://fjord.stout.zone"},
		{"fjord.stout.zone", "null"},
	} {
		response := request(handler, "/mcp", `{}`, tt.host, tt.origin, "application/json")
		if response.Code != http.StatusForbidden {
			t.Fatalf("untrusted MCP origin accepted: %+v %d", tt, response.Code)
		}
	}
	body, _ := json.Marshal(map[string]any{"id": "missing", "draft": emptySessionDraft(), "acknowledgements": []any{}})
	response := request(handler, "/api/session/sync", string(body), "fjord.stout.zone", "https://fjord.stout.zone", "application/json")
	if response.Code != 404 || !strings.Contains(response.Body.String(), `"code":"session_not_found"`) {
		t.Fatalf("missing session is not reconnectable: %d %s", response.Code, response.Body)
	}
	body, _ = json.Marshal(map[string]any{"url": sessionPR().URL, "headSHA": strings.Repeat("c", 40), "baseSHA": sessionPR().BaseSHA, "draft": emptySessionDraft()})
	response = request(handler, "/api/session/open", string(body), "fjord.stout.zone", "https://fjord.stout.zone", "application/json")
	if response.Code != 400 {
		t.Fatal("stale PR snapshot attached to newer session revision")
	}
}

func TestSuggestionFencesPreserveReplacement(t *testing.T) {
	for _, replacement := range []string{"", "replacement", "replacement\n", "```code```", "\n"} {
		body := suggestionBody("prose", replacement)
		if !strings.HasPrefix(body, "prose\n\n") {
			t.Fatal("suggestion lost prose")
		}
		if replacement == "```code```" && !strings.Contains(body, "````suggestion\n```code```\n````") {
			t.Fatal("code fences escaped suggestion block")
		}
		if replacement == "" && !strings.HasSuffix(body, "```suggestion\n```") {
			t.Fatal("deletion became blank-line replacement")
		}
	}
}

func TestMCPDraftRejectsStaleSidesAndOutsideDiffs(t *testing.T) {
	server := &Server{sessions: newSessionStore()}
	pr := sessionPR()
	id, _ := server.sessions.open(pr, emptySessionDraft())
	replacement := ""
	for _, in := range []commentInput{
		{SessionID: id, RequestID: "base-suggestion", Path: "main.go", SHA: pr.BaseSHA, Line: 3, Side: "LEFT", Replacement: &replacement},
		{SessionID: id, RequestID: "wrong-sha", Path: "main.go", SHA: pr.BaseSHA, Line: 3, Side: "RIGHT", Body: "Wrong revision"},
		{SessionID: id, RequestID: "outside", Path: "unchanged.go", SHA: pr.HeadSHA, Line: 3, Side: "RIGHT", Body: "Outside diff"},
		{SessionID: id, RequestID: "range", Path: "main.go", SHA: pr.HeadSHA, Line: 3, StartLine: 4, Side: "RIGHT", Body: "Reversed"},
	} {
		if _, err := server.mcpAddComment(context.Background(), in); err == nil {
			t.Fatalf("accepted unsafe comment %+v", in)
		}
	}
	_, err := server.mcpAddComment(context.Background(), commentInput{SessionID: id, RequestID: "delete", Path: "main.go", SHA: pr.HeadSHA, Line: 3, Side: "RIGHT", Replacement: &replacement})
	if err != nil {
		t.Fatalf("valid deletion draft rejected: %v", err)
	}
	session, _ := server.sessions.get(id)
	if len(session.Actions) != 1 || !strings.Contains(session.Actions[0].Comment.Body, "```suggestion\n```") {
		t.Fatal("invalid drafts leaked or deletion suggestion changed")
	}
}

func TestMCPReviewPaginationDoesNotMutateSnapshot(t *testing.T) {
	store := newSessionStore()
	pr := sessionPR()
	pr.Files = append(pr.Files, github.File{Path: "second.go", Patch: "private patch"})
	id, _ := store.open(pr, emptySessionDraft())
	server := &Server{sessions: store, config: Config{GitHub: &pagedSessionGitHub{}}}
	output, err := server.mcpReview(context.Background(), reviewInput{SessionID: id, CommentOffset: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	result := output.(map[string]any)
	comments := result["publishedComments"].([]github.ThreadComment)
	session := result["session"].(reviewSession)
	if len(comments) != 1 || comments[0].ID != 2 || result["nextCommentOffset"] != 2 ||
		result["nextFileOffset"] != 1 || len(session.PR.Files) != 1 || session.PR.Files[0].Patch != "" {
		t.Fatalf("review pagination failed: %+v", result)
	}
	original, _ := store.get(id)
	if len(original.PR.Files) != 2 || original.PR.Files[0].Patch == "" {
		t.Fatal("metadata pagination changed the immutable review diff")
	}
}
