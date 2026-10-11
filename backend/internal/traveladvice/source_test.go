package traveladvice

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// upstream is a stand-in for the GOV.UK Content API that counts requests.
type upstream struct {
	*httptest.Server
	requests atomic.Int64
}

func newUpstream(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.requests.Add(1)
		handler(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

const contentPath = "/api/content/foreign-travel-advice"

// checkedAt is the test clock's time, an hour ahead of UTC.
var checkedAt = time.Date(2026, 10, 11, 3, 0, 0, 0, time.FixedZone("BST", 60*60))

// testFetcher returns a Fetcher for u that uses the production client.
func testFetcher(u *upstream) *Fetcher {
	f := NewFetcher(u.URL+contentPath, NewClient())
	f.now = func() time.Time { return checkedAt }
	return f
}

func TestFetcherGetsCurrentAdvice(t *testing.T) {
	page := readPage(t, "thailand")
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != contentPath+"/thailand" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("request = %s %s, Accept %q", r.Method, r.URL.Path, r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(page)
	})
	got, err := testFetcher(u).Advice(t.Context(), "thailand")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Alerts, []Alert{AvoidAllButEssentialTravelToParts}) {
		t.Errorf("alerts = %v", got.Alerts)
	}
	if want := time.Date(2026, 10, 11, 2, 0, 0, 0, time.UTC); !got.CheckedAt.Equal(want) || got.CheckedAt.Location() != time.UTC {
		t.Errorf("checked = %s, want %s", got.CheckedAt, want)
	}
	if n := u.requests.Load(); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

func TestFetcherRejectsBadResponses(t *testing.T) {
	portugal := readPage(t, "portugal")
	serve := func(body []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { w.Write(body) }
	}
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"not found", http.NotFound},
		{"server error with a valid page", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(portugal)
		}},
		// The redirect target serves a page that would parse, so following
		// the redirect would show as advice.
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/moved") {
				w.Write(portugal)
				return
			}
			http.Redirect(w, r, contentPath+"/portugal/moved", http.StatusMovedPermanently)
		}},
		{"maintenance page", serve([]byte("<html><body>Maintenance</body></html>"))},
		{"empty body", serve(nil)},
		{"another country's page", serve(readPage(t, "ukraine"))},
		{"larger than 1 MiB", serve(variant(t, func(p map[string]any) { p["padding"] = strings.Repeat("x", maxBodyBytes) }))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := newUpstream(t, tt.handler)
			if got, err := testFetcher(u).Advice(t.Context(), "portugal"); err == nil {
				t.Errorf("got advice %+v, want an error", got)
			}
			if n := u.requests.Load(); n != 1 {
				t.Errorf("%d requests, want 1", n)
			}
		})
	}
}

func TestFetcherStopsAtTheDeadline(t *testing.T) {
	page := readPage(t, "portugal")
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		w.Write(page)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := testFetcher(u).Advice(ctx, "portugal"); err == nil {
		t.Error("got advice after the deadline")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("returned after %s, want soon after the 100ms deadline", elapsed)
	}
}

func TestFetcherAsksOnlyForKnownCountries(t *testing.T) {
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {})
	for _, slug := range []string{"atlantis", "../portugal", "portugal?x=1", ""} {
		if _, err := testFetcher(u).Advice(t.Context(), slug); err == nil {
			t.Errorf("%q: no error", slug)
		}
	}
	if n := u.requests.Load(); n != 0 {
		t.Errorf("%d requests, want 0", n)
	}
}

// syncBuffer is a log destination that the test server can read while the
// fetcher writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestFetcherLogsEachRequestBeforeSendingIt(t *testing.T) {
	logs := &syncBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	const line = `msg="requesting travel advice" country=portugal`
	var loggedFirst atomic.Bool
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		// The request must already be in the log when it reaches the server.
		loggedFirst.Store(strings.Contains(logs.String(), line))
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})
	if _, err := testFetcher(u).Advice(t.Context(), "portugal"); err == nil {
		t.Fatal("got advice from a failing server")
	}
	if !loggedFirst.Load() {
		t.Errorf("the request reached the server before it was logged; logs:\n%s", logs.String())
	}
}

// Go's transport retries a GET that was sent on a reused connection when the
// server closes it without answering. NewClient never reuses a connection,
// so each fetch is one request.
func TestNewClientSendsEachRequestOnce(t *testing.T) {
	page := readPage(t, "portugal")
	// The server answers the first request, reads the second and closes the
	// connection without answering it, and answers any later request.
	newServer := func(t *testing.T) *upstream {
		var seen atomic.Int64
		return newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
			if seen.Add(1) == 2 {
				if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
					conn.Close()
				}
				return
			}
			w.Write(page)
		})
	}
	fetchTwice := func(t *testing.T, u *upstream, client *http.Client) error {
		f := NewFetcher(u.URL+contentPath, client)
		if _, err := f.Advice(t.Context(), "portugal"); err != nil {
			t.Fatalf("first fetch: %v", err)
		}
		_, err := f.Advice(t.Context(), "portugal")
		return err
	}

	u := newServer(t)
	if err := fetchTwice(t, u, NewClient()); err == nil {
		t.Error("the second fetch got advice although the server closed the connection")
	}
	if n := u.requests.Load(); n != 2 {
		t.Errorf("NewClient: %d requests for 2 fetches, want 2", n)
	}

	// Control: a client that reuses connections retries the second request,
	// which shows the server above would have seen a retry.
	u = newServer(t)
	if err := fetchTwice(t, u, &http.Client{Transport: &http.Transport{}}); err != nil {
		t.Errorf("control: the retried fetch failed: %v", err)
	}
	if n := u.requests.Load(); n != 3 {
		t.Errorf("control: %d requests for 2 fetches, want 3", n)
	}
}

// HTTP/2 has retry rules of its own, so NewClient keeps to HTTP/1.
func TestNewClientSpeaksHTTP1(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Proto)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	protocol := func(client *http.Client) string {
		t.Helper()
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	client := NewClient()
	client.Transport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	if got := protocol(client); got != "HTTP/1.1" {
		t.Errorf("NewClient used %s, want HTTP/1.1", got)
	}
	// Control: the server speaks HTTP/2 to a client that offers it.
	if got := protocol(srv.Client()); got != "HTTP/2.0" {
		t.Errorf("control used %s, want HTTP/2.0", got)
	}
}
