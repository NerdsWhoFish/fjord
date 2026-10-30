package lspconfig

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "fjord.yaml")
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestDefaultProfilesMatchExistingLanguages(t *testing.T) {
	profiles, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(profiles); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, name, language string }{
		{"main.go", "go", "go"},
		{"internal/harbor/main.go", "go", "go"},
		{"main.tf", "terraform", "opentofu"},
		{"modules/network/main.tf.json", "terraform", "opentofu"},
		{"main.tofu", "terraform", "opentofu"},
		{"production.tfvars", "terraform-vars", "opentofu-vars"},
		{"env/production.tfvars.json", "terraform-vars", "opentofu-vars"},
	} {
		t.Run(test.path, func(t *testing.T) {
			profile, ok := Match(profiles, test.path)
			if !ok || profile.Name != test.name || profile.LanguageID != test.language {
				t.Fatalf("matched %+v, %v", profile, ok)
			}
		})
	}
	for _, file := range []string{"README.md", "main.tofu.json", "a.tofuvars", ".terraform.lock.hcl", "main.tftest.hcl"} {
		if _, ok := Match(profiles, file); ok {
			t.Errorf("unexpected default for %s", file)
		}
	}
	goProfile := profiles[0]
	if !goProfile.RootRequired || !reflect.DeepEqual(goProfile.RootMarkers, []string{"go.mod"}) || goProfile.Adapter != "go" {
		t.Fatalf("lost Go preflight: %+v", goProfile)
	}
	if !reflect.DeepEqual(goProfile.InitializationOptions, map[string]any{"staticcheck": false, "checkBugReports": false, "telemetryPrompt": false, "hints": map[string]any{"parameterNames": true, "assignVariableTypes": true, "rangeVariableTypes": true}}) {
		t.Fatal("lost safe Go initialization defaults")
	}
	for _, profile := range profiles[1:] {
		if !reflect.DeepEqual(profile.Command, []string{"tofu-ls", "serve"}) || profile.InitializationOptions["validation"].(map[string]any)["enableEnhancedValidation"] != false {
			t.Fatalf("lost safe Terraform defaults: %+v", profile)
		}
	}
}

func TestDefaultsDoNotShareMutableState(t *testing.T) {
	first := Defaults()
	first[0].Command[0] = "changed"
	first[1].InitializationOptions["validation"].(map[string]any)["enableEnhancedValidation"] = true
	first[1].Environment["TF_IN_AUTOMATION"] = "changed"
	second := Defaults()
	if second[0].Command[0] != "gopls" || second[1].Environment["TF_IN_AUTOMATION"] != "1" || second[1].InitializationOptions["validation"].(map[string]any)["enableEnhancedValidation"] != false || first[2].InitializationOptions["validation"].(map[string]any)["enableEnhancedValidation"] != false {
		t.Fatal("default profiles share mutable state")
	}
}

func TestExplicitProfilesReplaceDisableAndTakePriority(t *testing.T) {
	config := `languageServers:
  z-custom:
    command: [custom-lsp, --stdio]
    files: ["**/*.go", "**/*.ts"]
    languageId: typescript
  a-custom:
    command: [other-lsp]
    files: ["**/*.go"]
    languageId: go
  go:
    command: [alternate-gopls]
    files: ["**/*.go"]
    languageId: go
  terraform:
    disabled: true
`
	profiles, err := Load(writeConfig(t, config))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, profile := range profiles {
		names = append(names, profile.Name)
	}
	if !reflect.DeepEqual(names, []string{"a-custom", "go", "z-custom", "terraform-vars"}) {
		t.Fatalf("nondeterministic profile order: %v", names)
	}
	if profiles[1].Adapter != "" || profiles[1].RootRequired || len(profiles[1].RootMarkers) != 0 {
		t.Fatal("replacement inherited default privileges or root constraints")
	}
	if profile, ok := Match(profiles, "main.go"); !ok || profile.Name != "a-custom" {
		t.Fatal("explicit profile priority was not honored")
	}
	if _, ok := Match(profiles, "main.tf"); ok {
		t.Fatal("disabled default remained active")
	}
	if _, ok := Match(profiles, "main.tfvars"); !ok {
		t.Fatal("unmentioned default was removed")
	}
}

func TestLoadRejectsInvalidYAMLWithoutExposingPayload(t *testing.T) {
	for _, body := range []string{
		"",
		"{}",
		"languageServers: null",
		"languageServers: {}\nsecret-configuration-value: true",
		"languageServers: {}\nlanguageServers: {}",
		"languageServers: {}\n---\nlanguageServers: {}",
		"languageServers: {}\n---\n",
		"languageServers:\n  go: {disabled: true}\n  go: {disabled: true}",
		"languageServers:\n  go: {disabled: true, secret-configuration-value: true}",
		"languageServers:\n  go: {disabled: true, disabled: false}",
		"languageServers:\n  custom: {command: secret-configuration-value, files: ['*.go'], languageId: go}",
		"languageServers:\n  custom: {command: [server], files: ['*.go'], languageId: go, settings: {key: 1, key: 2}}",
		"languageServers:\n  custom: {command: [server], files: ['*.go'], languageId: go, settings: {key: .nan}}",
	} {
		_, err := Load(writeConfig(t, body))
		if err == nil {
			t.Errorf("accepted malformed configuration: %q", body)
			continue
		}
		if strings.Contains(err.Error(), "secret-configuration-value") {
			t.Fatalf("configuration payload leaked in error: %s", err)
		}
	}
	if _, err := Load(writeConfig(t, "languageServers: {}\n"+strings.Repeat(" ", maxConfigBytes))); err == nil {
		t.Fatal("oversized config accepted")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "secret-configuration-value.yaml")); err == nil || strings.Contains(err.Error(), "secret-configuration-value") {
		t.Fatalf("unsafe file error: %v", err)
	}
}

func validProfile() Profile {
	return Profile{Name: "custom", Command: []string{"language-server", "--stdio"}, Files: []string{"**/*.custom"}, LanguageID: "custom"}
}

func TestProfileBoundsAndSafety(t *testing.T) {
	for name, change := range map[string]func(*Profile){
		"invalid-name":         func(p *Profile) { p.Name = "../secret" },
		"name-too-long":        func(p *Profile) { p.Name = strings.Repeat("a", 65) },
		"empty-command":        func(p *Profile) { p.Command = nil },
		"blank-executable":     func(p *Profile) { p.Command = []string{" "} },
		"relative-executable":  func(p *Profile) { p.Command[0] = "./node_modules/.bin/server" },
		"unclean-executable":   func(p *Profile) { p.Command[0] = "/opt/bin/../server" },
		"command-control":      func(p *Profile) { p.Command = []string{"server", "--arg\nsecret"} },
		"command-too-many":     func(p *Profile) { p.Command = make([]string, 33); p.Command[0] = "server" },
		"argument-too-long":    func(p *Profile) { p.Command = []string{"server", strings.Repeat("a", 4097)} },
		"missing-language":     func(p *Profile) { p.LanguageID = "" },
		"missing-files":        func(p *Profile) { p.Files = nil },
		"invalid-glob":         func(p *Profile) { p.Files = []string{"**/["} },
		"absolute-glob":        func(p *Profile) { p.Files = []string{"/**/*.go"} },
		"escaping-glob":        func(p *Profile) { p.Files = []string{"../*.go"} },
		"inline-double-star":   func(p *Profile) { p.Files = []string{"src**/*.go"} },
		"glob-brace-expansion": func(p *Profile) { p.Files = []string{"**/*.{ts,tsx}"} },
		"unknown-adapter":      func(p *Profile) { p.Adapter = "shell" },
		"root-without-markers": func(p *Profile) { p.RootRequired = true },
		"marker-path":          func(p *Profile) { p.RootMarkers = []string{"nested/project.json"} },
		"marker-escape":        func(p *Profile) { p.RootMarkers = []string{".."} },
		"runtime-relative":     func(p *Profile) { p.RuntimePaths = []string{"runtime"} },
		"runtime-root":         func(p *Profile) { p.RuntimePaths = []string{"/"} },
		"runtime-unclean":      func(p *Profile) { p.RuntimePaths = []string{"/opt/runtime/../other"} },
		"invalid-environment":  func(p *Profile) { p.Environment = map[string]string{"A=B": "value"} },
		"environment-control":  func(p *Profile) { p.Environment = map[string]string{"CUSTOM": "secret\nvalue"} },
		"non-json-setting":     func(p *Profile) { p.Settings = map[string]any{"fn": func() {}} },
		"infinite-setting":     func(p *Profile) { p.Settings = map[string]any{"number": math.Inf(1)} },
		"oversized-setting":    func(p *Profile) { p.Settings = map[string]any{"large": strings.Repeat("x", maxJSONBytes)} },
		"non-json-initializer": func(p *Profile) { p.InitializationOptions = map[string]any{"number": math.NaN()} },
	} {
		t.Run(name, func(t *testing.T) {
			profile := validProfile()
			change(&profile)
			if err := Validate([]Profile{profile}); err == nil {
				t.Fatal("unsafe profile accepted")
			}
		})
	}
	profile := validProfile()
	profile.Settings = map[string]any{"root": map[string]any{}}
	current := profile.Settings["root"].(map[string]any)
	for range 20 {
		next := map[string]any{}
		current["nested"] = next
		current = next
	}
	if err := Validate([]Profile{profile}); err == nil {
		t.Fatal("deeply nested settings accepted")
	}
	if err := Validate([]Profile{validProfile(), validProfile()}); err == nil {
		t.Fatal("duplicate profile names accepted")
	}
}

func TestProtectedEnvironmentCannotOverrideIsolation(t *testing.T) {
	for _, key := range []string{"HOME", "PATH", "Path", "TMPDIR", "TMP", "TEMP", "XDG_CONFIG_HOME", "GOCACHE", "GOMODCACHE", "GOPATH", "GOROOT", "GOTOOLCHAIN", "GOENV", "GOPROXY", "GOFLAGS", "CGO_ENABLED", "CGO_CFLAGS", "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "NODE_OPTIONS", "NODE_PATH", "npm_config_node_options", "PYTHONPATH", "RUBYOPT", "PERL5OPT", "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "_JAVA_OPTIONS", "BASH_ENV", "ENV", "OPENSSL_MODULES", "DOTNET_STARTUP_HOOKS", "CORECLR_PROFILER_PATH"} {
		t.Run(key, func(t *testing.T) {
			profile := validProfile()
			profile.Environment = map[string]string{key: "private-environment-value"}
			err := Validate([]Profile{profile})
			if err == nil || strings.Contains(err.Error(), "private-environment-value") {
				t.Fatalf("unsafe environment validation: %v", err)
			}
		})
	}
	profile := validProfile()
	profile.Environment = map[string]string{"LOG_LEVEL": "warning", "RUST_LOG": "error"}
	if err := Validate([]Profile{profile}); err != nil {
		t.Fatalf("ordinary server environment rejected: %v", err)
	}
}

func TestRuntimePathsRejectHomeAncestorsAndSymlinks(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{home, filepath.Dir(home), alias, "/", "/opt", "/opt/homebrew", "/opt/homebrew/Cellar", "/usr", "/usr/local", "/var", "/private", "/tmp", "/etc"} {
		profile := validProfile()
		profile.RuntimePaths = []string{value}
		if err := Validate([]Profile{profile}); err == nil {
			t.Errorf("broad runtime path accepted: %s", value)
		}
	}
	profile := validProfile()
	profile.RuntimePaths = []string{filepath.Join(home, ".local", "language-server")}
	if err := Validate([]Profile{profile}); err != nil {
		t.Fatalf("specific runtime path rejected: %v", err)
	}
	profile.RuntimePaths = make([]string, 64)
	for i := range profile.RuntimePaths {
		profile.RuntimePaths[i] = filepath.Join(home, ".local", fmt.Sprintf("dependency-%d", i))
	}
	if err := Validate([]Profile{profile}); err != nil {
		t.Fatalf("precise runtime dependency paths rejected: %v", err)
	}
	profile.RuntimePaths = append(profile.RuntimePaths, filepath.Join(home, ".local", "overflow"))
	if err := Validate([]Profile{profile}); err == nil {
		t.Fatal("too many runtime paths accepted")
	}
}

func TestProfileCountLimit(t *testing.T) {
	var body strings.Builder
	body.WriteString("languageServers:\n")
	for i := range 33 {
		fmt.Fprintf(&body, "  server%d: {disabled: true}\n", i)
	}
	if _, err := Load(writeConfig(t, body.String())); err == nil {
		t.Fatal("too many named profiles accepted")
	}
	profiles := make([]Profile, 33)
	for i := range profiles {
		profiles[i] = validProfile()
		profiles[i].Name = fmt.Sprintf("server%d", i)
	}
	if err := Validate(profiles); err == nil {
		t.Fatal("too many direct profiles accepted")
	}
}

func TestGlobMatchingAndPathBoundary(t *testing.T) {
	profiles := []Profile{{Name: "one-level", Files: []string{"src/?.ts"}}, {Name: "all-levels", Files: []string{"**/*.ts"}}}
	for _, test := range []struct{ file, name string }{{"src/a.ts", "one-level"}, {"src/ab.ts", "all-levels"}, {"main.ts", "all-levels"}, {"deep/src/a.ts", "all-levels"}} {
		if profile, ok := Match(profiles, test.file); !ok || profile.Name != test.name {
			t.Errorf("%s matched %+v, %v", test.file, profile, ok)
		}
	}
	for _, file := range []string{"", ".", "..", "../main.ts", "/main.ts", "dir/../main.ts", "dir//main.ts", "dir\\main.ts", "a\n.ts"} {
		if _, ok := Match(profiles, file); ok {
			t.Errorf("unsafe match path accepted: %q", file)
		}
	}
	profiles[0].Disabled = true
	if profile, ok := Match(profiles, "src/a.ts"); !ok || profile.Name != "all-levels" {
		t.Fatal("disabled profile participated in matching")
	}
}
