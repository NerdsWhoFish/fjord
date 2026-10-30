package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	logs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	traces "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestOTLPExportsCorrelatedSignals(t *testing.T) {
	oldLogger, oldTracer, oldPropagator, oldErrors := slog.Default(), otel.GetTracerProvider(), otel.GetTextMapPropagator(), otel.GetErrorHandler()
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
		otel.SetTracerProvider(oldTracer)
		otel.SetTextMapPropagator(oldPropagator)
		otel.SetErrorHandler(oldErrors)
	})
	var mu sync.Mutex
	var traceRequest traces.ExportTraceServiceRequest
	var logRequest logs.ExportLogsServiceRequest
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Basic test-only" {
			t.Error("missing exporter authorization")
		}
		if r.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Error("not OTLP protobuf")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/traces":
			err = proto.Unmarshal(body, &traceRequest)
		case "/v1/logs":
			err = proto.Unmarshal(body, &logRequest)
		default:
			t.Errorf("unexpected signal path %s", r.URL.Path)
		}
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	defer collector.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Basic%20test-only")
	shutdown, err := Setup(context.Background(), "test-build")
	if err != nil {
		t.Fatal(err)
	}
	parent := otel.GetTextMapPropagator().Extract(context.Background(), propagation.MapCarrier{"traceparent": "00-11111111111111111111111111111111-2222222222222222-01"})
	ctx, span := otel.Tracer("test").Start(parent, "review.test")
	slog.InfoContext(ctx, "request completed", "operation", "/api/pr")
	span.End()
	flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = shutdown(flush); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(traceRequest.ResourceSpans) != 1 || len(logRequest.ResourceLogs) != 1 {
		t.Fatal("both signals must arrive")
	}
	s := traceRequest.ResourceSpans[0].ScopeSpans[0].Spans[0]
	l := logRequest.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	if string(s.TraceId) != string(l.TraceId) || string(s.SpanId) != string(l.SpanId) || string(s.TraceId) != stringsRepeatByte(0x11, 16) {
		t.Fatal("trace propagation or log correlation failed")
	}
	if l.Body.GetStringValue() != "request completed" {
		t.Fatal("unexpected log body")
	}
}
func stringsRepeatByte(value byte, count int) string {
	b := make([]byte, count)
	for i := range b {
		b[i] = value
	}
	return string(b)
}
