package tripcomparison

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

const sampleBody = `{
  "check_in": "2027-03-10",
  "check_out": "2027-03-13",
  "travelers": 2,
  "currency": "USD",
  "destinations": [
    {"label": "Lisbon", "flight_estimate": "600.10", "nightly_stay_estimate": "125.25"},
    {"label": "Porto", "flight_estimate": null, "nightly_stay_estimate": "90"}
  ]
}`

func post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/trip-comparisons", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	Handler(fixtureRates(t)).ServeHTTP(rec, req)
	return rec
}

func checkJSONHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
}

type errorBody struct {
	Message     string       `json:"message"`
	FieldErrors []FieldError `json:"field_errors"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not JSON: %v: %s", err, rec.Body)
	}
	if body.Message == "" {
		t.Errorf("error body has no message: %s", rec.Body)
	}
	return body
}

func TestHandlerReturnsComparison(t *testing.T) {
	// A null amount currency means the comparison currency.
	withNullCurrencies := strings.Replace(sampleBody, `"flight_estimate": null,`, `"flight_estimate": null, "flight_estimate_currency": null, "nightly_stay_estimate_currency": null,`, 1)
	for _, body := range []string{sampleBody, sampleBody + "\n  \t", withNullCurrencies} {
		rec := post(t, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		checkJSONHeaders(t, rec)

		// Decode raw values to pin the wire format: amounts are strings and
		// unknown values are null rather than omitted or zero.
		var got struct {
			Nights        int                          `json:"nights"`
			AllComplete   bool                         `json:"all_complete"`
			ExchangeRates json.RawMessage              `json:"exchange_rates"`
			Destinations  []map[string]json.RawMessage `json:"destinations"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("response is not JSON: %v", err)
		}
		if got.Nights != 3 || got.AllComplete || len(got.Destinations) != 2 {
			t.Fatalf("nights %d, all complete %v, %d destinations", got.Nights, got.AllComplete, len(got.Destinations))
		}
		if string(got.ExchangeRates) != "null" {
			t.Errorf("exchange_rates = %s, want null when nothing is converted", got.ExchangeRates)
		}
		lisbon, porto := got.Destinations[0], got.Destinations[1]
		want := map[string]map[string]string{
			"Lisbon": {
				"flight_estimate": `"600.10"`, "flight_estimate_currency": `"USD"`,
				"nightly_stay_estimate_currency": `"USD"`, "stay_estimate": `"375.75"`,
				"converted_flight_estimate": `"600.10"`, "converted_stay_estimate": `"375.75"`,
				"flight_and_stay_estimate": `"975.85"`, "complete": "true", "lowest_estimate": "false",
			},
			"Porto": {
				"flight_estimate": "null", "flight_estimate_currency": `"USD"`, "nightly_stay_estimate": `"90.00"`,
				"stay_estimate": `"270.00"`, "converted_flight_estimate": "null", "converted_stay_estimate": `"270.00"`,
				"flight_and_stay_estimate": "null", "complete": "false", "lowest_estimate": "false",
			},
		}
		for name, d := range map[string]map[string]json.RawMessage{"Lisbon": lisbon, "Porto": porto} {
			for key, value := range want[name] {
				raw, ok := d[key]
				if !ok || string(raw) != value {
					t.Errorf("%s %s = %s (present %v), want %s", name, key, raw, ok, value)
				}
			}
		}
	}
}

func TestHandlerReturnsConversions(t *testing.T) {
	body := strings.Replace(sampleBody, `"nightly_stay_estimate": "125.25"`, `"nightly_stay_estimate": "110", "nightly_stay_estimate_currency": "EUR"`, 1)
	rec := post(t, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got struct {
		ExchangeRates json.RawMessage              `json:"exchange_rates"`
		Destinations  []map[string]json.RawMessage `json:"destinations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if want := `{"available":true,"date":"2026-10-09","per_euro":{"USD":"1.1206"}}`; string(got.ExchangeRates) != want {
		t.Errorf("exchange_rates = %s, want %s", got.ExchangeRates, want)
	}
	lisbon := got.Destinations[0]
	for key, value := range map[string]string{
		"nightly_stay_estimate": `"110.00"`, "nightly_stay_estimate_currency": `"EUR"`, "stay_estimate": `"330.00"`,
		"converted_stay_estimate": `"369.80"`, "flight_and_stay_estimate": `"969.90"`,
	} {
		if raw, ok := lisbon[key]; !ok || string(raw) != value {
			t.Errorf("Lisbon %s = %s (present %v), want %s", key, raw, ok, value)
		}
	}
}

func TestHandlerReportsFieldErrors(t *testing.T) {
	body := strings.Replace(sampleBody, `"check_out": "2027-03-13"`, `"check_out": "2027-03-09"`, 1)
	body = strings.Replace(body, `"600.10"`, `"12.345"`, 1)
	rec := post(t, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	checkJSONHeaders(t, rec)
	got := decodeError(t, rec)
	var fields []string
	for _, e := range got.FieldErrors {
		fields = append(fields, e.Field)
		if e.Message == "" {
			t.Errorf("field %s has no message", e.Field)
		}
	}
	if want := []string{"check_out", "destinations[0].flight_estimate"}; !slices.Equal(fields, want) {
		t.Errorf("fields = %v, want %v", fields, want)
	}
}

func TestHandlerRejectsMalformedBodies(t *testing.T) {
	tests := []struct {
		name, body string
	}{
		{"empty", ""},
		{"not JSON", "{"},
		{"array", "[]"},
		{"null", "null"},
		{"string", `"trip"`},
		{"unknown field", strings.Replace(sampleBody, `"travelers": 2`, `"travelers": 2, "adults": 2`, 1)},
		{"number amount", strings.Replace(sampleBody, `"600.10"`, `600.10`, 1)},
		{"fractional travelers", strings.Replace(sampleBody, `"travelers": 2`, `"travelers": 2.5`, 1)},
		{"trailing object", sampleBody + "{}"},
		{"trailing text", sampleBody + " x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(t, tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			checkJSONHeaders(t, rec)
			if got := decodeError(t, rec); len(got.FieldErrors) != 0 {
				t.Errorf("unexpected field errors: %+v", got.FieldErrors)
			}
		})
	}
}

func TestHandlerRejectsOversizedBody(t *testing.T) {
	body := strings.Replace(sampleBody, `"Lisbon"`, `"`+strings.Repeat("a", maxBodyBytes)+`"`, 1)
	rec := post(t, body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	checkJSONHeaders(t, rec)
	decodeError(t, rec)
}
