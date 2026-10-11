package tripcomparison

import (
	"slices"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/climate"
	"github.com/erick-ti/flightbound/backend/internal/places"
)

// climateNormals is where typical weather comes from. Tests replace it.
var climateNormals = climate.Embedded()

// Climate is the typical weather at a destination's place for each calendar
// month its stay touches.
type Climate struct {
	PlaceName   string `json:"place_name"`
	PeriodStart int    `json:"period_start"`
	PeriodEnd   int    `json:"period_end"`
	// Months are in the order the stay reaches them.
	Months []ClimateMonth `json:"months"`
}

// ClimateMonth is one month's typical weather, rounded for display: the
// average daily high and low, and the average precipitation (rain and
// snow, as liquid water) for the whole month.
type ClimateMonth struct {
	Month           int    `json:"month"`
	HighC           int    `json:"high_c"`
	HighF           int    `json:"high_f"`
	LowC            int    `json:"low_c"`
	LowF            int    `json:"low_f"`
	PrecipitationMM int    `json:"precipitation_mm"`
	PrecipitationIn string `json:"precipitation_in"`
}

// stayMonths returns the calendar months of a stay's nights, from check-in
// to the night before check-out, each once and in order. checkIn is a civil
// date at UTC midnight, so adding days never crosses a clock change.
func stayMonths(checkIn time.Time, nights int) []time.Month {
	var months []time.Month
	for i := range nights {
		if m := checkIn.AddDate(0, 0, i).Month(); !slices.Contains(months, m) {
			months = append(months, m)
		}
	}
	return months
}

// climateFor returns the typical weather at a place for the months, or nil
// when there is no place or the place has no normals.
func climateFor(n *climate.Normals, p *places.Place, months []time.Month) *Climate {
	if p == nil {
		return nil
	}
	typical, ok := n.Typical(p.ID, months)
	if !ok {
		return nil
	}
	first, last := n.Period()
	c := &Climate{PlaceName: p.Name, PeriodStart: first, PeriodEnd: last, Months: make([]ClimateMonth, len(typical))}
	for i, m := range typical {
		c.Months[i] = ClimateMonth{
			Month:           int(m.Month),
			HighC:           m.HighC,
			HighF:           m.HighF,
			LowC:            m.LowC,
			LowF:            m.LowF,
			PrecipitationMM: m.PrecipitationMM,
			PrecipitationIn: m.PrecipitationIn,
		}
	}
	return c
}
