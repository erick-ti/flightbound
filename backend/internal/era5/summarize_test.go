package era5

import (
	"math"
	"strings"
	"testing"
	"time"
)

var normalPeriod = Period{First: 1991, Last: 2020}

// series builds a contiguous hourly series from first to last (UTC),
// asking temperature and precipitation for each hour's value.
func series(first, last time.Time, temperature, precipitation func(t time.Time) float64) Series {
	s := Series{Latitude: 35.75, Longitude: 139.75}
	for t := first; !t.After(last); t = t.Add(time.Hour) {
		s.Hours = append(s.Hours, Hour{Time: t, Temperature: temperature(t), Precipitation: precipitation(t)})
	}
	return s
}

// downloaded covers what the download script asks for: one UTC day either
// side of the period.
func downloaded(temperature, precipitation func(t time.Time) float64) Series {
	return series(
		time.Date(1990, 12, 31, 0, 0, 0, 0, time.UTC),
		time.Date(2021, 1, 1, 23, 0, 0, 0, time.UTC),
		temperature, precipitation,
	)
}

func constant(v float64) func(time.Time) float64 { return func(time.Time) float64 { return v } }

func TestSummarizeConstantSeries(t *testing.T) {
	// 10.0 °C all the time and 0.1 mm of precipitation every hour.
	months, err := Summarize(downloaded(constant(283.15), constant(0.0001)), 0, normalPeriod)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range months {
		if m.HighTenthsC != 100 || m.LowTenthsC != 100 {
			t.Errorf("month %d: high %d, low %d tenths of a degree; want 100 and 100", m.Month, m.HighTenthsC, m.LowTenthsC)
		}
	}
	// 31 days × 24 hours × 0.1 mm. February averages 22 years of 28 days
	// and 8 leap years of 29 days: (22 × 672 + 8 × 696) / 30 = 678.4.
	for month, want := range map[int]int{1: 744, 2: 678, 4: 720, 12: 744} {
		if got := months[month-1].PrecipitationTenthsMM; got != want {
			t.Errorf("month %d: precipitation %d tenths of a mm, want %d", month, got, want)
		}
	}
	for i, m := range months {
		if m.Month != i+1 {
			t.Errorf("months[%d].Month = %d", i, m.Month)
		}
	}
}

// localDay is the solar day a UTC instant falls in at the given offset.
func localDay(t time.Time, offsetHours int) time.Time {
	l := t.Add(time.Duration(offsetHours) * time.Hour)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
}

// Each local day has one temperature all day, alternating between 270 K and
// 280 K. A day boundary in the wrong place mixes two days, so every day's
// high becomes 280 K and its low 270 K, and the averages change.
func TestDailyHighsAndLowsUseLocalSolarDays(t *testing.T) {
	for _, tt := range []struct {
		longitude float64
		offset    int
	}{{-9.1468, -1}, {139.7495, 9}, {151.2125, 10}, {-99.1329, -7}, {180, 12}, {-179.9, -12}, {0, 0}} {
		dayValue := func(d time.Time) float64 { return 270 + 10*float64(d.YearDay()%2) }
		s := downloaded(
			func(u time.Time) float64 { return dayValue(localDay(u, tt.offset)) },
			constant(0),
		)
		months, err := Summarize(s, tt.longitude, normalPeriod)
		if err != nil {
			t.Fatalf("longitude %v: %v", tt.longitude, err)
		}
		// The expected mean comes straight from the calendar.
		var sum [12]float64
		var days [12]int
		for d := time.Date(1991, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() <= 2020; d = d.AddDate(0, 0, 1) {
			sum[d.Month()-1] += dayValue(d)
			days[d.Month()-1]++
		}
		for i, m := range months {
			want := int(math.Round((sum[i]/float64(days[i]) - 273.15) * 10))
			if m.HighTenthsC != want || m.LowTenthsC != want {
				t.Errorf("longitude %v, month %d: high %d, low %d; want %d for both", tt.longitude, i+1, m.HighTenthsC, m.LowTenthsC, want)
			}
		}
	}
}

// Precipitation falls only on each month's last local day, at a rate that
// depends on the month, so an hour counted in the wrong day moves
// precipitation between months.
func TestPrecipitationHoursCountTowardTheDayTheyCover(t *testing.T) {
	const offset = 9
	rate := func(start time.Time) float64 {
		d := localDay(start, offset)
		if d.AddDate(0, 0, 1).Month() == d.Month() {
			return 0
		}
		return 0.001 * float64(d.Month()) // metres an hour
	}
	// A value is the precipitation over the hour ending at its time.
	s := downloaded(constant(280), func(end time.Time) float64 { return rate(end.Add(-time.Hour)) })
	months, err := Summarize(s, 139.7495, normalPeriod)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range months {
		want := 24 * (i + 1) * 10 // 24 hours × (month) mm, in tenths
		if m.PrecipitationTenthsMM != want {
			t.Errorf("month %d: precipitation %d tenths of a mm, want %d", i+1, m.PrecipitationTenthsMM, want)
		}
	}
}

func TestSolarOffset(t *testing.T) {
	for _, tt := range []struct {
		longitude float64
		want      int
	}{
		{0, 0}, {7.4999, 0}, {7.5, 1}, {-7.5, -1}, {-9.1468, -1}, {139.7495, 9},
		{151.2125, 10}, {-58.4325, -4}, {180, 12}, {-180, -12}, {-179.9, -12}, {172.5, 12},
	} {
		if got := SolarOffset(tt.longitude); got != tt.want {
			t.Errorf("SolarOffset(%v) = %d, want %d", tt.longitude, got, tt.want)
		}
	}
}

func TestRoundTenths(t *testing.T) {
	// Halves (exact in binary) round away from zero.
	for _, tt := range []struct {
		v    float64
		want int
	}{{1.25, 13}, {-1.25, -13}, {0.25, 3}, {-0.25, -3}, {0.75, 8}, {1.24, 12}, {-1.24, -12}, {0, 0}, {21.96, 220}} {
		if got := roundTenths(tt.v); got != tt.want {
			t.Errorf("roundTenths(%v) = %d, want %d", tt.v, got, tt.want)
		}
	}
}

func TestSummarizeRefusesIncompleteSeries(t *testing.T) {
	full := downloaded(constant(280), constant(0))
	at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }
	tests := []struct {
		name      string
		series    Series
		longitude float64
		period    Period
	}{
		{"no hours", Series{}, 0, normalPeriod},
		// At UTC+9 the first local day starts at 15:00 UTC the day before.
		{"starts an hour late for an eastern place", series(at(1990, 12, 31, 16), at(2021, 1, 1, 23), constant(280), constant(0)), 139.7495, normalPeriod},
		// At UTC-7 the last local day's last hour ends at 07:00 UTC on 1 January.
		{"ends an hour early for a western place", series(at(1990, 12, 31, 0), at(2021, 1, 1, 6), constant(280), constant(0)), -99.1329, normalPeriod},
		{"a missing hour", Series{Hours: append(append([]Hour{}, full.Hours[:5000]...), full.Hours[5001:]...)}, 0, normalPeriod},
		{"a repeated hour", Series{Hours: append(append(append([]Hour{}, full.Hours[:5000]...), full.Hours[4999]), full.Hours[5000:]...)}, 0, normalPeriod},
		{"a backwards period", full, 0, Period{First: 2020, Last: 1991}},
		{"a period outside the series", full, 0, Period{First: 1991, Last: 2021}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Summarize(tt.series, tt.longitude, tt.period); err == nil {
				t.Error("no error")
			}
		})
	}
	// The exact hours needed are enough.
	if _, err := Summarize(series(at(1990, 12, 31, 15), at(2020, 12, 31, 15), constant(280), constant(0)), 139.7495, normalPeriod); err != nil {
		t.Errorf("exactly the hours needed at UTC+9: %v", err)
	}
	if _, err := Summarize(series(at(1991, 1, 1, 7), at(2021, 1, 1, 7), constant(280), constant(0)), -99.1329, normalPeriod); err != nil {
		t.Errorf("exactly the hours needed at UTC-7: %v", err)
	}
}

func TestSummarizeErrorsNameTheProblem(t *testing.T) {
	_, err := Summarize(Series{}, 0, normalPeriod)
	if err == nil || !strings.Contains(err.Error(), "1991") {
		t.Errorf("error = %v, want one naming the period", err)
	}
}
