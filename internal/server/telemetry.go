package server

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"
)

// Rebuild the envelope rather than forwarding browser-supplied metadata or text.
func safeEnvelope(raw map[string]json.RawMessage, version string) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result := map[string]any{"meta": map[string]any{"app": map[string]string{"name": "fjord", "version": version, "environment": "local"}, "sdk": map[string]string{"name": "fjord-relay", "version": "0.1.0"}}}
	var exceptions []json.RawMessage
	if json.Unmarshal(raw["exceptions"], &exceptions) == nil && len(exceptions) > 0 {
		result["exceptions"] = []any{map[string]any{"type": "Error", "value": "Browser operation failed", "timestamp": now}}
	}
	var logs []json.RawMessage
	if json.Unmarshal(raw["logs"], &logs) == nil && len(logs) > 0 {
		result["logs"] = []any{map[string]any{"level": "error", "message": "Browser operation failed", "timestamp": now}}
	}
	var events []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw["events"], &events) == nil {
		allowed := map[string]bool{"pr.load": true, "navigate": true, "review.submit": true, "assist": true, "reply": true, "view_changed": true}
		clean := []any{}
		for _, e := range events {
			if allowed[e.Name] && len(clean) < 20 {
				clean = append(clean, map[string]string{"name": e.Name, "timestamp": now})
			}
		}
		if len(clean) > 0 {
			result["events"] = clean
		}
	}
	var measures []struct {
		Type   string             `json:"type"`
		Values map[string]float64 `json:"values"`
	}
	if json.Unmarshal(raw["measurements"], &measures) == nil {
		allowed := map[string]bool{"CLS": true, "LCP": true, "FCP": true, "INP": true, "TTFB": true}
		clean := []any{}
		for _, m := range measures {
			if m.Type != "web-vitals" && m.Type != "web-vital" {
				continue
			}
			values := map[string]float64{}
			for k, v := range m.Values {
				if allowed[k] && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v < 1e9 {
					values[k] = v
				}
			}
			if len(values) > 0 && len(clean) < 10 {
				clean = append(clean, map[string]any{"type": "web-vitals", "values": values, "timestamp": now})
			}
		}
		if len(clean) > 0 {
			result["measurements"] = clean
		}
	}
	return result
}
func (s *Server) telemetry(w http.ResponseWriter, r *http.Request) error {
	if s.config.FaroURL == "" {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	var raw map[string]json.RawMessage
	if err := decode(r, &raw); err != nil {
		return err
	}
	envelope := safeEnvelope(raw, s.config.Version)
	if len(envelope) == 1 {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	if envelope["exceptions"] != nil || envelope["logs"] != nil {
		slog.ErrorContext(r.Context(), "browser operation failed", "operation", "browser.error")
	}
	body, _ := json.Marshal(envelope)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.config.FaroURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid telemetry configuration")
	}
	req.Header.Set("Content-Type", "application/json")
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return errors.New("telemetry identifier unavailable")
	}
	req.Header.Set("X-Faro-Session-Id", hex.EncodeToString(id[:]))
	client := http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("browser telemetry delivery failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("browser telemetry collector rejected delivery")
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}
