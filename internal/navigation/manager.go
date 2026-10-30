package navigation

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	maxArchiveBytes   = 64 << 20
	maxFileBytes      = 4 << 20
	maxFiles          = 20000
	maxWorkspaces     = 4
	workspaceLifetime = 20 * time.Minute
)

var (
	ErrNotFound = errors.New("navigation workspace not found or expired; prepare it again")
	ErrSandbox  = errors.New("navigation requires sandbox-exec on macOS or bubblewrap on Linux")
	ErrCapacity = errors.New("all navigation workspaces are busy; retry when a navigation request finishes")
	namePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	shaPattern  = regexp.MustCompile(`^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`)
)

type Location struct {
	Path         string `json:"path"`
	Line         int    `json:"line"`
	Character    int    `json:"character"`
	EndLine      int    `json:"endLine"`
	EndCharacter int    `json:"endCharacter"`
}

type Result struct {
	Locations []Location `json:"locations"`
	Hover     string     `json:"hover"`
	Message   string     `json:"message,omitempty"`
}

type workspace struct {
	mu           sync.Mutex
	active       atomic.Int32
	dir          string
	root         string
	expires      time.Time
	touched      time.Time
	timer        *time.Timer
	client       *rpcClient
	serverKey    string
	capabilities map[string]bool
	opened       map[string]bool
}

type Manager struct {
	mu         sync.Mutex
	workspaces map[string]*workspace
	closed     bool
}

func New() *Manager { return &Manager{workspaces: make(map[string]*workspace)} }

// Prepare accepts a GitHub tar.gz for the exact supplied commit, never a mutable ref.
func (m *Manager) Prepare(ctx context.Context, owner, repo, sha string, archive io.Reader) (string, error) {
	ctx, span := otel.Tracer("fjord/navigation").Start(ctx, "navigation.prepare")
	defer span.End()
	if !validName(owner) || !validName(repo) || !shaPattern.MatchString(sha) || archive == nil {
		return "", errors.New("invalid repository, revision, or archive")
	}
	key := sha256.Sum256([]byte(strings.ToLower(owner + "/" + repo + "@" + sha)))
	id := hex.EncodeToString(key[:16])
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return "", errors.New("navigation manager is closed")
	}
	for key, w := range m.workspaces {
		if time.Now().After(w.expires) {
			w.close()
			delete(m.workspaces, key)
		}
	}
	if w := m.workspaces[id]; w != nil {
		w.touched = time.Now()
		return id, nil
	}
	if len(m.workspaces) >= maxWorkspaces && !m.evictIdle() {
		return "", ErrCapacity
	}
	dir, err := os.MkdirTemp("", "fjord-navigation-")
	if err != nil {
		return "", errors.New("cannot create navigation workspace")
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", errors.New("cannot resolve navigation workspace")
	}
	w := &workspace{dir: dir, root: filepath.Join(dir, "source"), expires: time.Now().Add(workspaceLifetime), opened: make(map[string]bool)}
	keep := false
	defer func() {
		if !keep {
			w.close()
		}
	}()
	if err := os.Mkdir(w.root, 0700); err != nil {
		return "", errors.New("cannot create source directory")
	}
	if err := extract(ctx, archive, w.root); err != nil {
		return "", err
	}
	w.touched = time.Now()
	m.workspaces[id] = w
	w.timer = time.AfterFunc(time.Until(w.expires), func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.workspaces[id] == w {
			w.close()
			delete(m.workspaces, id)
		}
	})
	keep = true
	return id, nil
}

func (m *Manager) get(id string) (*workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.workspaces[id]
	if m.closed || w == nil {
		return nil, ErrNotFound
	}
	if time.Now().After(w.expires) {
		w.close()
		delete(m.workspaces, id)
		return nil, ErrNotFound
	}
	w.active.Add(1)
	w.touched = time.Now()
	return w, nil
}

// Called with the manager lock held so no new reader can acquire the victim.
func (m *Manager) evictIdle() bool {
	var oldest *workspace
	var key string
	for id, w := range m.workspaces {
		if w.active.Load() == 0 && (oldest == nil || w.touched.Before(oldest.touched)) {
			oldest, key = w, id
		}
	}
	if oldest == nil {
		return false
	}
	oldest.close()
	delete(m.workspaces, key)
	return true
}

func (m *Manager) File(ctx context.Context, id, name string) (string, error) {
	_, span := otel.Tracer("fjord/navigation").Start(ctx, "navigation.file")
	defer span.End()
	w, err := m.get(id)
	if err != nil {
		return "", err
	}
	defer w.active.Add(-1)
	w.mu.Lock()
	defer w.mu.Unlock()
	return readFile(w.root, name)
}

func (m *Manager) Query(ctx context.Context, id, name, method string, line, character int) (out Result, resultErr error) {
	allowed := map[string]string{"definition": "textDocument/definition", "implementation": "textDocument/implementation", "references": "textDocument/references", "hover": "textDocument/hover"}
	lspMethod, ok := allowed[method]
	if !ok {
		return Result{}, errors.New("unsupported navigation method")
	}
	ctx, span := otel.Tracer("fjord/navigation").Start(ctx, "navigation."+method)
	defer func() {
		if resultErr != nil {
			span.SetStatus(codes.Error, "navigation failed")
		}
		span.End()
	}()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	w, err := m.get(id)
	if err != nil {
		return Result{}, err
	}
	defer w.active.Add(-1)
	w.mu.Lock()
	defer w.mu.Unlock()
	content, err := readFile(w.root, name)
	if err != nil {
		return Result{}, err
	}
	if !validPosition(content, line, character) {
		return Result{}, errors.New("invalid UTF-16 source position")
	}
	route, err := routeFile(w.root, name)
	if err != nil {
		return Result{}, err
	}
	span.SetAttributes(attribute.String("language", route.language))
	if w.serverKey != route.key || w.stopped() {
		if w.client != nil {
			w.client.close()
			w.client = nil
		}
		loadCtx, loadSpan := otel.Tracer("fjord/navigation").Start(ctx, "navigation.server.start")
		loadSpan.SetAttributes(attribute.String("language", route.language))
		client, capabilities, err := startLanguageServer(loadCtx, w.dir, route.root, route.language)
		if err != nil {
			loadSpan.SetStatus(codes.Error, "language server startup failed")
		}
		loadSpan.End()
		if err != nil {
			return Result{}, err
		}
		w.client, w.capabilities, w.serverKey = client, capabilities, route.key
		w.opened = make(map[string]bool)
	}
	if !w.capabilities[method] {
		return Result{}, fmt.Errorf("%s language server does not support %s; choose another navigation action", route.displayName, method)
	}
	stopCancellation := context.AfterFunc(ctx, w.client.close)
	defer stopCancellation()
	uri := (&url.URL{Scheme: "file", Path: filepath.Join(w.root, filepath.FromSlash(name))}).String()
	if !w.opened[name] {
		if err := w.client.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": route.languageID, "version": 1, "text": content}}); err != nil {
			return Result{}, err
		}
		w.opened[name] = true
	}
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": map[string]int{"line": line, "character": character}}
	if method == "references" {
		params["context"] = map[string]bool{"includeDeclaration": true}
	}
	var raw jsonResult
	if err := w.client.call(ctx, lspMethod, params, &raw); err != nil {
		return Result{}, err
	}
	result, err := decodeResult(w.root, method, raw)
	if err == nil && len(result.Locations) == 0 && result.Hover == "" {
		result.Message = "No " + method + " result for this position."
		if route.language == "terraform" {
			result.Message += " Provider schemas and remote modules are unavailable in this isolated review; local variables, locals, and local module code can be navigated."
		}
	}
	return result, err
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for id, w := range m.workspaces {
		w.close()
		delete(m.workspaces, id)
	}
	return nil
}

func (w *workspace) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	if w.client != nil {
		w.client.close()
	}
	_ = os.RemoveAll(w.dir)
}

func (w *workspace) stopped() bool {
	if w.client == nil {
		return true
	}
	select {
	case <-w.client.done:
		return true
	default:
		return false
	}
}

func validName(s string) bool {
	return len(s) > 0 && len(s) <= 100 && s != "." && s != ".." && namePattern.MatchString(s)
}

func safePath(name string) bool {
	return name != "" && name != "." && !strings.ContainsAny(name, "\\\x00") && !strings.HasPrefix(name, "/") && path.Clean(name) == name && !strings.HasPrefix(name, "../")
}

func readFile(root, name string) (string, error) {
	if !safePath(name) {
		return "", errors.New("invalid source path")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return "", errors.New("source workspace is unavailable")
	}
	defer dir.Close()
	f, err := dir.Open(filepath.FromSlash(name))
	if err != nil {
		return "", errors.New("source file not found")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return "", errors.New("source file is not a supported text file")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil || len(b) > maxFileBytes || !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return "", errors.New("source file is not supported UTF-8 text")
	}
	return string(b), nil
}

func validPosition(content string, line, character int) bool {
	if line < 0 || character < 0 {
		return false
	}
	lines := strings.Split(content, "\n")
	if line >= len(lines) {
		return false
	}
	n := 0
	for _, r := range strings.TrimSuffix(lines[line], "\r") {
		if n == character {
			return true
		}
		n += utf16.RuneLen(r)
	}
	return n == character
}

func extract(ctx context.Context, archive io.Reader, root string) error {
	compressed := &io.LimitedReader{R: archive, N: maxArchiveBytes + 1}
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return errors.New("invalid source archive")
	}
	defer gz.Close()
	expanded := &io.LimitedReader{R: gz, N: maxArchiveBytes + 1}
	tr := tar.NewReader(expanded)
	var top string
	var total int64
	files := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("invalid or oversized source archive")
		}
		files++
		if files > maxFiles {
			return errors.New("source archive contains too many entries")
		}
		// GitHub archives begin with a global PAX commit comment. These metadata
		// headers are not source entries and must not establish the archive root.
		if h.Typeflag == tar.TypeXGlobalHeader {
			for key := range h.PAXRecords {
				switch key {
				case "comment", "mtime", "uid", "gid", "uname", "gname":
				default:
					return errors.New("source archive contains unsupported global metadata")
				}
			}
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		if !safePath(name) {
			return errors.New("source archive contains an unsafe path")
		}
		parts := strings.SplitN(name, "/", 2)
		if top == "" {
			top = parts[0]
		}
		if parts[0] != top {
			return errors.New("source archive must contain one root directory")
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return errors.New("source archive contains links or unsupported entries")
		}
		if len(parts) == 1 {
			if h.Typeflag != tar.TypeDir {
				return errors.New("source archive must contain a root directory")
			}
			continue
		}
		name = parts[1]
		if !safePath(name) {
			return errors.New("source archive contains an unsafe path")
		}
		for _, part := range strings.Split(name, "/") {
			if strings.EqualFold(part, ".git") {
				return errors.New("source archive contains Git metadata")
			}
		}
		filename := filepath.Join(root, filepath.FromSlash(name))
		if h.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(filename, 0700); err != nil {
				return errors.New("cannot extract source directory")
			}
			continue
		}
		total += h.Size
		if h.Size < 0 || h.Size > maxFileBytes || total > maxArchiveBytes {
			return errors.New("source archive exceeds size limits")
		}
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			return errors.New("cannot extract source directory")
		}
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("source archive contains duplicate or conflicting paths")
		}
		_, copyErr := io.CopyN(f, tr, h.Size)
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil {
			return errors.New("cannot extract source file")
		}
	}
	if files == 0 || top == "" {
		return errors.New("source archive is empty")
	}
	if _, err := io.Copy(io.Discard, expanded); err != nil || expanded.N == 0 || compressed.N == 0 {
		return errors.New("invalid or oversized source archive")
	}
	return nil
}

type jsonResult = json.RawMessage

func sourceLocation(root, uri string, start, end position) (Location, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
		return Location{}, errors.New("language server returned an unsupported source location")
	}
	rel, err := filepath.Rel(root, u.Path)
	if err != nil || !safePath(filepath.ToSlash(rel)) {
		return Location{}, errors.New("definition is outside this revision workspace")
	}
	return Location{Path: filepath.ToSlash(rel), Line: start.Line, Character: start.Character, EndLine: end.Line, EndCharacter: end.Character}, nil
}
