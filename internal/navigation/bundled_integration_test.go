package navigation

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBundledNodeServers(t *testing.T) {
	if os.Getenv("FJORD_TEST_NODE_LSP") != "1" {
		t.Skip("set FJORD_TEST_NODE_LSP=1 and install the pinned Node bundle")
	}
	for _, test := range []struct {
		path, language, source, use string
		definition                  bool
	}{
		{"main.ts", "typescript", "function target(value: number): number { return value; }\nconst result = target(1);\n", "target(1)", true},
		{"main.tsx", "typescriptreact", "function target(value: number): number { return value; }\nconst result = target(1);\n", "target(1)", true},
		{"main.js", "javascript", "function target(value) { return value; }\nconst result = target(1);\n", "target(1)", true},
		{"main.jsx", "javascriptreact", "function target(value) { return value; }\nconst result = target(1);\n", "target(1)", true},
		{"main.py", "python", "def target(value: int) -> int:\n    return value\n\nresult = target(1)\n", "target(1)", true},
		{"main.sh", "shellscript", "#!/bin/bash\ntarget() { echo hello; }\ntarget\n", "target\n", true},
		{"Dockerfile", "dockerfile", "FROM alpine:3.21 AS base\nWORKDIR /app\nFROM base AS app\n", "base AS app", true},
		{"index.html", "html", "<html><body><h1>Fjord</h1></body></html>\n", "<h1>", false},
		{"main.css", "css", ".fjord { color: red; }\n", "color", false},
		{"main.scss", "scss", "$color: red;\n.fjord { color: $color; }\n", "$color;", true},
	} {
		t.Run(test.language, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			m := New()
			defer m.Close()
			id, err := m.Prepare(ctx, "example", "bundled", strings.Repeat("c", 40), bytes.NewReader(archiveBytes(t,
				archiveEntry{name: "repo/" + test.path, body: test.source},
			)))
			if err != nil {
				t.Fatal(err)
			}
			caps, err := m.Capabilities(ctx, id, test.path)
			if err != nil || caps.LanguageID != test.language {
				t.Fatalf("capabilities: %+v, %v", caps, err)
			}
			result, err := m.Inspect(ctx, id, test.path, "documentSymbols", 0, 0, QueryOptions{})
			// Some servers finish workspace/configuration after initialized.
			for deadline := time.Now().Add(2 * time.Second); err == nil && len(result.Items) == 0 && time.Now().Before(deadline); {
				time.Sleep(50 * time.Millisecond)
				result, err = m.Inspect(ctx, id, test.path, "documentSymbols", 0, 0, QueryOptions{})
			}
			if err != nil || len(result.Items) == 0 {
				t.Fatalf("outline: %+v, %v", result, err)
			}
			if test.definition {
				line, character := sourcePosition(t, test.source, test.use, 1)
				result, err := m.Query(ctx, id, test.path, "definition", line, character)
				if err != nil || len(result.Locations) == 0 || result.Locations[0].Path != test.path {
					t.Fatalf("definition: %+v, %v", result, err)
				}
			}
		})
	}
}

func TestBundledTypeScriptDoesNotLoadRepositoryPlugins(t *testing.T) {
	if os.Getenv("FJORD_TEST_NODE_LSP") != "1" {
		t.Skip("set FJORD_TEST_NODE_LSP=1 and install the pinned Node bundle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	m := New()
	defer m.Close()
	id, err := m.Prepare(ctx, "example", "plugins", strings.Repeat("d", 40), bytes.NewReader(archiveBytes(t,
		archiveEntry{name: "repo/tsconfig.json", body: `{"compilerOptions":{"plugins":[{"name":"fjord-canary"}]}}`},
		archiveEntry{name: "repo/node_modules/fjord-canary/package.json", body: `{"name":"fjord-canary","main":"index.js"}`},
		archiveEntry{name: "repo/node_modules/fjord-canary/index.js", body: `require("fs").writeFileSync(require("path").join(require("os").tmpdir(), "fjord-plugin-executed"), "unsafe"); module.exports = () => ({create: info => info.languageService});`},
		archiveEntry{name: "repo/main.ts", body: "function target() { return 1; }\nconst result = target();\n"},
	)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.Query(ctx, id, "main.ts", "definition", 1, 16)
	if err != nil || len(result.Locations) == 0 {
		t.Fatalf("TypeScript project did not load: %+v, %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(m.workspaces[id].dir, "tmp", "fjord-plugin-executed")); !os.IsNotExist(err) {
		t.Fatalf("repository plugin executed or canary check failed: %v", err)
	}
}
