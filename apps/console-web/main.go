package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"
)

//go:embed static
var content embed.FS

func main() {
	handler, err := newHandler()
	if err != nil {
		slog.Error("console mock failed to initialize", "error", err)
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
	slog.Info("console mock started", "address", server.Addr)
	if err = server.ListenAndServe(); err != nil {
		slog.Error("console mock stopped", "error", err)
		os.Exit(1)
	}
}

func newHandler() (http.Handler, error) {
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
	mux.Handle("GET /", http.FileServerFS(directory))
	return mux, nil
}
