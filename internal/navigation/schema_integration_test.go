package navigation

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NerdsWhoFish/fjord/internal/lspconfig"
)

func TestBundledOfflineSchemaDiagnostics(t *testing.T) {
	if os.Getenv("FJORD_TEST_NODE_LSP") != "1" {
		t.Skip("set FJORD_TEST_NODE_LSP=1 with the installed language bundle to exercise offline schemas")
	}
	tests := []struct {
		name string
		path string
		body string
		want bool
	}{
		{"workflow invalid", ".github/workflows/build.yaml", "name: build\non: push\njobs:\n  build:\n    runs-on: true\n    steps:\n      - run: echo hello\n", true},
		{"workflow valid", ".github/workflows/build.yml", "name: build\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n", false},
		{"kubernetes invalid", "k8s/deployment.yaml", "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: demo\nspec:\n  replicas: wrong\n  selector:\n    matchLabels:\n      app: demo\n  template:\n    metadata:\n      labels:\n        app: demo\n    spec:\n      containers:\n        - name: demo\n          image: nginx\n", true},
		{"kubernetes valid", "kubernetes/config.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\ndata:\n  hello: world\n", false},
		{"arbitrary yaml", "config/settings.yaml", "jobs: true\nspec:\n  replicas: wrong\n", false},
		{"typescript config invalid", "tsconfig.json", "{\"compilerOptions\":{\"strict\":\"wrong\"}}", true},
		{"typescript config valid", "tsconfig.json", "{\"compilerOptions\":{\"strict\":true}}", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			m := NewWithProfiles(lspconfig.Defaults())
			defer m.Close()
			id, err := m.Prepare(ctx, "example", "schemas", strings.Repeat("c", 40), bytes.NewReader(archiveBytes(t,
				archiveEntry{name: "repo/" + test.path, body: test.body},
			)))
			if err != nil {
				t.Fatal(err)
			}
			result, err := m.Query(ctx, id, test.path, "diagnostics", 0, 0)
			for err == nil && strings.Contains(result.Message, "not published") && ctx.Err() == nil {
				result, err = m.Query(ctx, id, test.path, "diagnostics", 0, 0)
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result.Message, "not published") {
				t.Fatalf("server did not publish diagnostics: %+v", result)
			}
			for _, item := range result.Items {
				message := strings.ToLower(item.Label + " " + item.Detail)
				if strings.Contains(message, "unable to load") || strings.Contains(message, "schema request") {
					t.Fatalf("schema failed to load: %+v", result)
				}
			}
			if (len(result.Items) > 0) != test.want {
				t.Fatalf("wanted diagnostics=%v, got %+v", test.want, result)
			}
		})
	}
}
