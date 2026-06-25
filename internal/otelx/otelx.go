package otelx

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

var durationBucketsSeconds = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

// MustSetupMetrics configures the global MeterProvider with a Prometheus
// exporter and returns the HTTP handler to serve scraped metrics from
// (mount it at /metrics).
func MustSetupMetrics() http.Handler {
	exporter, err := prometheus.New()
	if err != nil {
		panic(err)
	}

	durationView := sdkmetric.NewView(
		sdkmetric.Instrument{Kind: sdkmetric.InstrumentKindHistogram},
		sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: durationBucketsSeconds}},
	)

	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter), sdkmetric.WithView(durationView)))

	return promhttp.Handler()
}

// MustSetupTracing configures the global TracerProvider and returns a
// shutdown function for the caller to call on exit.
//
// Spans are printed to stdout to avoid standing up extra infrastructure.
// Ideally this would be an OTLP exporter to a real collector (e.g. Jaeger)
// for a proper trace-viewer UI.
func MustSetupTracing(serviceName string) func(context.Context) error {
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		panic(err)
	}

	res := resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName))

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)

	return provider.Shutdown
}
