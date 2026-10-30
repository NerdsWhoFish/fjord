package navigation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type QueryOptions struct {
	Query   string `json:"query,omitempty"`
	NewName string `json:"newName,omitempty"`
}

type ReviewItem struct {
	Label    string       `json:"label"`
	Detail   string       `json:"detail,omitempty"`
	Kind     string       `json:"kind,omitempty"`
	Location *Location    `json:"location,omitempty"`
	Children []ReviewItem `json:"children,omitempty"`
}

type DocumentHighlight struct {
	Location Location `json:"location"`
	Kind     string   `json:"kind,omitempty"`
}

type InlayHint struct {
	Location     Location `json:"location"`
	Label        string   `json:"label"`
	Kind         string   `json:"kind,omitempty"`
	PaddingLeft  bool     `json:"paddingLeft,omitempty"`
	PaddingRight bool     `json:"paddingRight,omitempty"`
}

type TextEdit struct {
	Location Location `json:"location"`
	OldText  string   `json:"oldText"`
	NewText  string   `json:"newText"`
}

type ReviewAction struct {
	Title          string     `json:"title"`
	Kind           string     `json:"kind,omitempty"`
	Edits          []TextEdit `json:"edits,omitempty"`
	DisabledReason string     `json:"disabledReason,omitempty"`
}

const maxInsightItems = 512
const maxInsightDepth = 16
const maxInsightBytes = 4 << 20
const maxInsightText = 16 << 10
const maxInsightEditText = 256 << 10
const maxInsightSourceBytes = 8 << 20

var errInsight = errors.New("language server returned invalid or unsupported insight data")
var errInsightOutside = errors.New("insight location is outside this revision workspace")
var errInsightLimit = errors.New("language-server insight exceeds the supported size or depth limit")
var errEditPreview = errors.New("edit preview requires valid, non-overlapping edits in existing revision files; resource operations and commands are not supported")

type insightPosition struct {
	Line      *int `json:"line"`
	Character *int `json:"character"`
}

type insightRange struct {
	Start *insightPosition `json:"start"`
	End   *insightPosition `json:"end"`
}

type insightLocation struct {
	URI   string        `json:"uri"`
	Range *insightRange `json:"range"`
}

type insightSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail"`
	ContainerName  string           `json:"containerName"`
	Kind           int              `json:"kind"`
	URI            string           `json:"uri"`
	Range          *insightRange    `json:"range"`
	SelectionRange *insightRange    `json:"selectionRange"`
	Location       *insightLocation `json:"location"`
	Children       []insightSymbol  `json:"children"`
}

type insightSource struct {
	text   string
	lines  []string
	starts []int
}

type insightDecoder struct {
	root        string
	name        string
	items       int
	textBytes   int
	sourceBytes int
	skipped     int
	sources     map[string]insightSource
}

func decodeInsights(root, name, method string, raw json.RawMessage) (Result, error) {
	result := Result{Locations: []Location{}}
	if len(raw) > maxInsightBytes {
		return result, errInsightLimit
	}
	if !utf8.Valid(raw) {
		return result, errInsight
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return result, nil
	}
	d := &insightDecoder{root: root, name: name, sources: make(map[string]insightSource)}
	var err error
	switch method {
	case "diagnostics":
		result.Items, result.Message, err = d.diagnostics(raw)
	case "incomingCalls", "outgoingCalls", "supertypes", "subtypes":
		result.Items, err = d.hierarchy(method, raw)
	case "documentSymbols", "workspaceSymbols":
		var symbols []insightSymbol
		if json.Unmarshal(raw, &symbols) != nil {
			return result, errInsight
		}
		result.Items, err = d.symbols(symbols, method == "workspaceSymbols", 0)
	case "documentHighlights":
		result.Highlights, err = d.highlights(raw)
	case "inlayHints":
		result.Hints, err = d.hints(raw)
	case "codeActions":
		result.Actions, err = d.actions(raw)
	case "rename":
		result.Edits, err = d.edits(raw)
	default:
		err = errInsight
	}
	if err != nil {
		return Result{Locations: []Location{}}, err
	}
	if d.skipped > 0 {
		noun := "locations"
		if d.skipped == 1 {
			noun = "location"
		}
		result.Message = fmt.Sprintf("Omitted %d %s outside this revision workspace.", d.skipped, noun)
	}
	return result, nil
}

func (d *insightDecoder) count(depth int) error {
	d.items++
	if d.items > maxInsightItems || depth > maxInsightDepth {
		return errInsightLimit
	}
	return nil
}

func (d *insightDecoder) text(value string, limit int) error {
	d.textBytes += len(value)
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return errInsight
	}
	if len(value) > limit || d.textBytes > maxInsightBytes {
		return errInsightLimit
	}
	return nil
}

func (d *insightDecoder) uri(name string) string {
	return (&url.URL{Scheme: "file", Path: filepath.Join(d.root, filepath.FromSlash(name))}).String()
}

func (d *insightDecoder) source(name string) (insightSource, error) {
	if source, ok := d.sources[name]; ok {
		return source, nil
	}
	text, err := readFile(d.root, name)
	if err != nil {
		return insightSource{}, errInsight
	}
	d.sourceBytes += len(text)
	if d.sourceBytes > maxInsightSourceBytes || len(d.sources) >= 64 {
		return insightSource{}, errInsightLimit
	}
	lines := strings.Split(text, "\n")
	starts := make([]int, len(lines))
	for i := 1; i < len(lines); i++ {
		starts[i] = starts[i-1] + len(lines[i-1]) + 1
	}
	source := insightSource{text: text, lines: lines, starts: starts}
	d.sources[name] = source
	return source, nil
}

func checkedPosition(raw *insightPosition) (position, bool) {
	if raw == nil || raw.Line == nil || raw.Character == nil || *raw.Line < 0 || *raw.Character < 0 {
		return position{}, false
	}
	return position{Line: *raw.Line, Character: *raw.Character}, true
}

func positionBefore(a, b position) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Character < b.Character)
}

func checkedRange(r *insightRange) (position, position, bool) {
	if r == nil {
		return position{}, position{}, false
	}
	start, startOK := checkedPosition(r.Start)
	end, endOK := checkedPosition(r.End)
	return start, end, startOK && endOK && !positionBefore(end, start)
}

func (d *insightDecoder) location(uri string, r *insightRange, uriOnly bool) (Location, error) {
	var start, end position
	if r != nil {
		var valid bool
		start, end, valid = checkedRange(r)
		if !valid {
			return Location{}, errInsight
		}
	} else if !uriOnly {
		return Location{}, errInsight
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Host != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !filepath.IsAbs(u.Path) {
		return Location{}, errInsight
	}
	relative, err := filepath.Rel(d.root, u.Path)
	if err != nil {
		return Location{}, errInsight
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Location{}, errInsightOutside
	}
	location, err := sourceLocation(d.root, uri, start, end)
	if err != nil {
		return Location{}, errInsight
	}
	source, err := d.source(location.Path)
	if err != nil {
		return Location{}, err
	}
	for _, p := range []position{start, end} {
		if p.Line >= len(source.lines) || !validPosition(source.lines[p.Line], 0, p.Character) {
			return Location{}, errInsight
		}
	}
	return location, nil
}

func (d *insightDecoder) symbolLocation(symbol insightSymbol, workspace bool) (Location, error) {
	if symbol.Location != nil {
		return d.location(symbol.Location.URI, symbol.Location.Range, workspace)
	}
	// External targets may be skipped, but malformed ranges must still fail.
	start, end, valid := checkedRange(symbol.Range)
	if !valid {
		return Location{}, errInsight
	}
	if symbol.SelectionRange != nil {
		selectedStart, selectedEnd, valid := checkedRange(symbol.SelectionRange)
		if !valid || positionBefore(selectedStart, start) || positionBefore(end, selectedEnd) {
			return Location{}, errInsight
		}
	}
	uri := symbol.URI
	if uri == "" {
		if !safePath(d.name) {
			return Location{}, errInsight
		}
		uri = d.uri(d.name)
	}
	whole, err := d.location(uri, symbol.Range, false)
	if err != nil {
		return Location{}, err
	}
	if symbol.SelectionRange == nil {
		return whole, nil
	}
	selected, err := d.location(uri, symbol.SelectionRange, false)
	if err != nil || positionBefore(locationStart(selected), locationStart(whole)) || positionBefore(locationEnd(whole), locationEnd(selected)) {
		return Location{}, errInsight
	}
	return selected, nil
}

func locationStart(location Location) position {
	return position{Line: location.Line, Character: location.Character}
}

func locationEnd(location Location) position {
	return position{Line: location.EndLine, Character: location.EndCharacter}
}

func symbolKind(kind int) string {
	kinds := []string{"symbol", "file", "module", "namespace", "package", "class", "method", "property", "field", "constructor", "enum", "interface", "function", "variable", "constant", "string", "number", "boolean", "array", "object", "key", "null", "enumMember", "struct", "event", "operator", "typeParameter"}
	if kind < 1 || kind >= len(kinds) {
		return "symbol"
	}
	return kinds[kind]
}

func (d *insightDecoder) symbols(symbols []insightSymbol, workspace bool, depth int) ([]ReviewItem, error) {
	if len(symbols) > maxInsightItems {
		return nil, errInsightLimit
	}
	items := make([]ReviewItem, 0, len(symbols))
	for _, symbol := range symbols {
		if err := d.count(depth); err != nil {
			return nil, err
		}
		if d.text(symbol.Name, 4096) != nil || d.text(symbol.Detail, maxInsightText) != nil || d.text(symbol.ContainerName, 4096) != nil {
			return nil, errInsightLimit
		}
		location, err := d.symbolLocation(symbol, workspace)
		if errors.Is(err, errInsightOutside) {
			d.skipped++
			continue
		}
		if err != nil {
			return nil, err
		}
		children, err := d.symbols(symbol.Children, workspace, depth+1)
		if err != nil {
			return nil, err
		}
		detail := symbol.Detail
		if detail == "" {
			detail = symbol.ContainerName
		}
		items = append(items, ReviewItem{Label: symbol.Name, Detail: detail, Kind: symbolKind(symbol.Kind), Location: &location, Children: children})
	}
	return items, nil
}

func (d *insightDecoder) diagnostics(raw json.RawMessage) ([]ReviewItem, string, error) {
	if len(bytes.TrimSpace(raw)) > 0 && bytes.TrimSpace(raw)[0] == '{' {
		var report struct {
			Kind  string          `json:"kind"`
			Items json.RawMessage `json:"items"`
		}
		if json.Unmarshal(raw, &report) != nil {
			return nil, "", errInsight
		}
		if report.Kind == "unchanged" {
			return nil, "Diagnostics unchanged.", nil
		}
		raw = report.Items
	}
	var entries []struct {
		Range    *insightRange   `json:"range"`
		Severity int             `json:"severity"`
		Message  string          `json:"message"`
		Source   string          `json:"source"`
		Code     json.RawMessage `json:"code"`
	}
	if json.Unmarshal(raw, &entries) != nil || !safePath(d.name) {
		return nil, "", errInsight
	}
	if len(entries) > maxInsightItems {
		return nil, "", errInsightLimit
	}
	items := make([]ReviewItem, 0, len(entries))
	for _, entry := range entries {
		if err := d.count(0); err != nil {
			return nil, "", err
		}
		if d.text(entry.Message, maxInsightText) != nil || d.text(entry.Source, 4096) != nil || len(entry.Code) > 4096 {
			return nil, "", errInsightLimit
		}
		location, err := d.location(d.uri(d.name), entry.Range, false)
		if err != nil {
			return nil, "", err
		}
		kinds := []string{"diagnostic", "error", "warning", "information", "hint"}
		if entry.Severity < 0 || entry.Severity >= len(kinds) {
			return nil, "", errInsight
		}
		detail := entry.Source
		if len(entry.Code) > 0 && string(entry.Code) != "null" {
			var code string
			if json.Unmarshal(entry.Code, &code) != nil {
				var number json.Number
				if json.Unmarshal(entry.Code, &number) != nil {
					return nil, "", errInsight
				}
				code = number.String()
			}
			if d.text(code, 4096) != nil {
				return nil, "", errInsightLimit
			}
			if detail != "" {
				detail += " / "
			}
			detail += code
		}
		items = append(items, ReviewItem{Label: entry.Message, Detail: detail, Kind: kinds[entry.Severity], Location: &location})
	}
	return items, "", nil
}

func (d *insightDecoder) hierarchy(method string, raw json.RawMessage) ([]ReviewItem, error) {
	if method == "supertypes" || method == "subtypes" {
		var symbols []insightSymbol
		if json.Unmarshal(raw, &symbols) != nil {
			return nil, errInsight
		}
		return d.symbols(symbols, false, 0)
	}
	var calls []struct {
		From       *insightSymbol `json:"from"`
		To         *insightSymbol `json:"to"`
		FromURI    string         `json:"fromUri"`
		FromRanges []insightRange `json:"fromRanges"`
	}
	if json.Unmarshal(raw, &calls) != nil {
		return nil, errInsight
	}
	if len(calls) > maxInsightItems {
		return nil, errInsightLimit
	}
	items := make([]ReviewItem, 0, len(calls))
	for _, call := range calls {
		symbol := call.From
		if method == "outgoingCalls" {
			symbol = call.To
		}
		if symbol == nil || symbol.URI == "" {
			return nil, errInsight
		}
		decoded, err := d.symbols([]insightSymbol{*symbol}, false, 0)
		if err != nil {
			return nil, err
		}
		if len(decoded) == 0 {
			continue
		}
		item := decoded[0]
		uri := symbol.URI
		if method == "outgoingCalls" {
			uri = call.FromURI
			if uri == "" {
				if !safePath(d.name) {
					return nil, errInsight
				}
				uri = d.uri(d.name)
			}
		}
		for i := range call.FromRanges {
			if err := d.count(1); err != nil {
				return nil, err
			}
			location, err := d.location(uri, &call.FromRanges[i], false)
			if errors.Is(err, errInsightOutside) {
				d.skipped++
				continue
			}
			if err != nil {
				return nil, err
			}
			item.Children = append(item.Children, ReviewItem{Label: "Call site", Kind: "callSite", Location: &location})
		}
		items = append(items, item)
	}
	return items, nil
}

func (d *insightDecoder) highlights(raw json.RawMessage) ([]DocumentHighlight, error) {
	var entries []struct {
		Range *insightRange `json:"range"`
		Kind  int           `json:"kind"`
	}
	if json.Unmarshal(raw, &entries) != nil || !safePath(d.name) {
		return nil, errInsight
	}
	if len(entries) > maxInsightItems {
		return nil, errInsightLimit
	}
	result := make([]DocumentHighlight, 0, len(entries))
	for _, entry := range entries {
		if err := d.count(0); err != nil {
			return nil, err
		}
		kinds := []string{"text", "text", "read", "write"}
		if entry.Kind < 0 || entry.Kind >= len(kinds) {
			return nil, errInsight
		}
		location, err := d.location(d.uri(d.name), entry.Range, false)
		if err != nil {
			return nil, err
		}
		result = append(result, DocumentHighlight{Location: location, Kind: kinds[entry.Kind]})
	}
	return result, nil
}

func (d *insightDecoder) hints(raw json.RawMessage) ([]InlayHint, error) {
	var entries []struct {
		Position     *insightPosition `json:"position"`
		Label        json.RawMessage  `json:"label"`
		Kind         int              `json:"kind"`
		PaddingLeft  bool             `json:"paddingLeft"`
		PaddingRight bool             `json:"paddingRight"`
	}
	if json.Unmarshal(raw, &entries) != nil || !safePath(d.name) {
		return nil, errInsight
	}
	if len(entries) > maxInsightItems {
		return nil, errInsightLimit
	}
	result := make([]InlayHint, 0, len(entries))
	for _, entry := range entries {
		if err := d.count(0); err != nil {
			return nil, err
		}
		label := ""
		if json.Unmarshal(entry.Label, &label) != nil {
			var parts []struct {
				Value *string `json:"value"`
			}
			if json.Unmarshal(entry.Label, &parts) != nil || len(parts) > 64 {
				return nil, errInsight
			}
			for _, part := range parts {
				if part.Value == nil {
					return nil, errInsight
				}
				label += *part.Value
			}
		}
		if label == "" {
			return nil, errInsight
		}
		if d.text(label, maxInsightText) != nil {
			return nil, errInsightLimit
		}
		location, err := d.location(d.uri(d.name), &insightRange{Start: entry.Position, End: entry.Position}, false)
		if err != nil {
			return nil, err
		}
		kinds := []string{"hint", "type", "parameter"}
		if entry.Kind < 0 || entry.Kind >= len(kinds) {
			return nil, errInsight
		}
		result = append(result, InlayHint{Location: location, Label: label, Kind: kinds[entry.Kind], PaddingLeft: entry.PaddingLeft, PaddingRight: entry.PaddingRight})
	}
	return result, nil
}

func (d *insightDecoder) actions(raw json.RawMessage) ([]ReviewAction, error) {
	var entries []struct {
		Title    string          `json:"title"`
		Kind     string          `json:"kind"`
		Edit     json.RawMessage `json:"edit"`
		Command  json.RawMessage `json:"command"`
		Disabled *struct {
			Reason string `json:"reason"`
		} `json:"disabled"`
	}
	if json.Unmarshal(raw, &entries) != nil || len(entries) > maxInsightItems {
		return nil, errInsight
	}
	result := make([]ReviewAction, 0, len(entries))
	for _, entry := range entries {
		if err := d.count(0); err != nil {
			return nil, err
		}
		if d.text(entry.Title, 4096) != nil || d.text(entry.Kind, 256) != nil {
			return nil, errInsightLimit
		}
		action := ReviewAction{Title: entry.Title, Kind: entry.Kind}
		switch {
		case entry.Disabled != nil:
			if d.text(entry.Disabled.Reason, maxInsightText) != nil {
				return nil, errInsightLimit
			}
			action.DisabledReason = entry.Disabled.Reason
			if action.DisabledReason == "" {
				action.DisabledReason = "Disabled by the language server."
			}
		case len(entry.Command) > 0 && string(entry.Command) != "null":
			action.DisabledReason = "This action requires command execution, which is disabled in review workspaces."
		case len(entry.Edit) == 0 || string(entry.Edit) == "null":
			action.DisabledReason = "The language server did not provide an editable preview."
		default:
			edits, err := d.edits(entry.Edit)
			if err != nil {
				action.DisabledReason = errEditPreview.Error()
			} else if len(edits) == 0 {
				action.DisabledReason = "This action contains no text changes."
			} else {
				action.Edits = edits
			}
		}
		result = append(result, action)
	}
	return result, nil
}

type insightEdit struct {
	Range        *insightRange   `json:"range"`
	NewText      *string         `json:"newText"`
	AnnotationID json.RawMessage `json:"annotationId"`
}

func (d *insightDecoder) edits(raw json.RawMessage) ([]TextEdit, error) {
	var workspace struct {
		Changes           map[string][]insightEdit `json:"changes"`
		DocumentChanges   []json.RawMessage        `json:"documentChanges"`
		ChangeAnnotations map[string]any           `json:"changeAnnotations"`
	}
	if decodeInsightObject(raw, &workspace) != nil || len(workspace.ChangeAnnotations) > 0 || (len(workspace.Changes) > 0 && len(workspace.DocumentChanges) > 0) {
		return nil, errEditPreview
	}
	editsByURI := workspace.Changes
	if editsByURI == nil {
		editsByURI = make(map[string][]insightEdit)
	}
	if len(workspace.DocumentChanges) > maxInsightItems || len(editsByURI) > 64 {
		return nil, errInsightLimit
	}
	for _, rawChange := range workspace.DocumentChanges {
		var change struct {
			Kind         json.RawMessage `json:"kind"`
			TextDocument *struct {
				URI     string          `json:"uri"`
				Version json.RawMessage `json:"version"`
			} `json:"textDocument"`
			Edits []insightEdit `json:"edits"`
		}
		if decodeInsightObject(rawChange, &change) != nil || len(change.Kind) > 0 || change.TextDocument == nil || change.TextDocument.URI == "" {
			return nil, errEditPreview
		}
		version := change.TextDocument.Version
		if len(version) > 0 && string(version) != "null" {
			var number int
			if json.Unmarshal(version, &number) != nil || number != 1 {
				return nil, errEditPreview
			}
		}
		if _, duplicate := editsByURI[change.TextDocument.URI]; duplicate {
			return nil, errEditPreview
		}
		editsByURI[change.TextDocument.URI] = change.Edits
	}
	if len(editsByURI) > 64 {
		return nil, errInsightLimit
	}
	uris := make([]string, 0, len(editsByURI))
	for uri := range editsByURI {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	edits := make([]TextEdit, 0)
	for _, uri := range uris {
		for _, edit := range editsByURI[uri] {
			if err := d.count(0); err != nil {
				return nil, err
			}
			if edit.NewText == nil || len(edit.AnnotationID) > 0 || d.text(*edit.NewText, maxInsightEditText) != nil {
				return nil, errEditPreview
			}
			location, err := d.location(uri, edit.Range, false)
			if err != nil {
				return nil, errEditPreview
			}
			source := d.sources[location.Path]
			start := source.offset(locationStart(location))
			end := source.offset(locationEnd(location))
			oldText := source.text[start:end]
			if d.text(oldText, maxInsightEditText) != nil {
				return nil, errEditPreview
			}
			edits = append(edits, TextEdit{Location: location, OldText: oldText, NewText: *edit.NewText})
		}
	}
	sort.Slice(edits, func(i, j int) bool {
		a, b := edits[i].Location, edits[j].Location
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if locationStart(a) != locationStart(b) {
			return positionBefore(locationStart(a), locationStart(b))
		}
		return positionBefore(locationEnd(a), locationEnd(b))
	})
	for i := 1; i < len(edits); i++ {
		previous, next := edits[i-1].Location, edits[i].Location
		if previous.Path == next.Path && (positionBefore(locationStart(next), locationEnd(previous)) || locationStart(previous) == locationStart(next)) {
			return nil, errEditPreview
		}
	}
	return edits, nil
}

func (s insightSource) offset(p position) int {
	line := strings.TrimSuffix(s.lines[p.Line], "\r")
	units := 0
	for byteOffset, r := range line {
		if units == p.Character {
			return s.starts[p.Line] + byteOffset
		}
		units += utf16.RuneLen(r)
	}
	return s.starts[p.Line] + len(line)
}

func decodeInsightObject(raw json.RawMessage, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(output) != nil {
		return errInsight
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return errInsight
	}
	return nil
}
