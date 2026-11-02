package schemas

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestBundleIntegrityAndReferences(t *testing.T) {
	expected := map[string]string{
		"kubernetes.json":      "3a2b7a214f7f5be3cd47d2ac7dd33664d8e3c96ad896672358196100fe3e069a",
		"_definitions.json":    "4755c5231489317fe4be800f1d0b2427c06b24da8328dfc4b0c565946683700e",
		"github-workflow.json": "d10c9f4656e1bd5bc6727e9b35080e017dc167154726fca93da33c7a6bd1c4f3",
		"tsconfig.json":        "9e9599c2e6021574483e3a802eb075c872be1f5b4185ad45da03d33edce3c2b1",
	}
	documents := make(map[string]any)
	for name, digest := range expected {
		data, err := assets.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if actual := fmt.Sprintf("%x", sha256.Sum256(data)); actual != digest {
			t.Fatalf("%s checksum changed: %s", name, actual)
		}
		var document any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		documents[name] = document
	}
	var visit func(string, any)
	visit = func(name string, value any) {
		switch value := value.(type) {
		case map[string]any:
			if ref, ok := value["$ref"].(string); ok {
				target, fragment, _ := strings.Cut(ref, "#")
				if target == "" {
					target = name
				}
				document, exists := documents[target]
				if !exists {
					t.Fatalf("%s has unbundled reference %q", name, ref)
				}
				if fragment != "" {
					if !strings.HasPrefix(fragment, "/") {
						t.Fatalf("%s has unsupported reference %q", name, ref)
					}
					for _, part := range strings.Split(fragment[1:], "/") {
						part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
						switch node := document.(type) {
						case map[string]any:
							var ok bool
							document, ok = node[part]
							if !ok {
								t.Fatalf("%s has missing reference %q", name, ref)
							}
						case []any:
							index, err := strconv.Atoi(part)
							if err != nil || index < 0 || index >= len(node) {
								t.Fatalf("%s has invalid array reference %q", name, ref)
							}
							document = node[index]
						default:
							t.Fatalf("%s has invalid reference %q", name, ref)
						}
					}
				}
			}
			for _, child := range value {
				visit(name, child)
			}
		case []any:
			for _, child := range value {
				visit(name, child)
			}
		}
	}
	for name, document := range documents {
		visit(name, document)
	}
}

func TestMaterialize(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "runtime with spaces")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	directory, err := Materialize(parent)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("private directory: info=%v err=%v", info, err)
	}
	entries, err := assets.ReadDir("assets")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := filepath.Join(directory, entry.Name())
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := assets.ReadFile("assets/" + entry.Name())
		if string(got) != string(want) {
			t.Fatalf("materialized asset mismatch: %s", entry.Name())
		}
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0400 {
			t.Fatalf("read-only asset: info=%v err=%v", info, err)
		}
	}
	if _, err := Materialize("relative"); err == nil {
		t.Fatal("relative parent accepted")
	}
}

func TestExpand(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "schema # bundle")
	key := Prefix + "kubernetes.json"
	input := map[string]any{
		"yaml": map[string]any{"schemas": map[string]any{key: []string{"**/k8s/*.yaml"}}},
		"json": []any{map[string]any{"url": Prefix + "tsconfig.json"}},
		"keep": "https://example.com/schema.json",
	}
	before, _ := json.Marshal(input)
	got, err := Expand(input, directory)
	if err != nil {
		t.Fatal(err)
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(directory, "kubernetes.json"))}).String()
	mapping := got["yaml"].(map[string]any)["schemas"].(map[string]any)
	if !reflect.DeepEqual(mapping[uri], []string{"**/k8s/*.yaml"}) {
		t.Fatalf("schema map key not resolved: %v", mapping)
	}
	if got["keep"] != input["keep"] {
		t.Fatal("unrelated URL changed")
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("configuration mutated")
	}
	for _, name := range []string{"../secret", "_definitions.json", "missing.json", "kubernetes.json#x"} {
		if _, err := Expand(map[string]any{"url": Prefix + name}, directory); err == nil {
			t.Fatalf("accepted unknown schema %q", name)
		}
	}
	if _, err := Expand(input, "relative"); err == nil {
		t.Fatal("relative directory accepted")
	}
}
