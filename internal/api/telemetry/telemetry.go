// Package telemetry sets up OpenTelemetry tracing over OTLP gRPC and the pgx
// query tracer the database pool uses.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config selects where traces go and how the service names itself.
type Config struct {
	// ServiceName, ServiceVersion and Environment become the service.name,
	// service.version and deployment.environment resource attributes.
	ServiceName    string
	ServiceVersion string
	Environment    string
	// Endpoint is OTEL_EXPORTER_OTLP_ENDPOINT. Empty disables export: spans
	// are still created (trace ids still reach the logs) but go nowhere.
	// A URL with an http scheme is plaintext gRPC, https is TLS; a bare
	// host:port is TLS unless Insecure is set.
	Endpoint string
	// Insecure allows a plaintext gRPC connection to a bare host:port. It is
	// for the local collector only and is never a default.
	Insecure bool
}

// Setup installs the global tracer provider and W3C trace-context
// propagation. The returned shutdown flushes buffered spans; call it once on
// exit.
//
// Export never blocks a request: spans go through a batch processor whose
// queue drops spans when the collector is slow or down, and the gRPC
// connection is made lazily, so an unreachable collector does not fail Setup.
func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	if cfg.ServiceName == "" {
		return nil, errors.New("telemetry: service name is required")
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String("deployment.environment", cfg.Environment),
	))
	if err != nil {
		return nil, fmt.Errorf("telemetry: build resource: %w", err)
	}

	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if cfg.Endpoint != "" {
		exp, err := otlptracegrpc.New(ctx, exporterOptions(cfg)...)
		if err != nil {
			return nil, fmt.Errorf("telemetry: create OTLP exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	return tp.Shutdown, nil
}

func exporterOptions(cfg Config) []otlptracegrpc.Option {
	if strings.Contains(cfg.Endpoint, "://") {
		return []otlptracegrpc.Option{otlptracegrpc.WithEndpointURL(cfg.Endpoint)}
	}
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return opts
}
