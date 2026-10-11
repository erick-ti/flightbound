package traveladvice

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

// Alert is a warning against travel in a country's advice, named as GOV.UK
// names it.
type Alert string

// The alerts FCDO's publishing tool allows. A country can have several, such
// as a warning against all travel to parts of it and another against all but
// essential travel to other parts.
const (
	AvoidAllTravelToWholeCountry             Alert = "avoid_all_travel_to_whole_country"
	AvoidAllTravelToParts                    Alert = "avoid_all_travel_to_parts"
	AvoidAllButEssentialTravelToWholeCountry Alert = "avoid_all_but_essential_travel_to_whole_country"
	AvoidAllButEssentialTravelToParts        Alert = "avoid_all_but_essential_travel_to_parts"
)

// alertOrder lists the alerts most severe first, the order Advice uses.
var alertOrder = []Alert{
	AvoidAllTravelToWholeCountry,
	AvoidAllTravelToParts,
	AvoidAllButEssentialTravelToWholeCountry,
	AvoidAllButEssentialTravelToParts,
}

// maxBodyBytes bounds a content item. The largest country page seen was
// about 123 KB.
const maxBodyBytes = 1 << 20

// rfc3339 matches an RFC 3339 date-time with two-digit fields, uppercase
// separators, and a fraction of at most nanoseconds. time.Parse alone also
// accepts one-digit hours and offsets such as +24:00.
var rfc3339 = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-](\d{2}):(\d{2}))$`)

// Advice is the part of a country's travel advice that a comparison shows.
type Advice struct {
	// Alerts lists the country's alerts most severe first, without
	// duplicates. It is empty, never nil, when FCDO advises against no
	// travel to the country.
	Alerts []Alert
	// UpdatedAt is when FCDO last changed the advice, in UTC.
	UpdatedAt time.Time
	// CheckedAt is when the advice was received from GOV.UK, in UTC. It is
	// zero for advice read from a saved copy.
	CheckedAt time.Time
}

// page holds the fields of a GOV.UK travel advice content item that Parse
// relies on. Other fields are ignored, so GOV.UK can add fields.
type page struct {
	DocumentType    string         `json:"document_type"`
	SchemaName      string         `json:"schema_name"`
	BasePath        string         `json:"base_path"`
	PublicUpdatedAt string         `json:"public_updated_at"`
	WithdrawnNotice map[string]any `json:"withdrawn_notice"`
	Details         struct {
		AlertStatus *[]string `json:"alert_status"`
		Country     struct {
			Slug string `json:"slug"`
		} `json:"country"`
	} `json:"details"`
}

// Parse reads the GOV.UK Content API's travel advice content item for the
// country with the given slug. It fails unless the item is that country's
// current travel advice with alerts it knows, so that a changed or
// unexpected response is never shown as advice.
func Parse(slug string, body []byte) (Advice, error) {
	if len(body) > maxBodyBytes {
		return Advice{}, fmt.Errorf("content item is larger than %d bytes", maxBodyBytes)
	}
	// encoding/json/v2 matches field names exactly and rejects repeated
	// names and data after the value. encoding/json would let a copy of a
	// field in other letter case, or a repeated field, stand in for it.
	var p *page
	if err := json.Unmarshal(body, &p); err != nil {
		return Advice{}, fmt.Errorf("content item is not valid JSON: %w", err)
	}
	if p == nil {
		return Advice{}, errors.New("content item is null")
	}
	switch {
	case p.DocumentType != "travel_advice" || p.SchemaName != "travel_advice":
		return Advice{}, fmt.Errorf("content item is a %q with schema %q, not travel advice", p.DocumentType, p.SchemaName)
	case p.BasePath != "/foreign-travel-advice/"+slug:
		return Advice{}, fmt.Errorf("content item is for %q, not %q", p.BasePath, slug)
	case p.Details.Country.Slug != slug:
		return Advice{}, fmt.Errorf("content item's country is %q, not %q", p.Details.Country.Slug, slug)
	case len(p.WithdrawnNotice) > 0:
		return Advice{}, errors.New("the advice has been withdrawn")
	case p.Details.AlertStatus == nil:
		return Advice{}, errors.New("content item has no alert status")
	}
	alerts, err := sortAlerts(*p.Details.AlertStatus)
	if err != nil {
		return Advice{}, err
	}
	updated, ok := parseTime(p.PublicUpdatedAt)
	if !ok {
		return Advice{}, fmt.Errorf("content item's update time %q is not an RFC 3339 time", p.PublicUpdatedAt)
	}
	return Advice{Alerts: alerts, UpdatedAt: updated}, nil
}

// parseTime parses a strict RFC 3339 date-time and returns it in UTC.
func parseTime(s string) (time.Time, bool) {
	m := rfc3339.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	// The offset's hours and minutes are two digits each, so comparing
	// the strings compares the numbers.
	if m[2] != "Z" && (m[3] > "23" || m[4] > "59") {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil || t.IsZero() {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// sortAlerts returns the alerts most severe first without duplicates. Any
// value it does not know is an error.
func sortAlerts(values []string) ([]Alert, error) {
	present := make(map[Alert]bool, len(alertOrder))
	for _, v := range values {
		a := Alert(v)
		if !slices.Contains(alertOrder, a) {
			return nil, fmt.Errorf("unknown alert status %q", v)
		}
		present[a] = true
	}
	alerts := make([]Alert, 0, len(present))
	for _, a := range alertOrder {
		if present[a] {
			alerts = append(alerts, a)
		}
	}
	return alerts, nil
}
