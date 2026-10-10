package tripcomparison

import (
	"slices"
	"strings"
	"testing"
)

func strp(s string) *string { return &s }

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

func mustCompare(t *testing.T, req Request) Comparison {
	t.Helper()
	got, errs := Compare(req)
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
		got, errs := Compare(req)
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
			_, errs := Compare(req)
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
		_, errs := Compare(req)
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
			got, errs := Compare(req)
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
	_, errs := Compare(req)
	want := []string{"check_out", "travelers", "destinations[0].flight_estimate", "destinations[1].label"}
	if !slices.Equal(fields(errs), want) {
		t.Errorf("error fields = %v, want %v", fields(errs), want)
	}
}

func TestReportsDestinationErrorsWhenCountIsWrong(t *testing.T) {
	req := sampleRequest()
	req.Destinations = []DestinationInput{{Label: "", FlightEstimate: strp("12.345"), NightlyStayEstimate: strp("-1")}}
	_, errs := Compare(req)
	want := []string{"destinations", "destinations[0].label", "destinations[0].flight_estimate", "destinations[0].nightly_stay_estimate"}
	if !slices.Equal(fields(errs), want) {
		t.Errorf("error fields = %v, want %v", fields(errs), want)
	}
}

func TestValidatesAtMostMaxDestinations(t *testing.T) {
	req := sampleRequest()
	req.Destinations = make([]DestinationInput, 50) // every label is blank
	_, errs := Compare(req)
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
