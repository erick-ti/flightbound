package tripcomparison

import "github.com/erick-ti/flightbound/backend/internal/places"

// Location is where a destination's place is, in degrees: the city's point
// in Natural Earth's populated places, not the climate point its typical
// weather comes from.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// locationOf returns where p is, or nil when there is no place.
func locationOf(p *places.Place) *Location {
	if p == nil {
		return nil
	}
	return &Location{Latitude: p.Latitude, Longitude: p.Longitude}
}
