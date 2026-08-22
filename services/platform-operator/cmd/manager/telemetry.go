package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	ctrl "sigs.k8s.io/controller-runtime"
)

const defaultServiceName = "fruto-platform-operator"

type tracingShutdown func(context.Context) error

func configureTracing(
	ctx context.Context,
	serviceVersion string,
) (trace.TracerProvider, tracingShutdown, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		ctrl.Log.WithName("opentelemetry").Error(err, "telemetry export failed")
	}))

	if !tracingEnabled() {
		provider := noop.NewTracerProvider()
		otel.SetTracerProvider(provider)
		return provider, func(context.Context) error { return nil }, nil
	}

	exporter, err := newSpanExporter(ctx)
	if err != nil {
		return nil, nil, err
	}

	serviceName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = defaultServiceName
	}
	serviceResource, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", serviceVersion),
		),
	)
	if err != nil {
		return nil, nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(serviceResource),
	)
	otel.SetTracerProvider(provider)
	return provider, provider.Shutdown, nil
}

func newSpanExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	exporter := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER")))
	if exporter != "otlp" {
		return nil, fmt.Errorf("unsupported OTEL_TRACES_EXPORTER %q", exporter)
	}

	protocol := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL")))
	if protocol == "" {
		protocol = strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")))
	}
	if protocol == "" {
		protocol = "http/protobuf"
	}

	var (
		spanExporter *otlptrace.Exporter
		err          error
	)
	switch protocol {
	case "http/protobuf":
		spanExporter, err = otlptracehttp.New(ctx)
	case "grpc":
		spanExporter, err = otlptracegrpc.New(ctx)
	default:
		return nil, fmt.Errorf("unsupported OTLP trace protocol %q", protocol)
	}
	if err != nil {
		return nil, err
	}
	return spanExporter, nil
}

func tracingEnabled() bool {
	exporter := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER")))
	return exporter != "" && exporter != "none"
}
