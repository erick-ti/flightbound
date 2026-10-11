package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/fxrates"
	"github.com/erick-ti/flightbound/backend/internal/offlinetest"
	"github.com/erick-ti/flightbound/backend/internal/traveladvice"
)

func TestMain(m *testing.M) {
	os.Exit(offlinetest.Main(m))
}

// offlinetest.Main points the proxy variables at its guard. Both outside
// sources' clients take their proxy from the environment (the rates client
// through http.DefaultTransport), so their requests go to the guard instead
// of out.
func TestOutsideRequestsGoToTheTestGuard(t *testing.T) {
	guard := offlinetest.ProxyURL()
	if guard == "" {
		t.Fatal("tests are not running under offlinetest.Main")
	}
	transports := map[string]*http.Transport{
		"exchange rates": http.DefaultTransport.(*http.Transport),
		"travel advice":  traveladvice.NewClient().Transport.(*http.Transport),
	}
	for _, target := range []string{ecbDailyURL, travelAdviceURL + "/portugal"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		for name, transport := range transports {
			proxy, err := transport.Proxy(req)
			if err != nil || proxy == nil || proxy.String() != guard {
				t.Errorf("%s client, %s: proxy = %v, %v; want the test guard %s", name, target, proxy, err, guard)
			}
		}
	}
}

// fixture is the published ECB file kept in the fxrates test data.
var fixture = filepath.Join("..", "..", "internal", "fxrates", "testdata", "eurofxref-daily.xml")

// savedAdvice holds the GOV.UK travel advice pages kept in the traveladvice
// test data.
var savedAdvice = filepath.Join("..", "..", "internal", "traveladvice", "testdata")

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
	if cfg.adviceDir != "" {
		t.Errorf("advice directory without the flag: %q", cfg.adviceDir)
	}
	cfg, err = parseFlags([]string{"-travel-advice-dir", "advice"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.adviceDir != "advice" {
		t.Errorf("advice directory = %q, want advice", cfg.adviceDir)
	}
}

func TestAdviceSource(t *testing.T) {
	t.Run("saved copies are served without fetching", func(t *testing.T) {
		source, err := adviceSource(savedAdvice)
		if err != nil {
			t.Fatal(err)
		}
		if _, fetches := source.(*traveladvice.Fetcher); fetches {
			t.Fatal("directory mode returned a fetching source")
		}
		advice, err := source.Advice(t.Context(), "ukraine")
		if err != nil || len(advice.Alerts) != 2 || !advice.CheckedAt.IsZero() {
			t.Errorf("ukraine = %+v, %v; want two alerts and no check time", advice, err)
		}
	})

	t.Run("a missing or empty directory stops startup", func(t *testing.T) {
		for _, dir := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
			if _, err := adviceSource(dir); err == nil {
				t.Errorf("%s: no error", dir)
			}
		}
	})

	t.Run("without a directory the advice is fetched", func(t *testing.T) {
		// Building the fetcher sends nothing; no test here asks it for advice.
		source, err := adviceSource("")
		if err != nil {
			t.Fatal(err)
		}
		if _, fetches := source.(*traveladvice.Fetcher); !fetches {
			t.Errorf("source = %T, want a *traveladvice.Fetcher", source)
		}
	})
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
	advice, err := traveladvice.LoadDir(savedAdvice)
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(fxrates.Static(fxrates.Rates{}), advice)

	checkJSON := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q", got)
		}
	}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method+" lists the travel advice countries", func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(method, "/api/travel-advice/countries", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			checkJSON(t, rec)
			var body struct {
				Countries []traveladvice.Country `json:"countries"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			if !slices.Equal(body.Countries, traveladvice.Countries()) {
				t.Errorf("%d countries, want the %d saved ones", len(body.Countries), len(traveladvice.Countries()))
			}
		})
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		t.Run(method+" on the countries gets a JSON 405", func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(method, "/api/travel-advice/countries", nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d", rec.Code)
			}
			if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
				t.Errorf("Allow = %q, want GET, HEAD", got)
			}
			checkJSON(t, rec)
			var body struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Message == "" {
				t.Errorf("body = %q, want a JSON message", rec.Body)
			}
		})
	}

	t.Run("a comparison gets advice for its countries", func(t *testing.T) {
		body := `{"check_in": "2027-03-10", "check_out": "2027-03-13", "travelers": 1, "currency": "EUR", "destinations": [
			{"label": "Kyiv", "country": "ukraine", "flight_estimate": "100", "nightly_stay_estimate": "50"},
			{"label": "Lisbon", "flight_estimate": "100", "nightly_stay_estimate": "50"}]}`
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trip-comparisons", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		var got struct {
			Destinations []struct {
				TravelAdvice *struct {
					Available bool     `json:"available"`
					Alerts    []string `json:"alerts"`
				} `json:"travel_advice"`
			} `json:"destinations"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if a := got.Destinations[0].TravelAdvice; a == nil || !a.Available || len(a.Alerts) != 2 {
			t.Errorf("Kyiv advice = %+v, want two alerts", a)
		}
		if got.Destinations[1].TravelAdvice != nil {
			t.Errorf("Lisbon without a country got advice")
		}
	})

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
