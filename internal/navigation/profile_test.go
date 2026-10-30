package navigation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/NerdsWhoFish/fjord/internal/lspconfig"
)

func genericProfile(t *testing.T) lspconfig.Profile {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return lspconfig.Profile{
		Name: "fixture", Command: []string{executable, "-test.run=^TestGenericServerHelper$", "--", "literal argument;$(false)"},
		Files: []string{"**/*.example"}, LanguageID: "example", RootMarkers: []string{"project.marker"},
		InitializationOptions: map[string]any{"fixture": true},
		Settings:              map[string]any{"example": map[string]any{"analysis": map[string]any{"enabled": true}}},
		Environment:           map[string]string{"TEST_LSP_FIXTURE": "1"},
	}
}

func TestGenericProfileCapabilitiesAndSettings(t *testing.T) {
	p := genericProfile(t)
	m := NewWithProfiles([]lspconfig.Profile{p})
	defer m.Close()
	id, err := m.Prepare(context.Background(), "example", "generic", strings.Repeat("a", 40), bytes.NewReader(archiveBytes(t,
		archiveEntry{name: "repo/project/project.marker", body: "marker"},
		archiveEntry{name: "repo/project/main.example", body: "target"},
		archiveEntry{name: "repo/README.md", body: "readme"},
	)))
	if err != nil {
		t.Fatal(err)
	}
	unsupported, err := m.Capabilities(context.Background(), id, "README.md")
	if err != nil || unsupported.Methods == nil || len(unsupported.Methods) != 0 || unsupported.Message == "" {
		t.Fatalf("%+v %v", unsupported, err)
	}
	if m.workspaces[id].client != nil {
		t.Fatal("unsupported file started a runtime")
	}
	caps, err := m.Capabilities(context.Background(), id, "project/main.example")
	if err != nil {
		t.Fatal(err)
	}
	if caps.Server != p.Name || caps.LanguageID != p.LanguageID || !reflect.DeepEqual(caps.Methods, []string{"definition", "hover"}) {
		t.Fatalf("%+v", caps)
	}
	client := m.workspaces[id].client
	result, err := m.Query(context.Background(), id, "project/main.example", "hover", 0, 1)
	if err != nil || result.Hover != "settings and argv verified" {
		t.Fatalf("%+v %v", result, err)
	}
	if m.workspaces[id].client != client {
		t.Fatal("query restarted capabilities runtime")
	}
	if _, err := m.Query(context.Background(), id, "project/main.example", "implementation", 0, 1); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("%v", err)
	}
}

func TestGenericProfileRejectsPositionEncoding(t *testing.T) {
	p := genericProfile(t)
	p.Environment["TEST_LSP_ENCODING"] = "utf-8"
	dir := t.TempDir()
	client, _, err := startProfileServer(context.Background(), dir, dir, p)
	if client != nil {
		client.close()
		t.Fatal("accepted UTF-8 server")
	}
	if err == nil || !strings.Contains(err.Error(), "UTF-16") {
		t.Fatalf("%v", err)
	}
}

func TestGenericServerHelper(t *testing.T) {
	if os.Getenv("TEST_LSP_FIXTURE") != "1" {
		return
	}
	fail := func() { os.Exit(9) }
	if os.Args[len(os.Args)-1] != "literal argument;$(false)" {
		fail()
	}
	r := bufio.NewReader(os.Stdin)
	send := func(message any) {
		body, err := json.Marshal(message)
		if err != nil {
			fail()
		}
		if _, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
			fail()
		}
	}
	initialized, configured := false, false
	for {
		message, err := readMessage(r)
		if err != nil {
			os.Exit(0)
		}
		switch message.Method {
		case "initialize":
			var params struct {
				InitializationOptions map[string]any `json:"initializationOptions"`
			}
			if json.Unmarshal(message.Params, &params) != nil || params.InitializationOptions["fixture"] != true {
				fail()
			}
			send(map[string]any{"jsonrpc": "2.0", "id": "settings", "method": "workspace/configuration", "params": map[string]any{"items": []any{map[string]string{"section": "example.analysis"}, map[string]string{"section": "absent"}}}})
			response, err := readMessage(r)
			if err != nil || string(response.Result) != `[{"enabled":true},null]` {
				fail()
			}
			encoding := os.Getenv("TEST_LSP_ENCODING")
			if encoding == "" {
				encoding = "utf-16"
			}
			send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{"capabilities": map[string]any{"definitionProvider": map[string]any{}, "hoverProvider": true, "referencesProvider": false, "positionEncoding": encoding}}})
		case "initialized":
			initialized = true
		case "workspace/didChangeConfiguration":
			var params struct {
				Settings map[string]any `json:"settings"`
			}
			if json.Unmarshal(message.Params, &params) != nil || settingSection(params.Settings, "example.analysis.enabled") != true {
				fail()
			}
			configured = true
		case "textDocument/hover":
			if !initialized || !configured {
				fail()
			}
			send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{"contents": "settings and argv verified"}})
		}
	}
}

func TestProfileRootMarkersAndRuntimeValidation(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "project.marker"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	p := genericProfile(t)
	route, err := routeProfiles(root, "nested/main.example", []lspconfig.Profile{p})
	if err != nil || route.root != filepath.Join(root, "nested") {
		t.Fatalf("%+v %v", route, err)
	}
	p.RootRequired = true
	if _, err := routeProfiles(root, "main.example", []lspconfig.Profile{p}); err == nil {
		t.Fatal("missing required root accepted")
	}
	for _, path := range []string{"/", os.Getenv("HOME")} {
		p.RuntimePaths = []string{path}
		if _, err := configureProfile(context.Background(), root, root, p); err == nil {
			t.Fatal("broad runtime path accepted")
		}
	}
	p.RuntimePaths = nil
	p.Environment["PATH"] = "/usr/bin"
	if _, err := configureProfile(context.Background(), root, root, p); err == nil {
		t.Fatal("protected environment accepted")
	}
}

func TestPyrightNavigation(t *testing.T) {
	configPath := os.Getenv("FJORD_TEST_PYRIGHT_CONFIG")
	if configPath == "" {
		t.Skip("set FJORD_TEST_PYRIGHT_CONFIG to an operator profile YAML")
	}
	profiles, err := lspconfig.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	m := NewWithProfiles(profiles)
	defer m.Close()
	id, err := m.Prepare(context.Background(), "example", "python", strings.Repeat("b", 40), bytes.NewReader(archiveBytes(t,
		archiveEntry{name: "repo/pyproject.toml", body: "[tool.pyright]\ntypeCheckingMode = 'basic'\n"},
		archiveEntry{name: "repo/main.py", body: "def greeting(name: str) -> str:\n    return 'Hello ' + name\n\nmessage = greeting('Fjord')\n"},
	)))
	if err != nil {
		t.Fatal(err)
	}
	caps, err := m.Capabilities(context.Background(), id, "main.py")
	if err != nil {
		t.Fatal(err)
	}
	if caps.LanguageID != "python" {
		t.Fatalf("wrong profile: %+v", caps)
	}
	for _, method := range []string{"definition", "references", "hover"} {
		t.Run(method, func(t *testing.T) {
			result, err := m.Query(context.Background(), id, "main.py", method, 3, 13)
			if err != nil {
				t.Fatal(err)
			}
			if method == "hover" {
				if !strings.Contains(result.Hover, "greeting") {
					t.Fatalf("%+v", result)
				}
				return
			}
			if len(result.Locations) == 0 || result.Locations[0].Path != "main.py" {
				t.Fatalf("%+v", result)
			}
		})
	}
}
