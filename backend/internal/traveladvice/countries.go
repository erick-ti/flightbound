// Package traveladvice reads the UK Foreign, Commonwealth & Development
// Office (FCDO) travel advice published on GOV.UK: the countries it covers
// and each country's current warnings against travel.
package traveladvice

import (
	_ "embed"
	"encoding/json"
	"slices"
)

// Country is a country or territory in FCDO's travel advice index. Slug is
// the last segment of its advice page's path on GOV.UK.
type Country struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// countriesJSON is a copy of the slug and name of every entry in the GOV.UK
// Content API's travel advice index, in slug order, which is also
// alphabetical order of the names. It is refreshed by hand when FCDO adds or
// renames a country.
//
//go:embed countries.json
var countriesJSON []byte

var countries, bySlug = loadCountries(countriesJSON)

func loadCountries(data []byte) ([]Country, map[string]Country) {
	var list []Country
	if err := json.Unmarshal(data, &list); err != nil {
		panic("traveladvice: countries.json is not a JSON list of countries: " + err.Error())
	}
	m := make(map[string]Country, len(list))
	for _, c := range list {
		m[c.Slug] = c
	}
	return list, m
}

// Countries returns every country with travel advice, in slug order.
func Countries() []Country {
	return slices.Clone(countries)
}

// Lookup returns the country with the given slug.
func Lookup(slug string) (Country, bool) {
	c, ok := bySlug[slug]
	return c, ok
}

// PageURL returns the address of a country's travel advice page on GOV.UK.
func PageURL(slug string) string {
	return "https://www.gov.uk/foreign-travel-advice/" + slug
}
