package tripcomparison

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/erick-ti/flightbound/backend/internal/fxrates"
)

// budgetRequest is three nights in USD with a budget: Lisbon totals 975.85,
// Porto totals 1070.00, and Faro has only a flight estimate of 1200.00.
func budgetRequest(budget string) Request {
	req := sampleRequest()
	req.Budget = strp(budget)
	req.Destinations = []DestinationInput{
		{Label: "Lisbon", FlightEstimate: strp("600.10"), NightlyStayEstimate: strp("125.25")},
		{Label: "Porto", FlightEstimate: strp("800"), NightlyStayEstimate: strp("90")},
		{Label: "Faro", FlightEstimate: strp("1200"), NightlyStayEstimate: nil},
	}
	return req
}

// budgetResult is one destination's budget check, with an unknown
// difference shown as "<unknown>".
type budgetResult struct {
	status     BudgetStatus
	difference string
}

func budgetResults(c Comparison) []budgetResult {
	out := make([]budgetResult, len(c.Destinations))
	for i, d := range c.Destinations {
		out[i] = budgetResult{d.BudgetStatus, deref(d.BudgetDifference)}
	}
	return out
}

func TestWithoutBudgetNothingIsChecked(t *testing.T) {
	got := mustCompare(t, sampleRequest())
	if got.Budget != nil || got.BudgetCurrency != "USD" || got.ConvertedBudget != nil {
		t.Errorf("budget = %s %s, converted %s; want unknown USD, unknown",
			deref(got.Budget), got.BudgetCurrency, deref(got.ConvertedBudget))
	}
	want := []budgetResult{{BudgetNotSet, "<unknown>"}, {BudgetNotSet, "<unknown>"}}
	if results := budgetResults(got); !slices.Equal(results, want) {
		t.Errorf("budget checks = %v, want %v", results, want)
	}
}

func TestBudgetCheck(t *testing.T) {
	stayOnly := DestinationInput{Label: "Coimbra", FlightEstimate: nil, NightlyStayEstimate: strp("400")}
	nothingKnown := DestinationInput{Label: "Braga"}
	tests := []struct {
		name   string
		modify func(*Request)
		want   []budgetResult
	}{
		{
			"left, over, and over by at least",
			func(r *Request) {},
			[]budgetResult{{BudgetWithin, "24.15"}, {BudgetOver, "70.00"}, {BudgetOverAtLeast, "200.00"}},
		},
		{
			"a total equal to the budget leaves nothing",
			func(r *Request) { r.Budget = strp("975.85") },
			[]budgetResult{{BudgetWithin, "0.00"}, {BudgetOver, "94.15"}, {BudgetOverAtLeast, "224.15"}},
		},
		{
			"a total one cent over the budget is over",
			func(r *Request) { r.Budget = strp("975.84") },
			[]budgetResult{{BudgetOver, "0.01"}, {BudgetOver, "94.16"}, {BudgetOverAtLeast, "224.16"}},
		},
		{
			"a known part equal to the budget is unknown",
			func(r *Request) { r.Budget = strp("1200") },
			[]budgetResult{{BudgetWithin, "224.15"}, {BudgetWithin, "130.00"}, {BudgetUnknown, "<unknown>"}},
		},
		{
			"a known part below the budget is unknown",
			func(r *Request) { r.Budget = strp("5000") },
			[]budgetResult{{BudgetWithin, "4024.15"}, {BudgetWithin, "3930.00"}, {BudgetUnknown, "<unknown>"}},
		},
		{
			"a zero budget is a known budget",
			func(r *Request) { r.Budget = strp("0") },
			[]budgetResult{{BudgetOver, "975.85"}, {BudgetOver, "1070.00"}, {BudgetOverAtLeast, "1200.00"}},
		},
		{
			"a zero total is within a zero budget",
			func(r *Request) {
				r.Budget = strp("0")
				r.Destinations[0].FlightEstimate = strp("0")
				r.Destinations[0].NightlyStayEstimate = strp("0")
			},
			[]budgetResult{{BudgetWithin, "0.00"}, {BudgetOver, "1070.00"}, {BudgetOverAtLeast, "1200.00"}},
		},
		{
			// 3 nights at 400.00 is 1200.00.
			"a stay above the budget without a flight is over by at least",
			func(r *Request) { r.Destinations[2] = stayOnly },
			[]budgetResult{{BudgetWithin, "24.15"}, {BudgetOver, "70.00"}, {BudgetOverAtLeast, "200.00"}},
		},
		{
			"a destination with no estimates is unknown",
			func(r *Request) { r.Destinations[2] = nothingKnown },
			[]budgetResult{{BudgetWithin, "24.15"}, {BudgetOver, "70.00"}, {BudgetUnknown, "<unknown>"}},
		},
		{
			// 50000 + 3*8000 = 74000, 62000 + 3*4000 = 74000, 40000 + 3*12000 = 76000.
			"a budget in a currency without minor units",
			func(r *Request) {
				r.Currency = "JPY"
				r.Budget = strp("74000")
				r.Destinations = []DestinationInput{
					{Label: "Osaka", FlightEstimate: strp("50000"), NightlyStayEstimate: strp("8000")},
					{Label: "Sapporo", FlightEstimate: strp("62000"), NightlyStayEstimate: strp("4000")},
					{Label: "Naha", FlightEstimate: strp("40000"), NightlyStayEstimate: strp("12000")},
				}
			},
			[]budgetResult{{BudgetWithin, "0"}, {BudgetWithin, "0"}, {BudgetOver, "2000"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := budgetRequest("1000")
			tt.modify(&req)
			got := mustCompare(t, req)
			if results := budgetResults(got); !slices.Equal(results, tt.want) {
				t.Errorf("budget checks = %v, want %v", results, tt.want)
			}
			if got.Budget == nil || got.ConvertedBudget == nil || *got.Budget != *got.ConvertedBudget {
				t.Errorf("budget %s, converted %s; want the same known amount", deref(got.Budget), deref(got.ConvertedBudget))
			}
		})
	}
}

func TestBudgetInAnotherCurrencyIsConverted(t *testing.T) {
	// EUR 900.00 at 1.1206 is exactly USD 1008.54.
	req := sampleRequest()
	req.Budget = strp("900")
	req.BudgetCurrency = strp("EUR")
	rates := &countingRates{source: fixtureRates(t)}
	got := mustCompareWith(t, req, rates)
	if rates.calls != 1 {
		t.Errorf("asked for rates %d times, want 1", rates.calls)
	}
	if deref(got.Budget) != "900.00" || got.BudgetCurrency != "EUR" || deref(got.ConvertedBudget) != "1008.54" {
		t.Errorf("budget = %s %s, converted %s; want 900.00 EUR, 1008.54",
			deref(got.Budget), got.BudgetCurrency, deref(got.ConvertedBudget))
	}
	// Porto's known stay of 270.00 is below the budget, so its check waits
	// for its flight estimate.
	want := []budgetResult{{BudgetWithin, "32.69"}, {BudgetUnknown, "<unknown>"}}
	if results := budgetResults(got); !slices.Equal(results, want) {
		t.Errorf("budget checks = %v, want %v", results, want)
	}
	fx := got.ExchangeRates
	if fx == nil || !fx.Available || deref(fx.Date) != "2026-10-09" || !maps.Equal(fx.PerEuro, map[string]string{"USD": "1.1206"}) {
		t.Errorf("ExchangeRates = %+v", fx)
	}
}

func TestBudgetAndEstimatesShareOneRatesLookup(t *testing.T) {
	req := convertedSample()
	req.Budget = strp("100000")
	req.BudgetCurrency = strp("JPY")
	rates := &countingRates{source: fixtureRates(t)}
	got := mustCompareWith(t, req, rates)
	if rates.calls != 1 {
		t.Errorf("asked for rates %d times, want 1", rates.calls)
	}
	// JPY 100000 is EUR 563.888..., which is USD 631.89. Lisbon totals 969.90.
	if deref(got.ConvertedBudget) != "631.89" {
		t.Errorf("converted budget = %s, want 631.89", deref(got.ConvertedBudget))
	}
	if results := budgetResults(got); results[0] != (budgetResult{BudgetOver, "338.01"}) {
		t.Errorf("Lisbon budget check = %v, want over by 338.01", results[0])
	}
	want := map[string]string{"JPY": "177.34", "USD": "1.1206"}
	if got.ExchangeRates == nil || !maps.Equal(got.ExchangeRates.PerEuro, want) {
		t.Errorf("ExchangeRates = %+v, want per euro %v", got.ExchangeRates, want)
	}
}

func TestBudgetInComparisonCurrencyNeedsNoRates(t *testing.T) {
	req := budgetRequest("1000")
	req.BudgetCurrency = strp("USD")
	got := mustCompare(t, req)
	if got.ExchangeRates != nil {
		t.Errorf("ExchangeRates = %+v, want nil", got.ExchangeRates)
	}
	if deref(got.ConvertedBudget) != "1000.00" {
		t.Errorf("converted budget = %s, want 1000.00", deref(got.ConvertedBudget))
	}
}

func TestBudgetThatCannotBeConvertedIsUnknown(t *testing.T) {
	onlyUSD, err := fxrates.ParseECB(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
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
	tests := []struct {
		name     string
		currency string
		rates    fxrates.Source
	}{
		{"rates unavailable", "EUR", unavailableRates{}},
		{"no rate for the budget currency", "JPY", fxrates.Static(onlyUSD)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := budgetRequest("1000")
			req.BudgetCurrency = strp(tt.currency)
			got := mustCompareWith(t, req, tt.rates)
			if deref(got.Budget) == "<unknown>" || got.ConvertedBudget != nil {
				t.Errorf("budget = %s, converted %s; want a known budget that is not converted",
					deref(got.Budget), deref(got.ConvertedBudget))
			}
			// Totals in the comparison currency are unaffected, but nothing can
			// be checked against the budget, not even a complete destination.
			want := []budgetResult{{BudgetUnknown, "<unknown>"}, {BudgetUnknown, "<unknown>"}, {BudgetUnknown, "<unknown>"}}
			if results := budgetResults(got); !slices.Equal(results, want) {
				t.Errorf("budget checks = %v, want %v", results, want)
			}
			if total := deref(got.Destinations[0].FlightAndStayEstimate); total != "975.85" {
				t.Errorf("Lisbon total = %s, want 975.85", total)
			}
		})
	}
}

func TestUnconvertedPartLeavesTheOtherPartToCheck(t *testing.T) {
	// The budget is in the comparison currency, but Coimbra's flight is in
	// EUR and cannot be converted without rates. Its stay of 1200.00 alone
	// is over the budget.
	req := budgetRequest("1000")
	req.Destinations[2] = DestinationInput{
		Label:                  "Coimbra",
		FlightEstimate:         strp("300"),
		FlightEstimateCurrency: strp("EUR"),
		NightlyStayEstimate:    strp("400"),
	}
	got := mustCompareWith(t, req, unavailableRates{})
	coimbra := got.Destinations[2]
	if coimbra.ConvertedFlightEstimate != nil || deref(coimbra.ConvertedStayEstimate) != "1200.00" || coimbra.Complete {
		t.Fatalf("Coimbra = converted flight %s, converted stay %s, complete %v",
			deref(coimbra.ConvertedFlightEstimate), deref(coimbra.ConvertedStayEstimate), coimbra.Complete)
	}
	want := []budgetResult{{BudgetWithin, "24.15"}, {BudgetOver, "70.00"}, {BudgetOverAtLeast, "200.00"}}
	if results := budgetResults(got); !slices.Equal(results, want) {
		t.Errorf("budget checks = %v, want %v", results, want)
	}
}

func TestPartWithoutARateLeavesTheOtherPartToCheck(t *testing.T) {
	// The rates have USD but no JPY, so the EUR budget converts while each
	// destination's JPY flight cannot. Each stay is in USD.
	onlyUSD, err := fxrates.ParseECB(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
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
	req := sampleRequest()
	req.Budget = strp("900")
	req.BudgetCurrency = strp("EUR")
	// 3 nights at 400.00 is 1200.00, above the budget's 1008.54; 3 nights
	// at 300.00 is 900.00, below it.
	for i, nightly := range []string{"400", "300"} {
		req.Destinations[i] = DestinationInput{
			Label:                  []string{"Kyoto", "Nara"}[i],
			FlightEstimate:         strp("50000"),
			FlightEstimateCurrency: strp("JPY"),
			NightlyStayEstimate:    strp(nightly),
		}
	}
	got := mustCompareWith(t, req, fxrates.Static(onlyUSD))
	if deref(got.ConvertedBudget) != "1008.54" {
		t.Fatalf("converted budget = %s, want 1008.54", deref(got.ConvertedBudget))
	}
	for _, d := range got.Destinations {
		if d.ConvertedFlightEstimate != nil || d.Complete {
			t.Fatalf("%s = converted flight %s, complete %v; want unknown, false", d.Label, deref(d.ConvertedFlightEstimate), d.Complete)
		}
	}
	want := []budgetResult{{BudgetOverAtLeast, "191.46"}, {BudgetUnknown, "<unknown>"}}
	if results := budgetResults(got); !slices.Equal(results, want) {
		t.Errorf("budget checks = %v, want %v", results, want)
	}
}

func TestBudgetChangesNothingElse(t *testing.T) {
	tied := sampleRequest()
	tied.Currency = "JPY"
	tied.Destinations = []DestinationInput{
		{Label: "Osaka", FlightEstimate: strp("50000"), NightlyStayEstimate: strp("8000")},
		{Label: "Sapporo", FlightEstimate: strp("62000"), NightlyStayEstimate: strp("4000")},
	}
	tests := []struct {
		name  string
		req   Request
		rates func(*testing.T) fxrates.Source
	}{
		{"incomplete", sampleRequest(), func(t *testing.T) fxrates.Source { return unusedRates{t} }},
		{"tied lowest", tied, func(t *testing.T) fxrates.Source { return unusedRates{t} }},
		{"converted", convertedSample(), fixtureRates},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			without := mustCompareWith(t, tt.req, tt.rates(t))
			withBudget := tt.req
			withBudget.Budget = strp("500")
			with := mustCompareWith(t, withBudget, tt.rates(t))

			// Clearing the budget's own fields must leave the responses equal.
			with.Budget, with.ConvertedBudget = nil, nil
			for i := range with.Destinations {
				with.Destinations[i].BudgetStatus = BudgetNotSet
				with.Destinations[i].BudgetDifference = nil
			}
			withJSON, err := json.Marshal(with)
			if err != nil {
				t.Fatal(err)
			}
			withoutJSON, err := json.Marshal(without)
			if err != nil {
				t.Fatal(err)
			}
			if string(withJSON) != string(withoutJSON) {
				t.Errorf("with a budget:\n%s\nwithout:\n%s", withJSON, withoutJSON)
			}
		})
	}
}

func TestBudgetValidation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Request)
		want   []FieldError
	}{
		{
			"precision follows the comparison currency by default",
			func(r *Request) { r.Budget = strp("12.345") },
			[]FieldError{{"budget", "USD amounts can have at most 2 decimal places."}},
		},
		{
			"precision follows the budget currency",
			func(r *Request) {
				r.Budget = strp("100.5")
				r.BudgetCurrency = strp("JPY")
			},
			[]FieldError{{"budget", "JPY amounts must be whole numbers."}},
		},
		{
			"separators are rejected",
			func(r *Request) { r.Budget = strp("1,000") },
			[]FieldError{{"budget", "Use digits with an optional decimal point, like 1250.50."}},
		},
		{
			"an empty budget is not an unknown one",
			func(r *Request) { r.Budget = strp("") },
			[]FieldError{{"budget", "Use digits with an optional decimal point, like 1250.50."}},
		},
		{
			"too many digits",
			func(r *Request) { r.Budget = strp("1000000000000") },
			[]FieldError{{"budget", "This amount is too large."}},
		},
		{
			"an unsupported budget currency skips the amount check",
			func(r *Request) {
				r.Budget = strp("1.234")
				r.BudgetCurrency = strp("XYZ")
			},
			[]FieldError{{"budget_currency", "Choose a supported currency."}},
		},
		{
			"a budget currency is checked without a budget",
			func(r *Request) { r.BudgetCurrency = strp("eur") },
			[]FieldError{{"budget_currency", "Choose a supported currency."}},
		},
		{
			"a budget following an invalid comparison currency is not checked",
			func(r *Request) {
				r.Currency = "XYZ"
				r.Budget = strp("1.234")
			},
			[]FieldError{{"currency", "Choose a supported currency."}},
		},
		{
			"a budget in a valid currency is checked when the comparison currency is invalid",
			func(r *Request) {
				r.Currency = "XYZ"
				r.Budget = strp("1.234")
				r.BudgetCurrency = strp("EUR")
			},
			[]FieldError{
				{"currency", "Choose a supported currency."},
				{"budget", "EUR amounts can have at most 2 decimal places."},
			},
		},
		{
			"budget errors come after the trip's and before the destinations'",
			func(r *Request) {
				r.CheckOut = "2027-03-09"
				r.Budget = strp("12.345")
				r.Destinations[0].FlightEstimate = strp("12.345")
			},
			[]FieldError{
				{"check_out", "Check-out must be after check-in."},
				{"budget", "USD amounts can have at most 2 decimal places."},
				{"destinations[0].flight_estimate", "USD amounts can have at most 2 decimal places."},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := sampleRequest()
			tt.modify(&req)
			got, errs := Compare(req, unusedRates{t})
			if !slices.Equal(errs, tt.want) {
				t.Errorf("errors = %+v, want %+v", errs, tt.want)
			}
			if len(got.Destinations) != 0 {
				t.Error("returned a comparison along with errors")
			}
		})
	}
}

func TestBudgetConversionOverflowIsReported(t *testing.T) {
	// An IDR rate far above any published one makes the largest budget too
	// large to convert, and a smaller flight estimate too.
	huge, err := fxrates.ParseECB(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
	<Cube>
		<Cube time='2026-10-09'>
			<Cube currency='IDR' rate='99999999.99'/>
		</Cube>
	</Cube>
</gesmes:Envelope>
`))
	if err != nil {
		t.Fatal(err)
	}
	req := sampleRequest()
	req.Currency = "IDR"
	req.Budget = strp("999999999999.99")
	req.BudgetCurrency = strp("EUR")
	req.Destinations[0].FlightEstimate = strp("999999999.99")
	req.Destinations[0].FlightEstimateCurrency = strp("EUR")
	_, errs := Compare(req, fxrates.Static(huge))
	want := []FieldError{
		{"budget", "This budget is too large to convert."},
		{"destinations[0].flight_estimate", "This estimate is too large to convert."},
	}
	if !slices.Equal(errs, want) {
		t.Errorf("errors = %+v, want %+v", errs, want)
	}
}
