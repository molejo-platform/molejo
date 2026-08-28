package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
)

func tracingMiddleware(server *Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := server.Tracer.Start(r.Context(), "control-plane.http.request")
			defer span.End()
			span.SetAttributes(attribute.String("http.request.method", r.Method))
			next.ServeHTTP(w, r.WithContext(ctx))
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}
			span.SetAttributes(attribute.String("http.route", route))
		})
	}
}
