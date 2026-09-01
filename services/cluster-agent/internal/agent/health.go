package agent

import (
	"encoding/json"
	"net/http"
)

func HealthHandler(status *Status) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeStatusJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		code := http.StatusOK
		snapshot := Snapshot{State: StateFailed, Reason: "status is unavailable"}
		if status == nil || !status.Ready() {
			code = http.StatusServiceUnavailable
		}
		if status != nil {
			snapshot = status.Snapshot()
		}
		writeStatusJSON(w, code, snapshot)
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		if status == nil {
			writeStatusJSON(w, http.StatusServiceUnavailable, Snapshot{State: StateFailed, Reason: "status is unavailable"})
			return
		}
		writeStatusJSON(w, http.StatusOK, status.Snapshot())
	})
	return mux
}

func writeStatusJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
