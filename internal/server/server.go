package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NerdsWhoFish/fjord/internal/github"
	"github.com/NerdsWhoFish/fjord/internal/navigation"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
)

type GitHub interface {
	Load(context.Context, string, string, int) (github.PR, error)
	Content(context.Context, string, string, string, string) (string, error)
	Review(context.Context, string, string, int, github.Review) (string, error)
	Comments(context.Context, string, string, int) ([]github.ThreadComment, error)
	Conversation(context.Context, string, string, int) ([]github.ConversationEntry, error)
	Reply(context.Context, string, string, int, int64, string) (string, error)
	Archive(context.Context, string, string, string) (io.ReadCloser, error)
}
type Navigation interface {
	Prepare(context.Context, string, string, string, io.Reader) (string, error)
	Capabilities(context.Context, string, string) (navigation.Capabilities, error)
	Query(context.Context, string, string, string, int, int) (navigation.Result, error)
	Inspect(context.Context, string, string, string, int, int, navigation.QueryOptions) (navigation.Result, error)
	File(context.Context, string, string) (string, error)
}
type Config struct {
	Host       string
	Origin     string
	Assets     fs.FS
	GitHub     GitHub
	Navigation Navigation
	FaroURL    string
	Version    string
	PublicMode bool
	// PublicNavigation creates an independent sandbox manager per credential.
	PublicNavigation func() ManagedNavigation
}
type Server struct {
	config   Config
	sessions *sessionStore
	slots    chan struct{}
	mux      *http.ServeMux
}
type target struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	SHA    string `json:"sha"`
	Path   string `json:"path"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var shaPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

func New(c Config) http.Handler {
	if c.PublicMode {
		return newPublicHandler(c)
	}
	return newHandler(c)
}

func newHandler(c Config) http.Handler {
	if c.Origin == "" {
		c.Origin = "http://" + c.Host
	}
	s := &Server{config: c, sessions: newSessionStore(), slots: make(chan struct{}, 8), mux: http.NewServeMux()}
	s.route("/api/config", s.configHandler)
	s.route("/api/pr", s.load)
	s.route("/api/content", s.content)
	s.route("/api/diff", s.fileDiff)
	s.route("/api/workspace", s.workspace)
	s.route("/api/workspace/file", s.file)
	s.route("/api/navigate", s.navigate)
	s.route("/api/navigation/capabilities", s.navigationCapabilities)
	s.route("/api/review", s.review)
	s.route("/api/comments", s.comments)
	s.route("/api/reply", s.reply)
	s.route("/api/telemetry", s.telemetry)
	s.route("/api/session/open", s.openSession)
	s.route("/api/session/sync", s.syncSession)
	s.route("/api/session/close", s.closeSession)
	s.route("/api/session/list", s.listSessions)
	s.route("/api/session/remove", s.removeSession)
	mcpHandler := s.mcpHandler()
	for _, method := range []string{"GET", "POST", "DELETE"} {
		s.mux.Handle(method+" /mcp", mcpHandler)
	}
	s.mux.HandleFunc("GET /config.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		config, _ := json.Marshal(map[string]bool{"telemetryEnabled": c.FaroURL != "", "publicMode": c.PublicMode})
		_, _ = fmt.Fprintf(w, "globalThis.__FJORD_CONFIG__=%s;\n", config)
	})
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonReply(w, map[string]string{"status": "ok"}) })
	if c.Assets != nil {
		s.mux.Handle("GET /", staticHandler(c.Assets))
	}
	return secureHandler(c, s.mux)
}

func secureHandler(c Config, next http.Handler) http.Handler {
	if c.Origin == "" {
		c.Origin = "http://" + c.Host
	}
	origin, host, originErr := PublicAddress(c.Origin)
	validOrigin := originErr == nil && origin == c.Origin && host == c.Host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GitHub attachment URLs redirect to its dedicated user-asset bucket.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' https://github.com/user-attachments/ https://*.githubusercontent.com https://github-production-user-asset-6210df.s3.amazonaws.com; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		if !validOrigin || r.Host != c.Host {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if c.PublicMode && strings.HasPrefix(c.Origin, "https://") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) route(path string, fn func(http.ResponseWriter, *http.Request) error) {
	s.mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
		// Exact origin and JSON checks block drive-by web pages and DNS rebinding.
		if r.Header.Get("Origin") != s.config.Origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			jsonError(w, "request origin is not allowed", http.StatusForbidden)
			return
		}
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "application/json" {
			jsonError(w, "application/json is required", http.StatusUnsupportedMediaType)
			return
		}
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			jsonError(w, "Fjord is busy; try again shortly", http.StatusTooManyRequests)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))
		ctx, span := otel.Tracer("fjord/server").Start(ctx, path)
		defer span.End()
		defer func() {
			if recover() != nil {
				span.SetStatus(codes.Error, "handler failed")
				slog.ErrorContext(ctx, "request handler failed", "operation", path)
				jsonError(w, "Fjord could not complete this operation; check GitHub before retrying a review or reply", http.StatusInternalServerError)
			}
		}()
		span.SetAttributes(attribute.String("http.request.method", "POST"), attribute.String("http.route", path))
		r = r.WithContext(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
		if err := fn(w, r); err != nil {
			span.SetStatus(codes.Error, "operation failed")
			slog.ErrorContext(ctx, "request failed", "operation", path)
			if errors.Is(err, errSessionAuthorization) {
				jsonError(w, err.Error(), http.StatusForbidden)
			} else if errors.Is(err, errSessionRemoved) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusGone)
				jsonReply(w, map[string]string{"error": err.Error(), "code": "session_removed"})
			} else if errors.Is(err, errSessionGone) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				jsonReply(w, map[string]string{"error": err.Error(), "code": "session_not_found"})
			} else {
				jsonError(w, err.Error(), http.StatusBadRequest)
			}
			return
		}
		slog.InfoContext(ctx, "request completed", "operation", path)
	})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return errors.New("invalid request JSON")
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return errors.New("unexpected data after request")
	}
	return nil
}
func jsonReply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
func parsePR(raw string) (target, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return target{}, errors.New("enter a GitHub PR URL such as https://github.com/owner/repo/pull/123")
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(p) == 5 && (p[4] == "changes" || p[4] == "files") {
		p = p[:4]
	}
	if len(p) != 4 || p[2] != "pull" {
		return target{}, errors.New("URL must identify one GitHub pull request")
	}
	n, err := strconv.Atoi(p[3])
	if err != nil || n < 1 || !identifier.MatchString(p[0]) || !identifier.MatchString(p[1]) {
		return target{}, errors.New("invalid GitHub pull request")
	}
	return target{Owner: p[0], Repo: p[1], Number: n}, nil
}
func (s *Server) configHandler(w http.ResponseWriter, r *http.Request) error {
	var in struct{}
	if err := decode(r, &in); err != nil {
		return err
	}
	jsonReply(w, map[string]any{"telemetryEnabled": s.config.FaroURL != "", "publicMode": s.config.PublicMode})
	return nil
}
func (s *Server) load(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		URL string `json:"url"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	t, err := parsePR(in.URL)
	if err != nil {
		return err
	}
	p, err := s.config.GitHub.Load(r.Context(), t.Owner, t.Repo, t.Number)
	if err != nil {
		return err
	}
	jsonReply(w, p)
	return nil
}
func (s *Server) content(w http.ResponseWriter, r *http.Request) error {
	var in target
	if err := decode(r, &in); err != nil {
		return err
	}
	c, err := s.config.GitHub.Content(r.Context(), in.Owner, in.Repo, in.SHA, in.Path)
	if err != nil {
		return err
	}
	jsonReply(w, map[string]string{"content": c})
	return nil
}
func (s *Server) fileDiff(w http.ResponseWriter, r *http.Request) error {
	var in github.DiffRequest
	if err := decode(r, &in); err != nil {
		return err
	}
	result, err := github.ReconstructDiff(r.Context(), in, s.config.GitHub.Content)
	if err != nil {
		return err
	}
	jsonReply(w, result)
	return nil
}
func (s *Server) workspace(w http.ResponseWriter, r *http.Request) error {
	var in target
	if err := decode(r, &in); err != nil {
		return err
	}
	if !shaPattern.MatchString(in.SHA) {
		return errors.New("workspace requires an immutable 40-character commit SHA")
	}
	archive, err := s.config.GitHub.Archive(r.Context(), in.Owner, in.Repo, in.SHA)
	if err != nil {
		return err
	}
	defer archive.Close()
	id, err := s.config.Navigation.Prepare(r.Context(), in.Owner, in.Repo, in.SHA, archive)
	if err != nil {
		return err
	}
	jsonReply(w, map[string]string{"id": id})
	return nil
}
func (s *Server) file(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	c, err := s.config.Navigation.File(r.Context(), in.ID, in.Path)
	if err != nil {
		return err
	}
	jsonReply(w, map[string]string{"content": c})
	return nil
}
func (s *Server) navigate(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		ID        string                  `json:"id"`
		Path      string                  `json:"path"`
		Method    string                  `json:"method"`
		Line      int                     `json:"line"`
		Character int                     `json:"character"`
		Options   navigation.QueryOptions `json:"options"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	result, err := s.config.Navigation.Inspect(r.Context(), in.ID, in.Path, in.Method, in.Line, in.Character, in.Options)
	if err != nil {
		return err
	}
	jsonReply(w, result)
	return nil
}

func (s *Server) navigationCapabilities(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	result, err := s.config.Navigation.Capabilities(r.Context(), in.ID, in.Path)
	if err != nil {
		return err
	}
	jsonReply(w, result)
	return nil
}
func (s *Server) review(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
		Number int    `json:"number"`
		github.Review
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	u, err := s.config.GitHub.Review(r.Context(), in.Owner, in.Repo, in.Number, in.Review)
	if err != nil {
		return err
	}
	jsonReply(w, map[string]string{"url": u})
	return nil
}
func (s *Server) comments(w http.ResponseWriter, r *http.Request) error {
	var in target
	if err := decode(r, &in); err != nil {
		return err
	}
	c, err := s.config.GitHub.Comments(r.Context(), in.Owner, in.Repo, in.Number)
	if err != nil {
		return err
	}
	conversation, err := s.config.GitHub.Conversation(r.Context(), in.Owner, in.Repo, in.Number)
	if err != nil {
		return err
	}
	jsonReply(w, map[string]any{"comments": c, "conversation": conversation})
	return nil
}
func (s *Server) reply(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Owner     string `json:"owner"`
		Repo      string `json:"repo"`
		Number    int    `json:"number"`
		CommentID int64  `json:"commentID"`
		Body      string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	u, err := s.config.GitHub.Reply(r.Context(), in.Owner, in.Repo, in.Number, in.CommentID, in.Body)
	if err != nil {
		return err
	}
	jsonReply(w, map[string]string{"url": u})
	return nil
}
func ValidateFaro(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("FJORD_FARO_URL must be an HTTPS collector URL without query or credentials")
	}
	return nil
}
