package httpserver

import (
	"encoding/json"
	"net/http"
)

// Dependencies contains the health/readiness hooks and application routes.
type Dependencies struct {
	Ready    func() error
	Register func(*http.ServeMux)
}

// New builds the public HTTP handler for the Connect Portal.
func New(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if deps.Ready != nil {
			if err := deps.Ready(); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	if deps.Register != nil {
		deps.Register(mux)
	}
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
