package tripcomparison

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/fxrates"
	"github.com/erick-ti/flightbound/backend/internal/traveladvice"
)

// fakeAdvice serves fixed advice per country and records each request. A
// country without advice is unavailable.
type fakeAdvice struct {
	advice map[string]traveladvice.Advice
	mu     sync.Mutex
	calls  []string
}

func (f *fakeAdvice) Advice(_ context.Context, slug string) (traveladvice.Advice, error) {
	f.mu.Lock()
	f.calls = append(f.calls, slug)
	f.mu.Unlock()
	advice, ok := f.advice[slug]
	if !ok {
		return traveladvice.Advice{}, errors.New("unavailable")
	}
	return advice, nil
}

func (f *fakeAdvice) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

var (
	portugalUpdated = time.Date(2026, 8, 11, 9, 21, 44, 0, time.UTC)
	checkedAt       = time.Date(2026, 10, 11, 2, 0, 0, 0, time.UTC)
)

func newFakeAdvice() *fakeAdvice {
	return &fakeAdvice{advice: map[string]traveladvice.Advice{
		"portugal": {Alerts: []traveladvice.Alert{}, UpdatedAt: portugalUpdated, CheckedAt: checkedAt},
		"ukraine": {
			Alerts:    []traveladvice.Alert{traveladvice.AvoidAllTravelToParts, traveladvice.AvoidAllButEssentialTravelToParts},
			UpdatedAt: time.Date(2026, 10, 2, 8, 30, 25, 0, time.UTC),
			CheckedAt: checkedAt,
		},
	}}
}

// savedAdvice serves the GOV.UK pages kept in the traveladvice test data.
func savedAdvice(t *testing.T) traveladvice.Source {
	t.Helper()
	dir, err := traveladvice.LoadDir(filepath.Join("..", "traveladvice", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// withCountries returns the sample request with a country for each
// destination; an empty string means none.
func withCountries(countries ...string) Request {
	req := sampleRequest()
	for i, c := range countries {
		if c != "" {
			req.Destinations[i].Country = strp(c)
		}
	}
	return req
}

func TestTravelAdviceIsAttachedToItsDestination(t *testing.T) {
	advice := newFakeAdvice()
	got, errs := Compare(t.Context(), withCountries("portugal", ""), unusedRates{t}, advice)
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	lisbon, porto := got.Destinations[0], got.Destinations[1]
	want := &TravelAdvice{
		CountryName: "Portugal",
		URL:         "https://www.gov.uk/foreign-travel-advice/portugal",
		Available:   true,
		Alerts:      []traveladvice.Alert{},
		UpdatedAt:   strp("2026-08-11T09:21:44Z"),
		CheckedAt:   strp("2026-10-11T02:00:00Z"),
	}
	if deref(lisbon.Country) != "portugal" || !reflect.DeepEqual(lisbon.TravelAdvice, want) {
		t.Errorf("Lisbon: country %q, advice %+v; want portugal, %+v", deref(lisbon.Country), lisbon.TravelAdvice, want)
	}
	if porto.Country != nil || porto.TravelAdvice != nil {
		t.Errorf("Porto without a country: country %v, advice %+v", porto.Country, porto.TravelAdvice)
	}
	if calls := advice.requests(); !reflect.DeepEqual(calls, []string{"portugal"}) {
		t.Errorf("advice requests = %v, want [portugal]", calls)
	}
}

func TestTravelAdviceWireFormat(t *testing.T) {
	body := strings.Replace(sampleBody, `{"label": "Lisbon",`, `{"label": "Lisbon", "country": "portugal",`, 1)
	body = strings.Replace(body, `{"label": "Porto",`, `{"label": "Porto", "country": null,`, 1)
	rec := postWith(t, body, savedAdvice(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got struct {
		Destinations []map[string]json.RawMessage `json:"destinations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	// A saved copy was not received from GOV.UK, so it has no check time.
	want := []map[string]string{
		{
			"country":       `"portugal"`,
			"travel_advice": `{"country_name":"Portugal","url":"https://www.gov.uk/foreign-travel-advice/portugal","available":true,"alerts":[],"updated_at":"2026-08-11T09:21:44Z","checked_at":null}`,
		},
		{"country": "null", "travel_advice": "null"},
	}
	for i, fields := range want {
		for key, value := range fields {
			if raw, ok := got.Destinations[i][key]; !ok || string(raw) != value {
				t.Errorf("destinations[%d].%s = %s (present %v), want %s", i, key, raw, ok, value)
			}
		}
	}
}

// sourceFunc turns a function into a travel advice source.
type sourceFunc func(ctx context.Context, slug string) (traveladvice.Advice, error)

func (f sourceFunc) Advice(ctx context.Context, slug string) (traveladvice.Advice, error) {
	return f(ctx, slug)
}

func TestCountryValidation(t *testing.T) {
	for _, country := range []string{"atlantis", "", "Portugal", " portugal", "united-kingdom"} {
		t.Run(country, func(t *testing.T) {
			req := sampleRequest()
			req.Destinations[1].Country = strp(country)
			advice := newFakeAdvice()
			_, errs := Compare(t.Context(), req, unusedRates{t}, advice)
			want := []FieldError{{"destinations[1].country", "Choose a country from the list."}}
			if !slices.Equal(errs, want) {
				t.Errorf("errors = %+v, want %+v", errs, want)
			}
			if calls := advice.requests(); len(calls) != 0 {
				t.Errorf("asked for advice %v despite a field error", calls)
			}
		})
	}
}

func TestInvalidRequestFetchesNoAdvice(t *testing.T) {
	req := withCountries("portugal", "ukraine")
	req.Travelers = 0
	advice := newFakeAdvice()
	if _, errs := Compare(t.Context(), req, unusedRates{t}, advice); len(errs) == 0 {
		t.Fatal("no errors for zero travelers")
	}
	if calls := advice.requests(); len(calls) != 0 {
		t.Errorf("asked for advice %v for an invalid request", calls)
	}
}

func TestHandlerCountryMustBeAString(t *testing.T) {
	with := func(value string) string {
		return strings.Replace(sampleBody, `{"label": "Lisbon",`, `{"label": "Lisbon", "country": `+value+`,`, 1)
	}
	for _, value := range []string{`123`, `true`, `[]`, `{}`, `["portugal"]`} {
		rec := postWith(t, with(value), newFakeAdvice())
		if rec.Code != http.StatusBadRequest {
			t.Errorf("country %s: status = %d, want 400", value, rec.Code)
		}
	}
	rec := postWith(t, with(`"atlantis"`), newFakeAdvice())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown country: status = %d, body %s", rec.Code, rec.Body)
	}
	if got := decodeError(t, rec); !slices.Equal(fields(got.FieldErrors), []string{"destinations[0].country"}) {
		t.Errorf("unknown country: field errors %+v", got.FieldErrors)
	}
}

func TestEachCountryIsFetchedOnce(t *testing.T) {
	req := withCountries("portugal", "portugal")
	req.Destinations = append(req.Destinations, DestinationInput{
		Label: "Kyiv", Country: strp("ukraine"), FlightEstimate: strp("100"), NightlyStayEstimate: strp("50"),
	})
	advice := newFakeAdvice()
	got, errs := Compare(t.Context(), req, unusedRates{t}, advice)
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	calls := advice.requests()
	slices.Sort(calls)
	if !slices.Equal(calls, []string{"portugal", "ukraine"}) {
		t.Errorf("advice requests = %v, want one each for portugal and ukraine", calls)
	}
	lisbon, porto := got.Destinations[0].TravelAdvice, got.Destinations[1].TravelAdvice
	if lisbon == nil || !lisbon.Available || !reflect.DeepEqual(lisbon, porto) {
		t.Errorf("Lisbon %+v and Porto %+v should share Portugal's advice", lisbon, porto)
	}
}

func TestAdviceChangesNothingElse(t *testing.T) {
	// A budget and a conversion make every other part of the result count.
	req := withCountries("spain", "ukraine")
	req.Budget = strp("1000")
	req.Destinations[0].NightlyStayEstimateCurrency = strp("EUR")
	with, errs := Compare(t.Context(), req, fixtureRates(t), newFakeAdvice())
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	if a := with.Destinations[0].TravelAdvice; a == nil || a.Available || a.Alerts != nil || a.UpdatedAt != nil || a.CheckedAt != nil {
		t.Errorf("Spain has no advice in the fake, so it should be unavailable: %+v", a)
	}
	if a := with.Destinations[1].TravelAdvice; a == nil || !a.Available {
		t.Errorf("Ukraine: %+v, want available", a)
	}

	without := req
	without.Destinations = slices.Clone(req.Destinations)
	for i := range without.Destinations {
		without.Destinations[i].Country = nil
	}
	plain, errs := Compare(t.Context(), without, fixtureRates(t), nil)
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	for i := range with.Destinations {
		with.Destinations[i].Country, with.Destinations[i].TravelAdvice = nil, nil
	}
	if !reflect.DeepEqual(with, plain) {
		t.Errorf("advice changed the comparison:\nwith advice    %+v\nwithout advice %+v", with, plain)
	}
}

// meeting lets a set of callers wait until all of them have arrived.
type meeting struct {
	mu      sync.Mutex
	arrived int
	need    int
	all     chan struct{}
}

func newMeeting(need int) *meeting { return &meeting{need: need, all: make(chan struct{})} }

// arrive reports whether everyone arrived before ctx ended.
func (m *meeting) arrive(ctx context.Context) bool {
	m.mu.Lock()
	m.arrived++
	if m.arrived == m.need {
		close(m.all)
	}
	m.mu.Unlock()
	select {
	case <-m.all:
		return true
	case <-ctx.Done():
		return false
	}
}

type meetingRates struct {
	m     *meeting
	rates fxrates.Source
}

func (r meetingRates) Rates() (fxrates.Rates, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !r.m.arrive(ctx) {
		return fxrates.Rates{}, false
	}
	return r.rates.Rates()
}

func TestAdviceAndRatesAreFetchedTogether(t *testing.T) {
	// Each request waits until the other two have started, so all three
	// succeed only if the comparison makes them at the same time.
	m := newMeeting(3)
	advice := newFakeAdvice()
	source := sourceFunc(func(ctx context.Context, slug string) (traveladvice.Advice, error) {
		if !m.arrive(ctx) {
			return traveladvice.Advice{}, errors.New("the other requests did not start alongside this one")
		}
		return advice.Advice(ctx, slug)
	})
	req := withCountries("portugal", "ukraine")
	req.Destinations[0].NightlyStayEstimateCurrency = strp("EUR")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	got, errs := Compare(ctx, req, meetingRates{m, fixtureRates(t)}, source)
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	if got.ExchangeRates == nil || !got.ExchangeRates.Available {
		t.Error("the rates were not fetched alongside the advice")
	}
	for _, d := range got.Destinations {
		if d.TravelAdvice == nil || !d.TravelAdvice.Available {
			t.Errorf("%s: the advice requests did not overlap", d.Label)
		}
	}
}

func TestAdviceWaitIsBounded(t *testing.T) {
	t.Run("requests get the comparison's deadline", func(t *testing.T) {
		var deadline time.Time
		var set bool
		source := sourceFunc(func(ctx context.Context, slug string) (traveladvice.Advice, error) {
			deadline, set = ctx.Deadline()
			return newFakeAdvice().Advice(ctx, slug)
		})
		start := time.Now()
		if _, errs := Compare(context.Background(), withCountries("portugal", ""), unusedRates{t}, source); errs != nil {
			t.Fatalf("errors: %+v", errs)
		}
		end := time.Now()
		if !set || deadline.Before(start.Add(adviceTimeout)) || deadline.After(end.Add(adviceTimeout)) {
			t.Errorf("deadline %v (set %v), want %s after the comparison started", deadline, set, adviceTimeout)
		}
	})

	t.Run("a request that never answers leaves the advice unavailable", func(t *testing.T) {
		source := sourceFunc(func(ctx context.Context, slug string) (traveladvice.Advice, error) {
			<-ctx.Done()
			return traveladvice.Advice{}, ctx.Err()
		})
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		got, errs := Compare(ctx, withCountries("portugal", ""), unusedRates{t}, source)
		if errs != nil {
			t.Fatalf("errors: %+v", errs)
		}
		if a := got.Destinations[0].TravelAdvice; a == nil || a.Available {
			t.Errorf("advice = %+v, want unavailable", a)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("took %s, want soon after the 100ms deadline", elapsed)
		}
	})
}

func TestCalculationErrorCancelsAdviceRequests(t *testing.T) {
	var finished atomic.Bool
	var cause error
	source := sourceFunc(func(ctx context.Context, slug string) (traveladvice.Advice, error) {
		<-ctx.Done()
		cause = ctx.Err()
		finished.Store(true)
		return traveladvice.Advice{}, ctx.Err()
	})
	// Valid amounts whose stays are too large to convert to IDR.
	req := withCountries("portugal", "")
	req.Currency = "IDR"
	req.CheckIn, req.CheckOut = "2027-01-01", "2028-01-01"
	for i := range req.Destinations {
		req.Destinations[i].FlightEstimate = strp("0")
		req.Destinations[i].NightlyStayEstimate = strp("999999999999.99")
		req.Destinations[i].NightlyStayEstimateCurrency = strp("EUR")
	}
	start := time.Now()
	if _, errs := Compare(t.Context(), req, fixtureRates(t), source); len(errs) == 0 {
		t.Fatal("no errors for amounts too large to convert")
	}
	if !finished.Load() {
		t.Error("returned before the advice request finished")
	}
	if !errors.Is(cause, context.Canceled) {
		t.Errorf("the advice request ended with %v, want cancellation", cause)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s; the request should be cancelled, not left to the deadline", elapsed)
	}
}

func TestAvailableAdviceAlwaysListsAlerts(t *testing.T) {
	source := sourceFunc(func(context.Context, string) (traveladvice.Advice, error) {
		return traveladvice.Advice{UpdatedAt: portugalUpdated}, nil
	})
	got, errs := Compare(t.Context(), withCountries("portugal", ""), unusedRates{t}, source)
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	body, err := json.Marshal(got.Destinations[0].TravelAdvice)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"alerts":[]`) {
		t.Errorf("advice = %s, want an empty alerts list rather than null", body)
	}
}

func TestMissingSourceLeavesAdviceUnavailable(t *testing.T) {
	got, errs := Compare(t.Context(), withCountries("portugal", ""), unusedRates{t}, nil)
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	if a := got.Destinations[0].TravelAdvice; a == nil || a.Available || a.CountryName != "Portugal" {
		t.Errorf("advice = %+v, want Portugal's advice marked unavailable", a)
	}
}
