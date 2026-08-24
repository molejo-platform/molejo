package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/auth"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/runtime"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

type Config struct {
	CookieName         string
	CookieSecure       bool
	AllowedOrigin      string
	MaxReplicas        int32
	MaxCPU             int64
	MaxMemory          int64
	SessionTTL         time.Duration
	OperationLease     time.Duration
	WorkspaceNamespace string
}

func DefaultConfig() Config {
	return Config{CookieName: "fruto_session", MaxReplicas: 5, MaxCPU: 2000, MaxMemory: 2048, SessionTTL: 12 * time.Hour, OperationLease: 30 * time.Second, WorkspaceNamespace: "fruto-workspaces"}
}

type Server struct {
	Store   *store.Store
	Runtime runtime.Client
	Config  Config
	Logger  *slog.Logger
	limiter *loginLimiter
}

func NewServer(s *store.Store, r runtime.Client, cfg Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{Store: s, Runtime: r, Config: cfg, Logger: logger, limiter: &loginLimiter{entries: map[string]loginAttempt{}}}
}

func (s *Server) Handler() http.Handler { return securityMiddleware(s, http.HandlerFunc(s.route)) }

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/healthz") {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/readyz") {
		if err := s.Store.SchemaReady(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "database_unavailable", "service is not ready", r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/v1/session" {
		s.login(w, r)
		return
	}
	actorID, csrf, ok := s.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	if r.URL.Path == "/api/v1/session" && r.Method == http.MethodGet {
		s.sessionInfo(w, r, actorID, csrf)
		return
	}
	if r.URL.Path == "/api/v1/session" && r.Method == http.MethodDelete {
		if !s.validCSRF(r, csrf) {
			writeError(w, http.StatusForbidden, "csrf_failed", "request could not be verified", r)
			return
		}
		s.logout(w, r)
		return
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
		if !s.validCSRF(r, csrf) {
			writeError(w, http.StatusForbidden, "csrf_failed", "request could not be verified", r)
			return
		}
	}
	workspace, err := s.Store.WorkspaceForActor(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusForbidden, "workspace_forbidden", "workspace access is not configured", r)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/workspaces/current":
		writeJSON(w, http.StatusOK, workspace)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deployments":
		s.listDeployments(w, r, workspace)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/deployments":
		s.createDeployment(w, r, workspace, actorID)
	case strings.HasPrefix(r.URL.Path, "/api/v1/deployments/"):
		s.deploymentRoute(w, r, workspace, actorID)
	case strings.HasPrefix(r.URL.Path, "/api/v1/operations/"):
		s.operation(w, r, workspace)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_forbidden", "request origin is not allowed", r)
		return
	}
	ip := remoteIP(r)
	if !s.limiter.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "login_rate_limited", "too many login attempts", r)
		return
	}
	var input struct {
		Actor    string `json:"actor"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	actor, hash, err := s.Store.Authenticate(r.Context(), input.Actor)
	if err != nil || !auth.VerifyPassword(input.Password, hash) {
		s.limiter.fail(ip)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "credentials are invalid", r)
		return
	}
	s.limiter.success(ip)
	token, err := randomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not create session", r)
		return
	}
	csrf, err := randomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not create session", r)
		return
	}
	if err = s.Store.CreateSession(r.Context(), actor.ID, auth.HashToken(token), auth.HashToken(csrf), time.Now().Add(s.Config.SessionTTL)); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not create session", r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.Config.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.Config.CookieSecure || isHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: int(s.Config.SessionTTL.Seconds())})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"actor": map[string]string{"id": actor.Key, "role": actor.Role}, "csrfToken": csrf})
}

func (s *Server) sessionInfo(w http.ResponseWriter, r *http.Request, actorID int64, csrf []byte) {
	var actor domain.Actor
	err := s.Store.Pool.QueryRow(r.Context(), `SELECT id,actor_key,role FROM actors WHERE id=$1`, actorID).Scan(&actor.ID, &actor.Key, &actor.Role)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	token, _ := randomToken(32)
	newCSRF, _ := randomToken(32)
	cookie, _ := r.Cookie(s.Config.CookieName)
	_ = s.Store.RevokeSession(r.Context(), auth.HashToken(cookie.Value))
	_ = s.Store.CreateSession(r.Context(), actor.ID, auth.HashToken(token), auth.HashToken(newCSRF), time.Now().Add(s.Config.SessionTTL))
	http.SetCookie(w, &http.Cookie{Name: s.Config.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.Config.CookieSecure || isHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: int(s.Config.SessionTTL.Seconds())})
	_ = csrf
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"actor": map[string]string{"id": actor.Key, "role": actor.Role}, "csrfToken": newCSRF})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(s.Config.CookieName); err == nil {
		_ = s.Store.RevokeSession(r.Context(), auth.HashToken(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: s.Config.CookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listDeployments(w http.ResponseWriter, r *http.Request, workspace domain.Workspace) {
	items, err := s.Store.ListDeployments(r.Context(), workspace.ID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list deployments", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func (s *Server) createDeployment(w http.ResponseWriter, r *http.Request, workspace domain.Workspace, actorID int64) {
	idem, payload, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	var intent domain.Intent
	if err := decodeJSON(r, &intent); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	intent = domain.NormalizeIntent(intent)
	if err := domain.ValidateIntent(intent, s.Config.MaxReplicas, s.Config.MaxCPU, s.Config.MaxMemory); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_intent", err.Error(), r)
		return
	}
	depID, _ := domain.NewPublicID("dep")
	dep, op, _, err := s.Store.CreateDeployment(r.Context(), workspace.ID, actorID, depID, intent, auth.HashToken(idem), payload)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "idempotency_conflict", "request conflicts with an existing deployment", r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not create deployment", r)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"deployment": dep, "operation": op})
}

func (s *Server) deploymentRoute(w http.ResponseWriter, r *http.Request, workspace domain.Workspace, actorID int64) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/deployments/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	dep, err := s.Store.FindDeployment(r.Context(), workspace.ID, parts[0])
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNoRows) {
			writeError(w, http.StatusNotFound, "deployment_not_found", "deployment was not found", r)
		} else {
			writeError(w, http.StatusInternalServerError, "storage_failed", "could not read deployment", r)
		}
		return
	}
	if len(parts) == 2 && parts[1] == "operations" && r.Method == http.MethodGet {
		ops, err := s.Store.ListOperations(r.Context(), workspace.ID, dep.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "storage_failed", "could not read operations", r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": ops})
		return
	}
	if r.Method == http.MethodGet && len(parts) == 1 {
		s.detailDeployment(w, r, workspace, dep)
		return
	}
	if r.Method == http.MethodPut && len(parts) == 1 {
		s.updateDeployment(w, r, workspace, actorID, dep)
		return
	}
	if r.Method == http.MethodDelete && len(parts) == 1 {
		s.deleteDeployment(w, r, workspace, actorID, dep)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) detailDeployment(w http.ResponseWriter, r *http.Request, workspace domain.Workspace, dep domain.Deployment) {
	if s.Runtime != nil {
		obs, err := s.Runtime.ObserveDeployment(r.Context(), workspace.Namespace, dep.RuntimeName)
		if err == nil {
			dep.State = obs.State
			dep.Message = obs.Message
			if dep.State == domain.Ready && dep.ObservedVersion != dep.DesiredVersion {
				dep.State = domain.Progressing
				dep.Message = "runtime is ready; operation finalization is pending"
			}
		} else {
			dep.State = domain.Unknown
			dep.Message = "runtime observation unavailable"
		}
	}
	writeJSON(w, http.StatusOK, dep)
}

func (s *Server) updateDeployment(w http.ResponseWriter, r *http.Request, workspace domain.Workspace, actorID int64, dep domain.Deployment) {
	idem, payload, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	version, err := strconv.ParseInt(r.Header.Get("If-Match"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "if_match_required", "If-Match must contain the current version", r)
		return
	}
	var intent domain.Intent
	if err = decodeJSON(r, &intent); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	intent = domain.NormalizeIntent(intent)
	if err = domain.ValidateIntent(intent, s.Config.MaxReplicas, s.Config.MaxCPU, s.Config.MaxMemory); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_intent", err.Error(), r)
		return
	}
	updated, op, err := s.Store.UpdateDeployment(r.Context(), workspace.ID, actorID, dep.ID, intent, version, auth.HashToken(idem), payload)
	if errors.Is(err, store.ErrImmutableName) {
		writeError(w, http.StatusConflict, "deployment_name_immutable", "deployment name cannot be changed", r)
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "version_conflict", "deployment changed since it was read", r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not update deployment", r)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"deployment": updated, "operation": op})
}

func (s *Server) deleteDeployment(w http.ResponseWriter, r *http.Request, workspace domain.Workspace, actorID int64, dep domain.Deployment) {
	idem, payload, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	version, err := strconv.ParseInt(r.Header.Get("If-Match"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "if_match_required", "If-Match must contain the current version", r)
		return
	}
	op, err := s.Store.DeleteDeployment(r.Context(), workspace.ID, actorID, dep.ID, version, auth.HashToken(idem), payload)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "version_conflict", "deployment changed since it was read", r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not delete deployment", r)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"operation": op})
}

func (s *Server) operation(w http.ResponseWriter, r *http.Request, workspace domain.Workspace) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/operations/")
	op, err := s.Store.GetOperation(r.Context(), workspace.ID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "operation_not_found", "operation was not found", r)
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (s *Server) session(r *http.Request) (int64, []byte, bool) {
	cookie, err := r.Cookie(s.Config.CookieName)
	if err != nil || cookie.Value == "" {
		return 0, nil, false
	}
	actor, csrf, err := s.Store.Session(r.Context(), auth.HashToken(cookie.Value))
	return actor, csrf, err == nil
}
func (s *Server) validCSRF(r *http.Request, hash []byte) bool {
	return s.originAllowed(r) && len(hash) > 0 && strings.TrimSpace(r.Header.Get("X-CSRF-Token")) != "" && equal(auth.HashToken(r.Header.Get("X-CSRF-Token")), hash)
}
func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if s.Config.AllowedOrigin != "" {
		return origin == s.Config.AllowedOrigin
	}
	return origin == "http://127.0.0.1:8080" || origin == "http://localhost:8080" || origin == "http://127.0.0.1:5173" || origin == "http://localhost:5173"
}
func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

func securityMiddleware(s *Server, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if isHTTPS(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func decodeJSON(r *http.Request, target any) error {
	r.Body = io.NopCloser(io.LimitReader(r.Body, 64<<10))
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}
func idempotency(r *http.Request) (string, []byte, bool) {
	value := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if value == "" || len(value) > 128 {
		return "", nil, false
	}
	var body map[string]any
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return "", nil, false
	}
	r.Body = io.NopCloser(strings.NewReader(string(raw)))
	_ = json.Unmarshal(raw, &body)
	canonical, _ := json.Marshal(body)
	return value, domain.SHA256(canonical), true
}
func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string, r *http.Request) {
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID, _ = randomToken(8)
	}
	writeJSON(w, status, map[string]string{"code": code, "message": message, "requestId": requestID})
}

type loginAttempt struct {
	Count        int
	BlockedUntil time.Time
}
type loginLimiter struct {
	sync.Mutex
	entries map[string]loginAttempt
}

func (l *loginLimiter) allow(ip string) bool {
	l.Lock()
	defer l.Unlock()
	e := l.entries[ip]
	return time.Now().After(e.BlockedUntil) && e.Count < 8
}
func (l *loginLimiter) fail(ip string) {
	l.Lock()
	defer l.Unlock()
	e := l.entries[ip]
	e.Count++
	if e.Count >= 8 {
		e.BlockedUntil = time.Now().Add(time.Duration(e.Count) * time.Second)
		e.Count = 0
	}
	l.entries[ip] = e
}
func (l *loginLimiter) success(ip string) { l.Lock(); defer l.Unlock(); delete(l.entries, ip) }

func (s *Server) RunOnce(ctx context.Context, workerID string) (bool, error) {
	op, dep, ok, err := s.Store.ClaimNext(ctx, workerID, s.Config.OperationLease)
	if err != nil || !ok {
		return ok, err
	}
	if s.Runtime == nil {
		return true, s.Store.Fail(ctx, op, "runtime_unconfigured", "runtime is not configured", false)
	}
	workspace, err := s.Store.Workspace(ctx, dep.WorkspaceID)
	if err != nil {
		return true, s.Store.Fail(ctx, op, "workspace_unavailable", "workspace is not available", true)
	}
	if err = s.Runtime.EnsureWorkspace(ctx, workspace.Namespace); err != nil {
		return true, s.Store.Fail(ctx, op, "workspace_unavailable", "workspace is not available", true)
	}
	if op.Kind == "DeleteDeployment" {
		if err = s.Runtime.DeleteDeployment(ctx, workspace.Namespace, dep.RuntimeName); err != nil {
			return true, s.Store.Fail(ctx, op, "runtime_error", "runtime operation failed", true)
		}
		obs, observeErr := s.Runtime.ObserveDeployment(ctx, workspace.Namespace, dep.RuntimeName)
		if observeErr != nil {
			return true, s.Store.Fail(ctx, op, "runtime_observation_failed", "runtime observation failed", true)
		}
		if obs.Exists {
			return true, s.Store.Fail(ctx, op, "runtime_deletion_pending", "runtime removal is not yet observed", true)
		}
		return true, s.Store.Complete(ctx, op, domain.Ready, obs.Message, 0, obs.ObservedRelease, true)
	}
	if err = s.Runtime.ApplyDeployment(ctx, workspace.Namespace, dep.RuntimeName, op.Intent); err != nil {
		return true, s.Store.Fail(ctx, op, "runtime_error", "runtime operation failed", true)
	}
	obs, err := s.Runtime.ObserveDeployment(ctx, workspace.Namespace, dep.RuntimeName)
	if err != nil {
		return true, s.Store.Fail(ctx, op, "runtime_observation_failed", "runtime observation failed", true)
	}
	if obs.State != domain.Ready || !obs.Exists || obs.ObservedRelease != op.Intent.Image {
		return true, s.Store.Fail(ctx, op, "runtime_not_ready", "runtime has not observed the requested release", true)
	}
	return true, s.Store.Complete(ctx, op, domain.Ready, obs.Message, op.DesiredVersion, obs.ObservedRelease, false)
}

func (s *Server) RunWorker(ctx context.Context, workerID string) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := s.RunOnce(ctx, workerID)
			if err != nil {
				s.Logger.Error("run operation", "error", err)
			}
		}
	}
}
