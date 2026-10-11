package tripcomparison

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/climate"
	"github.com/erick-ti/flightbound/backend/internal/era5"
)

// withNormals replaces the saved climate normals, for this test, with
// normals for the given places: highs of 10.0 °C in January rising by one
// degree a month, lows 15 degrees below, and 25.4 mm of precipitation in
// January rising by 25.4 mm a month.
func withNormals(t *testing.T, ids ...string) {
	t.Helper()
	doc := era5.Normals{
		Dataset:     "reanalysis-era5-land-timeseries",
		Variables:   []string{"2m_temperature", "total_precipitation"},
		Period:      era5.Period{First: 1991, Last: 2020},
		Attribution: "Contains modified Copernicus Climate Change Service information 2026.",
	}
	for _, id := range ids {
		var months [12]era5.Month
		for i := range months {
			months[i] = era5.Month{Month: i + 1, HighTenthsC: 100 + 10*i, LowTenthsC: -50 + 10*i, PrecipitationTenthsMM: 254 * (i + 1)}
		}
		doc.Places = append(doc.Places, era5.PlaceNormals{
			ID: id, GridLatitude: 0, GridLongitude: 0, SourceSHA256: strings.Repeat("0", 64), Months: months,
		})
	}
	n, err := climate.Parse(era5.MarshalNormals(doc))
	if err != nil {
		t.Fatal(err)
	}
	saved := climateNormals
	climateNormals = n
	t.Cleanup(func() { climateNormals = saved })
}

func TestStayMonths(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	all := func(from time.Month) []time.Month {
		var out []time.Month
		for i := range 12 {
			out = append(out, time.Month((int(from)-1+i)%12+1))
		}
		return out
	}
	for _, tt := range []struct {
		checkIn string
		nights  int
		want    []time.Month
	}{
		{"2027-03-10", 3, []time.Month{time.March}},
		{"2026-10-28", 6, []time.Month{time.October, time.November}},
		{"2026-12-30", 5, []time.Month{time.December, time.January}},
		{"2028-02-28", 2, []time.Month{time.February}},
		{"2028-02-29", 2, []time.Month{time.February, time.March}},
		{"2027-02-28", 1, []time.Month{time.February}},
		{"2026-10-31", 1, []time.Month{time.October}},
		{"2026-10-01", 31, []time.Month{time.October}},
		{"2026-10-01", 32, []time.Month{time.October, time.November}},
		{"2027-01-01", 365, all(time.January)},
		{"2026-06-15", 365, all(time.June)},
	} {
		if got := stayMonths(day(tt.checkIn), tt.nights); !slices.Equal(got, tt.want) {
			t.Errorf("stayMonths(%s, %d) = %v, want %v", tt.checkIn, tt.nights, got, tt.want)
		}
	}
}

func TestClimateIsAttachedForPlacesWithNormals(t *testing.T) {
	withNormals(t, "lisbon")
	got, errs := Compare(t.Context(), withPlaces("lisbon", "rome"), unusedRates{t}, newFakeAdvice())
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	// The sample stay is 10 to 13 March.
	want := &Climate{
		PlaceName: "Lisbon", PeriodStart: 1991, PeriodEnd: 2020,
		Months: []ClimateMonth{{Month: 3, HighC: 12, HighF: 54, LowC: -3, LowF: 27, PrecipitationMM: 76, PrecipitationIn: "3.0"}},
	}
	if c := got.Destinations[0].Climate; !reflect.DeepEqual(c, want) {
		t.Errorf("Lisbon climate = %+v, want %+v", c, want)
	}
	if c := got.Destinations[1].Climate; c != nil {
		t.Errorf("Rome has no normals but got climate %+v", c)
	}
}

func TestNoPlaceMeansNoClimate(t *testing.T) {
	withNormals(t, "lisbon")
	got, errs := Compare(t.Context(), withCountries("portugal", ""), unusedRates{t}, newFakeAdvice())
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	for i, d := range got.Destinations {
		if d.Climate != nil {
			t.Errorf("destination %d without a place got climate %+v", i, d.Climate)
		}
	}
}

func TestClimateMonthsFollowTheStay(t *testing.T) {
	withNormals(t, "lisbon")
	req := withPlaces("lisbon")
	req.CheckIn, req.CheckOut = "2026-12-30", "2027-01-04"
	got, errs := Compare(t.Context(), req, unusedRates{t}, newFakeAdvice())
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	want := []ClimateMonth{
		{Month: 12, HighC: 21, HighF: 70, LowC: 6, LowF: 43, PrecipitationMM: 305, PrecipitationIn: "12.0"},
		{Month: 1, HighC: 10, HighF: 50, LowC: -5, LowF: 23, PrecipitationMM: 25, PrecipitationIn: "1.0"},
	}
	if c := got.Destinations[0].Climate; c == nil || !reflect.DeepEqual(c.Months, want) {
		t.Errorf("climate = %+v, want months %+v", c, want)
	}
}

func TestClimateChangesNothingElse(t *testing.T) {
	without, errs := Compare(t.Context(), withPlaces("lisbon", "rome"), unusedRates{t}, newFakeAdvice())
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	withNormals(t, "lisbon", "rome")
	with, errs := Compare(t.Context(), withPlaces("lisbon", "rome"), unusedRates{t}, newFakeAdvice())
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	for i := range with.Destinations {
		if with.Destinations[i].Climate == nil {
			t.Fatalf("destination %d has no climate", i)
		}
		with.Destinations[i].Climate = without.Destinations[i].Climate
	}
	if !reflect.DeepEqual(with, without) {
		t.Errorf("with climate = %+v, want %+v apart from climate", with, without)
	}
}

func TestClimateWireFormat(t *testing.T) {
	withNormals(t, "lisbon")
	body := strings.Replace(sampleBody, `{"label": "Lisbon",`, `{"label": "Lisbon", "place": "lisbon",`, 1)
	rec := postWith(t, body, newFakeAdvice())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got struct {
		Destinations []map[string]json.RawMessage `json:"destinations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	const want = `{"place_name":"Lisbon","period_start":1991,"period_end":2020,"months":[` +
		`{"month":3,"high_c":12,"high_f":54,"low_c":-3,"low_f":27,"precipitation_mm":76,"precipitation_in":"3.0"}]}`
	if c := string(got.Destinations[0]["climate"]); c != want {
		t.Errorf("Lisbon climate = %s\nwant %s", c, want)
	}
	if c := string(got.Destinations[1]["climate"]); c != "null" {
		t.Errorf("Porto climate = %s, want null", c)
	}
}
