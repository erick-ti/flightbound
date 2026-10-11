package traveladvice

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var portugalUpdated = time.Date(2026, 8, 11, 9, 21, 44, 0, time.UTC)

// variant returns the saved Portugal page with change applied to its decoded
// JSON, so derived cases keep every other field of a real response.
func variant(t testing.TB, change func(page map[string]any)) []byte {
	t.Helper()
	var page map[string]any
	if err := json.Unmarshal(readPage(t, "portugal"), &page); err != nil {
		t.Fatal(err)
	}
	change(page)
	body, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func details(page map[string]any) map[string]any { return page["details"].(map[string]any) }

func country(page map[string]any) map[string]any { return details(page)["country"].(map[string]any) }

// readPage returns a saved GOV.UK content item from the test data.
func readPage(t testing.TB, slug string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", slug+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestParseSavedPages(t *testing.T) {
	tests := []struct {
		slug    string
		alerts  []Alert
		updated time.Time
	}{
		{"portugal", []Alert{}, time.Date(2026, 8, 11, 9, 21, 44, 0, time.UTC)},
		{"thailand", []Alert{AvoidAllButEssentialTravelToParts}, time.Date(2026, 9, 28, 5, 10, 2, 0, time.UTC)},
		{"afghanistan", []Alert{AvoidAllTravelToWholeCountry}, time.Date(2026, 7, 30, 10, 36, 13, 0, time.UTC)},
		{"ukraine", []Alert{AvoidAllTravelToParts, AvoidAllButEssentialTravelToParts}, time.Date(2026, 10, 2, 8, 30, 25, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			got, err := Parse(tt.slug, readPage(t, tt.slug))
			if err != nil {
				t.Fatal(err)
			}
			if got.Alerts == nil || !slices.Equal(got.Alerts, tt.alerts) {
				t.Errorf("alerts = %#v, want %#v", got.Alerts, tt.alerts)
			}
			if !got.UpdatedAt.Equal(tt.updated) || got.UpdatedAt.Location() != time.UTC {
				t.Errorf("updated = %s, want %s", got.UpdatedAt, tt.updated)
			}
			if !got.CheckedAt.IsZero() {
				t.Errorf("checked = %s, want zero for a parsed page", got.CheckedAt)
			}
		})
	}
}

func TestParseRejectsUnexpectedContent(t *testing.T) {
	tests := []struct {
		name   string
		change func(p map[string]any)
	}{
		{"another document type", func(p map[string]any) { p["document_type"] = "guide" }},
		{"another schema", func(p map[string]any) { p["schema_name"] = "guide" }},
		// The path and the country each name the page; both must match.
		{"another country's path", func(p map[string]any) { p["base_path"] = "/foreign-travel-advice/spain" }},
		{"another country in the details", func(p map[string]any) { country(p)["slug"] = "spain" }},
		{"another country in both", func(p map[string]any) {
			p["base_path"] = "/foreign-travel-advice/spain"
			country(p)["slug"] = "spain"
		}},
		{"no country", func(p map[string]any) { delete(details(p), "country") }},
		{"withdrawn", func(p map[string]any) {
			p["withdrawn_notice"] = map[string]any{"explanation": "This advice has been withdrawn.", "withdrawn_at": "2026-10-01T00:00:00Z"}
		}},
		{"withdrawn notice that is not an object", func(p map[string]any) { p["withdrawn_notice"] = "withdrawn" }},
		{"no alert status", func(p map[string]any) { delete(details(p), "alert_status") }},
		{"null alert status", func(p map[string]any) { details(p)["alert_status"] = nil }},
		{"alert status that is not a list", func(p map[string]any) { details(p)["alert_status"] = "avoid_all_travel_to_parts" }},
		{"unknown alert", func(p map[string]any) { details(p)["alert_status"] = []any{"avoid_all_travel_to_the_coast"} }},
		{"known and unknown alerts", func(p map[string]any) {
			details(p)["alert_status"] = []any{"avoid_all_travel_to_parts", "AVOID_ALL_TRAVEL_TO_PARTS"}
		}},
		{"null alert", func(p map[string]any) { details(p)["alert_status"] = []any{nil} }},
		{"numeric alert", func(p map[string]any) { details(p)["alert_status"] = []any{1} }},
		{"no update time", func(p map[string]any) { delete(p, "public_updated_at") }},
		{"update time without a zone", func(p map[string]any) { p["public_updated_at"] = "2026-08-11T10:21:44" }},
		{"update date only", func(p map[string]any) { p["public_updated_at"] = "2026-08-11" }},
		{"zero update time", func(p map[string]any) { p["public_updated_at"] = "0001-01-01T00:00:00Z" }},
		{"offset of 24 hours", func(p map[string]any) { p["public_updated_at"] = "2026-08-11T10:21:44+24:00" }},
		{"offset of 60 minutes", func(p map[string]any) { p["public_updated_at"] = "2026-08-11T10:21:44+00:60" }},
		{"one-digit hour", func(p map[string]any) { p["public_updated_at"] = "2026-08-11T1:21:44+01:00" }},
		{"lowercase separators", func(p map[string]any) { p["public_updated_at"] = "2026-08-11t09:21:44z" }},
		{"no seconds", func(p map[string]any) { p["public_updated_at"] = "2026-08-11T10:21+01:00" }},
		{"ten fraction digits", func(p map[string]any) { p["public_updated_at"] = "2026-08-11T09:21:44.1234567890Z" }},
		{"padded update time", func(p map[string]any) { p["public_updated_at"] = " 2026-08-11T09:21:44Z" }},
		// Field names are matched exactly; a differently cased copy must not
		// stand in for the real field.
		{"case variant of the alert status", func(p map[string]any) {
			delete(details(p), "alert_status")
			details(p)["Alert_Status"] = []any{}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := Parse("portugal", variant(t, tt.change)); err == nil {
				t.Errorf("parsed as %+v, want an error", got)
			}
		})
	}

	portugal := readPage(t, "portugal")
	// replace changes the one occurrence of old in the saved page, to build
	// JSON that a decoded map cannot express: repeated keys, or a key placed
	// after another that differs only in case.
	replace := func(old, new string) []byte {
		t.Helper()
		if n := bytes.Count(portugal, []byte(old)); n != 1 {
			t.Fatalf("%q appears %d times in the saved page, want 1", old, n)
		}
		return bytes.Replace(portugal, []byte(old), []byte(new), 1)
	}
	// The page's own path follows analytics_identifier; links repeat it.
	const path = `"analytics_identifier":null,"base_path":"/foreign-travel-advice/portugal"`
	const slug = `"country":{"name":"Portugal","slug":"portugal"`
	bodies := []struct {
		name string
		body []byte
	}{
		{"empty", nil},
		{"maintenance page", []byte("<html><body>Maintenance</body></html>")},
		{"null", []byte("null")},
		{"list", []byte("[]")},
		{"data after the page", append(bytes.Clone(portugal), []byte(" {}")...)},
		{"larger than 1 MiB", variant(t, func(p map[string]any) { p["padding"] = strings.Repeat("x", maxBodyBytes) })},
		// A repeated key must not let a later copy replace or patch an
		// earlier one.
		{"repeated alert status", replace(`"alert_status":[]`, `"alert_status":["avoid_all_travel_to_parts"],"alert_status":[null]`)},
		{"repeated path", replace(path, `"analytics_identifier":null,"base_path":"/foreign-travel-advice/spain","base_path":"/foreign-travel-advice/portugal"`)},
		{"case variant of the path after it", replace(path, `"analytics_identifier":null,"base_path":"/foreign-travel-advice/spain","BASE_PATH":"/foreign-travel-advice/portugal"`)},
		{"case variant of the country after it", replace(slug, `"country":{"name":"Portugal","slug":"spain","SLUG":"portugal"`)},
	}
	for _, tt := range bodies {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := Parse("portugal", tt.body); err == nil {
				t.Errorf("parsed as %+v, want an error", got)
			}
		})
	}
	if _, err := Parse("spain", portugal); err == nil {
		t.Error("parsed Portugal's page as Spain's")
	}
}

func TestParseAcceptsVariants(t *testing.T) {
	alerts := func(values ...string) func(p map[string]any) {
		return func(p map[string]any) {
			list := make([]any, len(values))
			for i, v := range values {
				list[i] = v
			}
			details(p)["alert_status"] = list
		}
	}
	tests := []struct {
		name    string
		change  func(p map[string]any)
		alerts  []Alert
		updated time.Time
	}{
		{
			"all but essential travel to the whole country",
			alerts("avoid_all_but_essential_travel_to_whole_country"),
			[]Alert{AvoidAllButEssentialTravelToWholeCountry},
			portugalUpdated,
		},
		{
			"alerts out of order and repeated",
			alerts("avoid_all_but_essential_travel_to_parts", "avoid_all_travel_to_whole_country", "avoid_all_but_essential_travel_to_parts"),
			[]Alert{AvoidAllTravelToWholeCountry, AvoidAllButEssentialTravelToParts},
			portugalUpdated,
		},
		{
			"every alert, least severe first",
			alerts("avoid_all_but_essential_travel_to_parts", "avoid_all_but_essential_travel_to_whole_country", "avoid_all_travel_to_parts", "avoid_all_travel_to_whole_country"),
			[]Alert{AvoidAllTravelToWholeCountry, AvoidAllTravelToParts, AvoidAllButEssentialTravelToWholeCountry, AvoidAllButEssentialTravelToParts},
			portugalUpdated,
		},
		{
			"winter time",
			func(p map[string]any) { p["public_updated_at"] = "2026-12-01T09:00:00+00:00" },
			[]Alert{},
			time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC),
		},
		{
			"fractional seconds",
			func(p map[string]any) { p["public_updated_at"] = "2026-08-11T10:21:44.5+01:00" },
			[]Alert{},
			portugalUpdated.Add(500 * time.Millisecond),
		},
		{"no withdrawn notice", func(p map[string]any) { delete(p, "withdrawn_notice") }, []Alert{}, portugalUpdated},
		{"null withdrawn notice", func(p map[string]any) { p["withdrawn_notice"] = nil }, []Alert{}, portugalUpdated},
		{
			"fields GOV.UK may add",
			func(p map[string]any) {
				p["new_field"] = map[string]any{"x": 1}
				details(p)["new_detail"] = []any{"y"}
				country(p)["region"] = "Europe"
			},
			[]Alert{},
			portugalUpdated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse("portugal", variant(t, tt.change))
			if err != nil {
				t.Fatal(err)
			}
			if got.Alerts == nil || !slices.Equal(got.Alerts, tt.alerts) {
				t.Errorf("alerts = %#v, want %#v", got.Alerts, tt.alerts)
			}
			if !got.UpdatedAt.Equal(tt.updated) || got.UpdatedAt.Location() != time.UTC {
				t.Errorf("updated = %s, want %s", got.UpdatedAt, tt.updated)
			}
		})
	}
}
