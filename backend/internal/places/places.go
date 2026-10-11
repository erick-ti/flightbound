// Package places is the list of destinations the planner knows by name: real
// places with coordinates, each in a country with FCDO travel advice.
package places

import (
	_ "embed"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/erick-ti/flightbound/backend/internal/traveladvice"
)

// Place is a supported destination. ID never changes once published.
// Country is the FCDO travel advice slug of the country the place is in.
// SourceID is the place's ne_id in Natural Earth's populated places, where
// its coordinates come from. ClimateLatitude and ClimateLongitude are the
// ERA5-Land grid point its typical weather comes from: the point of the 0.1
// degree grid nearest the place that is at least half land in ECMWF's
// land-sea mask and that ERA5-Land covers.
type Place struct {
	ID               string
	Name             string
	Country          string
	Latitude         float64
	Longitude        float64
	ClimateLatitude  float64
	ClimateLongitude float64
	SourceID         int64
}

// entry is one place as saved in places.json. The pointers tell a missing
// member from a zero, so a place without a latitude cannot load as 0.
type entry struct {
	ID               *string  `json:"id"`
	Name             *string  `json:"name"`
	Country          *string  `json:"country"`
	Latitude         *float64 `json:"latitude"`
	Longitude        *float64 `json:"longitude"`
	ClimateLatitude  *float64 `json:"climate_latitude"`
	ClimateLongitude *float64 `json:"climate_longitude"`
	SourceID         *int64   `json:"source_id"`
}

// placesJSON is the hand-kept list of supported places, in name order.
//
//go:embed places.json
var placesJSON []byte

var places, byID = mustLoad(placesJSON)

func mustLoad(data []byte) ([]Place, map[string]Place) {
	list, err := load(data)
	if err != nil {
		panic("places: places.json: " + err.Error())
	}
	m := make(map[string]Place, len(list))
	for _, p := range list {
		m[p.ID] = p
	}
	return list, m
}

// load decodes and checks a saved list. encoding/json/v2 matches member
// names exactly and rejects repeated names and data after the list. Unknown
// members are rejected too, since the file's whole shape is ours.
func load(data []byte) ([]Place, error) {
	var entries []*entry
	if err := json.Unmarshal(data, &entries, json.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	list := make([]Place, 0, len(entries))
	for i, e := range entries {
		if e == nil || e.ID == nil || e.Name == nil || e.Country == nil || e.Latitude == nil || e.Longitude == nil ||
			e.ClimateLatitude == nil || e.ClimateLongitude == nil || e.SourceID == nil {
			return nil, fmt.Errorf("place %d is missing a member", i)
		}
		list = append(list, Place{
			ID:               *e.ID,
			Name:             *e.Name,
			Country:          *e.Country,
			Latitude:         *e.Latitude,
			Longitude:        *e.Longitude,
			ClimateLatitude:  *e.ClimateLatitude,
			ClimateLongitude: *e.ClimateLongitude,
			SourceID:         *e.SourceID,
		})
	}
	if err := validate(list); err != nil {
		return nil, err
	}
	return list, nil
}

var slug = regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)

// validate checks what an edit to the saved list must keep.
func validate(list []Place) error {
	if len(list) == 0 {
		return errors.New("no places")
	}
	ids := map[string]bool{}
	sources := map[int64]bool{}
	for i, p := range list {
		switch {
		case !slug.MatchString(p.ID):
			return fmt.Errorf("ID %q is not a slug", p.ID)
		case ids[p.ID]:
			return fmt.Errorf("ID %q appears twice", p.ID)
		case p.Name == "" || p.Name != strings.TrimSpace(p.Name) || strings.IndexFunc(p.Name, unicode.IsControl) >= 0:
			return fmt.Errorf("%s: name %q is empty, padded, or has control characters", p.ID, p.Name)
		case i > 0 && list[i-1].Name >= p.Name:
			return fmt.Errorf("%s: %q comes after %q; keep the names in order", p.ID, p.Name, list[i-1].Name)
		case p.SourceID <= 0:
			return fmt.Errorf("%s: no source record", p.ID)
		case sources[p.SourceID]:
			return fmt.Errorf("%s: source record %d appears twice", p.ID, p.SourceID)
		}
		if _, ok := traveladvice.Lookup(p.Country); !ok {
			return fmt.Errorf("%s: country %q has no FCDO travel advice", p.ID, p.Country)
		}
		if err := checkCoordinate("latitude", p.Latitude, 90); err != nil {
			return fmt.Errorf("%s: %w", p.ID, err)
		}
		if err := checkCoordinate("longitude", p.Longitude, 180); err != nil {
			return fmt.Errorf("%s: %w", p.ID, err)
		}
		if err := checkClimatePoint(p); err != nil {
			return fmt.Errorf("%s: climate point %v, %v %w", p.ID, p.ClimateLatitude, p.ClimateLongitude, err)
		}
		ids[p.ID] = true
		sources[p.SourceID] = true
	}
	return nil
}

// maxClimateOffset bounds how far, in degrees of latitude and longitude, a
// place's climate point may be from the place. The nearest grid point is
// within 0.05, and the nearest mostly-land point is usually within 0.1.
const maxClimateOffset = 0.3

// checkClimatePoint checks that a place's climate point lies on the 0.1
// degree grid, on the globe, and near the place.
func checkClimatePoint(p Place) error {
	lat, lon := p.ClimateLatitude, p.ClimateLongitude
	if math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return errors.New("is not on the globe")
	}
	for _, v := range []float64{lat, lon} {
		s := strconv.FormatFloat(v, 'f', -1, 64)
		if i := strings.IndexByte(s, '.'); i >= 0 && len(s)-i-1 > 1 {
			return errors.New("is not on the 0.1 degree grid")
		}
	}
	dLon := math.Mod(lon-p.Longitude+540, 360) - 180
	if math.Abs(lat-p.Latitude) > maxClimateOffset || math.Abs(dLon) > maxClimateOffset {
		return fmt.Errorf("is more than %v degrees from the place", maxClimateOffset)
	}
	return nil
}

// checkCoordinate allows at most four decimal places (about 11 metres),
// far finer than any grid the coordinates are matched to.
func checkCoordinate(name string, v, limit float64) error {
	if math.IsNaN(v) || v < -limit || v > limit {
		return fmt.Errorf("%s %v is outside -%v to %v", name, v, limit, limit)
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if i := strings.IndexByte(s, '.'); i >= 0 && len(s)-i-1 > 4 {
		return fmt.Errorf("%s %s has more than four decimal places", name, s)
	}
	return nil
}

// All returns every supported place, in name order.
func All() []Place {
	return slices.Clone(places)
}

// Lookup returns the place with the given ID.
func Lookup(id string) (Place, bool) {
	p, ok := byID[id]
	return p, ok
}
