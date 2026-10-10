package fxrates

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Source supplies reference rates.
type Source interface {
	// Rates returns the current reference rates, or false when no usable
	// rates are available.
	Rates() (Rates, bool)
}

// Static returns a Source that always serves the given rates, such as rates
// loaded from a file. It never fetches and applies no age limit.
func Static(rates Rates) Source {
	return static{rates: rates}
}

type static struct {
	rates Rates
}

func (s static) Rates() (Rates, bool) {
	return s.rates, true
}

const (
	// refreshAfter is how long fetched rates are kept before the next
	// fetch. The ECB publishes once per working day, around 16:00 CET.
	refreshAfter = time.Hour
	// retryAfter is how long to wait after a failed fetch.
	retryAfter = time.Minute
	// fetchTimeout bounds one whole fetch, including reading the body.
	fetchTimeout = 5 * time.Second
	// maxBodyBytes bounds the response. The daily file is under 2 KiB.
	maxBodyBytes = 64 << 10
	// maxAgeDays is how many days after their reference date rates stay
	// usable. The ECB publishes on every TARGET working day; the longest
	// gap between publications in its calendar is five days.
	maxAgeDays = 7
)

// Fetcher downloads the ECB's daily rates and keeps them in memory. It
// refreshes them hourly, waits a minute after a failed fetch, and keeps
// serving the last rates it fetched until they are more than maxAgeDays
// old. A Fetcher is safe for concurrent use: callers that arrive during a
// fetch wait for it instead of starting another.
type Fetcher struct {
	url     string
	client  *http.Client
	now     func() time.Time
	timeout time.Duration

	mu        sync.Mutex
	rates     Rates
	have      bool
	fetchedAt time.Time // time of the last successful fetch
	retryAt   time.Time // no fetch before this time after a failure
}

// NewFetcher returns a Fetcher for the ECB daily rates document at url.
func NewFetcher(url string, client *http.Client) *Fetcher {
	return &Fetcher{url: url, client: client, now: time.Now, timeout: fetchTimeout}
}

// Rates returns the cached rates, fetching them first when they are missing
// or due for a refresh. The fetch has its own deadline and no caller's
// context, so a caller that gives up cannot cancel it for others.
func (f *Fetcher) Rates() (Rates, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	due := !f.have || now.Sub(f.fetchedAt) >= refreshAfter
	if due && !now.Before(f.retryAt) {
		rates, err := f.fetch()
		// A fetch can take seconds, so the times below are read when it ends.
		now = f.now()
		if err != nil {
			slog.Warn("fetching exchange rates failed", "url", f.url, "err", err)
			f.retryAt = now.Add(retryAfter)
		} else {
			if !f.have || rates.Date != f.rates.Date {
				slog.Info("fetched exchange rates", "url", f.url, "date", rates.Date)
			}
			if !fresh(rates.Date, now) {
				slog.Warn("fetched exchange rates are too old to use", "url", f.url, "date", rates.Date, "max_age_days", maxAgeDays)
			}
			f.rates, f.have, f.fetchedAt = rates, true, now
		}
	}
	if !f.have || !fresh(f.rates.Date, now) {
		return Rates{}, false
	}
	return f.rates, true
}

func (f *Fetcher) fetch() (Rates, error) {
	ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return Rates{}, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return Rates{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Rates{}, fmt.Errorf("unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return Rates{}, err
	}
	if len(body) > maxBodyBytes {
		return Rates{}, errors.New("the response is larger than 64 KiB")
	}
	return ParseECB(bytes.NewReader(body))
}

// fresh reports whether rates with the given reference date are at most
// maxAgeDays old on now's UTC calendar date.
func fresh(date string, now time.Time) bool {
	ref, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return false
	}
	y, m, d := now.UTC().Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return today.Sub(ref) <= maxAgeDays*24*time.Hour
}
