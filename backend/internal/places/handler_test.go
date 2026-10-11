package places

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerListsPlacesWithTheirCountries(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/places", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var body struct {
		Places []map[string]any `json:"places"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	all := All()
	if len(body.Places) != len(all) {
		t.Fatalf("%d places, want %d", len(body.Places), len(all))
	}
	for i, got := range body.Places {
		if len(got) != 4 {
			t.Errorf("place %d has fields %v, want id, name, country, country_name", i, got)
		}
		if got["id"] != all[i].ID || got["name"] != all[i].Name || got["country"] != all[i].Country {
			t.Errorf("place %d = %v, want %+v in list order", i, got, all[i])
		}
	}
	for _, got := range body.Places {
		if got["id"] == "prague" && got["country_name"] != "Czechia" {
			t.Errorf("Prague's country_name = %v, want Czechia", got["country_name"])
		}
	}
}
