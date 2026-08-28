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
	"strings"
	"sync"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/auth"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/githubapp"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/parameters"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/runtime"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const maxRequestBody = 128 << 10

type Config struct {
	Mode               string
	PublicURL          string
	CookieName         string
	CookieSecure       bool
	AllowedOrigin      string
	AllowedHosts       []string
	AllowedRegistries  []string
	TrustedProxyCIDRs  []string
	MaxReplicas        int32
	MaxCPU             int64
	MaxMemory          int64
	SessionTTL         time.Duration
	OperationLease     time.Duration
	WorkspaceNamespace string
	GitHubStateTTL     time.Duration
	GitHubCookieName   string
}

func DefaultConfig() Config {
	return Config{Mode: "development", PublicURL: "http://127.0.0.1:8080", CookieName: "fruto_session", AllowedOrigin: "http://127.0.0.1:8080", AllowedHosts: []string{"127.0.0.1:8080", "localhost:8080"}, AllowedRegistries: []string{"ghcr.io"}, MaxReplicas: 5, MaxCPU: 2000, MaxMemory: 2048, SessionTTL: 12 * time.Hour, OperationLease: 30 * time.Second, WorkspaceNamespace: "fruto-workspaces", GitHubStateTTL: 10 * time.Minute, GitHubCookieName: "molejo_github_state"}
}

type Server struct {
	Store                *store.Store
	Runtime              runtime.Client
	Config               Config
	Logger               *slog.Logger
	Tracer               trace.Tracer
	GitHub               githubapp.Service
	ParameterSecrets     parameters.SecretValueStore
	SecretFingerprintKey []byte
	limiter              *loginLimiter
	token                func(int) (string, error)
	deploymentID         func() (string, error)
	parameterID          func() (string, error)
}

func NewServer(s *store.Store, r runtime.Client, cfg Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{Store: s, Runtime: r, Config: cfg, Logger: logger, Tracer: noop.NewTracerProvider().Tracer("github.com/fruto-platform/fruto/services/control-plane-api"), ParameterSecrets: parameters.UnavailableStore{}, limiter: &loginLimiter{entries: map[string]loginAttempt{}}, token: randomToken, deploymentID: func() (string, error) { return domain.NewPublicID("dpl") }, parameterID: func() (string, error) { return domain.NewPublicID("par") }}
}

func (s *Server) Handler() http.Handler {
	return requestIDMiddleware(tracingMiddleware(s, securityMiddleware(s, s.generatedHandler())))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_forbidden", "request origin is not allowed", r)
		return
	}
	ip := s.remoteIP(r)
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
	token, err := s.newToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not create session", r)
		return
	}
	csrf, err := s.newToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not create session", r)
		return
	}
	if err = s.Store.CreateSession(r.Context(), actor.ID, auth.HashToken(token), auth.HashToken(csrf), time.Now().Add(s.Config.SessionTTL)); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not create session", r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.Config.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.Config.CookieSecure || s.isHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: int(s.Config.SessionTTL.Seconds())})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"actor": map[string]string{"id": actor.Key, "role": actor.Role}, "csrfToken": csrf})
}

func (s *Server) sessionInfo(w http.ResponseWriter, r *http.Request, actorID int64) {
	actor, err := s.Store.Actor(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	token, err := s.newToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not refresh session", r)
		return
	}
	newCSRF, err := s.newToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not refresh session", r)
		return
	}
	cookie, err := r.Cookie(s.Config.CookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	if err = s.Store.RotateSession(r.Context(), actor.ID, auth.HashToken(cookie.Value), auth.HashToken(token), auth.HashToken(newCSRF), time.Now().Add(s.Config.SessionTTL)); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not refresh session", r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.Config.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.Config.CookieSecure || s.isHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: int(s.Config.SessionTTL.Seconds())})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"actor": map[string]string{"id": actor.Key, "role": actor.Role}, "csrfToken": newCSRF})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(s.Config.CookieName); err == nil {
		if err = s.Store.RevokeSession(r.Context(), auth.HashToken(cookie.Value)); err != nil {
			writeError(w, http.StatusInternalServerError, "session_failed", "could not end session", r)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: s.Config.CookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logAcceptedOperation(r *http.Request, op domain.Operation) {
	s.logger().Info("operation accepted", "request_id", requestID(r), "operation_id", op.PublicID, "app_environment_id", op.AppEnvironmentPublicID, "deployment_id", op.DeploymentPublicID, "operation_kind", op.Kind)
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
	if s.Config.AllowedOrigin != "" {
		return origin != "" && origin == s.Config.AllowedOrigin
	}
	return false
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
		if !s.hostAllowed(r.Host) {
			writeError(w, http.StatusMisdirectedRequest, "host_not_allowed", "request host is not allowed", r)
			return
		}
		if r.ContentLength > maxRequestBody {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large", r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/v1/session") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if strings.HasPrefix(s.Config.PublicURL, "https://") && s.isHTTPS(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger().Error("request panic recovered", "request_id", requestID(r))
				writeError(w, http.StatusInternalServerError, "internal_error", "request could not be completed", r)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) isHTTPS(r *http.Request) bool {
	return r.TLS != nil || s.proxyTrusted(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
func (s *Server) remoteIP(r *http.Request) string {
	if s.proxyTrusted(r) {
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
			return forwarded
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) proxyTrusted(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, value := range s.Config.TrustedProxyCIDRs {
		_, cidr, err := net.ParseCIDR(value)
		if err == nil && cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) hostAllowed(host string) bool {
	for _, allowed := range s.Config.AllowedHosts {
		if strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(allowed, ".")) {
			return true
		}
	}
	return false
}
func decodeJSON(r *http.Request, target any) error {
	r.Body = io.NopCloser(io.LimitReader(r.Body, maxRequestBody))
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
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
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

func (s *Server) newToken(size int) (string, error) {
	if s.token != nil {
		return s.token(size)
	}
	return randomToken(size)
}

type requestIDContextKey struct{}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if id == "" || len(id) > 64 || strings.IndexFunc(id, func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.')
		}) >= 0 {
			id, _ = randomToken(8)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id)))
	})
}

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDContextKey{}).(string)
	return id
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string, r *http.Request) {
	id := requestID(r)
	if id == "" {
		id, _ = randomToken(8)
	}
	writeJSON(w, status, map[string]string{"code": code, "message": message, "requestId": id})
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
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
	op, appEnvironment, deployment, ok, err := s.Store.ClaimNext(ctx, workerID, s.Config.OperationLease)
	if err != nil || !ok {
		return ok, err
	}
	s.logger().Info("operation claimed", "operation_id", op.PublicID, "app_environment_id", op.AppEnvironmentPublicID, "deployment_id", op.DeploymentPublicID, "operation_kind", op.Kind, "worker_id", workerID, "attempt", op.Attempts)
	if s.Runtime == nil {
		return true, s.failOperation(ctx, op, "runtime_unconfigured", "runtime is not configured", false)
	}
	workspaceID := appEnvironment.WorkspaceID
	if op.Kind == domain.OperationEnsureWorkspace {
		workspaceID = op.WorkspaceID
	}
	workspace, err := s.Store.Workspace(ctx, workspaceID)
	if err != nil {
		return true, s.failOperation(ctx, op, "workspace_unavailable", "workspace is not available", true)
	}
	if err = s.Runtime.EnsureWorkspace(ctx, workspace.Namespace); err != nil {
		return true, s.failOperation(ctx, op, "workspace_unavailable", "workspace is not available", true)
	}
	if op.Kind == domain.OperationEnsureWorkspace {
		return true, s.Store.CompleteWorkspace(ctx, op)
	}
	if op.Kind == domain.OperationDeleteAppEnv {
		if err = s.Runtime.DeleteDeployment(ctx, workspace.Namespace, appEnvironment.RuntimeName); err != nil {
			return true, s.failOperation(ctx, op, "runtime_error", "runtime operation failed", true)
		}
		obs, observeErr := s.Runtime.ObserveDeployment(ctx, workspace.Namespace, appEnvironment.RuntimeName)
		if observeErr != nil {
			return true, s.failOperation(ctx, op, "runtime_observation_failed", "runtime observation failed", true)
		}
		if obs.Exists {
			return true, s.failOperation(ctx, op, "runtime_deletion_pending", "runtime removal is not yet observed", true)
		}
		if err = s.Runtime.GarbageCollectConfiguration(ctx, workspace.Namespace, appEnvironment.RuntimeName); err != nil {
			return true, s.failOperation(ctx, op, "configuration_cleanup_failed", "runtime configuration cleanup failed", true)
		}
		return true, s.Store.CompleteAppEnvironmentDeletion(ctx, op, obs.Message)
	}
	intent := domain.IntentFromConfiguration(deployment.Image, deployment.Configuration)
	intent.ConfigurationVersion = deployment.ConfigurationVersion
	resolved, err := s.Store.ResolveParameterBindings(ctx, deployment.WorkspaceID, deployment.Configuration.Parameters)
	if err != nil {
		return true, s.failOperation(ctx, op, "configuration_unavailable", "configuration references are unavailable", false)
	}
	for _, parameter := range resolved {
		switch parameter.Kind {
		case domain.ParameterPlainText:
			intent.Variables = append(intent.Variables, domain.Variable{Name: parameter.Binding.Name, Value: parameter.PlainTextValue})
		case domain.ParameterSecret:
			value, secretErr := s.ParameterSecrets.Get(ctx, parameter.SecretReference, parameter.SecretBackendVersion)
			if secretErr != nil {
				return true, s.failOperation(ctx, op, "secret_unavailable", "secret configuration is unavailable", true)
			}
			intent.SecretVariables = append(intent.SecretVariables, domain.Variable{Name: parameter.Binding.Name, Value: value})
		default:
			return true, s.failOperation(ctx, op, "configuration_invalid", "configuration reference type is invalid", false)
		}
	}
	if err = s.Runtime.ApplyDeployment(ctx, workspace.Namespace, appEnvironment.RuntimeName, intent); err != nil {
		return true, s.failOperation(ctx, op, "runtime_error", "runtime operation failed", true)
	}
	obs, err := s.Runtime.ObserveDeployment(ctx, workspace.Namespace, appEnvironment.RuntimeName)
	if err != nil {
		return true, s.failOperation(ctx, op, "runtime_observation_failed", "runtime observation failed", true)
	}
	if obs.State != domain.Ready || !obs.Exists || obs.ObservedRelease != deployment.Image {
		return true, s.failOperation(ctx, op, "runtime_not_ready", "runtime has not observed the requested release", true)
	}
	if err = s.Runtime.GarbageCollectConfiguration(ctx, workspace.Namespace, appEnvironment.RuntimeName); err != nil {
		return true, s.failOperation(ctx, op, "configuration_cleanup_failed", "runtime configuration cleanup failed", true)
	}
	return true, s.Store.CompleteDeployment(ctx, op, obs.Message, obs.ObservedRelease)
}

func (s *Server) failOperation(ctx context.Context, op domain.Operation, code, message string, retryable bool) error {
	s.logger().Warn("operation failed", "operation_id", op.PublicID, "app_environment_id", op.AppEnvironmentPublicID, "deployment_id", op.DeploymentPublicID, "operation_kind", op.Kind, "worker_id", op.WorkerID, "error_code", code, "retryable", retryable)
	return s.Store.Fail(ctx, op, code, message, retryable)
}

func (s *Server) RunWorker(ctx context.Context, workerID string) {
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Store.ReleaseClaims(releaseCtx, workerID); err != nil {
			s.logger().Error("release worker claims", "worker_id", workerID, "error", err)
		}
	}()
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
