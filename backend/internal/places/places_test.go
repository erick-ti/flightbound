package places

import (
	"strings"
	"testing"

	"github.com/erick-ti/flightbound/backend/internal/traveladvice"
)

func TestLookup(t *testing.T) {
	for _, want := range []Place{
		{ID: "lisbon", Name: "Lisbon", Country: "portugal"},
		{ID: "singapore", Name: "Singapore", Country: "singapore"},
		{ID: "rio-de-janeiro", Name: "Rio de Janeiro", Country: "brazil"},
		{ID: "new-york", Name: "New York", Country: "usa"},
	} {
		p, ok := Lookup(want.ID)
		if !ok || p.Name != want.Name || p.Country != want.Country {
			t.Errorf("Lookup(%q) = %+v, %v; want %s in %s", want.ID, p, ok, want.Name, want.Country)
		}
	}
	for _, id := range []string{"", "Lisbon", " lisbon", "lisbon/", "london", "atlantis", "portugal"} {
		if p, ok := Lookup(id); ok {
			t.Errorf("Lookup(%q) = %+v, want no place", id, p)
		}
	}
}

// The list is maintained by hand, so check what an edit must keep.
func TestSavedListIsValid(t *testing.T) {
	list := All()
	if len(list) < 25 {
		t.Fatalf("%d places; the saved list looks truncated", len(list))
	}
	if err := validate(list); err != nil {
		t.Fatal(err)
	}
}

func TestAllReturnsACopy(t *testing.T) {
	list := All()
	list[0].Name = "changed"
	if All()[0].Name == "changed" {
		t.Error("changing the returned list changed the saved one")
	}
}

func TestValidateRefusesBadLists(t *testing.T) {
	good := func() []Place {
		return []Place{
			{ID: "lisbon", Name: "Lisbon", Country: "portugal", Latitude: 38.7247, Longitude: -9.1468, ClimateLatitude: 38.7, ClimateLongitude: -9.2, SourceID: 1},
			{ID: "tokyo", Name: "Tokyo", Country: "japan", Latitude: 35.687, Longitude: 139.7495, ClimateLatitude: 35.7, ClimateLongitude: 139.7, SourceID: 2},
		}
	}
	if err := validate(good()); err != nil {
		t.Fatalf("a good list: %v", err)
	}
	tests := []struct {
		name   string
		change func(p []Place) []Place
		want   string
	}{
		{"an empty list", func([]Place) []Place { return nil }, "no places"},
		{"an ID that is not a slug", func(p []Place) []Place { p[0].ID = "Lisbon"; return p }, "not a slug"},
		{"an ID with a trailing dash", func(p []Place) []Place { p[0].ID = "lisbon-"; return p }, "not a slug"},
		{"a repeated ID", func(p []Place) []Place { p[1].ID = "lisbon"; return p }, "appears twice"},
		{"an empty name", func(p []Place) []Place { p[0].Name = ""; return p }, "name"},
		{"a padded name", func(p []Place) []Place { p[0].Name = " Lisbon"; return p }, "name"},
		{"a name with a control character", func(p []Place) []Place { p[0].Name = "Lis\tbon"; return p }, "name"},
		{"names out of order", func(p []Place) []Place { p[0], p[1] = p[1], p[0]; return p }, "order"},
		{"a repeated name", func(p []Place) []Place { p[1].Name = "Lisbon"; return p }, "order"},
		{"an unknown country", func(p []Place) []Place { p[0].Country = "atlantis"; return p }, "country"},
		{"a country with no FCDO page", func(p []Place) []Place { p[0].Country = "united-kingdom"; return p }, "country"},
		{"latitude above 90", func(p []Place) []Place { p[0].Latitude = 90.0001; return p }, "latitude"},
		{"latitude below -90", func(p []Place) []Place { p[0].Latitude = -90.5; return p }, "latitude"},
		{"longitude above 180", func(p []Place) []Place { p[0].Longitude = 180.0001; return p }, "longitude"},
		{"longitude below -180", func(p []Place) []Place { p[0].Longitude = -181; return p }, "longitude"},
		{"latitude with five decimals", func(p []Place) []Place { p[0].Latitude = 38.72471; return p }, "latitude"},
		{"longitude with five decimals", func(p []Place) []Place { p[0].Longitude = -9.14681; return p }, "longitude"},
		{"a climate latitude off the grid", func(p []Place) []Place { p[0].ClimateLatitude = 38.75; return p }, "climate point"},
		{"a climate longitude off the grid", func(p []Place) []Place { p[0].ClimateLongitude = -9.15; return p }, "climate point"},
		{"a climate point too far north", func(p []Place) []Place { p[0].ClimateLatitude = 39.1; return p }, "climate point"},
		{"a climate point too far west", func(p []Place) []Place { p[0].ClimateLongitude = -9.5; return p }, "climate point"},
		{"a climate point off the globe", func(p []Place) []Place { p[0].ClimateLatitude = 90.1; return p }, "climate point"},
		{"no source record", func(p []Place) []Place { p[0].SourceID = 0; return p }, "source"},
		{"a repeated source record", func(p []Place) []Place { p[1].SourceID = 1; return p }, "source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.change(good()))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

func TestLoadRefusesBadDocuments(t *testing.T) {
	for name, doc := range map[string]string{
		"not JSON":                  `<html>`,
		"not a list":                `{"id": "lisbon"}`,
		"an unknown field":          `[{"id": "lisbon", "name": "Lisbon", "country": "portugal", "latitude": 38.7247, "longitude": -9.1468, "climate_latitude": 38.7, "climate_longitude": -9.2, "source_id": 1, "population": 1}]`,
		"a repeated field":          `[{"id": "lisbon", "id": "tokyo", "name": "Lisbon", "country": "portugal", "latitude": 38.7247, "longitude": -9.1468, "climate_latitude": 38.7, "climate_longitude": -9.2, "source_id": 1}]`,
		"a differently cased field": `[{"ID": "lisbon", "name": "Lisbon", "country": "portugal", "latitude": 38.7247, "longitude": -9.1468, "climate_latitude": 38.7, "climate_longitude": -9.2, "source_id": 1}]`,
		"a missing field":           `[{"name": "Lisbon", "country": "portugal", "latitude": 38.7247, "longitude": -9.1468, "climate_latitude": 38.7, "climate_longitude": -9.2, "source_id": 1}]`,
		"a missing latitude":        `[{"id": "lisbon", "name": "Lisbon", "country": "portugal", "longitude": -9.1468, "climate_latitude": 38.7, "climate_longitude": -9.2, "source_id": 1}]`,
		"a null longitude":          `[{"id": "lisbon", "name": "Lisbon", "country": "portugal", "latitude": 38.7247, "longitude": null, "climate_latitude": 38.7, "climate_longitude": -9.2, "source_id": 1}]`,
		"a null place":              `[null]`,
		"a missing climate point":   `[{"id": "lisbon", "name": "Lisbon", "country": "portugal", "latitude": 38.7247, "longitude": -9.1468, "climate_latitude": 38.7, "source_id": 1}]`,
		"a string latitude":         `[{"id": "lisbon", "name": "Lisbon", "country": "portugal", "latitude": "38.7247", "longitude": -9.1468, "climate_latitude": 38.7, "climate_longitude": -9.2, "source_id": 1}]`,
		"trailing data":             `[] []`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := load([]byte(doc)); err == nil {
				t.Error("no error")
			}
		})
	}
	list, err := load([]byte(`[{"id": "lisbon", "name": "Lisbon", "country": "portugal", "latitude": 38.7247, "longitude": -9.1468, "climate_latitude": 38.7, "climate_longitude": -9.2, "source_id": 1}]`))
	if err != nil || len(list) != 1 || list[0].Latitude != 38.7247 {
		t.Errorf("a good document: %+v, %v", list, err)
	}
}

func TestEveryCountryHasAName(t *testing.T) {
	for _, p := range All() {
		if c, ok := traveladvice.Lookup(p.Country); !ok || c.Name == "" {
			t.Errorf("%s: country %q has no FCDO name", p.ID, p.Country)
		}
	}
}
