package places

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/erick-ti/flightbound/backend/internal/traveladvice"
)

type summary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Country     string `json:"country"`
	CountryName string `json:"country_name"`
}

type listResponse struct {
	Places []summary `json:"places"`
}

type errorResponse struct {
	Message string `json:"message"`
}

// Handler serves the supported places as {"places": [{"id", "name",
// "country", "country_name"}, ...]} in name order. Register it on its path
// without a method pattern: it answers every method itself so that all of
// its responses, including 405, are JSON.
func Handler() http.Handler {
	list := make([]summary, len(places))
	for i, p := range places {
		// load has checked that every place's country exists.
		c, _ := traveladvice.Lookup(p.Country)
		list[i] = summary{ID: p.ID, Name: p.Name, Country: p.Country, CountryName: c.Name}
	}
	body := listResponse{Places: list}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Message: "Use GET to list the supported places."})
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
		slog.Warn("writing places response failed", "err", err)
	}
}
