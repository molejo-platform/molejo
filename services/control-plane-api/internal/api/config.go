package api

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"
)

func (c Config) Validate() error {
	if c.ObservabilityMaxWindow <= 0 || c.ObservabilityMaxWindow > 7*24*time.Hour {
		return fmt.Errorf("observability max window must be positive and at most 7 days")
	}
	if c.ObservabilityLiveTTL <= 0 || c.ObservabilityLiveTTL > 30*time.Minute || c.ObservabilityLivePoll < time.Second || c.ObservabilityLivePerUser < 1 || c.ObservabilityLivePerUser > 10 {
		return fmt.Errorf("observability live limits are invalid")
	}
	if c.ObservabilityMetricsLivePoll < 15*time.Second || c.ObservabilityMetricsLivePoll > 5*time.Minute || c.ObservabilityMetricsLivePerUser < 1 || c.ObservabilityMetricsLivePerUser > 10 {
		return fmt.Errorf("observability metrics live limits are invalid")
	}
	if c.GitHubStateTTL <= 0 || c.GitHubStateTTL > 30*time.Minute {
		return fmt.Errorf("GitHub state TTL must be positive and at most 30 minutes")
	}
	if strings.TrimSpace(c.GitHubCookieName) == "" {
		return fmt.Errorf("GitHub state cookie name is required")
	}
	if c.Mode != "development" && c.Mode != "production" {
		return fmt.Errorf("mode must be development or production")
	}
	publicURL, err := url.Parse(c.PublicURL)
	if err != nil || publicURL.Scheme == "" || publicURL.Host == "" || publicURL.Path != "" {
		return fmt.Errorf("public URL must be an origin without a path")
	}
	if c.AllowedOrigin != publicURL.Scheme+"://"+publicURL.Host {
		return fmt.Errorf("allowed origin must match the public URL origin")
	}
	if !slices.Contains(c.AllowedHosts, publicURL.Host) {
		return fmt.Errorf("allowed hosts must contain the public URL host")
	}
	if len(c.AllowedRegistries) == 0 {
		return fmt.Errorf("at least one registry must be allowed")
	}
	for _, cidr := range c.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q", cidr)
		}
	}
	if c.Mode == "development" && !c.CookieSecure {
		hostname := publicURL.Hostname()
		if publicURL.Scheme != "http" || !isLocalHostname(hostname) {
			return fmt.Errorf("insecure cookies require an explicit local development URL")
		}
		return nil
	}
	if publicURL.Scheme != "https" || !c.CookieSecure {
		return fmt.Errorf("non-local or production configuration requires HTTPS and Secure cookies")
	}
	if c.Mode == "production" && len(c.TrustedProxyCIDRs) == 0 {
		return fmt.Errorf("production requires an explicit trusted proxy CIDR")
	}
	return nil
}

func (c Config) RegistryAllowed(image string) bool {
	repository, _, ok := strings.Cut(image, "@")
	if !ok {
		return false
	}
	first, _, hasSlash := strings.Cut(repository, "/")
	registry := "docker.io"
	if hasSlash && (strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost") {
		registry = first
	}
	return slices.Contains(c.AllowedRegistries, registry)
}

func isLocalHostname(hostname string) bool {
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}
