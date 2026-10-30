package navigation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

var reviewMethods = []string{
	"definition", "implementation", "references", "hover", "diagnostics",
	"incomingCalls", "outgoingCalls", "supertypes", "subtypes", "documentSymbols",
	"workspaceSymbols", "documentHighlights", "inlayHints", "codeActions", "rename",
}

var methodProviders = map[string]string{
	"definition": "definitionProvider", "implementation": "implementationProvider",
	"references": "referencesProvider", "hover": "hoverProvider", "diagnostics": "diagnosticProvider",
	"incomingCalls": "callHierarchyProvider", "outgoingCalls": "callHierarchyProvider",
	"supertypes": "typeHierarchyProvider", "subtypes": "typeHierarchyProvider",
	"documentSymbols": "documentSymbolProvider", "workspaceSymbols": "workspaceSymbolProvider",
	"documentHighlights": "documentHighlightProvider", "inlayHints": "inlayHintProvider",
	"codeActions": "codeActionProvider", "rename": "renameProvider",
}

func validateQueryOptions(method string, options QueryOptions) error {
	if len(options.Query) > 256 || !utf8.ValidString(options.Query) || strings.IndexFunc(options.Query, unicode.IsControl) >= 0 {
		return errors.New("symbol search must be at most 256 bytes without control characters")
	}
	if method == "rename" && (strings.TrimSpace(options.NewName) == "" || len(options.NewName) > 256 || !utf8.ValidString(options.NewName) || strings.IndexFunc(options.NewName, unicode.IsControl) >= 0) {
		return errors.New("rename preview requires a new name of at most 256 bytes without control characters")
	}
	return nil
}

func (w *workspace) inspect(ctx context.Context, name, uri, content, method string, line, character int, options QueryOptions) (Result, error) {
	document := map[string]string{"uri": uri}
	point := position{Line: line, Character: character}
	params := map[string]any{"textDocument": document, "position": point}
	var raw json.RawMessage
	var err error
	message := ""
	switch method {
	case "definition", "implementation", "references", "hover":
		if method == "references" {
			params["context"] = map[string]bool{"includeDeclaration": true}
		}
		if err = w.client.call(ctx, "textDocument/"+method, params, &raw); err != nil {
			return Result{}, err
		}
		return decodeResult(w.root, method, raw)
	case "diagnostics":
		raw, message, err = w.diagnosticsFor(ctx, uri, 2*time.Second)
	case "incomingCalls", "outgoingCalls", "supertypes", "subtypes":
		raw, message, err = w.hierarchy(ctx, method, params)
	case "documentSymbols":
		err = w.client.call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": document}, &raw)
	case "workspaceSymbols":
		err = w.client.call(ctx, "workspace/symbol", map[string]string{"query": options.Query}, &raw)
	case "documentHighlights":
		err = w.client.call(ctx, "textDocument/documentHighlight", params, &raw)
	case "inlayHints":
		lines := strings.Split(content, "\n")
		end := position{Line: len(lines) - 1, Character: len(utf16.Encode([]rune(strings.TrimSuffix(lines[len(lines)-1], "\r"))))}
		err = w.client.call(ctx, "textDocument/inlayHint", map[string]any{"textDocument": document, "range": sourceRange{End: end}}, &raw)
	case "codeActions":
		var diagnostics json.RawMessage
		diagnostics, _, err = w.diagnosticsFor(ctx, uri, 250*time.Millisecond)
		if err != nil {
			break
		}
		params = map[string]any{"textDocument": document, "range": sourceRange{Start: point, End: point}, "context": map[string]any{"diagnostics": diagnosticsAt(diagnostics, point), "triggerKind": 1}}
		err = w.client.call(ctx, "textDocument/codeAction", params, &raw)
		if err == nil && w.capabilities["resolveCodeAction"] {
			raw, err = w.resolveActions(ctx, raw)
		}
	case "rename":
		params["newName"] = options.NewName
		err = w.client.call(ctx, "textDocument/rename", params, &raw)
	}
	if err != nil {
		return Result{}, err
	}
	result, err := decodeInsights(w.root, name, method, raw)
	if message != "" {
		if result.Message != "" {
			result.Message += " "
		}
		result.Message += message
	}
	if err == nil && result.Message == "" {
		if method == "diagnostics" && len(result.Items) == 0 {
			result.Message = "The language server reported no diagnostics for this file."
		}
		if method == "inlayHints" && len(result.Hints) == 0 {
			result.Message = "No inlay hints returned. The language server may require enabling hints in its settings."
		}
	}
	return result, err
}

func (w *workspace) hierarchy(ctx context.Context, method string, params map[string]any) (json.RawMessage, string, error) {
	prepare, follow := "textDocument/prepareCallHierarchy", "callHierarchy/"+method
	if method == "supertypes" || method == "subtypes" {
		prepare, follow = "textDocument/prepareTypeHierarchy", "typeHierarchy/"+method
	}
	var prepared []json.RawMessage
	if err := w.client.call(ctx, prepare, params, &prepared); err != nil {
		return nil, "", err
	}
	if len(prepared) > 16 {
		return nil, "", errors.New("language server returned too many hierarchy roots")
	}
	results := make([]json.RawMessage, 0)
	outside := false
	for _, item := range prepared {
		var target struct {
			URI string `json:"uri"`
		}
		if json.Unmarshal(item, &target) != nil {
			return nil, "", errors.New("invalid hierarchy root")
		}
		if _, err := sourceLocation(w.root, target.URI, position{}, position{}); err != nil {
			outside = true
			continue
		}
		var items []json.RawMessage
		if err := w.client.call(ctx, follow, map[string]any{"item": item}, &items); err != nil {
			return nil, "", err
		}
		if len(results)+len(items) > 512 {
			return nil, "", errors.New("language server returned too many hierarchy results")
		}
		if method == "outgoingCalls" {
			for i, item := range items {
				var outgoing map[string]json.RawMessage
				if json.Unmarshal(item, &outgoing) != nil || outgoing == nil {
					return nil, "", errors.New("invalid outgoing call")
				}
				outgoing["fromUri"], _ = json.Marshal(target.URI)
				items[i], _ = json.Marshal(outgoing)
			}
		}
		results = append(results, items...)
	}
	raw, err := json.Marshal(results)
	if len(raw) > 4<<20 {
		return nil, "", errors.New("hierarchy response exceeds review limits")
	}
	message := ""
	if outside {
		message = "Hierarchy roots outside this revision were omitted."
	}
	return raw, message, err
}

func (w *workspace) resolveActions(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var actions []json.RawMessage
	if json.Unmarshal(raw, &actions) != nil || len(actions) > 64 {
		return nil, errors.New("invalid or oversized code-action response")
	}
	for i, action := range actions {
		var fields map[string]json.RawMessage
		if json.Unmarshal(action, &fields) != nil {
			return nil, errors.New("invalid code action")
		}
		var command string
		if hasValue(fields["edit"]) || hasValue(fields["disabled"]) || json.Unmarshal(fields["command"], &command) == nil {
			continue
		}
		if err := w.client.call(ctx, "codeAction/resolve", action, &actions[i]); err != nil {
			return nil, err
		}
	}
	return json.Marshal(actions)
}

func hasValue(raw json.RawMessage) bool {
	value := bytes.TrimSpace(raw)
	return len(value) > 0 && !bytes.Equal(value, []byte("null"))
}

func diagnosticsAt(raw json.RawMessage, point position) []json.RawMessage {
	var diagnostics []json.RawMessage
	_ = json.Unmarshal(raw, &diagnostics)
	selected := make([]json.RawMessage, 0)
	for _, diagnostic := range diagnostics {
		var item struct {
			Range sourceRange `json:"range"`
		}
		if json.Unmarshal(diagnostic, &item) != nil {
			continue
		}
		start, end := item.Range.Start, item.Range.End
		if (point.Line > start.Line || point.Line == start.Line && point.Character >= start.Character) && (point.Line < end.Line || point.Line == end.Line && point.Character <= end.Character) {
			selected = append(selected, diagnostic)
		}
	}
	return selected
}

const maxDiagnosticDocuments = 64
const maxDiagnosticBytes = 256 << 10

type diagnosticPublication struct {
	raw          json.RawMessage
	published    bool
	errorMessage string
	changed      chan struct{}
}

func (c *rpcClient) watchDiagnostics(uri string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.diagnostics[uri] != nil {
		return false
	}
	if len(c.diagnosticOrder) == maxDiagnosticDocuments {
		delete(c.diagnostics, c.diagnosticOrder[0])
		c.diagnosticOrder = c.diagnosticOrder[1:]
	}
	c.diagnosticOrder = append(c.diagnosticOrder, uri)
	c.diagnostics[uri] = &diagnosticPublication{changed: make(chan struct{})}
	return true
}

func (c *rpcClient) publishDiagnostics(params json.RawMessage) {
	var publication struct {
		URI         string          `json:"uri"`
		Version     *int            `json:"version"`
		Diagnostics json.RawMessage `json:"diagnostics"`
	}
	if json.Unmarshal(params, &publication) != nil || publication.Version != nil && *publication.Version != 1 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.diagnostics[publication.URI]
	if entry == nil {
		return
	}
	entry.raw, entry.errorMessage = nil, ""
	entry.published = true
	var diagnostics []json.RawMessage
	if len(publication.Diagnostics) > maxDiagnosticBytes || json.Unmarshal(publication.Diagnostics, &diagnostics) != nil || len(diagnostics) > 512 || bytes.Equal(bytes.TrimSpace(publication.Diagnostics), []byte("null")) {
		entry.errorMessage = "The server's diagnostic publication exceeds review limits or is invalid."
	} else {
		entry.raw = append(json.RawMessage(nil), publication.Diagnostics...)
	}
	close(entry.changed)
	entry.changed = make(chan struct{})
}

func (w *workspace) diagnosticsFor(ctx context.Context, uri string, wait time.Duration) (json.RawMessage, string, error) {
	if w.capabilities["pullDiagnostics"] {
		var report struct {
			Kind  string          `json:"kind"`
			Items json.RawMessage `json:"items"`
		}
		params := map[string]any{"textDocument": map[string]string{"uri": uri}}
		if w.client.diagnosticIdentifier != "" {
			params["identifier"] = w.client.diagnosticIdentifier
		}
		if err := w.client.call(ctx, "textDocument/diagnostic", params, &report); err != nil {
			return nil, "", err
		}
		if report.Kind != "full" || !hasValue(report.Items) {
			return nil, "", errors.New("language server returned no full diagnostic report")
		}
		return report.Items, "", nil
	}
	w.client.watchDiagnostics(uri)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		w.client.mu.Lock()
		entry := w.client.diagnostics[uri]
		if entry == nil {
			w.client.mu.Unlock()
			return json.RawMessage("[]"), "Diagnostic publication is no longer cached; reopen this revision to request it again.", nil
		}
		raw, published, message, changed := entry.raw, entry.published, entry.errorMessage, entry.changed
		w.client.mu.Unlock()
		if published {
			if message != "" {
				return nil, "", errors.New(message)
			}
			return raw, "", nil
		}
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-w.client.done:
			return nil, "", errors.New("language server stopped before publishing diagnostics")
		case <-timer.C:
			return json.RawMessage("[]"), "The language server has not published diagnostics for this file yet. Try again; this does not mean the file is free of problems.", nil
		case <-changed:
		}
	}
}
