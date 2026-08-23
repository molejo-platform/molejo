package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

const maxUpstreamBodyBytes = 64 * 1024

var version = "devel"

type response struct {
	Status         string `json:"status"`
	Version        string `json:"version"`
	UpstreamStatus int    `json:"upstreamStatus,omitempty"`
	UpstreamBody   string `json:"upstreamBody,omitempty"`
}

func main() {
	server := &http.Server{
		Addr:              ":8080",
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	log.Printf("phase 2 HTTP fixture %s listening on %s", version, server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", exactGET("/", writeOK))
	mux.HandleFunc("/healthz", exactGET("/healthz", writeOK))
	mux.HandleFunc("/readyz", exactGET("/readyz", writeOK))
	mux.HandleFunc("/not-ready", exactGET("/not-ready", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusServiceUnavailable, response{Status: "not-ready", Version: version})
	}))
	mux.HandleFunc("/outbound", exactGET("/outbound", outbound))
	return mux
}

func exactGET(path string, handler http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != path {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(writer, request)
	}
}

func writeOK(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, response{Status: "ok", Version: version})
}

func outbound(writer http.ResponseWriter, request *http.Request) {
	upstream, err := url.ParseRequestURI(request.URL.Query().Get("url"))
	if err != nil || upstream.Host == "" || (upstream.Scheme != "http" && upstream.Scheme != "https") {
		http.Error(writer, "url must be an absolute HTTP or HTTPS URL", http.StatusBadRequest)
		return
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(redirected *http.Request, _ []*http.Request) error {
			if redirected.URL.Scheme != "http" && redirected.URL.Scheme != "https" {
				return fmt.Errorf("unsupported redirect scheme %q", redirected.URL.Scheme)
			}
			return nil
		},
	}
	upstreamRequest, err := http.NewRequestWithContext(request.Context(), http.MethodGet, upstream.String(), nil)
	if err != nil {
		http.Error(writer, "create upstream request", http.StatusBadGateway)
		return
	}
	upstreamRequest.Header.Set("User-Agent", "fruto-phase2-http-fixture/"+version)

	upstreamResponse, err := client.Do(upstreamRequest)
	if err != nil {
		http.Error(writer, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer upstreamResponse.Body.Close()
	body, err := io.ReadAll(io.LimitReader(upstreamResponse.Body, maxUpstreamBodyBytes))
	if err != nil {
		http.Error(writer, "read upstream response", http.StatusBadGateway)
		return
	}
	if upstreamResponse.StatusCode < http.StatusOK || upstreamResponse.StatusCode >= http.StatusMultipleChoices {
		http.Error(writer, "upstream returned a non-success status", http.StatusBadGateway)
		return
	}

	writeJSON(writer, http.StatusOK, response{
		Status:         "ok",
		Version:        version,
		UpstreamStatus: upstreamResponse.StatusCode,
		UpstreamBody:   string(body),
	})
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload response) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		log.Printf("encode response: %v", err)
	}
}
