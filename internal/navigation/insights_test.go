package navigation

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func insightFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	for name, text := range map[string]string{"main.go": "alpha \U0001f41f beta\r\nsecond line\n", "other.go": "other function\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	uri := func(name string) string { return (&url.URL{Scheme: "file", Path: filepath.Join(root, name)}).String() }
	return root, uri("main.go"), uri("other.go")
}

func insightJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func insightRangeJSON(startLine, startCharacter, endLine, endCharacter int) map[string]any {
	return map[string]any{"start": map[string]int{"line": startLine, "character": startCharacter}, "end": map[string]int{"line": endLine, "character": endCharacter}}
}

func insightSymbolJSON(uri, name string, r map[string]any) map[string]any {
	return map[string]any{"name": name, "kind": 12, "uri": uri, "range": r, "selectionRange": r}
}

func TestInsightDiagnosticsPullAndPush(t *testing.T) {
	root, _, _ := insightFixture(t)
	entries := []any{map[string]any{"range": insightRangeJSON(0, 6, 0, 8), "message": "Check this symbol", "severity": 2, "source": "server", "code": 123}}
	for _, payload := range []any{entries, map[string]any{"kind": "full", "items": entries}} {
		result, err := decodeInsights(root, "main.go", "diagnostics", insightJSON(t, payload))
		if err != nil || len(result.Items) != 1 {
			t.Fatalf("diagnostics: %+v, %v", result, err)
		}
		item := result.Items[0]
		if item.Kind != "warning" || item.Label != "Check this symbol" || item.Detail != "server / 123" || item.Location.Character != 6 || item.Location.EndCharacter != 8 {
			t.Fatalf("incorrect diagnostic: %+v", item)
		}
	}
	result, err := decodeInsights(root, "main.go", "diagnostics", json.RawMessage(`{"kind":"unchanged","resultId":"id"}`))
	if err != nil || result.Message == "" || len(result.Items) != 0 {
		t.Fatalf("unchanged diagnostics: %+v, %v", result, err)
	}
}

func TestInsightHierarchyUsesActualSourceFiles(t *testing.T) {
	root, mainURI, otherURI := insightFixture(t)
	other := insightSymbolJSON(otherURI, "caller", insightRangeJSON(0, 0, 0, 14))
	incoming := []any{map[string]any{"from": other, "fromRanges": []any{insightRangeJSON(0, 13, 0, 14)}}}
	result, err := decodeInsights(root, "main.go", "incomingCalls", insightJSON(t, incoming))
	if err != nil || result.Items[0].Location.Path != "other.go" || result.Items[0].Children[0].Location.Path != "other.go" {
		t.Fatalf("incoming calls: %+v, %v", result, err)
	}
	outgoing := []any{map[string]any{"to": insightSymbolJSON(mainURI, "callee", insightRangeJSON(0, 0, 0, 5)), "fromUri": otherURI, "fromRanges": []any{insightRangeJSON(0, 13, 0, 14)}}}
	result, err = decodeInsights(root, "main.go", "outgoingCalls", insightJSON(t, outgoing))
	if err != nil || result.Items[0].Location.Path != "main.go" || result.Items[0].Children[0].Location.Path != "other.go" {
		t.Fatalf("outgoing calls ignored prepared URI: %+v, %v", result, err)
	}
	for _, method := range []string{"supertypes", "subtypes"} {
		result, err := decodeInsights(root, "main.go", method, insightJSON(t, []any{other}))
		if err != nil || result.Items[0].Label != "caller" || result.Items[0].Kind != "function" {
			t.Fatalf("%s: %+v, %v", method, result, err)
		}
	}
}

func TestInsightDocumentAndWorkspaceSymbols(t *testing.T) {
	root, mainURI, otherURI := insightFixture(t)
	child := map[string]any{"name": "child", "kind": 13, "range": insightRangeJSON(1, 0, 1, 11), "selectionRange": insightRangeJSON(1, 0, 1, 6)}
	parent := map[string]any{"name": "parent", "detail": "scope", "kind": 5, "range": insightRangeJSON(0, 0, 1, 11), "selectionRange": insightRangeJSON(0, 0, 0, 5), "children": []any{child}}
	result, err := decodeInsights(root, "main.go", "documentSymbols", insightJSON(t, []any{parent}))
	if err != nil || result.Items[0].Children[0].Label != "child" || result.Items[0].Children[0].Location.Line != 1 {
		t.Fatalf("nested symbols: %+v, %v", result, err)
	}
	flat := []any{map[string]any{"name": "flat", "kind": 12, "containerName": "package", "location": map[string]any{"uri": mainURI, "range": insightRangeJSON(0, 0, 0, 5)}}}
	result, err = decodeInsights(root, "main.go", "documentSymbols", insightJSON(t, flat))
	if err != nil || result.Items[0].Detail != "package" {
		t.Fatalf("flat symbols: %+v, %v", result, err)
	}
	workspace := []any{map[string]any{"name": "unresolved", "kind": 12, "location": map[string]string{"uri": otherURI}}}
	result, err = decodeInsights(root, "main.go", "workspaceSymbols", insightJSON(t, workspace))
	if err != nil || result.Items[0].Location.Path != "other.go" || result.Items[0].Location.Line != 0 || result.Items[0].Location.Character != 0 {
		t.Fatalf("URI-only workspace symbol: %+v, %v", result, err)
	}
}

func TestInsightHighlightsAndInlayLabels(t *testing.T) {
	root, _, _ := insightFixture(t)
	highlights := []any{map[string]any{"range": insightRangeJSON(0, 6, 0, 8), "kind": 3}}
	result, err := decodeInsights(root, "main.go", "documentHighlights", insightJSON(t, highlights))
	if err != nil || result.Highlights[0].Kind != "write" || result.Highlights[0].Location.EndCharacter != 8 {
		t.Fatalf("highlights: %+v, %v", result, err)
	}
	hints := []any{
		map[string]any{"position": map[string]int{"line": 0, "character": 8}, "label": ": Fish", "kind": 1, "paddingLeft": true},
		map[string]any{"position": map[string]int{"line": 1, "character": 0}, "label": []any{map[string]any{"value": "name", "command": map[string]string{"command": "never.execute"}}, map[string]string{"value": ":"}}, "kind": 2, "paddingRight": true},
	}
	result, err = decodeInsights(root, "main.go", "inlayHints", insightJSON(t, hints))
	if err != nil || len(result.Hints) != 2 || result.Hints[0].Label != ": Fish" || result.Hints[1].Label != "name:" || !result.Hints[0].PaddingLeft || !result.Hints[1].PaddingRight {
		t.Fatalf("inlay hints: %+v, %v", result, err)
	}
}

func TestInsightRejectsInvalidUTF16RangesAndExternalLocations(t *testing.T) {
	root, mainURI, _ := insightFixture(t)
	for _, r := range []any{
		insightRangeJSON(0, 7, 0, 8),
		insightRangeJSON(0, 8, 0, 7),
		insightRangeJSON(0, 0, 0, 99),
		insightRangeJSON(99, 0, 99, 0),
		insightRangeJSON(-1, 0, 0, 0),
		map[string]any{"start": map[string]int{"line": 0}, "end": map[string]int{"line": 0, "character": 0}},
		nil,
	} {
		payload := []any{map[string]any{"name": "bad", "location": map[string]any{"uri": mainURI, "range": r}}}
		if _, err := decodeInsights(root, "main.go", "documentSymbols", insightJSON(t, payload)); err == nil {
			t.Fatalf("accepted invalid range: %+v", r)
		}
	}
	for _, uri := range []string{"https://example.com/source", "file://remote/path", mainURI + "?secret=value", mainURI + "#fragment"} {
		payload := []any{map[string]any{"name": "outside", "location": map[string]any{"uri": uri, "range": insightRangeJSON(0, 0, 0, 0)}}}
		result, err := decodeInsights(root, "main.go", "workspaceSymbols", insightJSON(t, payload))
		if err == nil || len(result.Items) != 0 || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), uri) {
			t.Fatalf("unsafe external location result: %+v, %v", result, err)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret.go")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	linked := (&url.URL{Scheme: "file", Path: filepath.Join(root, "linked.go")}).String()
	if _, err := decodeInsights(root, "main.go", "workspaceSymbols", insightJSON(t, []any{map[string]any{"name": "link", "location": map[string]string{"uri": linked}}})); err == nil {
		t.Fatal("symlink outside revision accepted")
	}
}

func TestInsightRenamePreviewPreservesUnicodeAndCRLF(t *testing.T) {
	root, mainURI, otherURI := insightFixture(t)
	payload := map[string]any{"changes": map[string]any{
		otherURI: []any{map[string]any{"range": insightRangeJSON(0, 0, 0, 5), "newText": "updated"}},
		mainURI: []any{
			map[string]any{"range": insightRangeJSON(0, 9, 1, 6), "newText": "replacement"},
			map[string]any{"range": insightRangeJSON(0, 6, 0, 8), "newText": "fish"},
		},
	}}
	result, err := decodeInsights(root, "main.go", "rename", insightJSON(t, payload))
	if err != nil || len(result.Edits) != 3 {
		t.Fatalf("rename preview: %+v, %v", result, err)
	}
	if result.Edits[0].OldText != "\U0001f41f" || result.Edits[1].OldText != "beta\r\nsecond" || result.Edits[2].Location.Path != "other.go" {
		t.Fatalf("incorrect old text or edit order: %+v", result.Edits)
	}
	data, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil || string(data) != "alpha \U0001f41f beta\r\nsecond line\n" {
		t.Fatal("preview modified source")
	}
}

func TestInsightVersionedDocumentEdits(t *testing.T) {
	root, mainURI, _ := insightFixture(t)
	for _, version := range []any{nil, 1} {
		payload := map[string]any{"documentChanges": []any{map[string]any{"textDocument": map[string]any{"uri": mainURI, "version": version}, "edits": []any{map[string]any{"range": insightRangeJSON(0, 0, 0, 5), "newText": ""}}}}}
		result, err := decodeInsights(root, "main.go", "rename", insightJSON(t, payload))
		if err != nil || len(result.Edits) != 1 || result.Edits[0].OldText != "alpha" || result.Edits[0].NewText != "" {
			t.Fatalf("versioned edits: %+v, %v", result, err)
		}
	}
}

func TestInsightAdjacentEditsAndEmptyInsertionsRemainValid(t *testing.T) {
	root, mainURI, _ := insightFixture(t)
	payload := map[string]any{"changes": map[string]any{mainURI: []any{
		map[string]any{"range": insightRangeJSON(0, 5, 0, 6), "newText": "_"},
		map[string]any{"range": insightRangeJSON(0, 0, 0, 5), "newText": "name"},
		map[string]any{"range": insightRangeJSON(1, 0, 1, 0), "newText": "prefix "},
	}}}
	result, err := decodeInsights(root, "main.go", "rename", insightJSON(t, payload))
	if err != nil || len(result.Edits) != 3 || result.Edits[0].OldText != "alpha" || result.Edits[1].OldText != " " || result.Edits[2].OldText != "" {
		t.Fatalf("valid adjacent edits rejected: %+v, %v", result, err)
	}
}

func TestInsightSelectionAndOutgoingCallerBoundaries(t *testing.T) {
	root, mainURI, _ := insightFixture(t)
	symbol := insightSymbolJSON(mainURI, "symbol", insightRangeJSON(0, 0, 0, 5))
	symbol["selectionRange"] = insightRangeJSON(0, 6, 0, 8)
	if _, err := decodeInsights(root, "main.go", "documentSymbols", insightJSON(t, []any{symbol})); err == nil {
		t.Fatal("selection outside symbol range accepted")
	}
	payload := []any{map[string]any{"to": insightSymbolJSON(mainURI, "target", insightRangeJSON(0, 0, 0, 5)), "fromUri": "file:///outside.go", "fromRanges": []any{insightRangeJSON(0, 0, 0, 0)}}}
	result, err := decodeInsights(root, "main.go", "outgoingCalls", insightJSON(t, payload))
	if err != nil || len(result.Items) != 1 || len(result.Items[0].Children) != 0 || result.Message != "Omitted 1 location outside this revision workspace." {
		t.Fatalf("local target lost or external prepared caller exposed: %+v, %v", result, err)
	}
}

func TestInsightRejectsMalformedHintLabels(t *testing.T) {
	root, _, _ := insightFixture(t)
	for _, label := range []any{nil, "", []any{}, []any{map[string]any{"tooltip": "missing value"}}, []any{map[string]any{"value": nil}}} {
		payload := []any{map[string]any{"position": map[string]int{"line": 0, "character": 0}, "label": label}}
		if _, err := decodeInsights(root, "main.go", "inlayHints", insightJSON(t, payload)); err == nil {
			t.Fatalf("invalid hint label accepted: %+v", label)
		}
	}
}

func TestInsightUnsafeEditsRejectRenameAndDisableEntireAction(t *testing.T) {
	root, mainURI, _ := insightFixture(t)
	safe := map[string]any{"range": insightRangeJSON(0, 0, 0, 5), "newText": "safe"}
	doc := func(version any, edits []any) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": mainURI, "version": version}, "edits": edits}
	}
	for name, payload := range map[string]any{
		"outside":            map[string]any{"changes": map[string]any{mainURI: []any{safe}, "file:///etc/passwd": []any{safe}}},
		"create":             map[string]any{"documentChanges": []any{doc(1, []any{safe}), map[string]any{"kind": "create", "uri": mainURI + "new"}}},
		"delete":             map[string]any{"documentChanges": []any{doc(1, []any{safe}), map[string]any{"kind": "delete", "uri": mainURI}}},
		"rename":             map[string]any{"documentChanges": []any{doc(1, []any{safe}), map[string]any{"kind": "rename", "oldUri": mainURI, "newUri": mainURI + "new"}}},
		"version":            map[string]any{"documentChanges": []any{doc(2, []any{safe})}},
		"overlap":            map[string]any{"changes": map[string]any{mainURI: []any{safe, map[string]any{"range": insightRangeJSON(0, 3, 0, 8), "newText": "overlap"}}}},
		"same-point-inserts": map[string]any{"changes": map[string]any{mainURI: []any{map[string]any{"range": insightRangeJSON(0, 0, 0, 0), "newText": "a"}, map[string]any{"range": insightRangeJSON(0, 0, 0, 0), "newText": "b"}}}},
		"invalid-utf16":      map[string]any{"changes": map[string]any{mainURI: []any{safe, map[string]any{"range": insightRangeJSON(0, 7, 0, 8), "newText": "bad"}}}},
		"missing-new-text":   map[string]any{"changes": map[string]any{mainURI: []any{safe, map[string]any{"range": insightRangeJSON(1, 0, 1, 1)}}}},
		"annotation":         map[string]any{"changes": map[string]any{mainURI: []any{map[string]any{"range": insightRangeJSON(0, 0, 0, 5), "newText": "x", "annotationId": "confirmation"}}}},
		"mixed-encodings":    map[string]any{"changes": map[string]any{mainURI: []any{safe}}, "documentChanges": []any{doc(1, []any{safe})}},
		"unknown-edit-shape": map[string]any{"changes": map[string]any{mainURI: []any{map[string]any{"insert": insightRangeJSON(0, 0, 0, 5), "newText": "x"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := decodeInsights(root, "main.go", "rename", insightJSON(t, payload))
			if err == nil || len(result.Edits) != 0 {
				t.Fatalf("unsafe rename preview: %+v, %v", result, err)
			}
			result, err = decodeInsights(root, "main.go", "codeActions", insightJSON(t, []any{map[string]any{"title": "Fix", "edit": payload}}))
			if err != nil || len(result.Actions) != 1 || result.Actions[0].DisabledReason == "" || len(result.Actions[0].Edits) != 0 {
				t.Fatalf("partially valid action escaped: %+v, %v", result, err)
			}
		})
	}
}

func TestInsightActionCommandAndDisabledPreviews(t *testing.T) {
	root, mainURI, _ := insightFixture(t)
	edit := map[string]any{"changes": map[string]any{mainURI: []any{map[string]any{"range": insightRangeJSON(0, 0, 0, 5), "newText": "new"}}}}
	actions := []any{
		map[string]any{"title": "Editable", "kind": "quickfix", "edit": edit},
		map[string]any{"title": "Command only", "command": "server.execute", "arguments": []any{"secret"}},
		map[string]any{"title": "Edit plus command", "edit": edit, "command": map[string]string{"title": "Execute", "command": "server.execute"}},
		map[string]any{"title": "Disabled", "edit": edit, "disabled": map[string]string{"reason": "Not available here"}},
		map[string]any{"title": "Unresolved", "data": "private"},
	}
	result, err := decodeInsights(root, "main.go", "codeActions", insightJSON(t, actions))
	if err != nil || len(result.Actions) != 5 || len(result.Actions[0].Edits) != 1 || result.Actions[0].DisabledReason != "" {
		t.Fatalf("code actions: %+v, %v", result, err)
	}
	for _, action := range result.Actions[1:] {
		if action.DisabledReason == "" || len(action.Edits) != 0 {
			t.Fatalf("unsafe action active: %+v", action)
		}
	}
	if result.Actions[3].DisabledReason != "Not available here" {
		t.Fatal("lost server disabled reason")
	}
}

func TestInsightLimitsAndAtomicFailure(t *testing.T) {
	root, mainURI, _ := insightFixture(t)
	if _, err := decodeInsights(root, "main.go", "diagnostics", json.RawMessage(strings.Repeat(" ", maxInsightBytes+1))); err == nil {
		t.Fatal("oversized raw response accepted")
	}
	symbol := insightSymbolJSON(mainURI, "symbol", insightRangeJSON(0, 0, 0, 5))
	items := make([]any, maxInsightItems+1)
	for i := range items {
		items[i] = symbol
	}
	if _, err := decodeInsights(root, "main.go", "documentSymbols", insightJSON(t, items)); err == nil {
		t.Fatal("unbounded item count accepted")
	}
	nested := symbol
	for range maxInsightDepth + 2 {
		nested = map[string]any{"name": "nested", "uri": mainURI, "range": insightRangeJSON(0, 0, 0, 5), "children": []any{nested}}
	}
	if _, err := decodeInsights(root, "main.go", "documentSymbols", insightJSON(t, []any{nested})); err == nil {
		t.Fatal("unbounded hierarchy depth accepted")
	}
	large := map[string]any{"range": insightRangeJSON(0, 0, 0, 5), "message": strings.Repeat("x", maxInsightText+1)}
	if _, err := decodeInsights(root, "main.go", "diagnostics", insightJSON(t, []any{large})); err == nil {
		t.Fatal("unbounded text accepted")
	}
	invalid := insightSymbolJSON(mainURI, "invalid", insightRangeJSON(0, 7, 0, 8))
	result, err := decodeInsights(root, "main.go", "documentSymbols", insightJSON(t, []any{symbol, invalid}))
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("partial result escaped: %+v, %v", result, err)
	}
	for _, method := range []string{"diagnostics", "incomingCalls", "outgoingCalls", "supertypes", "subtypes", "documentSymbols", "workspaceSymbols", "documentHighlights", "inlayHints", "codeActions", "rename"} {
		result, err := decodeInsights(root, "main.go", method, json.RawMessage("null"))
		if err != nil || !reflect.DeepEqual(result.Locations, []Location{}) {
			t.Errorf("null %s response failed: %+v, %v", method, result, err)
		}
	}
}

func TestInsightMixedLocalAndExternalTargetsPreserveLocalResults(t *testing.T) {
	root, mainURI, _ := insightFixture(t)
	local := insightSymbolJSON(mainURI, "local", insightRangeJSON(0, 0, 0, 5))
	external := insightSymbolJSON("file:///toolchain/src/stdlib.go", "external", insightRangeJSON(0, 0, 0, 5))
	for _, method := range []string{"incomingCalls", "outgoingCalls", "supertypes", "subtypes", "documentSymbols", "workspaceSymbols"} {
		t.Run(method, func(t *testing.T) {
			var payload any = []any{external, local}
			switch method {
			case "incomingCalls":
				payload = []any{map[string]any{"from": external, "fromRanges": []any{insightRangeJSON(0, 0, 0, 5)}}, map[string]any{"from": local, "fromRanges": []any{insightRangeJSON(0, 0, 0, 5)}}}
			case "outgoingCalls":
				payload = []any{map[string]any{"to": external, "fromRanges": []any{insightRangeJSON(0, 0, 0, 5)}}, map[string]any{"to": local, "fromRanges": []any{insightRangeJSON(0, 0, 0, 5)}}}
			case "workspaceSymbols":
				payload = []any{map[string]any{"name": "external", "location": map[string]string{"uri": "file:///toolchain/src/stdlib.go"}}, map[string]any{"name": "local", "location": map[string]string{"uri": mainURI}}}
			}
			result, err := decodeInsights(root, "main.go", method, insightJSON(t, payload))
			if err != nil || len(result.Items) != 1 || result.Items[0].Label != "local" || result.Items[0].Location.Path != "main.go" || result.Message != "Omitted 1 location outside this revision workspace." {
				t.Fatalf("mixed %s results: %+v, %v", method, result, err)
			}
		})
	}
	result, err := decodeInsights(root, "main.go", "supertypes", insightJSON(t, []any{external}))
	if err != nil || len(result.Items) != 0 || result.Message == "" {
		t.Fatalf("all-external result should explain omission: %+v, %v", result, err)
	}
	malformed := insightSymbolJSON("file:///toolchain/src/stdlib.go", "external", insightRangeJSON(0, 0, 0, 5))
	malformed["selectionRange"] = insightRangeJSON(-1, 0, 0, 0)
	if _, err := decodeInsights(root, "main.go", "subtypes", insightJSON(t, []any{local, malformed})); err == nil {
		t.Fatal("malformed external selection range was silently skipped")
	}
}
