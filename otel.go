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
	tracer = otel.Tracer("github.com/fujiwara/lamux")
)

type TraceConfig struct {
	TraceEndpoint string            `help:"Otel trace endpoint (e.g. localhost:4318)" env:"OTEL_EXPORTER_OTLP_ENDPOINT" name:"trace-endpoint"`
	TraceInsecure bool              `help:"Disable TLS for Otel trace endpoint" env:"OTEL_EXPORTER_OTLP_INSECURE" name:"trace-insecure"`
	TraceProtocol string            `help:"Otel trace protocol" env:"OTEL_EXPORTER_OTLP_PROTOCOL" name:"trace-protocol" default:"http/protobuf" enum:"http/protobuf,grpc"`
	TraceHeaders  map[string]string `help:"Additional headers for Otel trace endpoint (key1=value1;key2=value2)" env:"OTEL_EXPORTER_OTLP_HEADERS" name:"trace-headers"`
	TraceService  string            `help:"Service name for Otel trace" env:"OTEL_SERVICE_NAME" name:"trace-service" default:"lamux"`

	enableTrace bool
}

func setupOtelSDK(ctx context.Context, tc *TraceConfig) (shutdown func(context.Context) error, err error) {
	if !tc.enableTrace {
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
	tracerProvider, err := newTraceProvider(ctx, tc)
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

func newTraceProvider(ctx context.Context, tc *TraceConfig) (*trace.TracerProvider, error) {
	var client otlptrace.Client
	switch tc.TraceProtocol {
	case "http/protobuf":
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
	case "grpc":
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
	default:
		return nil, fmt.Errorf("unsupported trace protocol: %s", tc.TraceProtocol)
	}

	traceExporter, err := otlptrace.New(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	resources, err := resource.New(
		ctx,
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName(tc.TraceService),
			semconv.ServiceVersion(Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithSyncer(traceExporter),
		// trace.WithBatcher(traceExporter), TODO: configure batcher
		trace.WithResource(resources),
	)
	return traceProvider, nil
}
