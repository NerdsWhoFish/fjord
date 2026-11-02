// Package lspconfig loads operator-owned language-server profiles.
package lspconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v3"
)

const maxConfigBytes = 1 << 20
const maxProfiles = 32
const maxJSONBytes = 64 << 10

type Profile struct {
	Name                  string            `yaml:"-"`
	Command               []string          `yaml:"command"`
	Files                 []string          `yaml:"files"`
	LanguageID            string            `yaml:"languageId"`
	RootMarkers           []string          `yaml:"rootMarkers"`
	RootRequired          bool              `yaml:"rootRequired"`
	InitializationOptions map[string]any    `yaml:"initializationOptions"`
	Settings              map[string]any    `yaml:"settings"`
	RuntimePaths          []string          `yaml:"runtimePaths"`
	Environment           map[string]string `yaml:"environment"`
	Adapter               string            `yaml:"adapter"`
	Disabled              bool              `yaml:"disabled"`
}

func Defaults() []Profile {
	terraformOptions := func() map[string]any {
		return map[string]any{
			"experimentalFeatures": map[string]any{"validateOnSave": false},
			"validation":           map[string]any{"enableEnhancedValidation": false},
		}
	}
	return []Profile{
		{Name: "go", Command: []string{"gopls", "serve"}, Files: []string{"**/*.go"}, LanguageID: "go", RootMarkers: []string{"go.mod"}, RootRequired: true, Adapter: "go", InitializationOptions: map[string]any{"staticcheck": false, "checkBugReports": false, "telemetryPrompt": false, "hints": map[string]any{"parameterNames": true, "assignVariableTypes": true, "rangeVariableTypes": true}}},
		{Name: "terraform", Command: []string{"tofu-ls", "serve"}, Files: []string{"**/*.tf", "**/*.tf.json", "**/*.tofu"}, LanguageID: "opentofu", Adapter: "generic", InitializationOptions: terraformOptions(), Environment: map[string]string{"TF_IN_AUTOMATION": "1", "CHECKPOINT_DISABLE": "1"}},
		{Name: "terraform-vars", Command: []string{"tofu-ls", "serve"}, Files: []string{"**/*.tfvars", "**/*.tfvars.json"}, LanguageID: "opentofu-vars", Adapter: "generic", InitializationOptions: terraformOptions(), Environment: map[string]string{"TF_IN_AUTOMATION": "1", "CHECKPOINT_DISABLE": "1"}},
	}
}

// Load reads only the supplied operator path. Named entries replace a default
// profile completely; partial inheritance could retain surprising privileges.
func Load(configPath string) ([]Profile, error) {
	if configPath == "" {
		return Defaults(), nil
	}
	file, err := os.Open(configPath)
	if err != nil {
		return nil, errors.New("cannot open language-server configuration")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return nil, errors.New("cannot read language-server configuration")
	}
	if len(data) > maxConfigBytes {
		return nil, errors.New("language-server configuration exceeds the 1 MiB limit")
	}
	return parse(data)
}

func parse(data []byte) ([]Profile, error) {
	var config struct {
		LanguageServers map[string]Profile `yaml:"languageServers"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return nil, errors.New("invalid language-server YAML; check field names, duplicate keys, and value types")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("language-server configuration must contain exactly one YAML document")
	}
	if config.LanguageServers == nil {
		return nil, errors.New("language-server configuration requires a languageServers mapping")
	}
	if len(config.LanguageServers) > maxProfiles {
		return nil, errors.New("language-server configuration supports at most 32 named profiles")
	}
	names := make([]string, 0, len(config.LanguageServers))
	for name := range config.LanguageServers {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := make([]Profile, 0, len(names)+3)
	for _, name := range names {
		profile := config.LanguageServers[name]
		profile.Name = name
		if err := validateProfile(profile); err != nil {
			return nil, err
		}
		if !profile.Disabled {
			profiles = append(profiles, profile)
		}
	}
	for _, profile := range Defaults() {
		if _, overridden := config.LanguageServers[profile.Name]; !overridden {
			profiles = append(profiles, profile)
		}
	}
	if err := Validate(profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

// Validate applies the same boundary to profiles constructed by Go callers.
func Validate(profiles []Profile) error {
	if len(profiles) > maxProfiles {
		return errors.New("at most 32 language-server profiles may be active")
	}
	names := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		if err := validateProfile(profile); err != nil {
			return err
		}
		if names[profile.Name] {
			return errors.New("language-server profile names must be unique")
		}
		names[profile.Name] = true
	}
	return nil
}

var tokenPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
var environmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

func validateProfile(profile Profile) error {
	if !tokenPattern.MatchString(profile.Name) {
		return errors.New("language-server profile names must be safe tokens of at most 64 characters")
	}
	fail := func(reason string) error { return fmt.Errorf("language-server profile %s: %s", profile.Name, reason) }
	if profile.Disabled {
		return nil
	}
	if len(profile.Command) == 0 || len(profile.Command) > 32 || strings.TrimSpace(profile.Command[0]) == "" {
		return fail("command must contain an executable and at most 31 arguments")
	}
	commandBytes := 0
	for _, argument := range profile.Command {
		commandBytes += len(argument)
		if !safeText(argument, 4096) || commandBytes > 16*1024 {
			return fail("command arguments exceed bounds or contain control characters")
		}
	}
	executable := profile.Command[0]
	if strings.ContainsAny(executable, `/\`) && (!filepath.IsAbs(executable) || filepath.Clean(executable) != executable) {
		return fail("command executable must be a bare name or a clean absolute path")
	}
	if executable == "." || executable == ".." || executable == "/" {
		return fail("command must name an executable")
	}
	if !tokenPattern.MatchString(profile.LanguageID) {
		return fail("languageId must be a nonempty token of at most 64 characters")
	}
	if len(profile.Files) == 0 || len(profile.Files) > 32 {
		return fail("files must contain between 1 and 32 glob patterns")
	}
	for _, pattern := range profile.Files {
		if !validGlob(pattern) {
			return fail("files must use bounded relative glob patterns with *, **, or ?")
		}
	}
	if profile.Adapter != "" && profile.Adapter != "generic" && profile.Adapter != "go" {
		return fail("adapter must be generic or go")
	}
	if len(profile.RootMarkers) > 16 || (profile.RootRequired && len(profile.RootMarkers) == 0) {
		return fail("rootMarkers supports at most 16 names and is required when rootRequired is true")
	}
	for _, marker := range profile.RootMarkers {
		if marker == "" || marker == "." || marker == ".." || !safeText(marker, 255) || strings.ContainsAny(marker, `/\`) {
			return fail("rootMarkers must contain basenames without directory components")
		}
	}
	if len(profile.RuntimePaths) > 64 {
		return fail("runtimePaths supports at most 64 paths")
	}
	for _, runtimePath := range profile.RuntimePaths {
		if !validRuntimePath(runtimePath) {
			return fail("runtimePaths must be clean absolute paths that do not expose the entire home directory or filesystem")
		}
	}
	if len(profile.Environment) > 32 {
		return fail("environment supports at most 32 entries")
	}
	for key, value := range profile.Environment {
		if !environmentPattern.MatchString(key) || protectedEnvironment(key) || !safeText(value, 4096) {
			return fail("environment contains a protected variable, invalid name, or oversized/control-character value")
		}
	}
	if !validJSON(profile.InitializationOptions) || !validJSON(profile.Settings) {
		return fail("settings and initializationOptions must be JSON-compatible objects within 64 KiB and 16 levels")
	}
	return nil
}

func safeText(value string, limit int) bool {
	if len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validGlob(pattern string) bool {
	if pattern == "" || !safeText(pattern, 256) || strings.ContainsAny(pattern, `\[]{}:`) || path.IsAbs(pattern) || !doublestar.ValidatePattern(pattern) {
		return false
	}
	for _, component := range strings.Split(pattern, "/") {
		if component == "" || component == "." || component == ".." || (strings.Contains(component, "**") && component != "**") {
			return false
		}
	}
	return true
}

func validRuntimePath(value string) bool {
	if !safeText(value, 4096) || !filepath.IsAbs(value) || filepath.Clean(value) != value || broadRuntimePath(value) {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	home = filepath.Clean(home)
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	if resolved, err := filepath.EvalSymlinks(value); err == nil {
		value = resolved
	}
	if broadRuntimePath(value) {
		return false
	}
	relative, err := filepath.Rel(value, home)
	return err == nil && relative != "." && (relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func broadRuntimePath(value string) bool {
	switch value {
	case "/", "/opt", "/opt/homebrew", "/opt/homebrew/Cellar", "/opt/homebrew/opt", "/usr", "/usr/local", "/var", "/private", "/private/var", "/tmp", "/private/tmp", "/Users", "/home", "/root", "/etc", "/private/etc", "/run", "/var/run", "/private/var/run":
		return true
	}
	return false
}

func protectedEnvironment(key string) bool {
	key = strings.ToUpper(key)
	for _, prefix := range []string{"XDG_", "GO", "CGO_", "LD_", "DYLD_", "NODE_", "NPM_CONFIG_", "PYTHON", "RUBY", "GEM_", "PERL", "DOTNET_", "COMPLUS_", "CORECLR_", "FJORD_"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	switch key {
	case "HOME", "USERPROFILE", "PATH", "TMPDIR", "TMP", "TEMP", "TEMP_DIR", "BASH_ENV", "ENV", "ZDOTDIR", "SHELLOPTS", "BASHOPTS", "IFS", "CDPATH", "PS4", "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "_JAVA_OPTIONS", "JAVA_OPTS", "CLASSPATH", "DENO_DIR", "GCONV_PATH", "LOCPATH", "GLIBC_TUNABLES", "OPENSSL_CONF", "OPENSSL_MODULES":
		return true
	}
	return false
}

func validJSON(value map[string]any) bool {
	data, err := json.Marshal(value)
	if err != nil || len(data) > maxJSONBytes {
		return false
	}
	var decoded any
	if json.Unmarshal(data, &decoded) != nil {
		return false
	}
	var bounded func(any, int) bool
	bounded = func(node any, depth int) bool {
		if depth > 16 {
			return false
		}
		switch node := node.(type) {
		case map[string]any:
			for _, child := range node {
				if !bounded(child, depth+1) {
					return false
				}
			}
		case []any:
			for _, child := range node {
				if !bounded(child, depth+1) {
					return false
				}
			}
		}
		return true
	}
	return bounded(decoded, 0)
}

// Match selects the first enabled profile for a repository-relative file path.
func Match(profiles []Profile, filePath string) (Profile, bool) {
	if filePath == "" || !safeText(filePath, 4096) || path.IsAbs(filePath) || strings.Contains(filePath, `\`) || path.Clean(filePath) != filePath || filePath == "." || filePath == ".." || strings.HasPrefix(filePath, "../") {
		return Profile{}, false
	}
	for _, profile := range profiles {
		if profile.Disabled {
			continue
		}
		for _, pattern := range profile.Files {
			if !validGlob(pattern) {
				continue
			}
			if matches, err := doublestar.Match(pattern, filePath); err == nil && matches {
				return profile, true
			}
		}
	}
	return Profile{}, false
}
