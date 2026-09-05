package main

import (
	"crypto/tls"
	"crypto/x509"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

//go:embed static
var content embed.FS

func main() {
	handler, err := newHandlerFromEnvironment()
	if err != nil {
		slog.Error("console failed to initialize", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              ":8080",
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	slog.Info("console started", "address", server.Addr)
	if err = server.ListenAndServe(); err != nil {
		slog.Error("console stopped", "error", err)
		os.Exit(1)
	}
}

func newHandlerFromEnvironment() (http.Handler, error) {
	apiURL := strings.TrimSpace(os.Getenv("MOLEJO_API_URL"))
	if apiURL == "" {
		return nil, errors.New("MOLEJO_API_URL is required")
	}
	caPath := strings.TrimSpace(os.Getenv("MOLEJO_API_CA_FILE"))
	if caPath == "" {
		return nil, errors.New("MOLEJO_API_CA_FILE is required")
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("control plane API CA is invalid")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
		ServerName: strings.TrimSpace(os.Getenv("MOLEJO_API_SERVER_NAME")),
	}
	return newHandler(apiURL, transport)
}

func newHandler(apiAddress string, transport http.RoundTripper) (http.Handler, error) {
	directory, err := fs.Sub(content, "static")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
	})
	if apiAddress != "" {
		target, parseErr := url.Parse(apiAddress)
		if parseErr != nil || target.Scheme == "" || target.Host == "" {
			return nil, errors.New("MOLEJO_API_URL is invalid")
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		if transport != nil {
			proxy.Transport = transport
		}
		originalDirector := proxy.Director
		proxy.Director = func(request *http.Request) {
			originalDirector(request)
			request.Host = target.Host
		}
		proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, proxyErr error) {
			slog.Error("control plane API proxy failed", "error", proxyErr)
			http.Error(response, "control plane API is unavailable", http.StatusBadGateway)
		}
		mux.Handle("/api/", proxy)
	}
	mux.Handle("/", http.FileServerFS(directory))
	return mux, nil
}
