package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
)

type generatedHandler struct {
	server *Server
}

var _ generated.ServerInterface = (*generatedHandler)(nil)

func (s *Server) openAPIRouter() http.Handler {
	router := chi.NewRouter()
	router.Use(tracingMiddleware(s))
	router.Use(func(next http.Handler) http.Handler { return securityMiddleware(s, next) })
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.store.SchemaReady(r.Context()); err != nil {
			s.logger().Error("schema readiness failed", "request_id", requestID(r), "error", err)
			writeError(w, http.StatusServiceUnavailable, "database_unavailable", "service is not ready", r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	return generated.HandlerWithOptions(&generatedHandler{server: s}, generated.ChiServerOptions{
		BaseRouter: router,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			writeError(w, http.StatusBadRequest, "invalid_request", "request parameters are invalid", r)
		},
	})
}
