package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	logSDK "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	traceSDK "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type handler struct{ local, remote slog.Handler }

func Environment() string {
	if value := os.Getenv("FJORD_ENVIRONMENT"); value != "" {
		return value
	}
	return "local"
}

func (h handler) Enabled(ctx context.Context, l slog.Level) bool { return h.local.Enabled(ctx, l) }
func (h handler) Handle(ctx context.Context, r slog.Record) error {
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		r.AddAttrs(slog.String("trace_id", span.TraceID().String()), slog.String("span_id", span.SpanID().String()))
	}
	err := h.local.Handle(ctx, r)
	if h.remote != nil {
		err = errors.Join(err, h.remote.Handle(ctx, r))
	}
	return err
}
func (h handler) WithAttrs(a []slog.Attr) slog.Handler {
	h.local = h.local.WithAttrs(a)
	if h.remote != nil {
		h.remote = h.remote.WithAttrs(a)
	}
	return h
}
func (h handler) WithGroup(g string) slog.Handler {
	h.local = h.local.WithGroup(g)
	if h.remote != nil {
		h.remote = h.remote.WithGroup(g)
	}
	return h
}

func Setup(ctx context.Context, version string) (func(context.Context) error, error) {
	local := slog.NewJSONHandler(os.Stderr, nil)
	slog.SetDefault(slog.New(handler{local: local}))
	// Exporter diagnostics can contain transport URLs and credential-bearing headers.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) { slog.Error("telemetry export failed", "operation", "telemetry.export") }))
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	res := resource.NewSchemaless(attribute.String("service.name", "fjord"), attribute.String("service.version", version), attribute.String("deployment.environment.name", Environment()))
	opts := []traceSDK.TracerProviderOption{traceSDK.WithResource(res)}
	var lp *logSDK.LoggerProvider
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, errors.New("cannot configure trace exporter")
		}
		opts = append(opts, traceSDK.WithBatcher(exporter))
		logs, err := otlploghttp.New(ctx)
		if err != nil {
			_ = exporter.Shutdown(ctx)
			return nil, errors.New("cannot configure log exporter")
		}
		lp = logSDK.NewLoggerProvider(logSDK.WithResource(res), logSDK.WithProcessor(logSDK.NewBatchProcessor(logs)))
		slog.SetDefault(slog.New(handler{local: local, remote: otelslog.NewHandler("fjord", otelslog.WithLoggerProvider(lp))}))
	}
	tp := traceSDK.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	return func(ctx context.Context) error {
		err := tp.Shutdown(ctx)
		if lp != nil {
			err = errors.Join(err, lp.Shutdown(ctx))
		}
		return err
	}, nil
}
