package era5

import (
	"reflect"
	"strings"
	"testing"
)

func sampleNormals() Normals {
	var months [12]Month
	for i := range months {
		months[i] = Month{Month: i + 1, HighTenthsC: 100 + i, LowTenthsC: -20 + i, PrecipitationTenthsMM: 500 + i}
	}
	return Normals{
		Dataset:     "reanalysis-era5-land-timeseries",
		Variables:   []string{"2m_temperature", "total_precipitation"},
		Period:      Period{First: 1991, Last: 2020},
		Attribution: "Contains modified Copernicus Climate Change Service information 2026.",
		Places: []PlaceNormals{{
			ID:               "tokyo",
			GridLatitude:     35.75,
			GridLongitude:    139.75,
			SolarOffsetHours: 9,
			SourceSHA256:     strings.Repeat("ab", 32),
			Months:           months,
		}},
	}
}

func TestNormalsRoundTrip(t *testing.T) {
	want := sampleNormals()
	data := MarshalNormals(want)
	got, err := UnmarshalNormals(data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
	if again := MarshalNormals(got); string(again) != string(data) {
		t.Errorf("writing twice differs:\n%s\n%s", data, again)
	}
}

func TestMarshalNormalsLayout(t *testing.T) {
	n := sampleNormals()
	n.Places[0].Months = [12]Month{}
	for i := range n.Places[0].Months {
		n.Places[0].Months[i] = Month{Month: i + 1, HighTenthsC: 1, LowTenthsC: -1, PrecipitationTenthsMM: 0}
	}
	got := string(MarshalNormals(n))
	for _, want := range []string{
		"{\n  \"dataset\": \"reanalysis-era5-land-timeseries\",\n",
		"  \"variables\": [\"2m_temperature\", \"total_precipitation\"],\n",
		"  \"period\": {\"first\": 1991, \"last\": 2020},\n",
		"      \"grid_latitude\": 35.75,\n",
		"        {\"month\": 1, \"high_tenths_c\": 1, \"low_tenths_c\": -1, \"precipitation_tenths_mm\": 0},\n",
		"        {\"month\": 12, \"high_tenths_c\": 1, \"low_tenths_c\": -1, \"precipitation_tenths_mm\": 0}\n      ]\n    }\n  ]\n}\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	empty := sampleNormals()
	empty.Places = nil
	if got := string(MarshalNormals(empty)); !strings.HasSuffix(got, "  \"places\": []\n}\n") {
		t.Errorf("no places: %s", got)
	}
	if _, err := UnmarshalNormals(MarshalNormals(empty)); err != nil {
		t.Errorf("no places does not load: %v", err)
	}
}

func TestUnmarshalNormalsRefusesBadDocuments(t *testing.T) {
	good := string(MarshalNormals(sampleNormals()))
	replace := func(old, new string) string {
		if !strings.Contains(good, old) {
			t.Fatalf("sample lacks %q", old)
		}
		return strings.Replace(good, old, new, 1)
	}
	tests := map[string]string{
		"not JSON":                   "<html>",
		"trailing data":              good + "{}",
		"an unknown member":          replace(`"dataset":`, `"source": "x", "dataset":`),
		"an unknown month member":    replace(`"month": 1,`, `"month": 1, "mean_tenths_c": 3,`),
		"a repeated member":          replace(`"id": "tokyo",`, `"id": "tokyo", "id": "lisbon",`),
		"a differently cased member": replace(`"high_tenths_c": 100,`, `"High_tenths_c": 100,`),
		"a missing high":             replace(`"high_tenths_c": 100, `, ``),
		"a missing precipitation":    replace(`, "precipitation_tenths_mm": 500}`, `}`),
		"a missing period":           replace(`"period": {"first": 1991, "last": 2020},`, ``),
		"a null place":               replace(`"places": [`, `"places": [null, `),
		"a null month":               replace(`"months": [`, `"months": [null, `),
		"a fractional temperature":   replace(`"high_tenths_c": 100,`, `"high_tenths_c": 100.5,`),
		"eleven months":              replace(`{"month": 12, "high_tenths_c": 111, "low_tenths_c": -9, "precipitation_tenths_mm": 511}`, ``),
		"months out of order":        replace(`"month": 1,`, `"month": 2,`),
		"a high below the low":       replace(`"high_tenths_c": 100, "low_tenths_c": -20,`, `"high_tenths_c": -30, "low_tenths_c": -20,`),
		"negative precipitation":     replace(`"precipitation_tenths_mm": 500}`, `"precipitation_tenths_mm": -1}`),
		"an impossible temperature":  replace(`"high_tenths_c": 100,`, `"high_tenths_c": 1000,`),
		"a backwards period":         replace(`{"first": 1991, "last": 2020}`, `{"first": 2020, "last": 1991}`),
		"a solar offset past 12":     replace(`"solar_offset_hours": 9,`, `"solar_offset_hours": 13,`),
		"a grid latitude past 90":    replace(`"grid_latitude": 35.75,`, `"grid_latitude": 90.25,`),
		"a short hash":               replace(strings.Repeat("ab", 32), "abcd"),
		"an empty place ID":          replace(`"id": "tokyo",`, `"id": "",`),
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := UnmarshalNormals([]byte(doc)); err == nil {
				t.Error("no error")
			}
		})
	}
	twice := sampleNormals()
	twice.Places = append(twice.Places, twice.Places[0])
	if _, err := UnmarshalNormals(MarshalNormals(twice)); err == nil {
		t.Error("a place listed twice: no error")
	}
}
