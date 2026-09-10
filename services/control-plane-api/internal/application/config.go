package application

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api"
)

func loadHTTPConfig() (api.Config, error) {
	cfg := api.DefaultConfig()
	cfg.Mode = env("MOLEJO_MODE", cfg.Mode)
	cfg.PublicURL = env("MOLEJO_PUBLIC_URL", cfg.PublicURL)
	cfg.AllowedOrigin = env("MOLEJO_ALLOWED_ORIGIN", cfg.AllowedOrigin)
	cfg.AllowedHosts = csvEnv("MOLEJO_ALLOWED_HOSTS", cfg.AllowedHosts)
	cfg.AllowedRegistries = csvEnv("MOLEJO_ALLOWED_REGISTRIES", cfg.AllowedRegistries)
	cfg.TrustedProxyCIDRs = csvEnv("MOLEJO_TRUSTED_PROXY_CIDRS", nil)
	cfg.CookieSecure = os.Getenv("MOLEJO_COOKIE_SECURE") == "true"
	cfg.TOTPEnabled = os.Getenv("MOLEJO_TOTP_ENABLED") == "true"
	cfg.PublicDomain = env("MOLEJO_PUBLIC_DOMAIN", cfg.PublicDomain)
	cfg.PublicStatefulDomain = os.Getenv("MOLEJO_PUBLIC_STATEFUL_DOMAIN")
	cfg.PublicTCPEnabled = os.Getenv("MOLEJO_PUBLIC_TCP_ENABLED") == "true"
	cfg.PublicTCPAddress = os.Getenv("MOLEJO_PUBLIC_TCP_ADDRESS")
	var err error
	if cfg.PublicTCPMinimumPort, err = int32Env("MOLEJO_PUBLIC_TCP_MIN_PORT", cfg.PublicTCPMinimumPort); err != nil {
		return api.Config{}, err
	}
	if cfg.PublicTCPMaximumPort, err = int32Env("MOLEJO_PUBLIC_TCP_MAX_PORT", cfg.PublicTCPMaximumPort); err != nil {
		return api.Config{}, err
	}
	if cfg.MaxReplicas, err = int32Env("MOLEJO_MAX_REPLICAS", cfg.MaxReplicas); err != nil {
		return api.Config{}, err
	}
	if cfg.MaxCPU, err = int64Env("MOLEJO_MAX_CPU_MILLIS", cfg.MaxCPU); err != nil {
		return api.Config{}, err
	}
	if cfg.MaxMemory, err = int64Env("MOLEJO_MAX_MEMORY_MIB", cfg.MaxMemory); err != nil {
		return api.Config{}, err
	}
	if cfg.SessionTTL, err = durationEnv("MOLEJO_SESSION_TTL", cfg.SessionTTL); err != nil {
		return api.Config{}, err
	}
	if cfg.SessionIdleTTL, err = durationEnv("MOLEJO_SESSION_IDLE_TTL", cfg.SessionIdleTTL); err != nil {
		return api.Config{}, err
	}
	if cfg.OperationLease, err = durationEnv("MOLEJO_OPERATION_LEASE", cfg.OperationLease); err != nil {
		return api.Config{}, err
	}
	if cfg.ParameterRetention, err = durationEnv("MOLEJO_PARAMETER_RETENTION", cfg.ParameterRetention); err != nil {
		return api.Config{}, err
	}
	if cfg.ParameterMutationTimeout, err = durationEnv("MOLEJO_PARAMETER_MUTATION_TIMEOUT", cfg.ParameterMutationTimeout); err != nil {
		return api.Config{}, err
	}
	if cfg.ObservabilityLogMaxWindow, err = durationEnv("MOLEJO_OBSERVABILITY_LOG_MAX_WINDOW", cfg.ObservabilityLogMaxWindow); err != nil {
		return api.Config{}, err
	}
	if cfg.ObservabilityMetricMaxWindow, err = durationEnv("MOLEJO_OBSERVABILITY_METRIC_MAX_WINDOW", cfg.ObservabilityMetricMaxWindow); err != nil {
		return api.Config{}, err
	}
	if cfg.ObservabilityEventMaxWindow, err = durationEnv("MOLEJO_OBSERVABILITY_EVENT_MAX_WINDOW", cfg.ObservabilityEventMaxWindow); err != nil {
		return api.Config{}, err
	}
	if cfg.ObservabilityLiveTTL, err = durationEnv("MOLEJO_OBSERVABILITY_LIVE_TTL", cfg.ObservabilityLiveTTL); err != nil {
		return api.Config{}, err
	}
	if cfg.ObservabilityLivePoll, err = durationEnv("MOLEJO_OBSERVABILITY_LIVE_POLL", cfg.ObservabilityLivePoll); err != nil {
		return api.Config{}, err
	}
	livePerUser, err := int64Env("MOLEJO_OBSERVABILITY_LIVE_PER_USER", int64(cfg.ObservabilityLivePerUser))
	if err != nil {
		return api.Config{}, err
	}
	cfg.ObservabilityLivePerUser = int(livePerUser)
	if cfg.ObservabilityMetricsLivePoll, err = durationEnv("MOLEJO_OBSERVABILITY_METRICS_LIVE_POLL", cfg.ObservabilityMetricsLivePoll); err != nil {
		return api.Config{}, err
	}
	metricsLivePerUser, err := int64Env("MOLEJO_OBSERVABILITY_METRICS_LIVE_PER_USER", int64(cfg.ObservabilityMetricsLivePerUser))
	if err != nil {
		return api.Config{}, err
	}
	cfg.ObservabilityMetricsLivePerUser = int(metricsLivePerUser)
	if err = cfg.Validate(); err != nil {
		return api.Config{}, fmt.Errorf("invalid HTTP configuration: %w", err)
	}
	return cfg, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return duration, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func csvEnv(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	items := strings.Split(value, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func int64Env(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return parsed, nil
}

func int32Env(key string, fallback int32) (int32, error) {
	parsed, err := int64Env(key, int64(fallback))
	if err != nil || parsed > int64(^uint32(0)>>1) {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return int32(parsed), nil
}
