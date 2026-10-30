package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/NerdsWhoFish/fjord/internal/github"
	"github.com/NerdsWhoFish/fjord/internal/navigation"
)

const sessionTTL = 2 * time.Hour
const maxSessions = 32
const maxPendingActions = 100
const maxActionReceipts = 512

var draftIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var errSessionGone = errors.New("review session expired or was closed")

type sessionComment struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartLine int    `json:"startLine,omitempty"`
	StartSide string `json:"startSide,omitempty"`
	Body      string `json:"body"`
	Source    string `json:"source,omitempty"`
}

type sessionDraft struct {
	Comments []sessionComment `json:"comments"`
	Body     string           `json:"body"`
	Event    string           `json:"event"`
}

type sessionSelection struct {
	Path      string `json:"path"`
	SHA       string `json:"sha"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

type browserNavigation struct {
	navigation.Result
	Definitions []navigation.Location `json:"definitions,omitempty"`
	References  []navigation.Location `json:"references,omitempty"`
}

type browserAction struct {
	ID        string             `json:"id"`
	Kind      string             `json:"kind"`
	Comment   *sessionComment    `json:"comment,omitempty"`
	Path      string             `json:"path,omitempty"`
	SHA       string             `json:"sha,omitempty"`
	Line      int                `json:"line,omitempty"`
	Character int                `json:"character,omitempty"`
	Method    string             `json:"method,omitempty"`
	Result    *browserNavigation `json:"result,omitempty"`
}

type actionReceipt struct {
	ID        string `json:"id"`
	RequestID string `json:"requestId"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	hash      [32]byte
}

type reviewSession struct {
	ID         string            `json:"id"`
	PR         github.PR         `json:"pr"`
	Draft      sessionDraft      `json:"draft"`
	Selection  *sessionSelection `json:"selection,omitempty"`
	Actions    []browserAction   `json:"actions"`
	Receipts   []actionReceipt   `json:"receipts"`
	UpdatedAt  time.Time         `json:"updatedAt"`
	ExpiresAt  time.Time         `json:"expiresAt"`
	weight     int
	workspaces map[string]string
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*reviewSession
	now      func() time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]*reviewSession), now: time.Now}
}

func randomID() string {
	var bytes [16]byte
	_, _ = rand.Read(bytes[:])
	return hex.EncodeToString(bytes[:])
}

func (s *sessionStore) prune() {
	now := s.now()
	for id, session := range s.sessions {
		if !now.Before(session.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

func cloneSession(session *reviewSession) reviewSession {
	next := *session
	next.Draft.Comments = slices.Clone(session.Draft.Comments)
	next.Actions = slices.Clone(session.Actions)
	next.Receipts = slices.Clone(session.Receipts)
	if session.Selection != nil {
		selected := *session.Selection
		next.Selection = &selected
	}
	return next
}

func (s *sessionStore) open(pr github.PR, draft sessionDraft) (string, error) {
	if err := validateSessionDraft(pr, draft); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if len(s.sessions) >= maxSessions {
		return "", errors.New("too many active review sessions; close another Fjord tab first")
	}
	encoded, _ := json.Marshal(pr)
	weight := len(encoded) + 384*1024
	total := weight
	for _, session := range s.sessions {
		total += session.weight
	}
	if total > 64<<20 {
		return "", errors.New("review sessions exceed the 64 MiB snapshot limit; close another Fjord tab first")
	}
	id := randomID()
	now := s.now()
	s.sessions[id] = &reviewSession{ID: id, PR: pr, Draft: draft, Actions: []browserAction{}, Receipts: []actionReceipt{}, UpdatedAt: now, ExpiresAt: now.Add(sessionTTL), weight: weight, workspaces: map[string]string{}}
	return id, nil
}

func (s *sessionStore) get(id string) (reviewSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	session := s.sessions[id]
	if session == nil {
		return reviewSession{}, errSessionGone
	}
	return cloneSession(session), nil
}

func (s *sessionStore) list() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	result := make([]map[string]any, 0, len(s.sessions))
	for _, session := range s.sessions {
		result = append(result, map[string]any{
			"id": session.ID, "owner": session.PR.Owner, "repo": session.PR.Repo,
			"number": session.PR.Number, "title": session.PR.Title,
			"headSHA": session.PR.HeadSHA, "baseSHA": session.PR.BaseSHA,
			"updatedAt": session.UpdatedAt, "expiresAt": session.ExpiresAt,
		})
	}
	return result
}

func (s *sessionStore) close(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *sessionStore) workspace(id, sha, value string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.sessions[id]; session != nil {
		if value != "" {
			session.workspaces[sha] = value
		}
		return session.workspaces[sha]
	}
	return ""
}

func (s *sessionStore) sync(id string, draft sessionDraft, selection *sessionSelection, acknowledgements []actionReceipt) (reviewSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	session := s.sessions[id]
	if session == nil {
		return reviewSession{}, errSessionGone
	}
	if err := validateSessionDraft(session.PR, draft); err != nil {
		return reviewSession{}, err
	}
	if selection != nil && (!validSessionPath(selection.Path) || !sessionSHA(session.PR, selection.SHA) || selection.Line < 1 || selection.Character < 0) {
		return reviewSession{}, errors.New("selected code must identify a valid path and position in this review revision")
	}
	if len(acknowledgements) > maxPendingActions {
		return reviewSession{}, errors.New("too many action acknowledgements")
	}
	for _, ack := range acknowledgements {
		if ack.Status != "applied" && ack.Status != "failed" {
			return reviewSession{}, errors.New("action acknowledgement must be applied or failed")
		}
		if len(ack.Message) > 512 || !utf8.ValidString(ack.Message) || strings.IndexFunc(ack.Message, unicode.IsControl) >= 0 {
			return reviewSession{}, errors.New("invalid action acknowledgement message")
		}
		if !slices.ContainsFunc(session.Receipts, func(receipt actionReceipt) bool { return receipt.ID == ack.ID }) {
			return reviewSession{}, errors.New("unknown action acknowledgement")
		}
	}
	// Commit drafts with acknowledgements so lost responses can replay safely.
	session.Draft = draft
	session.Selection = selection
	for _, ack := range acknowledgements {
		for i := range session.Receipts {
			if session.Receipts[i].ID == ack.ID && session.Receipts[i].Status == "pending" {
				session.Receipts[i].Status = ack.Status
				session.Receipts[i].Message = ack.Message
			}
		}
		session.Actions = slices.DeleteFunc(session.Actions, func(action browserAction) bool { return action.ID == ack.ID })
	}
	now := s.now()
	session.UpdatedAt, session.ExpiresAt = now, now.Add(sessionTTL)
	return cloneSession(session), nil
}

func (s *sessionStore) enqueue(id, requestID string, payload any, action browserAction) (actionReceipt, error) {
	if !draftIDPattern.MatchString(requestID) {
		return actionReceipt{}, errors.New("requestId must be 1 to 128 letters, numbers, underscores, or hyphens")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return actionReceipt{}, errors.New("invalid action request")
	}
	hash := sha256.Sum256(encoded)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	session := s.sessions[id]
	if session == nil {
		return actionReceipt{}, errSessionGone
	}
	for _, receipt := range session.Receipts {
		if receipt.RequestID == requestID {
			if receipt.hash != hash {
				return actionReceipt{}, errors.New("requestId was already used for a different action")
			}
			return receipt, nil
		}
	}
	if len(session.Actions) >= maxPendingActions || len(session.Receipts) >= maxActionReceipts {
		return actionReceipt{}, errors.New("review action limit reached; apply pending actions or reconnect the review")
	}
	actionBytes, _ := json.Marshal(action)
	pendingBytes, _ := json.Marshal(session.Actions)
	if len(actionBytes) > 1<<20 || len(actionBytes)+len(pendingBytes) > 4<<20 {
		return actionReceipt{}, errors.New("browser action results exceed the review queue size limit")
	}
	if action.Kind == "draft" {
		count := len(session.Draft.Comments)
		for _, pending := range session.Actions {
			if pending.Kind == "draft" && pending.Comment != nil &&
				!slices.ContainsFunc(session.Draft.Comments, func(comment sessionComment) bool { return comment.ID == pending.Comment.ID }) {
				count++
			}
		}
		if count >= 100 {
			return actionReceipt{}, errors.New("review already contains 100 comments including pending AI drafts")
		}
	}
	action.ID = randomID()
	if action.Comment != nil {
		action.Comment.ID = action.ID
	}
	receipt := actionReceipt{ID: action.ID, RequestID: requestID, Status: "pending", hash: hash}
	session.Actions = append(session.Actions, action)
	session.Receipts = append(session.Receipts, receipt)
	return receipt, nil
}

func validateSessionDraft(pr github.PR, draft sessionDraft) error {
	data, err := json.Marshal(draft)
	if err != nil || len(data) > 384*1024 {
		return errors.New("review draft exceeds the 384 KiB session limit")
	}
	comments := make([]github.Comment, 0, len(draft.Comments))
	ids := make(map[string]bool)
	for _, comment := range draft.Comments {
		if !draftIDPattern.MatchString(comment.ID) || ids[comment.ID] || (comment.Source != "" && comment.Source != "ai" && comment.Source != "human") {
			return errors.New("draft comments need distinct valid IDs and recognized sources")
		}
		ids[comment.ID] = true
		comments = append(comments, github.Comment{Path: comment.Path, Line: comment.Line, Side: comment.Side, StartLine: comment.StartLine, StartSide: comment.StartSide, Body: comment.Body})
	}
	return github.ValidateDraft(pr, github.Review{HeadSHA: pr.HeadSHA, BaseSHA: pr.BaseSHA, Event: draft.Event, Body: draft.Body, Comments: comments})
}

func sessionSHA(pr github.PR, sha string) bool {
	return sha == pr.HeadSHA || sha == pr.BaseSHA
}

func validSessionPath(path string) bool {
	if path == "" || len(path) > 4096 || !utf8.ValidString(path) || strings.Contains(path, "\\") || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (s *Server) openSession(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		URL     string       `json:"url"`
		HeadSHA string       `json:"headSHA"`
		BaseSHA string       `json:"baseSHA"`
		Draft   sessionDraft `json:"draft"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	t, err := parsePR(input.URL)
	if err != nil {
		return err
	}
	pr, err := s.config.GitHub.Load(r.Context(), t.Owner, t.Repo, t.Number)
	if err != nil {
		return err
	}
	if input.HeadSHA != pr.HeadSHA || input.BaseSHA != pr.BaseSHA {
		return errors.New("PR revision changed; refresh before connecting AI assistance")
	}
	id, err := s.sessions.open(pr, input.Draft)
	if err != nil {
		return err
	}
	session, _ := s.sessions.get(id)
	jsonReply(w, map[string]any{"id": id, "expiresAt": session.ExpiresAt})
	return nil
}

func (s *Server) syncSession(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		ID               string            `json:"id"`
		Draft            sessionDraft      `json:"draft"`
		Selection        *sessionSelection `json:"selection,omitempty"`
		Acknowledgements []actionReceipt   `json:"acknowledgements"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	session, err := s.sessions.sync(input.ID, input.Draft, input.Selection, input.Acknowledgements)
	if err != nil {
		return err
	}
	jsonReply(w, map[string]any{"actions": session.Actions, "expiresAt": session.ExpiresAt})
	return nil
}

func (s *Server) closeSession(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		ID string `json:"id"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	s.sessions.close(input.ID)
	jsonReply(w, map[string]bool{"closed": true})
	return nil
}
