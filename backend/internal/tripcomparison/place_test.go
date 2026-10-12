package tripcomparison

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// withPlaces returns the sample request with a place for each destination;
// an empty string means none.
func withPlaces(ids ...string) Request {
	req := sampleRequest()
	for i, id := range ids {
		if id != "" {
			req.Destinations[i].Place = strp(id)
		}
	}
	return req
}

func TestPlaceSetsTheCountry(t *testing.T) {
	advice := newFakeAdvice()
	got, errs := Compare(t.Context(), withPlaces("lisbon", ""), unusedRates{t}, advice)
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	lisbon, porto := got.Destinations[0], got.Destinations[1]
	if deref(lisbon.Place) != "lisbon" || deref(lisbon.Country) != "portugal" {
		t.Errorf("Lisbon: place %q, country %q; want lisbon in portugal", deref(lisbon.Place), deref(lisbon.Country))
	}
	if a := lisbon.TravelAdvice; a == nil || !a.Available || a.CountryName != "Portugal" {
		t.Errorf("Lisbon advice = %+v, want Portugal's", a)
	}
	if porto.Place != nil || porto.Country != nil || porto.TravelAdvice != nil {
		t.Errorf("Porto without a place: place %v, country %v, advice %+v", porto.Place, porto.Country, porto.TravelAdvice)
	}
	if calls := advice.requests(); !reflect.DeepEqual(calls, []string{"portugal"}) {
		t.Errorf("advice requests = %v, want [portugal]", calls)
	}
}

func TestPlaceSetsTheLocation(t *testing.T) {
	got, errs := Compare(t.Context(), withPlaces("lisbon", "rome"), unusedRates{t}, newFakeAdvice())
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	// The cities' points from places.json, not their climate points (38.7,
	// -9.2 and 41.9, 12.5).
	want := []Location{{Latitude: 38.7247, Longitude: -9.1468}, {Latitude: 41.8979, Longitude: 12.4813}}
	for i, d := range got.Destinations {
		if d.Location == nil || *d.Location != want[i] {
			t.Errorf("destination %d location = %+v, want %+v", i, d.Location, want[i])
		}
	}
	without, errs := Compare(t.Context(), sampleRequest(), unusedRates{t}, newFakeAdvice())
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	for i, d := range without.Destinations {
		if d.Location != nil {
			t.Errorf("destination %d without a place: location = %+v, want nil", i, d.Location)
		}
	}
}

func TestPlaceWithItsOwnCountry(t *testing.T) {
	req := withPlaces("lisbon")
	req.Destinations[0].Country = strp("portugal")
	advice := newFakeAdvice()
	got, errs := Compare(t.Context(), req, unusedRates{t}, advice)
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	if d := got.Destinations[0]; deref(d.Place) != "lisbon" || deref(d.Country) != "portugal" {
		t.Errorf("place %q, country %q; want lisbon in portugal", deref(d.Place), deref(d.Country))
	}
	if calls := advice.requests(); !reflect.DeepEqual(calls, []string{"portugal"}) {
		t.Errorf("advice requests = %v, want [portugal]", calls)
	}
}

func TestPlaceWithAnotherCountryIsRejected(t *testing.T) {
	for _, country := range []string{"spain", "atlantis", ""} {
		t.Run(country, func(t *testing.T) {
			req := withPlaces("", "lisbon")
			req.Destinations[1].Country = strp(country)
			advice := newFakeAdvice()
			_, errs := Compare(t.Context(), req, unusedRates{t}, advice)
			want := []FieldError{{"destinations[1].country", "This place is in Portugal. Choose Portugal or leave the country empty."}}
			if !slices.Equal(errs, want) {
				t.Errorf("errors = %+v, want %+v", errs, want)
			}
			if calls := advice.requests(); len(calls) != 0 {
				t.Errorf("asked for advice %v despite a field error", calls)
			}
		})
	}
}

func TestPlaceValidation(t *testing.T) {
	for _, place := range []string{"atlantis", "", "Lisbon", " lisbon", "lisbon/", "london", "portugal"} {
		t.Run(place, func(t *testing.T) {
			req := sampleRequest()
			req.Destinations[1].Place = strp(place)
			advice := newFakeAdvice()
			_, errs := Compare(t.Context(), req, unusedRates{t}, advice)
			want := []FieldError{{"destinations[1].place", "Choose a place from the list."}}
			if !slices.Equal(errs, want) {
				t.Errorf("errors = %+v, want %+v", errs, want)
			}
			if calls := advice.requests(); len(calls) != 0 {
				t.Errorf("asked for advice %v despite a field error", calls)
			}
		})
	}
}

// An unknown place says nothing about the country, which is checked on its
// own.
func TestUnknownPlaceLeavesTheCountryToItself(t *testing.T) {
	req := withPlaces("atlantis")
	req.Destinations[0].Country = strp("spain")
	_, errs := Compare(t.Context(), req, unusedRates{t}, newFakeAdvice())
	if want := []string{"destinations[0].place"}; !slices.Equal(fields(errs), want) {
		t.Errorf("unknown place with a valid country: errors %+v, want only the place", errs)
	}
	req.Destinations[0].Country = strp("atlantis")
	_, errs = Compare(t.Context(), req, unusedRates{t}, newFakeAdvice())
	if want := []string{"destinations[0].place", "destinations[0].country"}; !slices.Equal(fields(errs), want) {
		t.Errorf("unknown place and country: errors %+v, want both", errs)
	}
}

func TestPlaceChangesNothingElse(t *testing.T) {
	without, errs := Compare(t.Context(), sampleRequest(), unusedRates{t}, newFakeAdvice())
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	with, errs := Compare(t.Context(), withPlaces("lisbon", "rome"), unusedRates{t}, newFakeAdvice())
	if errs != nil {
		t.Fatalf("errors: %+v", errs)
	}
	for i := range with.Destinations {
		d := with.Destinations[i]
		d.Place, d.Country, d.TravelAdvice, d.Climate, d.Location = nil, nil, nil, nil, nil
		if !reflect.DeepEqual(d, without.Destinations[i]) {
			t.Errorf("destination %d with a place = %+v, want %+v apart from place, country, advice, climate and location", i, d, without.Destinations[i])
		}
	}
	with.Destinations, without.Destinations = nil, nil
	if !reflect.DeepEqual(with, without) {
		t.Errorf("comparison with places = %+v, want %+v", with, without)
	}
}

func TestHandlerPlaceMustBeAString(t *testing.T) {
	with := func(value string) string {
		return strings.Replace(sampleBody, `{"label": "Lisbon",`, `{"label": "Lisbon", "place": `+value+`,`, 1)
	}
	for _, value := range []string{`123`, `true`, `[]`, `{}`, `["lisbon"]`} {
		rec := postWith(t, with(value), newFakeAdvice())
		if rec.Code != http.StatusBadRequest {
			t.Errorf("place %s: status = %d, want 400", value, rec.Code)
		}
	}
	rec := postWith(t, with(`"atlantis"`), newFakeAdvice())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown place: status = %d, body %s", rec.Code, rec.Body)
	}
	if got := decodeError(t, rec); !slices.Equal(fields(got.FieldErrors), []string{"destinations[0].place"}) {
		t.Errorf("unknown place: field errors %+v", got.FieldErrors)
	}
}

func TestPlaceWireFormat(t *testing.T) {
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
	for i, want := range []struct{ place, country, location string }{
		{`"lisbon"`, `"portugal"`, `{"latitude":38.7247,"longitude":-9.1468}`},
		{`null`, `null`, `null`},
	} {
		d := got.Destinations[i]
		if string(d["place"]) != want.place || string(d["country"]) != want.country || string(d["location"]) != want.location {
			t.Errorf("destination %d: place %s, country %s, location %s; want %s, %s, %s",
				i, d["place"], d["country"], d["location"], want.place, want.country, want.location)
		}
	}
}
