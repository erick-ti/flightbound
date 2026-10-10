package tripcomparison

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/erick-ti/flightbound/backend/internal/fxrates"
)

func strp(s string) *string { return &s }

// unusedRates fails the test if a comparison asks it for rates. Comparisons
// that convert nothing must not need them.
type unusedRates struct{ t *testing.T }

func (u unusedRates) Rates() (fxrates.Rates, bool) {
	u.t.Helper()
	u.t.Error("asked for exchange rates without anything to convert")
	return fxrates.Rates{}, false
}

// unavailableRates has no usable rates, as when the ECB cannot be reached.
type unavailableRates struct{}

func (unavailableRates) Rates() (fxrates.Rates, bool) { return fxrates.Rates{}, false }

// countingRates counts how often a comparison asks for rates.
type countingRates struct {
	source fxrates.Source
	calls  int
}

func (c *countingRates) Rates() (fxrates.Rates, bool) {
	c.calls++
	return c.source.Rates()
}

// fixtureRates returns the ECB rates for 2026-10-09 from the published file
// kept in the fxrates test data.
func fixtureRates(t *testing.T) fxrates.Source {
	t.Helper()
	rates, err := fxrates.LoadFile(filepath.Join("..", "fxrates", "testdata", "eurofxref-daily.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return fxrates.Static(rates)
}

// sampleRequest is three nights in USD: Lisbon has both estimates, Porto
// has no flight estimate yet.
func sampleRequest() Request {
	return Request{
		CheckIn:   "2027-03-10",
		CheckOut:  "2027-03-13",
		Travelers: 2,
		Currency:  "USD",
		Destinations: []DestinationInput{
			{Label: "Lisbon", FlightEstimate: strp("600.10"), NightlyStayEstimate: strp("125.25")},
			{Label: "Porto", FlightEstimate: nil, NightlyStayEstimate: strp("90")},
		},
	}
}

// mustCompare compares a request that converts nothing.
func mustCompare(t *testing.T, req Request) Comparison {
	t.Helper()
	return mustCompareWith(t, req, unusedRates{t})
}

func mustCompareWith(t *testing.T, req Request, rates fxrates.Source) Comparison {
	t.Helper()
	got, errs := Compare(req, rates)
	if len(errs) > 0 {
		t.Fatalf("Compare returned errors: %+v", errs)
	}
	return got
}

func deref(s *string) string {
	if s == nil {
		return "<unknown>"
	}
	return *s
}

func fields(errs []FieldError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Field
	}
	return out
}

func TestCompareSample(t *testing.T) {
	got := mustCompare(t, sampleRequest())

	if got.Nights != 3 || got.Travelers != 2 || got.Currency != "USD" {
		t.Errorf("trip = %d nights, %d travelers, %s; want 3, 2, USD", got.Nights, got.Travelers, got.Currency)
	}
	if got.CheckIn != "2027-03-10" || got.CheckOut != "2027-03-13" {
		t.Errorf("dates = %s to %s", got.CheckIn, got.CheckOut)
	}
	if got.AllComplete {
		t.Error("AllComplete = true with Porto's flight unknown")
	}

	lisbon, porto := got.Destinations[0], got.Destinations[1]
	if lisbon.Label != "Lisbon" || deref(lisbon.FlightEstimate) != "600.10" || deref(lisbon.NightlyStayEstimate) != "125.25" {
		t.Errorf("Lisbon inputs = %q, %s, %s", lisbon.Label, deref(lisbon.FlightEstimate), deref(lisbon.NightlyStayEstimate))
	}
	if deref(lisbon.StayEstimate) != "375.75" || deref(lisbon.FlightAndStayEstimate) != "975.85" || !lisbon.Complete {
		t.Errorf("Lisbon = stay %s, total %s, complete %v; want 375.75, 975.85, true",
			deref(lisbon.StayEstimate), deref(lisbon.FlightAndStayEstimate), lisbon.Complete)
	}
	if porto.FlightEstimate != nil || deref(porto.NightlyStayEstimate) != "90.00" || deref(porto.StayEstimate) != "270.00" {
		t.Errorf("Porto = flight %s, nightly %s, stay %s; want unknown, 90.00, 270.00",
			deref(porto.FlightEstimate), deref(porto.NightlyStayEstimate), deref(porto.StayEstimate))
	}
	if porto.FlightAndStayEstimate != nil || porto.Complete {
		t.Errorf("Porto total = %s, complete %v; want unknown, false", deref(porto.FlightAndStayEstimate), porto.Complete)
	}
	if lisbon.LowestEstimate || porto.LowestEstimate {
		t.Error("a lowest estimate was marked while a destination is incomplete")
	}
}

func TestCompareLongerStay(t *testing.T) {
	req := sampleRequest()
	req.CheckOut = "2027-03-14"
	got := mustCompare(t, req)
	if got.Nights != 4 || deref(got.Destinations[0].FlightAndStayEstimate) != "1101.10" {
		t.Errorf("4 nights: nights %d, total %s; want 4, 1101.10", got.Nights, deref(got.Destinations[0].FlightAndStayEstimate))
	}
}

func TestTravelersDoNotMultiplyWholePartyAmounts(t *testing.T) {
	for _, travelers := range []int{1, 2, 4, MaxTravelers} {
		req := sampleRequest()
		req.Travelers = travelers
		got := mustCompare(t, req)
		lisbon := got.Destinations[0]
		if deref(lisbon.StayEstimate) != "375.75" || deref(lisbon.FlightAndStayEstimate) != "975.85" {
			t.Errorf("%d travelers: stay %s, total %s; want 375.75, 975.85",
				travelers, deref(lisbon.StayEstimate), deref(lisbon.FlightAndStayEstimate))
		}
		if got.Travelers != travelers {
			t.Errorf("Travelers = %d, want %d", got.Travelers, travelers)
		}
	}
}

func TestCalendarNights(t *testing.T) {
	tests := []struct {
		checkIn, checkOut string
		want              int
	}{
		{"2027-01-30", "2027-02-02", 3},   // month end
		{"2027-12-30", "2028-01-02", 3},   // year end
		{"2028-02-28", "2028-03-01", 2},   // leap year
		{"2027-02-28", "2027-03-01", 1},   // common year
		{"2027-03-27", "2027-04-03", 7},   // spans the spring clock change in Europe (Mar 28)
		{"2027-10-30", "2027-11-08", 9},   // spans the autumn changes in Europe (Oct 31) and the US (Nov 7)
		{"2027-01-01", "2028-01-01", 365}, // longest allowed stay
	}
	for _, tt := range tests {
		req := sampleRequest()
		req.CheckIn, req.CheckOut = tt.checkIn, tt.checkOut
		got, errs := Compare(req, unusedRates{t})
		if len(errs) > 0 || got.Nights != tt.want {
			t.Errorf("%s to %s: nights %d, errors %+v; want %d", tt.checkIn, tt.checkOut, got.Nights, errs, tt.want)
		}
	}
}

func TestInvalidDates(t *testing.T) {
	tests := []struct {
		name              string
		checkIn, checkOut string
		field             string
	}{
		{"missing check-in", "", "2027-03-13", "check_in"},
		{"missing check-out", "2027-03-10", "", "check_out"},
		{"nonexistent day", "2027-02-30", "2027-03-13", "check_in"},
		{"nonexistent month", "2027-03-10", "2027-13-01", "check_out"},
		{"unpadded", "2027-3-10", "2027-03-13", "check_in"},
		{"other format", "10/03/2027", "2027-03-13", "check_in"},
		{"timestamp", "2027-03-10T00:00:00Z", "2027-03-13", "check_in"},
		{"surrounding space", " 2027-03-10", "2027-03-13", "check_in"},
		{"same day", "2027-03-10", "2027-03-10", "check_out"},
		{"reversed", "2027-03-10", "2027-03-09", "check_out"},
		{"too long", "2027-01-01", "2028-01-02", "check_out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := sampleRequest()
			req.CheckIn, req.CheckOut = tt.checkIn, tt.checkOut
			_, errs := Compare(req, unusedRates{t})
			if !slices.Equal(fields(errs), []string{tt.field}) {
				t.Errorf("errors = %+v, want one error on %s", errs, tt.field)
			}
		})
	}
}

func TestUnknownEstimates(t *testing.T) {
	tests := []struct {
		name            string
		flight, nightly *string
		stay, total     string
		complete        bool
	}{
		{"both known", strp("600.10"), strp("125.25"), "375.75", "975.85", true},
		{"flight unknown", nil, strp("125.25"), "375.75", "<unknown>", false},
		{"stay unknown", strp("600.10"), nil, "<unknown>", "<unknown>", false},
		{"both unknown", nil, nil, "<unknown>", "<unknown>", false},
		{"zero flight is known", strp("0"), strp("125.25"), "375.75", "375.75", true},
		{"zero stay is known", strp("600.10"), strp("0"), "0.00", "600.10", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := sampleRequest()
			req.Destinations[0].FlightEstimate = tt.flight
			req.Destinations[0].NightlyStayEstimate = tt.nightly
			d := mustCompare(t, req).Destinations[0]
			if deref(d.StayEstimate) != tt.stay || deref(d.FlightAndStayEstimate) != tt.total || d.Complete != tt.complete {
				t.Errorf("stay %s, total %s, complete %v; want %s, %s, %v",
					deref(d.StayEstimate), deref(d.FlightAndStayEstimate), d.Complete, tt.stay, tt.total, tt.complete)
			}
		})
	}
}

func TestLowestEstimate(t *testing.T) {
	dest := func(label string, flight, nightly *string) DestinationInput {
		return DestinationInput{Label: label, FlightEstimate: flight, NightlyStayEstimate: nightly}
	}
	tests := []struct {
		name         string
		destinations []DestinationInput
		want         []bool
		allComplete  bool
	}{
		{
			"unique lowest",
			[]DestinationInput{dest("A", strp("600"), strp("100")), dest("B", strp("500"), strp("100"))},
			[]bool{false, true}, true,
		},
		{
			// 50000 + 3*8000 = 74000 = 62000 + 3*4000; C is 76000.
			"tie marks every lowest",
			[]DestinationInput{dest("A", strp("50000"), strp("8000")), dest("B", strp("62000"), strp("4000")), dest("C", strp("40000"), strp("12000"))},
			[]bool{true, true, false}, true,
		},
		{
			"any incomplete destination suppresses the label",
			[]DestinationInput{dest("A", strp("600"), strp("100")), dest("B", strp("500"), strp("100")), dest("C", nil, strp("10"))},
			[]bool{false, false, false}, false,
		},
		{
			"incomplete destination never wins even when its known part is lowest",
			[]DestinationInput{dest("A", strp("600"), strp("100")), dest("B", strp("1"), nil)},
			[]bool{false, false}, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := sampleRequest()
			req.Currency = "JPY"
			req.Destinations = tt.destinations
			got := mustCompare(t, req)
			var flags []bool
			for _, d := range got.Destinations {
				flags = append(flags, d.LowestEstimate)
			}
			if !slices.Equal(flags, tt.want) || got.AllComplete != tt.allComplete {
				t.Errorf("lowest = %v, all complete %v; want %v, %v", flags, got.AllComplete, tt.want, tt.allComplete)
			}
		})
	}
}

func TestPrecisionFollowsCurrency(t *testing.T) {
	req := sampleRequest()
	req.Currency = "JPY"
	req.Destinations[0].FlightEstimate = strp("74000")
	req.Destinations[0].NightlyStayEstimate = strp("8000")
	req.Destinations[1].FlightEstimate = strp("62000")
	got := mustCompare(t, req)
	if deref(got.Destinations[0].FlightAndStayEstimate) != "98000" || deref(got.Destinations[1].NightlyStayEstimate) != "90" {
		t.Errorf("JPY formatting: total %s, nightly %s; want 98000, 90",
			deref(got.Destinations[0].FlightAndStayEstimate), deref(got.Destinations[1].NightlyStayEstimate))
	}

	tests := []struct {
		currency, amount, message string
	}{
		{"JPY", "100.5", "JPY amounts must be whole numbers."},
		{"USD", "12.345", "USD amounts can have at most 2 decimal places."},
		{"USD", "1,250", "Use digits with an optional decimal point, like 1250.50."},
		{"JPY", "-5", "Use digits with an optional decimal point, like 1250."},
		{"USD", "1000000000000", "This amount is too large."},
	}
	for _, tt := range tests {
		req := sampleRequest()
		req.Currency = tt.currency
		req.Destinations[0].NightlyStayEstimate = strp("125") // valid in both currencies
		req.Destinations[0].FlightEstimate = strp(tt.amount)
		_, errs := Compare(req, unusedRates{t})
		want := []FieldError{{Field: "destinations[0].flight_estimate", Message: tt.message}}
		if !slices.Equal(errs, want) {
			t.Errorf("%s %q: errors %+v, want %+v", tt.currency, tt.amount, errs, want)
		}
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Request)
		field  string
	}{
		{"no travelers", func(r *Request) { r.Travelers = 0 }, "travelers"},
		{"too many travelers", func(r *Request) { r.Travelers = MaxTravelers + 1 }, "travelers"},
		{"missing currency", func(r *Request) { r.Currency = "" }, "currency"},
		{"lowercase currency", func(r *Request) { r.Currency = "usd" }, "currency"},
		{"unsupported currency", func(r *Request) { r.Currency = "XYZ" }, "currency"},
		{"one destination", func(r *Request) { r.Destinations = r.Destinations[:1] }, "destinations"},
		{"four destinations", func(r *Request) {
			r.Destinations = append(r.Destinations, r.Destinations[0], r.Destinations[1])
		}, "destinations"},
		{"blank label", func(r *Request) { r.Destinations[1].Label = "   " }, "destinations[1].label"},
		{"long label", func(r *Request) { r.Destinations[0].Label = strings.Repeat("a", MaxLabelLength+1) }, "destinations[0].label"},
		{"bad nightly", func(r *Request) { r.Destinations[1].NightlyStayEstimate = strp("") }, "destinations[1].nightly_stay_estimate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := sampleRequest()
			tt.modify(&req)
			got, errs := Compare(req, unusedRates{t})
			if !slices.Equal(fields(errs), []string{tt.field}) {
				t.Errorf("errors = %+v, want one error on %s", errs, tt.field)
			}
			if len(got.Destinations) != 0 {
				t.Error("returned a comparison along with errors")
			}
		})
	}
}

func TestLabelIsTrimmedAndCountsCharacters(t *testing.T) {
	req := sampleRequest()
	long := strings.Repeat("é", MaxLabelLength) // 80 characters, 160 bytes
	req.Destinations[0].Label = "  " + long + "  "
	req.Destinations[1].Label = "\tPorto\n"
	got := mustCompare(t, req)
	if got.Destinations[0].Label != long || got.Destinations[1].Label != "Porto" {
		t.Errorf("labels = %q, %q", got.Destinations[0].Label, got.Destinations[1].Label)
	}
}

func TestCollectsEveryError(t *testing.T) {
	req := sampleRequest()
	req.CheckOut = "2027-03-09"
	req.Travelers = 0
	req.Destinations[0].FlightEstimate = strp("12.345")
	req.Destinations[1].Label = ""
	_, errs := Compare(req, unusedRates{t})
	want := []string{"check_out", "travelers", "destinations[0].flight_estimate", "destinations[1].label"}
	if !slices.Equal(fields(errs), want) {
		t.Errorf("error fields = %v, want %v", fields(errs), want)
	}
}

func TestReportsDestinationErrorsWhenCountIsWrong(t *testing.T) {
	req := sampleRequest()
	req.Destinations = []DestinationInput{{Label: "", FlightEstimate: strp("12.345"), NightlyStayEstimate: strp("-1")}}
	_, errs := Compare(req, unusedRates{t})
	want := []string{"destinations", "destinations[0].label", "destinations[0].flight_estimate", "destinations[0].nightly_stay_estimate"}
	if !slices.Equal(fields(errs), want) {
		t.Errorf("error fields = %v, want %v", fields(errs), want)
	}
}

func TestValidatesAtMostMaxDestinations(t *testing.T) {
	req := sampleRequest()
	req.Destinations = make([]DestinationInput, 50) // every label is blank
	_, errs := Compare(req, unusedRates{t})
	want := []string{"destinations", "destinations[0].label", "destinations[1].label", "destinations[2].label"}
	if !slices.Equal(fields(errs), want) {
		t.Errorf("error fields = %v, want %v", fields(errs), want)
	}
}

func TestLargestInputsDoNotOverflow(t *testing.T) {
	req := sampleRequest()
	req.CheckIn, req.CheckOut = "2027-01-01", "2028-01-01" // 365 nights
	largest := strp("999999999999.99")
	req.Destinations[0].FlightEstimate = largest
	req.Destinations[0].NightlyStayEstimate = largest
	got := mustCompare(t, req)
	// 99999999999999 cents * 366 = 36599999999999634 cents.
	if total := deref(got.Destinations[0].FlightAndStayEstimate); total != "365999999999996.34" {
		t.Errorf("total = %s, want 365999999999996.34", total)
	}
}

// convertedSample is the sample with Lisbon's stay at EUR 110 a night, so
// its stay needs converting to USD.
func convertedSample() Request {
	req := sampleRequest()
	req.Destinations[0].NightlyStayEstimate = strp("110")
	req.Destinations[0].NightlyStayEstimateCurrency = strp("EUR")
	return req
}

func TestConvertsAmountsInOtherCurrencies(t *testing.T) {
	rates := &countingRates{source: fixtureRates(t)}
	got := mustCompareWith(t, convertedSample(), rates)
	if rates.calls != 1 {
		t.Errorf("asked for rates %d times, want 1", rates.calls)
	}

	lisbon, porto := got.Destinations[0], got.Destinations[1]
	for _, c := range []struct{ name, got, want string }{
		{"flight", deref(lisbon.FlightEstimate), "600.10"},
		{"flight currency", lisbon.FlightEstimateCurrency, "USD"},
		{"nightly", deref(lisbon.NightlyStayEstimate), "110.00"},
		{"nightly currency", lisbon.NightlyStayEstimateCurrency, "EUR"},
		{"stay in EUR", deref(lisbon.StayEstimate), "330.00"},
		{"converted flight", deref(lisbon.ConvertedFlightEstimate), "600.10"},
		{"converted stay", deref(lisbon.ConvertedStayEstimate), "369.80"}, // 330 x 1.1206 = 369.798
		{"total", deref(lisbon.FlightAndStayEstimate), "969.90"},
	} {
		if c.got != c.want {
			t.Errorf("Lisbon %s = %s, want %s", c.name, c.got, c.want)
		}
	}
	if !lisbon.Complete {
		t.Error("Lisbon is incomplete")
	}
	if porto.NightlyStayEstimateCurrency != "USD" || deref(porto.ConvertedStayEstimate) != "270.00" ||
		porto.ConvertedFlightEstimate != nil || porto.FlightAndStayEstimate != nil {
		t.Errorf("Porto = nightly currency %s, converted stay %s, converted flight %s, total %s",
			porto.NightlyStayEstimateCurrency, deref(porto.ConvertedStayEstimate),
			deref(porto.ConvertedFlightEstimate), deref(porto.FlightAndStayEstimate))
	}

	fx := got.ExchangeRates
	if fx == nil {
		t.Fatal("ExchangeRates is nil after a conversion")
	}
	if !fx.Available || deref(fx.Date) != "2026-10-09" || !maps.Equal(fx.PerEuro, map[string]string{"USD": "1.1206"}) {
		t.Errorf("ExchangeRates = available %v, date %s, per euro %v", fx.Available, deref(fx.Date), fx.PerEuro)
	}
}

func TestConversionBetweenTwoOtherCurrencies(t *testing.T) {
	req := sampleRequest()
	req.Destinations[1].FlightEstimate = strp("54000")
	req.Destinations[1].FlightEstimateCurrency = strp("JPY")
	got := mustCompareWith(t, req, fixtureRates(t))
	porto := got.Destinations[1]
	if deref(porto.FlightEstimate) != "54000" || deref(porto.ConvertedFlightEstimate) != "341.22" || deref(porto.FlightAndStayEstimate) != "611.22" {
		t.Errorf("Porto = flight %s, converted %s, total %s; want 54000, 341.22, 611.22",
			deref(porto.FlightEstimate), deref(porto.ConvertedFlightEstimate), deref(porto.FlightAndStayEstimate))
	}
	// Both rates used are listed: JPY to USD goes through the euro.
	want := map[string]string{"JPY": "177.34", "USD": "1.1206"}
	if got.ExchangeRates == nil || !maps.Equal(got.ExchangeRates.PerEuro, want) {
		t.Errorf("ExchangeRates = %+v, want per euro %v", got.ExchangeRates, want)
	}
	if !got.AllComplete || !got.Destinations[1].LowestEstimate {
		t.Errorf("all complete %v, Porto lowest %v; want true, true", got.AllComplete, got.Destinations[1].LowestEstimate)
	}
}

func TestNothingToConvertNeedsNoRates(t *testing.T) {
	req := sampleRequest()
	// A currency equal to the comparison currency, and another currency on
	// an unknown amount, leave nothing to convert.
	req.Destinations[0].FlightEstimateCurrency = strp("USD")
	req.Destinations[1].FlightEstimateCurrency = strp("EUR")
	got := mustCompare(t, req)
	if got.ExchangeRates != nil {
		t.Errorf("ExchangeRates = %+v, want nil", got.ExchangeRates)
	}
	lisbon, porto := got.Destinations[0], got.Destinations[1]
	if deref(lisbon.ConvertedFlightEstimate) != "600.10" || deref(lisbon.ConvertedStayEstimate) != "375.75" {
		t.Errorf("Lisbon converted = flight %s, stay %s; want 600.10, 375.75",
			deref(lisbon.ConvertedFlightEstimate), deref(lisbon.ConvertedStayEstimate))
	}
	if porto.FlightEstimateCurrency != "EUR" || porto.ConvertedFlightEstimate != nil {
		t.Errorf("Porto flight = currency %s, converted %s; want EUR, unknown",
			porto.FlightEstimateCurrency, deref(porto.ConvertedFlightEstimate))
	}
}

func TestUnavailableRatesLeaveConvertedAmountsUnknown(t *testing.T) {
	got := mustCompareWith(t, convertedSample(), unavailableRates{})
	lisbon := got.Destinations[0]
	if deref(lisbon.StayEstimate) != "330.00" || lisbon.ConvertedStayEstimate != nil {
		t.Errorf("Lisbon stay = %s, converted %s; want 330.00, unknown", deref(lisbon.StayEstimate), deref(lisbon.ConvertedStayEstimate))
	}
	if deref(lisbon.ConvertedFlightEstimate) != "600.10" {
		t.Errorf("Lisbon converted flight = %s, want 600.10 (no conversion needed)", deref(lisbon.ConvertedFlightEstimate))
	}
	if lisbon.FlightAndStayEstimate != nil || lisbon.Complete || got.AllComplete || lisbon.LowestEstimate {
		t.Errorf("Lisbon = total %s, complete %v, lowest %v; all complete %v",
			deref(lisbon.FlightAndStayEstimate), lisbon.Complete, lisbon.LowestEstimate, got.AllComplete)
	}
	fx := got.ExchangeRates
	if fx == nil || fx.Available || fx.Date != nil || fx.PerEuro == nil || len(fx.PerEuro) != 0 {
		t.Errorf("ExchangeRates = %+v, want unavailable with no date and no rates", fx)
	}
}

func TestMissingRateLeavesOnlyThatAmountUnknown(t *testing.T) {
	rates, err := fxrates.ParseECB(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
	<Cube>
		<Cube time='2026-10-09'>
			<Cube currency='USD' rate='1.1206'/>
		</Cube>
	</Cube>
</gesmes:Envelope>
`))
	if err != nil {
		t.Fatal(err)
	}
	req := convertedSample()
	req.Destinations[1].NightlyStayEstimate = strp("9000")
	req.Destinations[1].NightlyStayEstimateCurrency = strp("JPY")
	got := mustCompareWith(t, req, fxrates.Static(rates))
	lisbon, porto := got.Destinations[0], got.Destinations[1]
	if deref(lisbon.ConvertedStayEstimate) != "369.80" || !lisbon.Complete {
		t.Errorf("Lisbon converted stay = %s, complete %v; want 369.80, true", deref(lisbon.ConvertedStayEstimate), lisbon.Complete)
	}
	if deref(porto.StayEstimate) != "27000" || porto.ConvertedStayEstimate != nil || porto.Complete {
		t.Errorf("Porto stay = %s, converted %s, complete %v; want 27000, unknown, false",
			deref(porto.StayEstimate), deref(porto.ConvertedStayEstimate), porto.Complete)
	}
	fx := got.ExchangeRates
	if fx == nil || !fx.Available || deref(fx.Date) != "2026-10-09" || !maps.Equal(fx.PerEuro, map[string]string{"USD": "1.1206"}) {
		t.Errorf("ExchangeRates = %+v", fx)
	}
}

func TestAmountCurrencyValidation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Request)
		want   []FieldError
	}{
		{
			"unsupported amount currency",
			func(r *Request) { r.Destinations[0].FlightEstimateCurrency = strp("XYZ") },
			[]FieldError{{"destinations[0].flight_estimate_currency", "Choose a supported currency."}},
		},
		{
			"lowercase amount currency",
			func(r *Request) { r.Destinations[1].NightlyStayEstimateCurrency = strp("eur") },
			[]FieldError{{"destinations[1].nightly_stay_estimate_currency", "Choose a supported currency."}},
		},
		{
			"empty amount currency",
			func(r *Request) { r.Destinations[0].NightlyStayEstimateCurrency = strp("") },
			[]FieldError{{"destinations[0].nightly_stay_estimate_currency", "Choose a supported currency."}},
		},
		{
			"JPY precision in a USD comparison",
			func(r *Request) {
				r.Destinations[0].NightlyStayEstimateCurrency = strp("JPY")
				r.Destinations[0].NightlyStayEstimate = strp("100.5")
			},
			[]FieldError{{"destinations[0].nightly_stay_estimate", "JPY amounts must be whole numbers."}},
		},
		{
			"USD precision in a JPY comparison",
			func(r *Request) {
				r.Currency = "JPY"
				r.Destinations[0].NightlyStayEstimate = strp("125")
				r.Destinations[0].FlightEstimateCurrency = strp("USD")
				r.Destinations[0].FlightEstimate = strp("12.345")
			},
			[]FieldError{{"destinations[0].flight_estimate", "USD amounts can have at most 2 decimal places."}},
		},
		{
			"an amount in a valid currency is checked when the comparison currency is invalid",
			func(r *Request) {
				r.Currency = "XYZ"
				r.Destinations[0].FlightEstimateCurrency = strp("EUR")
				r.Destinations[0].FlightEstimate = strp("1.234")
			},
			[]FieldError{
				{"currency", "Choose a supported currency."},
				{"destinations[0].flight_estimate", "EUR amounts can have at most 2 decimal places."},
			},
		},
		{
			"an amount in an invalid currency is not checked",
			func(r *Request) {
				r.Destinations[0].FlightEstimateCurrency = strp("XYZ")
				r.Destinations[0].FlightEstimate = strp("1.234")
			},
			[]FieldError{{"destinations[0].flight_estimate_currency", "Choose a supported currency."}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := sampleRequest()
			tt.modify(&req)
			if _, errs := Compare(req, unusedRates{t}); !slices.Equal(errs, tt.want) {
				t.Errorf("errors = %+v, want %+v", errs, tt.want)
			}
		})
	}
}

func TestLowestEstimateComparesConvertedTotals(t *testing.T) {
	dest := func(label, flight, flightCurrency, nightly, nightlyCurrency string) DestinationInput {
		return DestinationInput{
			Label:                       label,
			FlightEstimate:              strp(flight),
			FlightEstimateCurrency:      strp(flightCurrency),
			NightlyStayEstimate:         strp(nightly),
			NightlyStayEstimateCurrency: strp(nightlyCurrency),
		}
	}
	// Three nights in USD: A is 600.00 + EUR 300 (336.18) = 936.18, B is
	// 900.00 + 36.18 = 936.18, and C is JPY 100000 (631.89) + 300.00 = 931.89.
	a := dest("A", "600.00", "USD", "100", "EUR")
	b := dest("B", "900.00", "USD", "12.06", "USD")
	c := dest("C", "100000", "JPY", "100", "USD")
	tests := []struct {
		name         string
		destinations []DestinationInput
		totals       []string
		lowest       []bool
	}{
		{"converted totals can tie", []DestinationInput{a, b}, []string{"936.18", "936.18"}, []bool{true, true}},
		{"a converted total can be lowest", []DestinationInput{a, b, c}, []string{"936.18", "936.18", "931.89"}, []bool{false, false, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := sampleRequest()
			req.Destinations = tt.destinations
			got := mustCompareWith(t, req, fixtureRates(t))
			var totals []string
			var lowest []bool
			for _, d := range got.Destinations {
				totals = append(totals, deref(d.FlightAndStayEstimate))
				lowest = append(lowest, d.LowestEstimate)
			}
			if !slices.Equal(totals, tt.totals) || !slices.Equal(lowest, tt.lowest) {
				t.Errorf("totals %v, lowest %v; want %v, %v", totals, lowest, tt.totals, tt.lowest)
			}
		})
	}
}

func TestConversionOverflowIsReportedForEveryDestination(t *testing.T) {
	req := sampleRequest()
	req.Currency = "IDR"
	req.CheckIn, req.CheckOut = "2027-01-01", "2028-01-01" // 365 nights
	for i := range req.Destinations {
		req.Destinations[i].FlightEstimate = strp("0")
		req.Destinations[i].NightlyStayEstimate = strp("999999999999.99")
		req.Destinations[i].NightlyStayEstimateCurrency = strp("EUR")
	}
	_, errs := Compare(req, fixtureRates(t))
	want := []FieldError{
		{"destinations[0].nightly_stay_estimate", "This estimate is too large to convert."},
		{"destinations[1].nightly_stay_estimate", "This estimate is too large to convert."},
	}
	if !slices.Equal(errs, want) {
		t.Errorf("errors = %+v, want %+v", errs, want)
	}
}

func TestOverflowOfConvertedPartsSum(t *testing.T) {
	// In IDR, the largest flight converts to about 2.0e18 minor units and a
	// four-night stay at the largest nightly amount to about 8.0e18. Each
	// fits in int64; their sum does not.
	req := sampleRequest()
	req.Currency = "IDR"
	req.CheckOut = "2027-03-14" // 4 nights
	largest := strp("999999999999.99")
	req.Destinations[0].FlightEstimate = largest
	req.Destinations[0].FlightEstimateCurrency = strp("EUR")
	req.Destinations[0].NightlyStayEstimate = largest
	req.Destinations[0].NightlyStayEstimateCurrency = strp("EUR")
	_, errs := Compare(req, fixtureRates(t))
	want := []FieldError{{"destinations[0].flight_estimate", "This estimate is too large to calculate."}}
	if !slices.Equal(errs, want) {
		t.Errorf("errors = %+v, want %+v", errs, want)
	}
}
