package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestFaroRelayAcceptsSDKLifecycleAndVitalsWithoutBrowserMetadata(t *testing.T) {
	t.Setenv("FJORD_ENVIRONMENT", "production")
	deliveries := make(chan []byte, 1)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Error("invalid collector request")
		}
		if session := r.Header.Get("X-Faro-Session-Id"); len(session) != 32 || strings.Contains(session, "private") {
			t.Error("collector did not receive a fresh opaque envelope identifier")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("relay forwarded browser credentials")
		}
		body, _ := io.ReadAll(r.Body)
		deliveries <- body
		w.WriteHeader(http.StatusAccepted)
	}))
	defer collector.Close()
	handler := New(Config{Host: "fjord.example", Origin: "https://fjord.example", PublicMode: true, FaroURL: collector.URL, Version: "server-version"})
	t.Cleanup(func() { _ = handler.(interface{ Close() error }).Close() })
	// Faro 2.11 and browser-telemetry 0.1 send these lifecycle events and
	// lowercase vital keys. Extra untrusted fields exercise the relay boundary.
	payload := `{
		"meta":{"app":{"version":"private-browser-version"},"session":{"id":"private-session-id"},"user":{"email":"private@example.com"},"page":{"url":"https://private.example/repo?token=private-token"}},
		"events":[
			{"name":"session_start","timestamp":"2000-01-01T00:00:00Z","attributes":{"token":"private-token"}},
			{"name":"page_load","context":{"url":"https://private.example/repo"}},
			{"name":"session_resume"},{"name":"session_extend"},{"name":"private-event"}
		],
		"measurements":[
			{"type":"web-vitals","values":{"ttfb":31.25},"context":{"navigation_entry_id":"private-navigation-id"}},
			{"type":"web-vitals","values":{"fcp":104},"context":{"element":"private-dom-selector"}}
		]
	}`
	request := httptest.NewRequest(http.MethodPost, "https://fjord.example/api/telemetry", strings.NewReader(payload))
	request.Header.Set("Origin", "https://fjord.example")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+publicTokenA)
	request.Header.Set("Cookie", "private-cookie=private-value")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("SDK payload was dropped or rejected: %d %s", response.Code, response.Body.String())
	}
	body := <-deliveries
	for _, forbidden := range []string{"private", "2000-01-01", `"session"`, `"user"`, `"page"`, `"attributes"`, `"context"`} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("relay leaked browser field %q", forbidden)
		}
	}
	var envelope struct {
		Meta struct {
			App struct{ Name, Version, Environment string }
			SDK struct{ Name, Version string }
		}
		Events       []struct{ Name, Timestamp string }
		Measurements []struct {
			Type, Timestamp string
			Values          map[string]float64
		}
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Meta.App.Name != "fjord" || envelope.Meta.App.Version != "server-version" ||
		envelope.Meta.App.Environment != "production" || envelope.Meta.SDK.Version == "" {
		t.Fatal("relay did not supply trusted collector metadata")
	}
	var names []string
	for _, event := range envelope.Events {
		names = append(names, event.Name)
		if event.Timestamp == "" {
			t.Fatal("event missing relay timestamp")
		}
	}
	if !reflect.DeepEqual(names, []string{"session_start", "page_load", "session_resume", "session_extend"}) {
		t.Fatalf("SDK lifecycle events were changed or omitted: %v", names)
	}
	if len(envelope.Measurements) != 2 || envelope.Measurements[0].Values["ttfb"] != 31.25 ||
		envelope.Measurements[1].Values["fcp"] != 104 {
		t.Fatal("SDK vital measurements were changed or omitted")
	}
}

func TestFaroVitalNormalizationKeepsBoundsAndCanonicalPrecedence(t *testing.T) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{
		"measurements":[
			{"type":"web-vitals","values":{"CLS":0,"FCP":12,"FID":13,"INP":14,"LCP":999,"lcp":15,"TTFB":16,"private_value":17}},
			{"type":"web-vital","values":{"cls":-1,"fcp":1e9,"inp":1e309,"ttfb":4}},
			{"type":"private-type","values":{"lcp":1}}
		]
	}`), &raw); err != nil {
		t.Fatal(err)
	}
	// A JSON numeric overflow invalidates its measurement batch instead of
	// silently admitting a non-finite value.
	if _, ok := safeEnvelope(raw, "test")["measurements"]; ok {
		t.Fatal("accepted a non-finite measurement batch")
	}
	raw["measurements"] = json.RawMessage(`[
		{"type":"web-vitals","values":{"CLS":0,"FCP":12,"FID":13,"INP":14,"LCP":999,"lcp":15,"TTFB":16,"private_value":17}},
		{"type":"web-vital","values":{"cls":-1,"fcp":1e9,"LCP":10,"lcp":-1,"ttfb":4}},
		{"type":"private-type","values":{"lcp":1}}
	]`)
	body, _ := json.Marshal(safeEnvelope(raw, "test"))
	var envelope struct {
		Measurements []struct {
			Type   string
			Values map[string]float64
		}
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	want := []map[string]float64{
		{"cls": 0, "fcp": 12, "fid": 13, "inp": 14, "lcp": 15, "ttfb": 16},
		{"ttfb": 4},
	}
	if len(envelope.Measurements) != len(want) {
		t.Fatal("accepted an unknown measurement type or lost valid measurements")
	}
	for i, measurement := range envelope.Measurements {
		if measurement.Type != "web-vitals" || !reflect.DeepEqual(measurement.Values, want[i]) {
			t.Fatalf("incorrect canonical measurement: %+v", measurement)
		}
	}
}

func TestFaroSDKSignalsRemainBounded(t *testing.T) {
	events := make([]map[string]string, 25)
	measurements := make([]map[string]any, 15)
	for i := range events {
		events[i] = map[string]string{"name": "page_load", "sessionId": "private-session-id"}
	}
	for i := range measurements {
		measurements[i] = map[string]any{"type": "web-vitals", "values": map[string]float64{"fcp": 10}}
	}
	eventJSON, _ := json.Marshal(events)
	measurementJSON, _ := json.Marshal(measurements)
	clean := safeEnvelope(map[string]json.RawMessage{"events": eventJSON, "measurements": measurementJSON}, "test")
	if len(clean["events"].([]any)) != 20 || len(clean["measurements"].([]any)) != 10 {
		t.Fatal("SDK compatibility widened envelope limits")
	}
}
