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
	"sort"
	"strings"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/audit"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/auth"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/githubapp"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/identity"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/observability"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/parameters"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/runtime"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const maxRequestBody = 128 << 10
const maxGitHubWebhookBody = 10 << 20

type Config struct {
	Mode                            string
	PublicURL                       string
	CookieName                      string
	CookieSecure                    bool
	AllowedOrigin                   string
	AllowedHosts                    []string
	AllowedRegistries               []string
	TrustedProxyCIDRs               []string
	MaxReplicas                     int32
	MaxCPU                          int64
	MaxMemory                       int64
	SessionTTL                      time.Duration
	SessionIdleTTL                  time.Duration
	OperationLease                  time.Duration
	ParameterRetention              time.Duration
	ParameterMutationTimeout        time.Duration
	WorkspaceNamespace              string
	GitHubStateTTL                  time.Duration
	GitHubCookieName                string
	ObservabilityLogMaxWindow       time.Duration
	ObservabilityMetricMaxWindow    time.Duration
	ObservabilityEventMaxWindow     time.Duration
	ObservabilityLiveTTL            time.Duration
	ObservabilityLivePoll           time.Duration
	ObservabilityLivePerUser        int
	ObservabilityMetricsLivePoll    time.Duration
	ObservabilityMetricsLivePerUser int
	TOTPEnabled                     bool
	PublicDomain                    string
	PublicTCPEnabled                bool
	PublicTCPAddress                string
	PublicTCPMinimumPort            int32
	PublicTCPMaximumPort            int32
}

func DefaultConfig() Config {
	return Config{Mode: "development", PublicURL: "http://127.0.0.1:8080", CookieName: "fruto_session", AllowedOrigin: "http://127.0.0.1:8080", AllowedHosts: []string{"127.0.0.1:8080", "localhost:8080"}, AllowedRegistries: []string{"ghcr.io"}, MaxReplicas: 5, MaxCPU: 2000, MaxMemory: 2048, SessionTTL: 12 * time.Hour, SessionIdleTTL: 2 * time.Hour, OperationLease: 30 * time.Second, ParameterRetention: 7 * 24 * time.Hour, ParameterMutationTimeout: 5 * time.Minute, WorkspaceNamespace: "fruto-workspaces", GitHubStateTTL: 10 * time.Minute, GitHubCookieName: "molejo_github_state", ObservabilityLogMaxWindow: 24 * time.Hour, ObservabilityMetricMaxWindow: 30 * 24 * time.Hour, ObservabilityEventMaxWindow: 7 * 24 * time.Hour, ObservabilityLiveTTL: 10 * time.Minute, ObservabilityLivePoll: 2 * time.Second, ObservabilityLivePerUser: 3, ObservabilityMetricsLivePoll: 30 * time.Second, ObservabilityMetricsLivePerUser: 2, PublicDomain: "molejo.dev", PublicTCPMinimumPort: 20000, PublicTCPMaximumPort: 20015}
}

type Server struct {
	Store                 *store.Store
	Runtime               runtime.Client
	Config                Config
	Logger                *slog.Logger
	Tracer                trace.Tracer
	GitHub                githubapp.Service
	GitHubWebhookSecret   []byte
	ParameterSecrets      parameters.SecretValueStore
	SecretFingerprintKey  []byte
	PasswordResetKey      []byte
	AuthenticationSecrets parameters.SecretValueStore
	Observability         observability.Reader
	logLiveLimiter        *concurrencyLimiter
	metricsLiveLimiter    *concurrencyLimiter
	metricSnapshots       *metricSnapshotCache
	token                 func(int) (string, error)
	deploymentID          func() (string, error)
	parameterID           func() (string, error)
	dummyPasswordHash     string
}

func NewServer(s *store.Store, r runtime.Client, cfg Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	dummyHash, _ := auth.HashPassword("molejo-invalid-credential")
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	if cfg.SessionIdleTTL <= 0 || cfg.SessionIdleTTL > cfg.SessionTTL {
		cfg.SessionIdleTTL = min(2*time.Hour, cfg.SessionTTL)
	}
	if s != nil {
		publication := s.Publication
		if cfg.PublicDomain != "" {
			publication.Domain = cfg.PublicDomain
		}
		if cfg.PublicTCPMinimumPort > 0 || cfg.PublicTCPMaximumPort > 0 || cfg.PublicTCPEnabled {
			publication.TCPEnabled = cfg.PublicTCPEnabled
			publication.TCPMinimumPort = cfg.PublicTCPMinimumPort
			publication.TCPMaximumPort = cfg.PublicTCPMaximumPort
		}
		s.Publication = publication
	}
	return &Server{Store: s, Runtime: r, Config: cfg, Logger: logger, Tracer: noop.NewTracerProvider().Tracer("github.com/fruto-platform/fruto/services/control-plane-api"), ParameterSecrets: parameters.UnavailableStore{}, AuthenticationSecrets: parameters.UnavailableStore{}, Observability: observability.UnavailableReader{}, logLiveLimiter: &concurrencyLimiter{active: map[int64]int{}}, metricsLiveLimiter: &concurrencyLimiter{active: map[int64]int{}}, metricSnapshots: newMetricSnapshotCache(cfg.ObservabilityMetricsLivePoll), token: randomToken, deploymentID: func() (string, error) { return domain.NewPublicID("dpl") }, parameterID: func() (string, error) { return domain.NewPublicID("par") }, dummyPasswordHash: dummyHash}
}

func (s *Server) Handler() http.Handler {
	return requestIDMiddleware(s.generatedHandler())
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_forbidden", "request origin is not allowed", r)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	username, normalizeErr := identity.NormalizeUsername(input.Username)
	if normalizeErr != nil {
		username = strings.ToLower(strings.TrimSpace(input.Username))
	}
	ipKey := auth.HashToken("ip:" + s.remoteIP(r))
	userKey := auth.HashToken("user:" + username)
	ipAllowed, ipErr := s.Store.AuthenticationAllowed(r.Context(), ipKey)
	userAllowed, userErr := s.Store.AuthenticationAllowed(r.Context(), userKey)
	if ipErr != nil || userErr != nil {
		writeError(w, http.StatusServiceUnavailable, "authentication_unavailable", "authentication is temporarily unavailable", r)
		return
	}
	if !ipAllowed || !userAllowed {
		writeError(w, http.StatusTooManyRequests, "login_rate_limited", "too many login attempts", r)
		return
	}
	user, hash, err := s.Store.AuthenticateUser(r.Context(), username)
	if err != nil {
		hash = s.dummyPasswordHash
	}
	passwordValid := auth.VerifyPassword(input.Password, hash)
	if err != nil || !passwordValid || user.Status != identity.StatusActive || normalizeErr != nil {
		_ = s.Store.RecordAuthenticationFailure(r.Context(), ipKey)
		_ = s.Store.RecordAuthenticationFailure(r.Context(), userKey)
		_ = s.recordAudit(r, audit.Event{Action: "authentication.login", TargetType: "User", Outcome: audit.Failed, Reason: "invalid_credentials"})
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "credentials are invalid", r)
		return
	}
	_ = s.Store.ClearAuthenticationFailures(r.Context(), userKey)
	_ = s.Store.ClearAuthenticationFailures(r.Context(), ipKey)
	mfa, err := s.Store.UserMFAStatus(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "authentication_unavailable", "authentication is temporarily unavailable", r)
		return
	}
	if mfa.TOTPEnabled {
		challenge, tokenErr := s.newToken(32)
		if tokenErr != nil || s.Store.CreateAuthenticationChallenge(r.Context(), auth.HashToken(challenge), user.ID, "Login", nil, time.Now().Add(5*time.Minute)) != nil {
			writeError(w, http.StatusInternalServerError, "authentication_failed", "authentication could not continue", r)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"mfaRequired": true, "method": "TOTP", "challengeToken": challenge})
		return
	}
	s.issueSession(w, r, user, "AAL1")
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, user identity.User, assuranceLevel string) {
	installationAdmin, workspaceRoles, err := s.sessionAuthorization(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not create session", r)
		return
	}
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
	sessionPublicID, err := domain.NewPublicID("ses")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not create session", r)
		return
	}
	now := time.Now()
	event := s.auditEvent(r, "authentication.login", "User", user.PublicID, audit.Succeeded)
	event.ActorUserID = &user.ID
	if err = s.Store.CreateUserSession(r.Context(), sessionPublicID, user, auth.HashToken(token), auth.HashToken(csrf), assuranceLevel, now.Add(s.Config.SessionIdleTTL), now.Add(s.Config.SessionTTL), event); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not create session", r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.Config.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.Config.CookieSecure || s.isHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: int(s.Config.SessionTTL.Seconds())})
	s.setCSRFCookie(w, r, csrf, int(s.Config.SessionTTL.Seconds()))
	w.Header().Set("Cache-Control", "no-store")
	s.writeSession(w, user, assuranceLevel, csrf, installationAdmin, workspaceRoles)
}

func (s *Server) sessionInfo(w http.ResponseWriter, r *http.Request, userID int64, assuranceLevel string) {
	user, err := s.Store.User(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	csrfCookie, err := r.Cookie(s.Config.CookieName + "_csrf")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	installationAdmin, workspaceRoles, err := s.sessionAuthorization(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not load session", r)
		return
	}
	s.writeSession(w, user, assuranceLevel, csrfCookie.Value, installationAdmin, workspaceRoles)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(s.Config.CookieName); err == nil {
		event := s.auditEvent(r, "authentication.logout", "Session", "", audit.Succeeded)
		if principal, sessionErr := s.Store.UserSession(r.Context(), auth.HashToken(cookie.Value), s.Config.SessionIdleTTL); sessionErr == nil {
			event.ActorUserID = &principal.UserID
			event.SessionID = &principal.SessionID
		}
		if err = s.Store.RevokeSessionToken(r.Context(), auth.HashToken(cookie.Value), event); err != nil {
			writeError(w, http.StatusInternalServerError, "session_failed", "could not end session", r)
			return
		}
	}
	s.clearSessionCookies(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sessionAuthorization(ctx context.Context, userID int64) (bool, map[string]string, error) {
	installationAdmin, err := s.Store.IsInstallationAdministrator(ctx, userID)
	if err != nil {
		return false, nil, err
	}
	workspaceRoles, err := s.Store.UserWorkspaceRoles(ctx, userID)
	return installationAdmin, workspaceRoles, err
}

func (s *Server) writeSession(w http.ResponseWriter, user identity.User, assuranceLevel, csrf string, installationAdmin bool, workspaceRoles map[string]string) {
	workspaceMemberships := make([]map[string]string, 0, len(workspaceRoles))
	workspaceIDs := make([]string, 0, len(workspaceRoles))
	for workspaceID := range workspaceRoles {
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	sort.Strings(workspaceIDs)
	for _, workspaceID := range workspaceIDs {
		role := workspaceRoles[workspaceID]
		workspaceMemberships = append(workspaceMemberships, map[string]string{"workspaceId": workspaceID, "role": role})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":           user,
		"assuranceLevel": assuranceLevel,
		"csrfToken":      csrf,
		"installationCapabilities": map[string]any{
			"manageUsers":     installationAdmin,
			"createWorkspace": installationAdmin,
			"publicTCP":       map[string]any{"enabled": s.Config.PublicTCPEnabled, "address": s.Config.PublicTCPAddress, "minimumPort": s.Config.PublicTCPMinimumPort, "maximumPort": s.Config.PublicTCPMaximumPort},
		},
		"workspaceMemberships": workspaceMemberships,
	})
}

func (s *Server) setCSRFCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: s.Config.CookieName + "_csrf", Value: value, Path: "/", HttpOnly: false, Secure: s.Config.CookieSecure || s.isHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

func (s *Server) clearSessionCookies(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: s.Config.CookieName, Value: "", Path: "/", HttpOnly: true, Secure: s.Config.CookieSecure || s.isHTTPS(r), MaxAge: -1, SameSite: http.SameSiteStrictMode})
	s.setCSRFCookie(w, r, "", -1)
}

func (s *Server) logAcceptedOperation(r *http.Request, op domain.Operation) {
	s.logger().Info("operation accepted", "request_id", requestID(r), "operation_id", op.PublicID, "app_environment_id", op.AppEnvironmentPublicID, "deployment_id", op.DeploymentPublicID, "operation_kind", op.Kind)
}

func (s *Server) session(r *http.Request) (int64, []byte, bool) {
	cookie, err := r.Cookie(s.Config.CookieName)
	if err != nil || cookie.Value == "" {
		return 0, nil, false
	}
	principal, err := s.Store.UserSession(r.Context(), auth.HashToken(cookie.Value), s.Config.SessionIdleTTL)
	return principal.UserID, principal.CSRFHash, err == nil
}

func (s *Server) sessionPrincipal(r *http.Request) (store.SessionPrincipal, bool) {
	cookie, err := r.Cookie(s.Config.CookieName)
	if err != nil || cookie.Value == "" {
		return store.SessionPrincipal{}, false
	}
	principal, err := s.Store.UserSession(r.Context(), auth.HashToken(cookie.Value), s.Config.SessionIdleTTL)
	return principal, err == nil
}

func (s *Server) validCSRF(r *http.Request, hash []byte) bool {
	csrf := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
	cookie, err := r.Cookie(s.Config.CookieName + "_csrf")
	return err == nil && s.originAllowed(r) && len(hash) > 0 && csrf != "" && csrf == cookie.Value && equal(auth.HashToken(csrf), hash)
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
		requestLimit := int64(maxRequestBody)
		if r.URL.Path == "/api/v1/github/webhooks" {
			requestLimit = maxGitHubWebhookBody
		}
		if r.ContentLength > requestLimit {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large", r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, requestLimit)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/v1/session") || strings.HasPrefix(r.URL.Path, "/api/v1/users") || strings.HasPrefix(r.URL.Path, "/api/v1/admin/users") || strings.HasPrefix(r.URL.Path, "/api/v1/password-resets") {
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

func (s *Server) auditEvent(r *http.Request, action, targetType, targetPublicID, outcome string) audit.Event {
	publicID, _ := domain.NewPublicID("aud")
	span := trace.SpanContextFromContext(r.Context())
	return audit.Event{
		PublicID:       publicID,
		Action:         action,
		TargetType:     targetType,
		TargetPublicID: targetPublicID,
		Outcome:        outcome,
		RequestID:      requestID(r),
		TraceID:        span.TraceID().String(),
		SourceHash:     auth.HashToken("ip:" + s.remoteIP(r)),
		UserAgentHash:  auth.HashToken("ua:" + r.UserAgent()),
	}
}

func (s *Server) recordAudit(r *http.Request, event audit.Event) error {
	base := s.auditEvent(r, event.Action, event.TargetType, event.TargetPublicID, event.Outcome)
	base.ActorUserID = event.ActorUserID
	base.SessionID = event.SessionID
	base.WorkspaceID = event.WorkspaceID
	base.Reason = event.Reason
	base.Metadata = event.Metadata
	return s.Store.RecordAudit(r.Context(), base)
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
	if op.AppVolumeID != 0 {
		volumeRuntime, volumeErr := s.Store.VolumeRuntime(ctx, workspaceID, op.AppVolumeID)
		if volumeErr != nil {
			return true, s.failOperation(ctx, op, "volume_unavailable", "persistent storage intent is unavailable", true)
		}
		if err = s.Runtime.ApplyVolume(ctx, workspace.Namespace, volumeRuntime.Volume.PublicID, runtime.VolumeIntent{
			RuntimeBinding: volumeRuntime.RuntimeBinding, SizeGiB: volumeRuntime.Volume.SizeGiB,
			RetentionPolicy: volumeRuntime.Volume.RetentionPolicy, DesiredState: volumeRuntime.Volume.DesiredState,
		}); err != nil {
			return true, s.failOperation(ctx, op, "runtime_error", "persistent storage operation failed", true)
		}
		observation, observeErr := s.Runtime.ObserveVolume(ctx, workspace.Namespace, volumeRuntime.Volume.PublicID)
		if observeErr != nil {
			return true, s.failOperation(ctx, op, "runtime_observation_failed", "persistent storage observation failed", true)
		}
		expectedState := domain.VolumeStateReady
		if op.Kind == domain.OperationDeleteVolume {
			expectedState = domain.VolumeStateRetained
		}
		waitingForFirstConsumer := op.Kind == domain.OperationEnsureVolume && observation.Exists && observation.State == domain.VolumeStateProvisioning
		if !waitingForFirstConsumer && (!observation.Exists || observation.State != expectedState || (expectedState == domain.VolumeStateReady && observation.ObservedSizeGiB < volumeRuntime.Volume.SizeGiB)) {
			return true, s.failOperation(ctx, op, "runtime_not_ready", "persistent storage has not reached the requested state", true)
		}
		return true, s.Store.CompleteVolume(ctx, op, observation.State, observation.Message, observation.ObservedSizeGiB)
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
	intent.WorkloadKind = deployment.WorkloadKind
	if deployment.WorkloadKind == domain.WorkloadStateful {
		volume, volumeErr := s.Store.FindAppVolume(ctx, deployment.WorkspaceID, appEnvironment.PublicID)
		if volumeErr != nil || volume.PublicID != deployment.AppVolumePublicID || (volume.State != domain.VolumeStateProvisioning && volume.State != domain.VolumeStateReady) {
			return true, s.failOperation(ctx, op, "volume_unavailable", "persistent storage is unavailable", true)
		}
		intent.Volume = &volume
	}
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
	var volumeObservation runtime.VolumeObservation
	if deployment.WorkloadKind == domain.WorkloadStateful {
		volumeObservation, err = s.Runtime.ObserveVolume(ctx, workspace.Namespace, deployment.AppVolumePublicID)
		if err != nil {
			return true, s.failOperation(ctx, op, "runtime_observation_failed", "persistent storage observation failed", true)
		}
		if !volumeObservation.Exists || volumeObservation.State != domain.VolumeStateReady || volumeObservation.ObservedSizeGiB < intent.Volume.SizeGiB {
			return true, s.failOperation(ctx, op, "runtime_not_ready", "persistent storage has not reached the requested state", true)
		}
	}
	if err = s.Runtime.GarbageCollectConfiguration(ctx, workspace.Namespace, appEnvironment.RuntimeName); err != nil {
		return true, s.failOperation(ctx, op, "configuration_cleanup_failed", "runtime configuration cleanup failed", true)
	}
	if deployment.WorkloadKind == domain.WorkloadStateful {
		return true, s.Store.CompleteStatefulDeployment(ctx, op, obs.Message, obs.ObservedRelease, volumeObservation.Message, volumeObservation.ObservedSizeGiB)
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
