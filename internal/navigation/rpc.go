package navigation

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const maxMessageBytes = 16 << 20

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcClient struct {
	reader   io.ReadCloser
	writer   io.WriteCloser
	writeMu  sync.Mutex
	mu       sync.Mutex
	next     int64
	pending  map[int64]chan rpcMessage
	done     chan struct{}
	once     sync.Once
	stop     func()
	settings map[string]any
}

func newRPC(reader io.ReadCloser, writer io.WriteCloser, stop func()) *rpcClient {
	return newRPCWithSettings(reader, writer, stop, nil)
}

func newRPCWithSettings(reader io.ReadCloser, writer io.WriteCloser, stop func(), settings map[string]any) *rpcClient {
	c := &rpcClient{reader: reader, writer: writer, pending: make(map[int64]chan rpcMessage), done: make(chan struct{}), stop: stop, settings: settings}
	go c.readLoop()
	return c
}

func (c *rpcClient) close() {
	c.once.Do(func() {
		close(c.done)
		if c.stop != nil {
			c.stop()
		}
		_ = c.reader.Close()
		_ = c.writer.Close()
	})
}

func (c *rpcClient) send(message any) error {
	b, err := json.Marshal(message)
	if err != nil {
		return errors.New("cannot encode language-server message")
	}
	if len(b) > maxMessageBytes {
		return errors.New("language-server message exceeds limit")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return errors.New("language server stopped")
	default:
	}
	if _, err = fmt.Fprintf(c.writer, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
		return errors.New("language server disconnected")
	}
	if _, err = c.writer.Write(b); err != nil {
		return errors.New("language server disconnected")
	}
	return nil
}

func (c *rpcClient) notify(method string, params any) error {
	return c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *rpcClient) call(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// A pipe has no portable write deadline. Cancellation kills this session so a
	// stalled server cannot retain the request while its stdin buffer is full.
	stopCancellation := context.AfterFunc(ctx, c.close)
	defer stopCancellation()
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan rpcMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("language server stopped; check the selected language server and platform sandbox")
	case message := <-ch:
		if message.Error != nil {
			return fmt.Errorf("language server rejected navigation (code %d); dependencies may be unavailable in the isolated workspace", message.Error.Code)
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(message.Result, result); err != nil {
			return errors.New("language server returned an invalid response")
		}
		return nil
	}
}

func (c *rpcClient) readLoop() {
	defer c.close()
	r := bufio.NewReader(c.reader)
	for {
		message, err := readMessage(r)
		if err != nil {
			return
		}
		if message.Method != "" {
			if len(message.ID) > 0 {
				c.answer(message)
			}
			continue
		}
		var id int64
		if json.Unmarshal(message.ID, &id) != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[id]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- message:
			default:
			}
		}
	}
}

func (c *rpcClient) answer(message rpcMessage) {
	var result any
	switch message.Method {
	case "workspace/configuration":
		var params struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		_ = json.Unmarshal(message.Params, &params)
		items := make([]any, len(params.Items))
		for i, item := range params.Items {
			items[i] = settingSection(c.settings, item.Section)
		}
		result = items
	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create":
	case "workspace/applyEdit":
		result = map[string]any{"applied": false, "failureReason": "Fjord revision workspaces are read-only"}
	default:
		_ = c.send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": rpcError{Code: -32601, Message: "unsupported client method"}})
		return
	}
	_ = c.send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result})
}

func settingSection(settings map[string]any, section string) any {
	if section == "" {
		return settings
	}
	if value, ok := settings[section]; ok {
		return value
	}
	var current any = settings
	for _, part := range strings.Split(section, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func readMessage(r *bufio.Reader) (rpcMessage, error) {
	length := -1
	headerBytes := 0
	for {
		lineBytes, err := r.ReadSlice('\n')
		if err != nil {
			return rpcMessage{}, err
		}
		line := string(lineBytes)
		headerBytes += len(line)
		if headerBytes > 8192 {
			return rpcMessage{}, errors.New("oversized language-server header")
		}
		if line == "\r\n" {
			break
		}
		name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			return rpcMessage{}, errors.New("invalid language-server header")
		}
		if strings.EqualFold(name, "Content-Length") {
			if length != -1 {
				return rpcMessage{}, errors.New("duplicate language-server length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil || length < 0 || length > maxMessageBytes {
				return rpcMessage{}, errors.New("invalid language-server length")
			}
		}
	}
	if length < 0 {
		return rpcMessage{}, errors.New("missing language-server length")
	}
	b := make([]byte, length)
	if _, err := io.ReadFull(r, b); err != nil {
		return rpcMessage{}, err
	}
	var message rpcMessage
	if err := json.Unmarshal(b, &message); err != nil {
		return rpcMessage{}, err
	}
	return message, nil
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type sourceRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}
type lspLocation struct {
	URI                  string      `json:"uri"`
	Range                sourceRange `json:"range"`
	TargetURI            string      `json:"targetUri"`
	TargetSelectionRange sourceRange `json:"targetSelectionRange"`
}

func decodeResult(root, method string, raw json.RawMessage) (Result, error) {
	result := Result{Locations: []Location{}}
	if string(raw) == "null" {
		return result, nil
	}
	if method == "hover" {
		var hover struct {
			Contents json.RawMessage `json:"contents"`
		}
		if json.Unmarshal(raw, &hover) != nil {
			return result, errors.New("invalid hover response")
		}
		result.Hover = hoverText(hover.Contents)
		return result, nil
	}
	var items []lspLocation
	if json.Unmarshal(raw, &items) != nil {
		var item lspLocation
		if json.Unmarshal(raw, &item) != nil {
			return result, errors.New("invalid navigation response")
		}
		items = []lspLocation{item}
	}
	outside := false
	for _, item := range items {
		uri, r := item.URI, item.Range
		if item.TargetURI != "" {
			uri, r = item.TargetURI, item.TargetSelectionRange
		}
		location, err := sourceLocation(root, uri, r.Start, r.End)
		if err != nil {
			outside = true
			continue
		}
		result.Locations = append(result.Locations, location)
	}
	if outside && len(result.Locations) == 0 {
		return result, errors.New("target is outside this revision workspace; dependency navigation is not available yet")
	}
	return result, nil
}

func hoverText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var markup struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &markup) == nil && markup.Value != "" {
		return markup.Value
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) == nil {
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			texts = append(texts, hoverText(part))
		}
		return strings.Join(texts, "\n\n")
	}
	return ""
}
