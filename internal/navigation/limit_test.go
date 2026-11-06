package navigation

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NerdsWhoFish/fjord/internal/lspconfig"
)

func TestTenantWorkspaceLimitEvictsUnusedSourceTrees(t *testing.T) {
	manager := NewWithLimit(lspconfig.Defaults(), 1)
	defer manager.Close()
	archive := archiveBytes(t, archiveEntry{name: "repo/main.go", body: "package main\n"})
	first, err := manager.Prepare(context.Background(), "acme", "one", strings.Repeat("a", 40), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Prepare(context.Background(), "acme", "two", strings.Repeat("b", 40), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.File(context.Background(), first, "main.go"); !errors.Is(err, ErrNotFound) {
		t.Fatal("tenant retained more source trees than its configured limit")
	}
	if source, err := manager.File(context.Background(), second, "main.go"); err != nil || source != "package main\n" {
		t.Fatal("tenant could not read its current source tree")
	}
}
