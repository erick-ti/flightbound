package traveladvice

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type countriesResponse struct {
	Countries []Country `json:"countries"`
}

type errorResponse struct {
	Message string `json:"message"`
}

// CountriesHandler serves the countries with travel advice as
// {"countries": [{"slug", "name"}, ...]} in slug order. Register it on its
// path without a method pattern: it answers every method itself so that all
// of its responses, including 405, are JSON.
func CountriesHandler() http.Handler {
	body := countriesResponse{Countries: Countries()}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Message: "Use GET to list the countries with travel advice."})
			return
		}
		writeJSON(w, http.StatusOK, body)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("writing countries response failed", "err", err)
	}
}
