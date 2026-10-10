package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/fxrates"
)

// fixture is the published ECB file kept in the fxrates test data.
var fixture = filepath.Join("..", "..", "internal", "fxrates", "testdata", "eurofxref-daily.xml")

// ratesServer stands in for the rates URL. It counts requests and serves a
// document dated today, so a fetched copy is always recent enough to use.
func ratesServer(t *testing.T) (srv *httptest.Server, requests *atomic.Int64, date string) {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Format(time.DateOnly)
	body := strings.Replace(string(data), "time='2026-10-09'", "time='"+today+"'", 1)
	requests = new(atomic.Int64)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, requests, today
}

func TestParseFlags(t *testing.T) {
	cfg, err := parseFlags([]string{"-fx-rates-file", "rates.xml"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ratesFile != "rates.xml" || cfg.ratesURLSet || cfg.ratesURL != ecbDailyURL || cfg.addr != "127.0.0.1:8080" {
		t.Errorf("file only: %+v", cfg)
	}
	cfg, err = parseFlags([]string{"-fx-rates-url", "http://127.0.0.1:9/rates.xml", "-addr", "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ratesURLSet || cfg.ratesURL != "http://127.0.0.1:9/rates.xml" || cfg.ratesFile != "" || cfg.addr != "127.0.0.1:0" {
		t.Errorf("URL: %+v", cfg)
	}
}

func TestRatesSource(t *testing.T) {
	t.Run("a rates file is used without contacting the URL", func(t *testing.T) {
		srv, requests, _ := ratesServer(t)
		source, err := ratesSource(fixture, srv.URL, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, fetches := source.(*fxrates.Fetcher); fetches {
			t.Fatal("file mode returned a fetching source")
		}
		if rates, ok := source.Rates(); !ok || rates.Date != "2026-10-09" {
			t.Errorf("rates = date %q, %v; want the file's 2026-10-09", rates.Date, ok)
		}
		if got := requests.Load(); got != 0 {
			t.Errorf("%d requests to the rates URL, want 0", got)
		}
	})

	t.Run("a bad rates file stops startup without contacting the URL", func(t *testing.T) {
		srv, requests, _ := ratesServer(t)
		bad := filepath.Join(t.TempDir(), "rates.xml")
		if err := os.WriteFile(bad, []byte("<html>Maintenance</html>"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{filepath.Join(t.TempDir(), "missing.xml"), bad} {
			if _, err := ratesSource(file, srv.URL, false); err == nil {
				t.Errorf("%s: no error", filepath.Base(file))
			}
		}
		if got := requests.Load(); got != 0 {
			t.Errorf("%d requests to the rates URL, want 0", got)
		}
	})

	t.Run("a rates file and an explicit URL are refused together", func(t *testing.T) {
		srv, requests, _ := ratesServer(t)
		if _, err := ratesSource(fixture, srv.URL, true); err == nil {
			t.Error("no error")
		}
		if got := requests.Load(); got != 0 {
			t.Errorf("%d requests to the rates URL, want 0", got)
		}
	})

	t.Run("without a rates file the URL is fetched", func(t *testing.T) {
		srv, requests, today := ratesServer(t)
		source, err := ratesSource("", srv.URL, true)
		if err != nil {
			t.Fatal(err)
		}
		if rates, ok := source.Rates(); !ok || rates.Date != today {
			t.Errorf("rates = date %q, %v", rates.Date, ok)
		}
		if got := requests.Load(); got != 1 {
			t.Errorf("%d requests to the rates URL, want 1", got)
		}
	})
}

func TestRoutes(t *testing.T) {
	mux := newMux(fxrates.Static(fxrates.Rates{}))

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
