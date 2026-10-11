package climate

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/era5"
	"github.com/erick-ti/flightbound/backend/internal/places"
)

func TestConversions(t *testing.T) {
	for _, tt := range []struct{ tenths, c, f int }{
		{220, 22, 72}, {225, 23, 73}, {224, 22, 72}, {0, 0, 32}, {100, 10, 50},
		{-25, -3, 28}, {-224, -22, -8}, {-225, -23, -9}, {-5, -1, 31}, {-4, 0, 31},
		{-178, -18, 0}, {-175, -18, 1}, {-181, -18, -1}, {-185, -19, -1}, {-200, -20, -4}, {-206, -21, -5},
	} {
		if got := celsius(tt.tenths); got != tt.c {
			t.Errorf("celsius(%d tenths) = %d, want %d", tt.tenths, got, tt.c)
		}
		if got := fahrenheit(tt.tenths); got != tt.f {
			t.Errorf("fahrenheit(%d tenths) = %d, want %d", tt.tenths, got, tt.f)
		}
	}
	for _, tt := range []struct {
		tenths int
		mm     int
		in     string
	}{
		{0, 0, "0.0"}, {4, 0, "0.0"}, {5, 1, "0.0"}, {13, 1, "0.1"}, {900, 90, "3.5"},
		{1000, 100, "3.9"}, {8994, 899, "35.4"}, {8995, 900, "35.4"}, {254, 25, "1.0"}, {127, 13, "0.5"},
	} {
		if got := millimetres(tt.tenths); got != tt.mm {
			t.Errorf("millimetres(%d tenths) = %d, want %d", tt.tenths, got, tt.mm)
		}
		if got := inches(tt.tenths); got != tt.in {
			t.Errorf("inches(%d tenths of a mm) = %q, want %q", tt.tenths, got, tt.in)
		}
	}
}

func fixture(t *testing.T, ids ...string) *Normals {
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
			ID: id, GridLatitude: 35.75, GridLongitude: 139.75, SolarOffsetHours: 9,
			SourceSHA256: strings.Repeat("0", 64), Months: months,
		})
	}
	n, err := Parse(era5.MarshalNormals(doc))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTypical(t *testing.T) {
	n := fixture(t, "tokyo")
	got, ok := n.Typical("tokyo", []time.Month{time.December, time.January})
	if !ok {
		t.Fatal("no normals for tokyo")
	}
	want := []Month{
		{Month: time.December, HighC: 21, HighF: 70, LowC: 6, LowF: 43, PrecipitationMM: 305, PrecipitationIn: "12.0"},
		{Month: time.January, HighC: 10, HighF: 50, LowC: -5, LowF: 23, PrecipitationMM: 25, PrecipitationIn: "1.0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Typical = %+v, want %+v", got, want)
	}
	if got, ok := n.Typical("lisbon", []time.Month{time.January}); ok || got != nil {
		t.Errorf("a place without normals: %+v, %v; want nothing", got, ok)
	}
	if first, last := n.Period(); first != 1991 || last != 2020 {
		t.Errorf("Period = %d-%d", first, last)
	}
}

func TestParseRefusesNormalsForUnknownPlaces(t *testing.T) {
	doc := era5.Normals{
		Dataset: "x", Variables: []string{}, Period: era5.Period{First: 1991, Last: 2020}, Attribution: "x",
		Places: []era5.PlaceNormals{{ID: "atlantis", SourceSHA256: strings.Repeat("0", 64)}},
	}
	for i := range doc.Places[0].Months {
		doc.Places[0].Months[i].Month = i + 1
	}
	if _, err := Parse(era5.MarshalNormals(doc)); err == nil || !strings.Contains(err.Error(), "atlantis") {
		t.Errorf("error = %v, want one naming atlantis", err)
	}
	if _, err := Parse([]byte("{}")); err == nil {
		t.Error("an empty object: no error")
	}
}

func TestEmbeddedNormalsLoad(t *testing.T) {
	if Embedded() == nil {
		t.Fatal("no embedded normals")
	}
	if first, last := Embedded().Period(); first != 1991 || last != 2020 {
		t.Errorf("embedded period = %d-%d, want 1991-2020", first, last)
	}
}

var allMonths = []time.Month{
	time.January, time.February, time.March, time.April, time.May, time.June,
	time.July, time.August, time.September, time.October, time.November, time.December,
}

// missingPlaces lists the places that n has no normals for.
func missingPlaces(n *Normals) []string {
	var missing []string
	for _, p := range places.All() {
		if _, ok := n.Typical(p.ID, allMonths); !ok {
			missing = append(missing, p.ID)
		}
	}
	return missing
}

// implausible lists months outside what any supported place can have,
// which catches figures in the wrong unit.
func implausible(n *Normals) []string {
	var found []string
	for _, p := range places.All() {
		months, _ := n.Typical(p.ID, allMonths)
		for _, m := range months {
			if m.HighC < -20 || m.HighC > 50 || m.LowC < -30 || m.LowC > 35 || m.LowC > m.HighC || m.PrecipitationMM > 1000 {
				found = append(found, fmt.Sprintf("%s in %s: %+v", p.ID, m.Month, m))
			}
		}
	}
	return found
}

// seasonProblems checks seasons that a hemisphere, unit or month mix-up
// would break: Sydney is warmer in January than in July, Reykjavík is the
// reverse, and Bangkok is wetter in September than in January.
func seasonProblems(n *Normals) []string {
	var problems []string
	pair := func(id string, a, b time.Month) (Month, Month, bool) {
		m, ok := n.Typical(id, []time.Month{a, b})
		if !ok {
			problems = append(problems, id+" has no normals")
			return Month{}, Month{}, false
		}
		return m[0], m[1], true
	}
	if jan, jul, ok := pair("sydney", time.January, time.July); ok && jan.HighC <= jul.HighC {
		problems = append(problems, fmt.Sprintf("Sydney's January high %d °C is not above its July high %d °C", jan.HighC, jul.HighC))
	}
	if jan, jul, ok := pair("reykjavik", time.January, time.July); ok && jul.HighC <= jan.HighC {
		problems = append(problems, fmt.Sprintf("Reykjavík's July high %d °C is not above its January high %d °C", jul.HighC, jan.HighC))
	}
	if jan, sep, ok := pair("bangkok", time.January, time.September); ok && sep.PrecipitationMM <= jan.PrecipitationMM {
		problems = append(problems, fmt.Sprintf("Bangkok's September precipitation %d mm is not above January's %d mm", sep.PrecipitationMM, jan.PrecipitationMM))
	}
	return problems
}

func TestEmbeddedNormalsCoverEveryPlace(t *testing.T) {
	if missing := missingPlaces(Embedded()); len(missing) > 0 {
		t.Errorf("no normals for %v; download them and rebuild normals.json (see docs/trip-comparison.md)", missing)
	}
	// The check finds what is missing: normals for Tokyo alone miss the rest.
	if missing := missingPlaces(fixture(t, "tokyo")); len(missing) != len(places.All())-1 {
		t.Errorf("a Tokyo-only fixture misses %d places, want %d", len(missing), len(places.All())-1)
	}
}

func TestEmbeddedNormalsArePlausible(t *testing.T) {
	if found := implausible(Embedded()); len(found) > 0 {
		t.Errorf("implausible normals:\n%s", strings.Join(found, "\n"))
	}
	// The check finds a figure in the wrong unit: a high of 55, as if in
	// Fahrenheit.
	doc := era5.Normals{Dataset: "x", Variables: []string{}, Period: era5.Period{First: 1991, Last: 2020}, Attribution: "x"}
	var months [12]era5.Month
	for i := range months {
		months[i] = era5.Month{Month: i + 1, HighTenthsC: 200, LowTenthsC: 100, PrecipitationTenthsMM: 500}
	}
	months[6].HighTenthsC = 550
	doc.Places = []era5.PlaceNormals{{ID: "tokyo", SourceSHA256: strings.Repeat("0", 64), Months: months}}
	n, err := Parse(era5.MarshalNormals(doc))
	if err != nil {
		t.Fatal(err)
	}
	if found := implausible(n); len(found) != 1 || !strings.Contains(found[0], "July") {
		t.Errorf("found %v, want one month: July", found)
	}
}

func TestEmbeddedNormalsFollowTheSeasons(t *testing.T) {
	if problems := seasonProblems(Embedded()); len(problems) > 0 {
		t.Errorf("%s", strings.Join(problems, "\n"))
	}
	// The check finds a hemisphere mix-up: the fixture warms from January
	// to July everywhere, so Sydney fails, while Reykjavík and Bangkok pass.
	problems := seasonProblems(fixture(t, "sydney", "reykjavik", "bangkok"))
	if len(problems) != 1 || !strings.Contains(problems[0], "Sydney") {
		t.Errorf("fixture problems %v, want one about Sydney", problems)
	}
}
