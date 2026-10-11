package tripcomparison

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/traveladvice"
)

// adviceTimeout bounds how long a comparison waits for travel advice. The
// requests run alongside the exchange-rate fetch, which has its own
// 5-second limit, so a comparison finishes well within the server's
// 10-second write timeout.
const adviceTimeout = 5 * time.Second

// TravelAdvice is FCDO's travel advice for a destination's country.
type TravelAdvice struct {
	CountryName string `json:"country_name"`
	// URL is the country's advice page on GOV.UK.
	URL string `json:"url"`
	// Available is false when the advice could not be fetched or read in
	// time. Alerts, UpdatedAt, and CheckedAt are then nil.
	Available bool `json:"available"`
	// Alerts lists FCDO's warnings against travel, most severe first. It is
	// empty when FCDO advises against no travel to the country.
	Alerts []traveladvice.Alert `json:"alerts"`
	// UpdatedAt is when FCDO last changed the advice. CheckedAt is when the
	// server received it from GOV.UK for this comparison, or nil when it
	// came from a saved copy. Both are RFC 3339 times in UTC.
	UpdatedAt *string `json:"updated_at"`
	CheckedAt *string `json:"checked_at"`
}

// adviceLookup asks for each distinct country's advice concurrently.
type adviceLookup struct {
	wg      sync.WaitGroup
	slugs   []string
	results []adviceResult
}

type adviceResult struct {
	advice traveladvice.Advice
	err    error
}

// errNoSource marks advice asked of a comparison that has no source.
var errNoSource = errors.New("no travel advice source")

// lookUpAdvice starts one request per distinct country among the
// destinations and returns without waiting for them.
func lookUpAdvice(ctx context.Context, source traveladvice.Source, parsed []destination) *adviceLookup {
	l := &adviceLookup{}
	for _, d := range parsed {
		if d.country != nil && !slices.Contains(l.slugs, d.country.Slug) {
			l.slugs = append(l.slugs, d.country.Slug)
		}
	}
	l.results = make([]adviceResult, len(l.slugs))
	for i, slug := range l.slugs {
		if source == nil {
			l.results[i] = adviceResult{err: errNoSource}
			continue
		}
		l.wg.Go(func() {
			advice, err := source.Advice(ctx, slug)
			l.results[i] = adviceResult{advice, err}
		})
	}
	return l
}

// wait blocks until every request has returned.
func (l *adviceLookup) wait() {
	l.wg.Wait()
}

// attach waits for the requests, then sets each destination's country and
// advice. Advice that could not be had is marked unavailable and logged.
func (l *adviceLookup) attach(destinations []DestinationEstimate, parsed []destination) {
	l.wait()
	results := make(map[string]adviceResult, len(l.slugs))
	for i, slug := range l.slugs {
		if err := l.results[i].err; err != nil {
			slog.Warn("travel advice unavailable", "country", slug, "err", err)
		}
		results[slug] = l.results[i]
	}
	for i, d := range parsed {
		if d.country == nil {
			continue
		}
		slug := d.country.Slug
		destinations[i].Country = &slug
		destinations[i].TravelAdvice = travelAdvice(*d.country, results[slug])
	}
}

func travelAdvice(c traveladvice.Country, r adviceResult) *TravelAdvice {
	t := &TravelAdvice{CountryName: c.Name, URL: traveladvice.PageURL(c.Slug)}
	if r.err != nil {
		return t
	}
	t.Available = true
	t.Alerts = r.advice.Alerts
	if t.Alerts == nil {
		t.Alerts = []traveladvice.Alert{}
	}
	t.UpdatedAt = timestamp(r.advice.UpdatedAt)
	if !r.advice.CheckedAt.IsZero() {
		t.CheckedAt = timestamp(r.advice.CheckedAt)
	}
	return t
}

func timestamp(t time.Time) *string {
	s := t.UTC().Format(time.RFC3339)
	return &s
}
