package lamux

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

var (
	tracer = otel.Tracer("lamux")
)

func (l *Lamux) setupOtelSDK(ctx context.Context) (shutdown func(context.Context) error, err error) {
	if !l.Config.TraceConfig.enableTrace {
		return func(context.Context) error { return nil }, nil
	}
	slog.InfoContext(ctx, "setting up Otel SDK")

	var shutdownFuncs []func(context.Context) error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	// Set up propagator.
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	// Set up trace provider.
	tracerProvider, err := l.newTraceProvider(ctx)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	return
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func (l *Lamux) newTraceProvider(ctx context.Context) (*trace.TracerProvider, error) {
	tc := l.Config.TraceConfig
	var client otlptrace.Client
	if tc.TraceProtocol == "http" {
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(tc.TraceEndpoint),
			otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
		}
		if tc.TraceInsecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(tc.TraceHeaders) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(tc.TraceHeaders))
		}
		client = otlptracehttp.NewClient(opts...)
	} else {
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(tc.TraceEndpoint),
		}
		if tc.TraceInsecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(tc.TraceHeaders) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(tc.TraceHeaders))
		}
		client = otlptracegrpc.NewClient(opts...)
	}
	traceExporter, err := otlptrace.New(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	resources, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceName(tc.TraceService),
			semconv.ServiceVersion(Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	traceProvider := trace.NewTracerProvider(
		// trace.WithSyncer(traceExporter), TODO: use this instead of WithBatcher
		trace.WithBatcher(traceExporter),
		trace.WithResource(resources),
	)
	return traceProvider, nil
}
