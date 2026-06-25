package httpx

import (
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	requestsReceived  metric.Int64Counter
	requestsCompleted metric.Int64Counter
	requestErrors     metric.Int64Counter
	requestDuration   metric.Float64Histogram
)

func init() {
	meter := otel.Meter(instrumentationName)

	var err error

	requestsReceived, err = meter.Int64Counter("http.server.requests.received", metric.WithDescription("Number of HTTP requests received"))
	if err != nil {
		panic(err)
	}

	requestsCompleted, err = meter.Int64Counter("http.server.requests.completed", metric.WithDescription("Number of HTTP requests completed"))
	if err != nil {
		panic(err)
	}

	requestErrors, err = meter.Int64Counter("http.server.requests.errors", metric.WithDescription("Number of HTTP requests that returned a 5xx response"))
	if err != nil {
		panic(err)
	}

	requestDuration, err = meter.Float64Histogram("http.server.request.duration", metric.WithDescription("HTTP request duration"), metric.WithUnit("s"))
	if err != nil {
		panic(err)
	}
}

func Metrics() func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			saw := &statusAwareResponseWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			requestsReceived.Add(r.Context(), 1, metric.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", r.URL.Path),
			))

			defer func() {
				attrs := metric.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.route", r.URL.Path),
					attribute.Int("http.status_code", saw.status),
				)

				requestsCompleted.Add(r.Context(), 1, attrs)
				requestDuration.Record(r.Context(), time.Since(start).Seconds(), attrs)

				if saw.status >= http.StatusInternalServerError {
					requestErrors.Add(r.Context(), 1, attrs)
				}
			}()

			handler.ServeHTTP(saw, r)
		})
	}
}
