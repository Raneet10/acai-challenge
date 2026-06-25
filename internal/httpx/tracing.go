package httpx

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const instrumentationName = "github.com/acai-travel/tech-challenge/internal/httpx"

var tracer = otel.Tracer(instrumentationName)

func Tracing() func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path)
			defer span.End()

			span.SetAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", r.URL.Path),
			)

			saw := &statusAwareResponseWriter{ResponseWriter: w, status: http.StatusOK}

			handler.ServeHTTP(saw, r.WithContext(ctx))

			span.SetAttributes(attribute.Int("http.status_code", saw.status))
			if saw.status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(saw.status))
			}
		})
	}
}
