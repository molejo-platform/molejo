package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", ready)
	mux.HandleFunc("/readyz", ready)
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		log.Printf("molejo-conformance request method=%s path=%s", request.Method, request.URL.Path)
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(writer, "molejo conformance")
	})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			log.Print("molejo-conformance heartbeat")
		}
	}()
	log.Print("molejo-conformance started")
	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func ready(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(writer, "ok")
}
