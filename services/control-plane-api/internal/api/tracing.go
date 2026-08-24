package api

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

func tracingMiddleware(server *Server, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := server.Tracer.Start(r.Context(), "control-plane.http.request")
		defer span.End()
		span.SetAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("http.route", sanitizedRoute(r.URL.Path)),
		)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func sanitizedRoute(path string) string {
	switch {
	case path == "/healthz", path == "/readyz", path == "/api/v1/session", path == "/api/v1/workspaces/current", path == "/api/v1/deployments":
		return path
	case strings.HasPrefix(path, "/api/v1/operations/"):
		return "/api/v1/operations/{operationId}"
	case strings.HasSuffix(path, "/operations") && strings.HasPrefix(path, "/api/v1/deployments/"):
		return "/api/v1/deployments/{deploymentId}/operations"
	case strings.HasPrefix(path, "/api/v1/deployments/"):
		return "/api/v1/deployments/{deploymentId}"
	default:
		return "unmatched"
	}
}
