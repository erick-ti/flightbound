package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	mux := newMux()

	t.Run("POST reaches the comparison handler", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trip-comparisons", strings.NewReader("{}")))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
	})

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		t.Run(method+" gets a JSON 405", func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(method, "/api/trip-comparisons", nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d", rec.Code)
			}
			if got := rec.Header().Get("Allow"); got != http.MethodPost {
				t.Errorf("Allow = %q, want POST", got)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q", got)
			}
			if method == http.MethodHead {
				return
			}
			var body struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Message == "" {
				t.Errorf("body = %q, want a JSON message", rec.Body)
			}
		})
	}

	t.Run("unknown path is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trip-comparisons/extra", strings.NewReader("{}")))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}
