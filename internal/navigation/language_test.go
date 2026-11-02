package navigation

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareDoesNotRequireLanguageRuntime(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := New()
	defer m.Close()
	id, err := m.Prepare(context.Background(), "owner", "repo", strings.Repeat("c", 40), bytes.NewReader(archiveBytes(t, archiveEntry{name: "repo/main.tf", body: "locals { name = \"fjord\" }\n"})))
	if err != nil {
		t.Fatal(err)
	}
	if m.workspaces[id].client != nil {
		t.Fatal("preparation started a language server")
	}
	if _, err := m.File(context.Background(), id, "main.tf"); err != nil {
		t.Fatal(err)
	}
	_, err = m.Query(context.Background(), id, "main.tf", "definition", 0, 10)
	if err == nil || !strings.Contains(err.Error(), "tofu-ls") {
		t.Fatalf("wrong language runtime error: %v", err)
	}
}

func TestRouteFileSelectsNestedModule(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"service", "service/inner"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "go.mod"), []byte("module example.test/test\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	route, err := routeFile(root, "service/inner/pkg/main.go")
	if err != nil || route.root != filepath.Join(root, "service", "inner") {
		t.Fatalf("%+v %v", route, err)
	}
	for _, name := range []string{"admin/main.tf", "admin/main.tofu", "admin/main.tf.json", "admin/example.tfvars", "admin/example.tfvars.json"} {
		route, err := routeFile(root, name)
		if err != nil || route.language != "terraform" || route.root != root {
			t.Fatalf("%s: %+v %v", name, route, err)
		}
	}
	for _, name := range []string{".terraform.lock.hcl", "main.tftest.hcl", "README.md", "example.tofuvars"} {
		if _, err := routeFile(root, name); err == nil || strings.Contains(err.Error(), "Go packages") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func sourcePosition(t *testing.T, content, needle string, offset int) (int, int) {
	t.Helper()
	index := strings.Index(content, needle)
	if index < 0 {
		t.Fatalf("fixture token %q missing", needle)
	}
	prefix := content[:index+offset]
	line := strings.Count(prefix, "\n")
	character := len(prefix) - strings.LastIndex(prefix, "\n") - 1
	return line, character
}

func TestTerraformNavigation(t *testing.T) {
	if os.Getenv("FJORD_TEST_TOFU_LS") != "1" {
		t.Skip("set FJORD_TEST_TOFU_LS=1 for installed tofu-ls integration")
	}
	entries := []archiveEntry{
		{name: "repo/admin/main.tf", body: "variable \"name\" { default = \"fjord\" }\nlocals { greeting = var.name }\nmodule \"child\" {\n  source = \"./modules/child\"\n  name = local.greeting\n}\noutput \"message\" { value = module.child.message }\n"},
		{name: "repo/admin/modules/child/main.tf", body: "variable \"name\" { type = string }\noutput \"message\" { value = var.name }\n"},
		{name: "repo/admin/config.tf.json", body: "{\"output\":{\"json_message\":{\"value\":\"${local.greeting}\"}}}\n"},
		{name: "repo/admin/example.tfvars", body: "name = \"review\"\n"},
		{name: "repo/admin/example.tfvars.json", body: "{\"name\":\"review\"}\n"},
		{name: "repo/opentofu/main.tofu", body: "variable \"name\" { default = \"fjord\" }\noutput \"message\" { value = var.name }\n"},
	}
	m := New()
	defer m.Close()
	id, err := m.Prepare(context.Background(), "example", "terraform", strings.Repeat("d", 40), bytes.NewReader(archiveBytes(t, entries...)))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path, needle string
		offset             int
		target             string
	}{
		{"variable", "admin/main.tf", "var.name", 5, "admin/main.tf"},
		{"local", "admin/main.tf", "local.greeting", 7, "admin/main.tf"},
		{"module-output", "admin/main.tf", "module.child.message", 15, "admin/modules/child/main.tf"},
		{"json", "admin/config.tf.json", "local.greeting", 7, "admin/main.tf"},
		{"variables", "admin/example.tfvars", "name", 1, "admin/main.tf"},
		{"json-variables", "admin/example.tfvars.json", "name", 1, "admin/main.tf"},
		{"opentofu", "opentofu/main.tofu", "var.name", 5, "opentofu/main.tofu"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content, err := m.File(context.Background(), id, tc.path)
			if err != nil {
				t.Fatal(err)
			}
			line, character := sourcePosition(t, content, tc.needle, tc.offset)
			result, err := m.Query(context.Background(), id, tc.path, "definition", line, character)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, location := range result.Locations {
				if location.Path == tc.target {
					found = true
				}
			}
			if !found {
				t.Fatalf("target %s absent: %+v", tc.target, result)
			}
		})
	}
	if _, err := m.Query(context.Background(), id, "admin/main.tf", "implementation", 0, 1); err == nil || !strings.Contains(err.Error(), "does not support implementation") {
		t.Fatalf("wrong capability response: %v", err)
	}
	for _, method := range []string{"references", "hover"} {
		t.Run(method, func(t *testing.T) {
			content, err := m.File(context.Background(), id, "admin/main.tf")
			if err != nil {
				t.Fatal(err)
			}
			line, character := sourcePosition(t, content, "local.greeting", 7)
			result, err := m.Query(context.Background(), id, "admin/main.tf", method, line, character)
			if err != nil {
				t.Fatal(err)
			}
			if method == "hover" && result.Hover == "" {
				t.Fatal("empty Terraform hover")
			}
			if method == "references" && len(result.Locations) == 0 {
				t.Fatal("empty Terraform references")
			}
		})
	}
}

func TestTerraformRuntimeDoesNotInheritCredentialsOrCLI(t *testing.T) {
	if os.Getenv("FJORD_TEST_TOFU_LS") != "1" {
		t.Skip("requires installed tofu-ls")
	}
	t.Setenv("TF_CLI_CONFIG_FILE", "must-not-be-inherited")
	t.Setenv("AWS_ACCESS_KEY_ID", "must-not-be-inherited")
	t.Setenv("GITHUB_TOKEN", "must-not-be-inherited")
	dir := t.TempDir()
	config, err := configureLanguage(context.Background(), dir, dir, "terraform")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range config.env {
		if strings.Contains(value, "must-not-be-inherited") {
			t.Fatal("host credential environment inherited")
		}
		if strings.HasPrefix(value, "PATH=") && value != "PATH="+filepath.Join(dir, "bin") {
			t.Fatal("Terraform runtime can discover host executables")
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "bin"))
	if err != nil || len(entries) != 0 {
		t.Fatal("private executable directory is not empty")
	}
	if _, ok := config.options["tofu"]; ok {
		t.Fatal("Terraform CLI executable configured")
	}
}

func TestNestedGoModuleAndRuntimeSwitch(t *testing.T) {
	if os.Getenv("FJORD_TEST_GOPLS") != "1" || os.Getenv("FJORD_TEST_TOFU_LS") != "1" {
		t.Skip("requires both native language servers")
	}
	archive := archiveBytes(t,
		archiveEntry{name: "repo/services/api/go.mod", body: "module example.test/api\n\ngo 1.24.0\n"},
		archiveEntry{name: "repo/services/api/main.go", body: "package api\nfunc Target() string { return \"fjord\" }\nfunc Call() string { return Target() }\n"},
		archiveEntry{name: "repo/admin/main.tf", body: "variable \"name\" { default = \"fjord\" }\noutput \"message\" { value = var.name }\n"},
	)
	m := New()
	defer m.Close()
	id, err := m.Prepare(context.Background(), "example", "mixed", strings.Repeat("e", 40), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	var previous *rpcClient
	for _, tc := range []struct {
		path, needle string
		offset       int
	}{{"services/api/main.go", "return Target", 9}, {"admin/main.tf", "var.name", 5}, {"services/api/main.go", "return Target", 9}} {
		content, err := m.File(context.Background(), id, tc.path)
		if err != nil {
			t.Fatal(err)
		}
		line, character := sourcePosition(t, content, tc.needle, tc.offset)
		result, err := m.Query(context.Background(), id, tc.path, "definition", line, character)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Locations) == 0 || result.Locations[0].Path != tc.path {
			t.Fatalf("missing nested definition: %+v", result)
		}
		if previous != nil {
			select {
			case <-previous.done:
			default:
				t.Fatal("previous runtime still running")
			}
		}
		previous = m.workspaces[id].client
	}
}

func TestDemoPRTerraformNavigation(t *testing.T) {
	filename := os.Getenv("FJORD_TEST_DEMO_ARCHIVE")
	if filename == "" {
		t.Skip("set FJORD_TEST_DEMO_ARCHIVE to exact demo PR229 tarball")
	}
	f, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m := New()
	defer m.Close()
	id, err := m.Prepare(context.Background(), "spacelift-solutions", "demo", "633a515ece85dbd099839e3d68bd2b4110d22a15", f)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path, needle string
		offset             int
		target             string
	}{
		{"nested-variable", "admin/modules/mayday/main.tf", "var.parent_space_id", 6, "admin/modules/mayday/variables.tf"},
		{"nested-local", "admin/modules/mayday/main.tf", "local.ansible_hooks", 8, "admin/modules/mayday/main.tf"},
		{"root-variable", "admin/mayday.tf", "var.mayday_flows_token", 6, "admin/variables.tf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, err := m.File(context.Background(), id, tc.path)
			if err != nil {
				t.Fatal(err)
			}
			line, character := sourcePosition(t, content, tc.needle, tc.offset)
			result, err := m.Query(context.Background(), id, tc.path, "definition", line, character)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, location := range result.Locations {
				if location.Path == tc.target {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected %s, got %+v", tc.target, result)
			}
		})
	}
}
