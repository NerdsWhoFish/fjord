package navigation

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestGoPartialPackageMetadata(t *testing.T) {
	const usable = `{"GoFiles":["valid.go"]}`
	const missingEmbed = `{"GoFiles":["assets.go"],"Incomplete":true,"Error":{"Err":"pattern web/dist: no matching files found"}}`
	const missingDependency = `{"GoFiles":["main.go"],"Incomplete":true,"DepsErrors":[{"Err":"no required module provides package example.com/missing"}]}`
	for _, test := range []struct {
		name, metadata, wantError string
		requested, complete       int
	}{
		{"broken-before-usable", missingEmbed + usable, "", 2, 1},
		{"usable-before-broken", usable + missingEmbed, "", 2, 1},
		{"test-only-package", missingEmbed + `{"TestGoFiles":["valid_test.go"]}`, "", 2, 1},
		{"external-test-only-package", missingEmbed + `{"XTestGoFiles":["valid_test.go"]}`, "", 2, 1},
		{"only-dependency-is-usable", `{"DepOnly":true,"GoFiles":["fmt.go"]}` + missingDependency, "vendored dependencies", 1, 0},
		{"all-incomplete", missingDependency + missingEmbed, "vendored dependencies", 2, 0},
		{"empty-pseudo-package", `{}`, "could not be loaded", 1, 0},
		{"only-dependencies", `{"DepOnly":true,"GoFiles":["fmt.go"]}`, "no Go packages", 0, 0},
		{"malformed-after-usable", usable + `{`, "invalid Go package metadata", 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			summary, err := inspectGoPackages(strings.NewReader(test.metadata))
			if summary.requested != test.requested || summary.usable != test.complete {
				t.Fatalf("unexpected package summary: %+v", summary)
			}
			if test.wantError == "" {
				if err != nil || summary.incomplete != 1 {
					t.Fatalf("partial workspace rejected: %+v %v", summary, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("wanted %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestGoplsPartialWorkspace(t *testing.T) {
	if os.Getenv("FJORD_TEST_GOPLS") != "1" {
		t.Skip("opt-in gopls and sandbox test")
	}
	for _, test := range []struct {
		name, brokenPath, brokenSource string
	}{
		{
			"missing-generated-embed",
			"repo/assets.go",
			"package assets\nimport \"embed\"\n//go:embed web/dist\nvar Assets embed.FS\n",
		},
		{
			"partial-source-package",
			"repo/valid/broken.go",
			"package valid\nvar Broken = DoesNotExist\n",
		},
		{
			"test-only-package",
			"repo/assets.go",
			"package assets\nimport \"embed\"\n//go:embed web/dist\nvar Assets embed.FS\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := New()
			defer m.Close()
			sourcePath := "valid/valid.go"
			if test.name == "test-only-package" {
				sourcePath = "valid/valid_test.go"
			}
			archive := archiveBytes(t,
				archiveEntry{name: "repo/go.mod", body: "module example.com/partial\n\ngo 1.24.0\n"},
				archiveEntry{name: test.brokenPath, body: test.brokenSource},
				archiveEntry{name: "repo/" + sourcePath, body: "package valid\nfunc Target() int { return 1 }\nfunc Call() int { return Target() }\n"},
			)
			id, err := m.Prepare(context.Background(), "example", "partial", strings.Repeat("e", 40), bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			result, err := m.Query(context.Background(), id, sourcePath, "definition", 2, 26)
			if err != nil {
				t.Fatalf("usable package navigation failed: %v", err)
			}
			if len(result.Locations) != 1 || result.Locations[0].Path != sourcePath || result.Locations[0].Line != 1 {
				t.Fatalf("unexpected definition: %+v", result)
			}
		})
	}
}
