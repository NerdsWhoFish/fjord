package navigation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/NerdsWhoFish/fjord/internal/lspconfig"
)

func featureManager(t *testing.T, mode string) (*Manager, string) {
	t.Helper()
	profile := genericProfile(t)
	profile.Command = []string{profile.Command[0], "-test.run=^TestFeatureServerHelper$"}
	profile.Environment = map[string]string{"TEST_LSP_FEATURES": mode}
	m := NewWithProfiles([]lspconfig.Profile{profile})
	t.Cleanup(func() { _ = m.Close() })
	id, err := m.Prepare(context.Background(), "example", "features", strings.Repeat("e", 40), bytes.NewReader(archiveBytes(t,
		archiveEntry{name: "repo/main.example", body: "func target() {}\nfunc call(){ target() }\n"},
		archiveEntry{name: "repo/other.example", body: "func other() {}\n"},
	)))
	if err != nil {
		t.Fatal(err)
	}
	return m, id
}

func TestHierarchyPreparedRootAndBounds(t *testing.T) {
	m, id := featureManager(t, "hierarchy-root")
	result, err := m.Query(context.Background(), id, "main.example", "outgoingCalls", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || len(result.Items[0].Children) != 1 || result.Items[0].Children[0].Location == nil || result.Items[0].Children[0].Location.Path != "other.example" {
		t.Fatalf("callsite used original query file instead of prepared root: %+v", result)
	}
	m, id = featureManager(t, "oversized-hierarchy")
	if _, err := m.Query(context.Background(), id, "main.example", "incomingCalls", 0, 1); err == nil || !strings.Contains(err.Error(), "too many hierarchy roots") {
		t.Fatalf("%v", err)
	}
}

func TestReviewFeaturesProtocol(t *testing.T) {
	m, id := featureManager(t, "pull")
	caps, err := m.Capabilities(context.Background(), id, "main.example")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(caps.Methods, reviewMethods) {
		t.Fatalf("capabilities: %+v", caps)
	}
	for _, method := range reviewMethods {
		t.Run(method, func(t *testing.T) {
			result, err := m.Inspect(context.Background(), id, "main.example", method, 0, 1, QueryOptions{Query: "target", NewName: "renamed"})
			if err != nil {
				t.Fatal(err)
			}
			switch method {
			case "definition", "implementation", "references":
				if len(result.Locations) != 1 {
					t.Fatalf("%+v", result)
				}
			case "hover":
				if result.Hover == "" {
					t.Fatal("missing hover")
				}
			case "diagnostics", "incomingCalls", "outgoingCalls", "supertypes", "subtypes", "documentSymbols", "workspaceSymbols":
				if len(result.Items) == 0 {
					t.Fatalf("missing items: %+v", result)
				}
			case "documentHighlights":
				if len(result.Highlights) != 1 {
					t.Fatalf("%+v", result)
				}
			case "inlayHints":
				if len(result.Hints) != 1 {
					t.Fatalf("%+v", result)
				}
			case "codeActions":
				if len(result.Actions) != 2 || len(result.Actions[0].Edits) != 1 || result.Actions[1].DisabledReason == "" {
					t.Fatalf("%+v", result)
				}
			case "rename":
				if len(result.Edits) != 1 || result.Edits[0].NewText != "renamed" || result.Edits[0].OldText != "func" {
					t.Fatalf("%+v", result)
				}
			}
		})
	}
	content, err := m.File(context.Background(), id, "main.example")
	if err != nil || !strings.HasPrefix(content, "func target") {
		t.Fatal("readonly review changed source")
	}
}

func TestReviewFeatureUnsupportedAndCancellation(t *testing.T) {
	m, id := featureManager(t, "unsupported")
	if _, err := m.Inspect(context.Background(), id, "main.example", "rename", 0, 1, QueryOptions{NewName: "renamed"}); err == nil || !strings.Contains(err.Error(), "does not support rename") {
		t.Fatalf("%v", err)
	}
	if _, err := m.Inspect(context.Background(), id, "main.example", "workspace/executeCommand", 0, 1, QueryOptions{}); err == nil {
		t.Fatal("executeCommand accepted")
	}
	if _, err := m.Inspect(context.Background(), id, "main.example", "rename", 0, 1, QueryOptions{}); err == nil || !strings.Contains(err.Error(), "new name") {
		t.Fatalf("%v", err)
	}
	m, id = featureManager(t, "stall")
	if _, err := m.Capabilities(context.Background(), id, "main.example"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := m.Query(ctx, id, "main.example", "hover", 0, 1); err == nil {
		t.Fatal("stalled request ignored cancellation")
	}
	select {
	case <-m.workspaces[id].client.done:
	case <-time.After(time.Second):
		t.Fatal("cancelled language server still running")
	}
}

func TestInstalledServerReviewFeatures(t *testing.T) {
	for _, language := range []string{"go", "python"} {
		t.Run(language, func(t *testing.T) {
			var profiles []lspconfig.Profile
			var entries []archiveEntry
			var name, query string
			var line, character int
			if language == "go" {
				if os.Getenv("FJORD_TEST_GOPLS") != "1" {
					t.Skip("requires installed gopls")
				}
				profiles = lspconfig.Defaults()
				profiles[0].InitializationOptions["hints"] = map[string]bool{"parameterNames": true}
				entries = []archiveEntry{{name: "repo/go.mod", body: "module example.test/features\n\ngo 1.24.0\n"}, {name: "repo/main.go", body: "package fixture\nfunc Target(value int) int { return value }\nfunc Call() int { return Target(1) }\ntype Animal interface { Speak() string }\ntype Dog struct{}\nfunc (Dog) Speak() string { return \"woof\" }\n"}}
				name, query, line, character = "main.go", "Target", 1, 6
			} else {
				configPath := os.Getenv("FJORD_TEST_PYRIGHT_CONFIG")
				if configPath == "" {
					t.Skip("requires operator Python profile")
				}
				var err error
				profiles, err = lspconfig.Load(configPath)
				if err != nil {
					t.Fatal(err)
				}
				entries = []archiveEntry{{name: "repo/main.py", body: "def target(value: int) -> int:\n    return value\n\ndef caller() -> int:\n    return target(1)\n"}}
				name, query, line, character = "main.py", "target", 0, 5
			}
			m := NewWithProfiles(profiles)
			defer m.Close()
			id, err := m.Prepare(context.Background(), "example", "review-features", strings.Repeat("f", 40), bytes.NewReader(archiveBytes(t, entries...)))
			if err != nil {
				t.Fatal(err)
			}
			caps, err := m.Capabilities(context.Background(), id, name)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("advertised methods: %v", caps.Methods)
			for _, method := range caps.Methods {
				if method == "definition" || method == "implementation" || method == "references" || method == "hover" {
					continue
				}
				t.Run(method, func(t *testing.T) {
					queryLine, queryCharacter := line, character
					if language == "go" && method == "supertypes" {
						queryLine, queryCharacter = 4, 6
					}
					if language == "go" && method == "subtypes" {
						queryLine, queryCharacter = 3, 6
					}
					result, err := m.Inspect(context.Background(), id, name, method, queryLine, queryCharacter, QueryOptions{Query: query, NewName: "renamed"})
					if err != nil {
						t.Fatal(err)
					}
					if method == "documentSymbols" && len(result.Items) == 0 {
						t.Fatal("installed server returned no outline")
					}
					if method == "incomingCalls" && len(result.Items) == 0 {
						t.Fatal("installed server returned no callers")
					}
					if method == "inlayHints" && len(result.Hints) == 0 {
						t.Fatal("installed server returned no enabled hints")
					}
					if method == "rename" && len(result.Edits) < 2 {
						t.Fatalf("rename preview missed references: %+v", result)
					}
				})
			}
		})
	}
}

func TestPushDiagnosticsAndSilence(t *testing.T) {
	for _, mode := range []string{"push", "silent", "empty"} {
		t.Run(mode, func(t *testing.T) {
			m, id := featureManager(t, mode)
			result, err := m.Query(context.Background(), id, "main.example", "diagnostics", 0, 1)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "push" && len(result.Items) != 1 {
				t.Fatalf("missing published diagnostics: %+v", result)
			}
			if mode == "push" {
				for i := range maxDiagnosticDocuments {
					m.workspaces[id].client.watchDiagnostics(fmt.Sprintf("file:///unused/%d", i))
				}
				refreshed, err := m.Query(context.Background(), id, "main.example", "diagnostics", 0, 1)
				if err != nil || len(refreshed.Items) != 1 {
					t.Fatalf("evicted publication did not refresh: %+v %v", refreshed, err)
				}
			}
			if mode == "silent" && !strings.Contains(result.Message, "not published") {
				t.Fatalf("silence reported as clean: %+v", result)
			}
			if mode == "empty" && (len(result.Items) != 0 || strings.Contains(result.Message, "not published")) {
				t.Fatalf("empty publication not accepted: %+v", result)
			}
			previous := m.workspaces[id].client
			previous.close()
			if _, err := m.Capabilities(context.Background(), id, "main.example"); err != nil {
				t.Fatal(err)
			}
			if len(m.workspaces[id].client.diagnostics) != 0 {
				t.Fatal("restarted server retained stale diagnostics")
			}
		})
	}
}

func TestDiagnosticCacheBoundsAndVersions(t *testing.T) {
	c := &rpcClient{diagnostics: make(map[string]*diagnosticPublication)}
	for i := range maxDiagnosticDocuments + 20 {
		c.watchDiagnostics(fmt.Sprintf("file:///source/%d", i))
	}
	if len(c.diagnostics) != maxDiagnosticDocuments {
		t.Fatal("unbounded watched documents")
	}
	uri := "file:///source/83"
	publish := func(target string, version int, diagnostics any) {
		params, _ := json.Marshal(map[string]any{"uri": target, "version": version, "diagnostics": diagnostics})
		c.publishDiagnostics(params)
	}
	publish("file:///outside", 1, []any{})
	if len(c.diagnostics) != maxDiagnosticDocuments {
		t.Fatal("unwatched URI retained")
	}
	publish(uri, 0, []any{})
	if c.diagnostics[uri].published {
		t.Fatal("stale version accepted")
	}
	publish(uri, 1, []any{})
	if !c.diagnostics[uri].published {
		t.Fatal("current publication discarded")
	}
	publish(uri, 1, []any{map[string]string{"message": strings.Repeat("x", maxDiagnosticBytes)}})
	if c.diagnostics[uri].errorMessage == "" || c.diagnostics[uri].raw != nil {
		t.Fatal("oversized diagnostics cached")
	}
}

func TestFeatureServerHelper(t *testing.T) {
	mode := os.Getenv("TEST_LSP_FEATURES")
	if mode == "" {
		return
	}
	r := bufio.NewReader(os.Stdin)
	send := func(message any) {
		body, err := json.Marshal(message)
		if err != nil {
			os.Exit(8)
		}
		if _, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
			os.Exit(9)
		}
	}
	rng := map[string]any{"start": position{Line: 0, Character: 0}, "end": position{Line: 0, Character: 4}}
	diagnostic := map[string]any{"range": rng, "severity": 2, "source": "fixture", "message": "fixture diagnostic"}
	uri := ""
	for {
		message, err := readMessage(r)
		if err != nil {
			os.Exit(0)
		}
		var result any
		switch message.Method {
		case "initialize":
			providers := map[string]any{"positionEncoding": "utf-16"}
			for _, provider := range methodProviders {
				providers[provider] = true
			}
			if mode == "push" || mode == "silent" || mode == "empty" {
				delete(providers, "diagnosticProvider")
			}
			if mode == "unsupported" {
				delete(providers, "renameProvider")
			}
			providers["codeActionProvider"] = map[string]bool{"resolveProvider": true}
			send(map[string]any{"jsonrpc": "2.0", "id": "readonly", "method": "workspace/applyEdit", "params": map[string]any{"edit": map[string]any{}}})
			response, err := readMessage(r)
			if err != nil || !bytes.Contains(response.Result, []byte(`"applied":false`)) {
				os.Exit(10)
			}
			result = map[string]any{"capabilities": providers}
		case "initialized", "workspace/didChangeConfiguration", "textDocument/didClose":
			continue
		case "textDocument/didOpen":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			if json.Unmarshal(message.Params, &params) != nil {
				os.Exit(11)
			}
			uri = params.TextDocument.URI
			if mode == "push" || mode == "empty" {
				diagnostics := []any{}
				if mode == "push" {
					diagnostics = append(diagnostics, diagnostic)
				}
				send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{"uri": uri, "version": 1, "diagnostics": diagnostics}})
			}
			continue
		case "textDocument/definition", "textDocument/implementation", "textDocument/references":
			result = []any{map[string]any{"uri": uri, "range": rng}}
		case "textDocument/hover":
			if mode == "stall" {
				continue
			}
			result = map[string]any{"contents": "fixture hover"}
		case "textDocument/diagnostic":
			result = map[string]any{"kind": "full", "items": []any{diagnostic}}
		case "textDocument/prepareCallHierarchy", "textDocument/prepareTypeHierarchy", "typeHierarchy/supertypes", "typeHierarchy/subtypes":
			targetURI := uri
			if mode == "hierarchy-root" {
				targetURI = strings.TrimSuffix(uri, "main.example") + "other.example"
			}
			item := map[string]any{"uri": targetURI, "name": "target", "kind": 12, "range": rng, "selectionRange": rng}
			items := []any{item}
			if mode == "oversized-hierarchy" {
				for len(items) < 17 {
					items = append(items, item)
				}
			}
			result = items
		case "callHierarchy/incomingCalls":
			result = []any{map[string]any{"from": map[string]any{"uri": uri, "name": "caller", "kind": 12, "range": rng, "selectionRange": rng}, "fromRanges": []any{rng}}}
		case "callHierarchy/outgoingCalls":
			result = []any{map[string]any{"to": map[string]any{"uri": uri, "name": "callee", "kind": 12, "range": rng, "selectionRange": rng}, "fromRanges": []any{rng}}}
		case "textDocument/documentSymbol":
			result = []any{map[string]any{"name": "target", "kind": 12, "range": rng, "selectionRange": rng}}
		case "workspace/symbol":
			var params struct {
				Query string `json:"query"`
			}
			if json.Unmarshal(message.Params, &params) != nil || params.Query != "target" {
				os.Exit(12)
			}
			result = []any{map[string]any{"name": "target", "kind": 12, "location": map[string]any{"uri": uri, "range": rng}}}
		case "textDocument/documentHighlight":
			result = []any{map[string]any{"range": rng, "kind": 2}}
		case "textDocument/inlayHint":
			var params struct {
				Range sourceRange `json:"range"`
			}
			if json.Unmarshal(message.Params, &params) != nil || params.Range.End.Line != 2 {
				os.Exit(13)
			}
			result = []any{map[string]any{"position": position{Line: 0, Character: 4}, "label": "value:", "kind": 2}}
		case "textDocument/codeAction":
			var params struct {
				Context struct {
					Diagnostics []json.RawMessage `json:"diagnostics"`
				} `json:"context"`
			}
			if json.Unmarshal(message.Params, &params) != nil || len(params.Context.Diagnostics) != 1 {
				os.Exit(14)
			}
			result = []any{map[string]any{"title": "Resolve edit", "kind": "quickfix", "data": "resolve-me"}, map[string]any{"title": "Unsafe command", "command": "fixture.mutate"}}
		case "codeAction/resolve":
			result = map[string]any{"title": "Resolved edit", "kind": "quickfix", "edit": map[string]any{"changes": map[string]any{uri: []any{map[string]any{"range": rng, "newText": "changed"}}}}}
		case "textDocument/rename":
			var params struct {
				NewName string `json:"newName"`
			}
			if json.Unmarshal(message.Params, &params) != nil || params.NewName != "renamed" {
				os.Exit(15)
			}
			result = map[string]any{"changes": map[string]any{uri: []any{map[string]any{"range": rng, "newText": params.NewName}}}}
		default:
			os.Exit(16)
		}
		send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result})
	}
}
