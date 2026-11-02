package navigation

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRPCRequestCancellation(t *testing.T) {
	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()
	defer serverRead.Close()
	defer serverWrite.Close()
	c := newRPC(clientRead, clientWrite, nil)
	defer c.close()
	seen := make(chan rpcMessage, 1)
	go func() {
		r := bufio.NewReader(serverRead)
		for i := 0; i < 1; i++ {
			m, err := readMessage(r)
			if err != nil {
				return
			}
			seen <- m
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var result json.RawMessage
	if err := c.call(ctx, "textDocument/definition", nil, &result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	first := <-seen
	if first.Method != "textDocument/definition" {
		t.Fatalf("%s", first.Method)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) != 0 {
		t.Fatal("cancelled request retained")
	}
}

func TestRPCStalledWriteCancellation(t *testing.T) {
	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()
	defer serverRead.Close()
	defer serverWrite.Close()
	c := newRPC(clientRead, clientWrite, nil)
	defer c.close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- c.call(ctx, "initialize", nil, nil) }()
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled write ignored cancellation")
	}
}

func TestRPCServerRequestAndResponse(t *testing.T) {
	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()
	defer serverRead.Close()
	defer serverWrite.Close()
	c := newRPC(clientRead, clientWrite, nil)
	defer c.close()
	go func() {
		r := bufio.NewReader(serverRead)
		message, err := readMessage(r)
		if err != nil {
			return
		}
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "server-1", "method": "workspace/applyEdit", "params": map[string]any{}})
		fmt.Fprintf(serverWrite, "Content-Length: %d\r\n\r\n%s", len(b), b)
		answer, err := readMessage(r)
		if err != nil {
			return
		}
		var result struct{ Applied bool }
		_ = json.Unmarshal(answer.Result, &result)
		if result.Applied {
			return
		}
		b, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": []any{}})
		fmt.Fprintf(serverWrite, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var result []any
	if err := c.call(ctx, "textDocument/references", nil, &result); err != nil {
		t.Fatal(err)
	}
}

func TestRPCRejectsMalformedFrames(t *testing.T) {
	for _, frame := range []string{"Content-Length: -1\r\n\r\n", "Content-Length: 999999999\r\n\r\n", "Content-Length: 0\r\nContent-Length: 0\r\n\r\n", "X: y\r\n\r\n", "Content-Length: 2\r\n\r\nx", strings.Repeat("x", 16384)} {
		if _, err := readMessage(bufio.NewReader(strings.NewReader(frame))); err == nil {
			t.Fatalf("accepted frame %q", frame)
		}
	}
}
