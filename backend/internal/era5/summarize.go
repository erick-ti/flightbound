package era5

import (
	"fmt"
	"math"
	"time"
)

// Summarize returns the monthly normals of s over the period. Days are local
// solar days at the given longitude (see SolarOffset), because daily highs
// and lows follow the sun: at UTC+10 a UTC day would run from 10:00 to 10:00
// local time. It fails unless s holds every hour, in order and without gaps,
// that the period's local days need.
func Summarize(s Series, longitude float64, p Period) ([12]Month, error) {
	var months [12]Month
	if p.First > p.Last {
		return months, fmt.Errorf("period %v ends before it starts", p)
	}
	if err := checkConsecutive(s.Hours); err != nil {
		return months, err
	}
	offset := time.Duration(SolarOffset(longitude)) * time.Hour
	start := time.Date(p.First, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(p.Last+1, 1, 1, 0, 0, 0, 0, time.UTC)
	// The first local day's first temperature is at its start, and the last
	// local day's last precipitation value is for the hour ending at its end.
	from, to := start.Add(-offset), end.Add(-offset)
	n := len(s.Hours)
	if n == 0 || s.Hours[0].Time.After(from) || s.Hours[n-1].Time.Before(to) {
		return months, fmt.Errorf("%v at UTC%+d needs every hour from %s to %s, but the series %s",
			p, offset/time.Hour, from.Format(time.RFC3339), to.Format(time.RFC3339), describe(s.Hours))
	}
	first := s.Hours[0].Time
	at := func(t time.Time) Hour { return s.Hours[t.Sub(first)/time.Hour] }

	var highs, lows, precipitation [12]float64
	var days [12]int
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		dayStart := day.Add(-offset)
		high, low := math.Inf(-1), math.Inf(1)
		var total float64
		for h := range 24 {
			t := dayStart.Add(time.Duration(h) * time.Hour)
			high = max(high, at(t).Temperature)
			low = min(low, at(t).Temperature)
			// The value an hour later covers the hour starting at t.
			total += at(t.Add(time.Hour)).Precipitation
		}
		m := day.Month() - 1
		highs[m] += high
		lows[m] += low
		precipitation[m] += total
		days[m]++
	}
	years := float64(p.Last - p.First + 1)
	for i := range months {
		months[i] = Month{
			Month:                 i + 1,
			HighTenthsC:           roundTenths(highs[i]/float64(days[i]) - 273.15),
			LowTenthsC:            roundTenths(lows[i]/float64(days[i]) - 273.15),
			PrecipitationTenthsMM: roundTenths(precipitation[i] / years * 1000),
		}
	}
	return months, nil
}

// SolarOffset is the whole number of hours that local solar time is ahead of
// UTC at a longitude: 15 degrees an hour, rounded half away from zero.
func SolarOffset(longitude float64) int {
	return int(math.Round(longitude / 15))
}

// roundTenths rounds to tenths, halves away from zero, and returns the
// number of tenths.
func roundTenths(v float64) int {
	return int(math.Round(v * 10))
}

func checkConsecutive(hours []Hour) error {
	for i := 1; i < len(hours); i++ {
		if want := hours[i-1].Time.Add(time.Hour); !hours[i].Time.Equal(want) {
			return fmt.Errorf("hour %d is %s, want %s", i, hours[i].Time.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	}
	return nil
}

func describe(hours []Hour) string {
	if len(hours) == 0 {
		return "is empty"
	}
	return fmt.Sprintf("runs from %s to %s", hours[0].Time.Format(time.RFC3339), hours[len(hours)-1].Time.Format(time.RFC3339))
}
