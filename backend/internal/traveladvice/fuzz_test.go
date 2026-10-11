package traveladvice

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// envelope returns a saved page cut down to the fields Parse reads. Seeds of
// full pages, around 100 KB each, stall the fuzzer while it minimizes inputs
// grown from them; the full pages are covered by the unit tests.
func envelope(t testing.TB, slug string) []byte {
	t.Helper()
	var page map[string]any
	if err := json.Unmarshal(readPage(t, slug), &page); err != nil {
		t.Fatal(err)
	}
	kept := map[string]any{"details": map[string]any{
		"alert_status": details(page)["alert_status"],
		"country":      details(page)["country"],
	}}
	for _, key := range []string{"document_type", "schema_name", "base_path", "public_updated_at", "withdrawn_notice"} {
		kept[key] = page[key]
	}
	body, err := json.Marshal(kept)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// The seeds must parse, or the fuzz target's checks never run on them.
func TestEnvelopesParse(t *testing.T) {
	for _, slug := range []string{"portugal", "thailand", "afghanistan", "ukraine"} {
		full, err := Parse(slug, readPage(t, slug))
		if err != nil {
			t.Fatal(err)
		}
		cut, err := Parse(slug, envelope(t, slug))
		if err != nil || !slices.Equal(cut.Alerts, full.Alerts) || !cut.UpdatedAt.Equal(full.UpdatedAt) {
			t.Errorf("%s: envelope parsed as %+v, %v; want %+v", slug, cut, err, full)
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, slug := range []string{"portugal", "thailand", "afghanistan", "ukraine"} {
		f.Add(slug, envelope(f, slug))
	}
	f.Add("portugal", []byte(`{"document_type":"travel_advice","schema_name":"travel_advice","base_path":"/foreign-travel-advice/portugal",`+
		`"public_updated_at":"2026-08-11T10:21:44+01:00","details":{"alert_status":["avoid_all_but_essential_travel_to_parts",`+
		`"avoid_all_travel_to_parts","avoid_all_travel_to_parts"],"country":{"slug":"portugal"}}}`))
	f.Fuzz(func(t *testing.T, slug string, body []byte) {
		advice, err := Parse(slug, body)
		if err != nil {
			return
		}
		if len(body) > maxBodyBytes {
			t.Fatalf("parsed a %d-byte body", len(body))
		}
		if advice.Alerts == nil {
			t.Fatal("parsed advice has nil alerts")
		}
		for i, a := range advice.Alerts {
			at := slices.Index(alertOrder, a)
			if at < 0 {
				t.Fatalf("unknown alert %q", a)
			}
			if i > 0 && slices.Index(alertOrder, advice.Alerts[i-1]) >= at {
				t.Fatalf("alerts out of order or repeated: %v", advice.Alerts)
			}
		}
		if advice.UpdatedAt.IsZero() || advice.UpdatedAt.Location() != time.UTC {
			t.Fatalf("updated = %v", advice.UpdatedAt)
		}
		if !advice.CheckedAt.IsZero() {
			t.Fatalf("checked = %v, want zero", advice.CheckedAt)
		}
		// Decoding into maps keeps keys exactly as written, unlike decoding
		// into a struct, so this checks the fields Parse must have used.
		var page map[string]any
		if err := json.Unmarshal(body, &page); err != nil {
			t.Fatalf("parsed a body that is not a JSON object: %v", err)
		}
		details, _ := page["details"].(map[string]any)
		country, _ := details["country"].(map[string]any)
		if page["base_path"] != "/foreign-travel-advice/"+slug || country["slug"] != slug {
			t.Fatalf("parsed as %q a page naming %v and %v", slug, page["base_path"], country["slug"])
		}
		if stamp, _ := page["public_updated_at"].(string); !rfc3339.MatchString(stamp) {
			t.Fatalf("parsed an update time %q that is not strict RFC 3339", stamp)
		}
	})
}
