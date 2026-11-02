package navigation

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProfileSchemasLifecycle(t *testing.T) {
	profile := genericProfile(t)
	profile.Settings = map[string]any{"yaml": map[string]any{"schemas": map[string]any{
		"fjord-schema://github-workflow.json": []string{".github/workflows/*.yml"},
	}}}
	profile.InitializationOptions = map[string]any{"schema": "fjord-schema://kubernetes.json"}
	profile.RuntimePaths = []string{"/usr/share"}
	before := profile.Settings
	parent := t.TempDir()
	prepared, cleanup, err := prepareProfileSchemas(parent, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(prepared.RuntimePaths) != 2 || !reflect.DeepEqual(profile.RuntimePaths, []string{"/usr/share"}) {
		t.Fatal("runtime paths mutated or bundle omitted")
	}
	bundle := prepared.RuntimePaths[1]
	if _, err := os.Stat(filepath.Join(bundle, "github-workflow.json")); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prepared.InitializationOptions["schema"].(string), "file://") {
		t.Fatal("initialization schema was not resolved")
	}
	if !reflect.DeepEqual(profile.Settings, before) || profile.InitializationOptions["schema"] != "fjord-schema://kubernetes.json" {
		t.Fatal("operator profile mutated")
	}
	cleanup()
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Fatal("schema directory was not removed")
	}
}

func TestProfileSchemasSkipAndFailureCleanup(t *testing.T) {
	parent := t.TempDir()
	profile := genericProfile(t)
	_, cleanup, err := prepareProfileSchemas(parent, profile)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatal("profile without schemas created files")
	}
	profile.Settings = map[string]any{"schema": "fjord-schema://unknown.json"}
	_, cleanup, err = prepareProfileSchemas(parent, profile)
	cleanup()
	if err == nil {
		t.Fatal("unknown schema accepted")
	}
	entries, err = os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed schema expansion left files behind")
	}
}
