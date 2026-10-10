package fxrates

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// clock is a settable time source.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func (c *clock) Advance(d time.Duration) { c.Set(c.Now().Add(d)) }

// upstream is a stand-in for the ECB server that counts requests.
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

func serve(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.Write(body) }
}

// failable serves body, or a 503 while fail is set.
func failable(body []byte, fail *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write(body)
	}
}

// testFetcher returns a Fetcher for u whose clock starts the day after the
// fixture's reference date.
func testFetcher(u *upstream) (*Fetcher, *clock) {
	c := &clock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	f := NewFetcher(u.URL, u.Client())
	f.now = c.Now
	f.timeout = 200 * time.Millisecond
	return f, c
}

func TestStaticServesItsRates(t *testing.T) {
	got, ok := Static(fixtureRates(t)).Rates()
	if !ok || got.Date != "2026-10-09" {
		t.Errorf("Static rates = date %q, %v", got.Date, ok)
	}
}

func TestFetcherCachesRatesForAnHour(t *testing.T) {
	u := newUpstream(t, serve(readFixture(t)))
	f, c := testFetcher(u)
	for _, step := range []struct {
		advance  time.Duration
		requests int64
	}{
		{0, 1},
		{0, 1},
		{59 * time.Minute, 1},
		{time.Minute, 2}, // an hour after the first fetch
		{30 * time.Minute, 2},
	} {
		c.Advance(step.advance)
		rates, ok := f.Rates()
		if !ok || rates.Date != "2026-10-09" {
			t.Fatalf("at %s: rates = date %q, %v", c.Now().Format(time.TimeOnly), rates.Date, ok)
		}
		if got := u.requests.Load(); got != step.requests {
			t.Fatalf("at %s: %d upstream requests, want %d", c.Now().Format(time.TimeOnly), got, step.requests)
		}
	}
}

func TestFetcherWaitsAMinuteAfterAFailure(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	u := newUpstream(t, failable(readFixture(t), &fail))
	f, c := testFetcher(u)
	if _, ok := f.Rates(); ok {
		t.Fatal("rates after a failed first fetch")
	}
	fail.Store(false)
	c.Advance(59 * time.Second)
	if _, ok := f.Rates(); ok || u.requests.Load() != 1 {
		t.Fatalf("retried within a minute: ok %v, %d requests", ok, u.requests.Load())
	}
	c.Advance(time.Second)
	if rates, ok := f.Rates(); !ok || rates.Date != "2026-10-09" || u.requests.Load() != 2 {
		t.Fatalf("after a minute: date %q, ok %v, %d requests", rates.Date, ok, u.requests.Load())
	}
}

func TestFetcherKeepsLastRatesUntilTheyAreTooOld(t *testing.T) {
	var fail atomic.Bool
	u := newUpstream(t, failable(readFixture(t), &fail))
	f, c := testFetcher(u)
	if _, ok := f.Rates(); !ok {
		t.Fatal("no rates from a working upstream")
	}
	fail.Store(true)
	c.Advance(2 * time.Hour)
	if rates, ok := f.Rates(); !ok || rates.Date != "2026-10-09" || u.requests.Load() != 2 {
		t.Fatalf("after a failed refresh: date %q, ok %v, %d requests", rates.Date, ok, u.requests.Load())
	}
	c.Set(time.Date(2026, 10, 16, 23, 0, 0, 0, time.UTC)) // 7 days after the reference date
	if _, ok := f.Rates(); !ok {
		t.Fatal("stopped serving rates that are 7 days old")
	}
	c.Set(time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)) // 8 days
	if _, ok := f.Rates(); ok {
		t.Fatal("served rates that are 8 days old")
	}
}

func TestFetcherChecksAgeWhenTheFetchEnds(t *testing.T) {
	// Rates dated 2026-10-09 are usable through 2026-10-16. A refresh that
	// starts before midnight and fails after it must not serve them.
	tests := []struct {
		name    string
		refresh time.Time
		ok      bool
	}{
		{"failed refresh within the day", time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), true},
		{"failed refresh across midnight", time.Date(2026, 10, 16, 23, 59, 58, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := readFixture(t)
			var c *clock
			var calls atomic.Int64
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.Write(body)
					return
				}
				c.Advance(4 * time.Second) // the refresh takes 4 seconds
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			})
			f, testClock := testFetcher(u)
			c = testClock
			c.Set(tt.refresh.Add(-refreshAfter))
			if _, ok := f.Rates(); !ok {
				t.Fatal("no rates from the first fetch")
			}
			c.Set(tt.refresh)
			if _, ok := f.Rates(); ok != tt.ok {
				t.Errorf("after the refresh ended at %s: ok = %v, want %v", c.Now().Format(time.RFC3339), ok, tt.ok)
			}
			if got := u.requests.Load(); got != 2 {
				t.Errorf("%d upstream requests, want 2", got)
			}
		})
	}
}

func TestFetcherWarnsWhenFetchedRatesAreTooOld(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		warn bool
	}{
		{"usable rates", time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC), false},
		{"8 days old", time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })

			u := newUpstream(t, serve(readFixture(t)))
			f, c := testFetcher(u)
			c.Set(tt.now)
			_, ok := f.Rates()
			warned := strings.Contains(logs.String(), "too old")
			if ok == tt.warn || warned != tt.warn {
				t.Errorf("ok = %v, warned = %v; want ok %v, warned %v; logs:\n%s", ok, warned, !tt.warn, tt.warn, logs.String())
			}
		})
	}
}

func TestFetcherFreshnessUsesTheUTCDate(t *testing.T) {
	// The fixture is dated 2026-10-09, so it is usable through 2026-10-16 in
	// UTC, whatever the local time zone.
	cest := time.FixedZone("CEST", 2*60*60)
	tests := []struct {
		now time.Time
		ok  bool
	}{
		{time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), true},
		{time.Date(2026, 10, 16, 23, 59, 0, 0, time.UTC), true},
		{time.Date(2026, 10, 17, 1, 0, 0, 0, cest), true}, // 2026-10-16 in UTC
		{time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), false},
		{time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		u := newUpstream(t, serve(readFixture(t)))
		f, c := testFetcher(u)
		c.Set(tt.now)
		if _, ok := f.Rates(); ok != tt.ok {
			t.Errorf("at %s: ok = %v, want %v", tt.now.Format(time.RFC3339), ok, tt.ok)
		}
	}
}

func TestFetcherRejectsBadResponses(t *testing.T) {
	body := readFixture(t)
	oversized := append(bytes.Clone(body), []byte("<!--"+string(bytes.Repeat([]byte("x"), maxBodyBytes))+"-->")...)
	tests := []struct {
		name    string
		handler http.HandlerFunc
		ok      bool
	}{
		{"published file", serve(body), true},
		{"not found", http.NotFound, false},
		{"server error with a valid body", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(body)
		}, false},
		{"maintenance page", serve([]byte("<html><body>Maintenance</body></html>")), false},
		{"empty body", serve(nil), false},
		{"larger than 64 KiB", serve(oversized), false},
		{"slower than the timeout", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			w.Write(body)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := newUpstream(t, tt.handler)
			f, _ := testFetcher(u)
			if _, ok := f.Rates(); ok != tt.ok {
				t.Errorf("ok = %v, want %v", ok, tt.ok)
			}
			if got := u.requests.Load(); got != 1 {
				t.Errorf("%d upstream requests, want 1", got)
			}
		})
	}
}

func TestFetcherSharesOneFetchAcrossConcurrentCalls(t *testing.T) {
	body := readFixture(t)
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Write(body)
	})
	f, _ := testFetcher(u)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if rates, ok := f.Rates(); !ok || rates.Date != "2026-10-09" {
				t.Errorf("rates = date %q, %v", rates.Date, ok)
			}
		})
	}
	wg.Wait()
	if got := u.requests.Load(); got != 1 {
		t.Errorf("%d upstream requests, want 1", got)
	}
}

func TestFetcherWaitersShareABlockedRefresh(t *testing.T) {
	body := readFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce, releaseOnce sync.Once
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		startOnce.Do(func() { close(started) })
		<-release
		w.Write(body)
	})
	// Registered after newUpstream, so it runs first and unblocks the
	// handler before the server closes, even when the test fails early.
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	f, _ := testFetcher(u)
	f.timeout = 10 * time.Second

	done := make(chan bool, 6)
	call := func() {
		_, ok := f.Rates()
		done <- ok
	}
	go call()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first caller never fetched")
	}
	for range 5 {
		go call()
	}
	select {
	case <-done:
		t.Fatal("a caller returned while the fetch was blocked")
	case <-time.After(100 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	for range 6 {
		select {
		case ok := <-done:
			if !ok {
				t.Error("a caller got no rates")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a caller never returned after the fetch finished")
		}
	}
	if got := u.requests.Load(); got != 1 {
		t.Errorf("%d upstream requests, want 1", got)
	}
}
