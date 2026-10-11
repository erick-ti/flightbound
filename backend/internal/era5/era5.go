// Package era5 reads hourly ERA5-Land reanalysis time series for one point,
// as the Copernicus Climate Data Store exports them, and summarizes them into
// monthly climate normals.
package era5

import (
	"fmt"
	"time"
)

// Hour is one hourly record at a grid point. Temperature is the air
// temperature 2 m above the ground at Time, in kelvin. Precipitation is the
// total precipitation (rain and snow, as liquid water) over the hour ending
// at Time, in metres.
type Hour struct {
	Time          time.Time
	Temperature   float64
	Precipitation float64
}

// Series is a run of consecutive hours at one grid point.
type Series struct {
	Latitude, Longitude float64
	Hours               []Hour
}

// Period is a span of whole calendar years, such as 1991 to 2020.
type Period struct{ First, Last int }

func (p Period) String() string { return fmt.Sprintf("%d-%d", p.First, p.Last) }

// Month is one calendar month's normals: the mean of the daily highs and of
// the daily lows, in tenths of a degree Celsius, and the mean monthly total
// precipitation, in tenths of a millimetre.
type Month struct {
	Month                 int
	HighTenthsC           int
	LowTenthsC            int
	PrecipitationTenthsMM int
}
