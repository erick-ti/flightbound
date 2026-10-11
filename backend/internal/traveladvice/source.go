package traveladvice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Source provides a country's travel advice.
type Source interface {
	// Advice returns the advice for the country with the given slug, or an
	// error when it is unavailable. ctx bounds how long it may take.
	Advice(ctx context.Context, slug string) (Advice, error)
}

// Fetcher gets a country's advice from the GOV.UK Content API on every
// call. It keeps no copy: GOV.UK asks clients to cache the advice for at
// most 10 seconds, and a comparison shows only advice it has just received.
type Fetcher struct {
	baseURL string
	client  *http.Client
	now     func() time.Time
}

// NewFetcher returns a Fetcher for the content items under baseURL, such as
// https://www.gov.uk/api/content/foreign-travel-advice. It uses a copy of
// client that does not follow redirects. Fetcher sets no time limit of its
// own; callers bound each call with its context.
func NewFetcher(baseURL string, client *http.Client) *Fetcher {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Fetcher{baseURL: strings.TrimSuffix(baseURL, "/"), client: &c, now: time.Now}
}

// NewClient returns an HTTP client for GOV.UK requests that opens a new
// HTTP/1 connection for each request. Go's transport retries a failed GET
// only on a reused connection, so with this client every fetch is exactly
// one request.
func NewClient() *http.Client {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	return &http.Client{Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   true,
		Protocols:           &protocols,
	}}
}

// Advice fetches and parses the country's content item. Each call is logged
// before the request is sent, so the log counts every request to GOV.UK.
func (f *Fetcher) Advice(ctx context.Context, slug string) (Advice, error) {
	if _, ok := Lookup(slug); !ok {
		return Advice{}, fmt.Errorf("%q is not an FCDO country", slug)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/"+slug, nil)
	if err != nil {
		return Advice{}, err
	}
	req.Header.Set("Accept", "application/json")
	slog.Info("requesting travel advice", "country", slug)
	resp, err := f.client.Do(req)
	if err != nil {
		return Advice{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Advice{}, fmt.Errorf("unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return Advice{}, err
	}
	advice, err := Parse(slug, body)
	if err != nil {
		return Advice{}, err
	}
	advice.CheckedAt = f.now().UTC()
	slog.Info("received travel advice", "country", slug)
	return advice, nil
}

// Dir serves advice from saved copies of GOV.UK content items and never
// contacts GOV.UK. It is for tests and offline development.
type Dir struct {
	advice map[string]Advice
}

// LoadDir reads the saved content items in path, one <slug>.json file per
// country; other files and subdirectories are ignored. Every saved item
// must be named after a known country and parse as its advice.
func LoadDir(path string) (*Dir, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	d := &Dir{advice: map[string]Advice{}}
	for _, e := range entries {
		slug, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() {
			continue
		}
		if _, known := Lookup(slug); !known {
			return nil, fmt.Errorf("%s: %q is not an FCDO country", e.Name(), slug)
		}
		body, err := os.ReadFile(filepath.Join(path, e.Name()))
		if err != nil {
			return nil, err
		}
		advice, err := Parse(slug, body)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		d.advice[slug] = advice
	}
	if len(d.advice) == 0 {
		return nil, fmt.Errorf("%s: no saved travel advice", path)
	}
	return d, nil
}

// errNotSaved reports a country without a saved copy.
var errNotSaved = errors.New("no saved copy of this country's advice")

// Advice returns the saved advice for the country. Its CheckedAt is zero,
// since it was not received from GOV.UK.
func (d *Dir) Advice(_ context.Context, slug string) (Advice, error) {
	advice, ok := d.advice[slug]
	if !ok {
		return Advice{}, errNotSaved
	}
	return advice, nil
}
