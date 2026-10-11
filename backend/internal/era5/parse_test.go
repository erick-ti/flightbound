package era5

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestParseSavedMembers also checks that FuzzParseERA5's seeds parse.
func TestParseSavedMembers(t *testing.T) {
	first := time.Date(1991, 1, 6, 0, 0, 0, 0, time.UTC)
	t2m, err := parseMember(bytes.NewReader(readFixture(t, "era5-land-lisbon-t2m.csv")))
	if err != nil {
		t.Fatal(err)
	}
	if t2m.variable != "t2m" || t2m.latTenths != 387 || t2m.lonTenths != -91 || len(t2m.values) != 48 || !t2m.first.Equal(first) {
		t.Errorf("t2m member: %s at %d, %d tenths, %d values from %v", t2m.variable, t2m.latTenths, t2m.lonTenths, len(t2m.values), t2m.first)
	}
	if t2m.values[0] != 285.07925 || t2m.values[47] != 284.50476 {
		t.Errorf("t2m values %v ... %v", t2m.values[0], t2m.values[47])
	}
	tp, err := parseMember(bytes.NewReader(readFixture(t, "era5-land-lisbon-tp.csv")))
	if err != nil {
		t.Fatal(err)
	}
	if tp.variable != "tp" || tp.latTenths != 387 || tp.lonTenths != -91 || len(tp.values) != 48 || !tp.first.Equal(first) {
		t.Errorf("tp member: %s at %d, %d tenths, %d values from %v", tp.variable, tp.latTenths, tp.lonTenths, len(tp.values), tp.first)
	}
	// The first value, -7.948984e-09 m, is rounding residue and counts as
	// none; 1991-01-07 14:00 is the wettest hour.
	if tp.values[0] != 0 || tp.values[38] != 0.0017636367 || tp.values[3] != 2.7567148e-07 {
		t.Errorf("tp values %v, %v, %v", tp.values[0], tp.values[3], tp.values[38])
	}
}

// Three rows of each file in the downloaded layout.
const (
	sampleT2M = "valid_time,t2m,latitude,longitude\n" +
		"1991-01-06 00:00:00,285.07925,38.70000000000112,-9.099999999988768\n" +
		"1991-01-06 01:00:00,285.05774,38.70000000000112,-9.099999999988768\n" +
		"1991-01-06 02:00:00,284.94308,38.70000000000112,-9.099999999988768\n"
	sampleTP = "valid_time,tp,latitude,longitude\n" +
		"1991-01-06 00:00:00,-7.948984e-09,38.70000000000112,-9.099999999988768\n" +
		"1991-01-06 01:00:00,0.0,38.70000000000112,-9.099999999988768\n" +
		"1991-01-06 02:00:00,2.7567148e-07,38.70000000000112,-9.099999999988768\n"
)

func TestParseMemberSamples(t *testing.T) {
	for name, tt := range map[string]struct {
		doc           string
		lat, lon      int
		variable      string
		second, third float64
	}{
		"temperature":              {sampleT2M, 387, -91, "t2m", 285.05774, 284.94308},
		"precipitation":            {sampleTP, 387, -91, "tp", 0, 2.7567148e-07},
		"without the last newline": {strings.TrimSuffix(sampleT2M, "\n"), 387, -91, "t2m", 285.05774, 284.94308},
		"exact coordinates":        {strings.ReplaceAll(sampleT2M, "38.70000000000112,-9.099999999988768", "38.7,-9.1"), 387, -91, "t2m", 285.05774, 284.94308},
		"east and south":           {strings.ReplaceAll(sampleT2M, "38.70000000000112,-9.099999999988768", "-33.9,151.2"), -339, 1512, "t2m", 285.05774, 284.94308},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := parseMember(strings.NewReader(tt.doc))
			if err != nil {
				t.Fatal(err)
			}
			if m.variable != tt.variable || m.latTenths != tt.lat || m.lonTenths != tt.lon || len(m.values) != 3 ||
				m.values[1] != tt.second || m.values[2] != tt.third {
				t.Errorf("parsed %+v", m)
			}
		})
	}
}

func TestParseMemberRefusesBadFiles(t *testing.T) {
	replace := func(doc, old, new string) string {
		if !strings.Contains(doc, old) {
			t.Fatalf("sample lacks %q", old)
		}
		return strings.Replace(doc, old, new, 1)
	}
	replaceAll := func(doc, old, new string) string {
		if !strings.Contains(doc, old) {
			t.Fatalf("sample lacks %q", old)
		}
		return strings.ReplaceAll(doc, old, new)
	}
	const second = "1991-01-06 01:00:00"
	const third = "1991-01-06 02:00:00"
	s := sampleT2M
	tests := map[string]string{
		"nothing":                   "",
		"only the header":           "valid_time,t2m,latitude,longitude\n",
		"the single-file header":    replace(s, "valid_time,t2m,latitude,longitude", "valid_time,t2m,tp,latitude,longitude"),
		"columns in another order":  replace(s, "valid_time,t2m,latitude,", "valid_time,latitude,t2m,"),
		"an unknown variable":       replace(s, "valid_time,t2m,", "valid_time,d2m,"),
		"an extra column":           replace(s, "latitude,longitude\n", "latitude,longitude,sp\n"),
		"an extra field":            replace(s, "284.94308,38.70000000000112,-9.099999999988768\n", "284.94308,38.70000000000112,-9.099999999988768,1\n"),
		"a missing field":           replace(s, "284.94308,38.70000000000112,", "38.70000000000112,"),
		"a gap":                     replace(s, third, "1991-01-06 03:00:00"),
		"a repeated hour":           replace(s, third, second),
		"hours out of order":        replace(s, second, "1991-01-05 23:00:00"),
		"times between hours":       replaceAll(s, ":00:00,", ":30:00,"),
		"a one-digit hour":          replace(s, second, "1991-01-06 1:00:00"),
		"fractional seconds":        replace(s, second, second+".000"),
		"a time zone":               replace(s, second, "1991-01-06T01:00:00Z"),
		"a day that does not exist": replace(s, third, "1991-02-30 02:00:00"),
		"an empty value":            replace(s, "285.05774", ""),
		"NaN":                       replace(s, "285.05774", "NaN"),
		"infinity":                  replace(s, "285.05774", "+Inf"),
		"a plus sign":               replace(s, "285.05774", "+285.05774"),
		"no digits after the point": replace(s, "285.05774", "285."),
		"hexadecimal":               replace(s, "285.05774", "0x1.1cp8"),
		"a digit separator":         replace(s, "285.05774", "285.057_74"),
		"a quoted number":           replace(s, "285.05774", `"285.05774"`),
		"a space":                   replace(s, ",285.05774,", ", 285.05774,"),
		"too cold":                  replace(s, "285.05774", "169.9"),
		"too hot":                   replace(s, "285.05774", "340.1"),
		"a huge exponent":           replace(sampleTP, "2.7567148e-07", "1e999"),
		"precipitation below zero":  replace(sampleTP, "2.7567148e-07", "-2e-06"),
		"too much precipitation":    replace(sampleTP, "2.7567148e-07", "0.51"),
		"an empty precipitation":    replace(sampleTP, "2.7567148e-07", ""),
		"a second grid point":       replace(s, "284.94308,38.70000000000112,", "284.94308,38.8,"),
		"a latitude off the grid":   replaceAll(s, "38.70000000000112,", "38.75,"),
		"a longitude off the grid":  replaceAll(s, ",-9.099999999988768\n", ",-9.15\n"),
		"a latitude off the globe":  replaceAll(s, "38.70000000000112,", "90.1,"),
		"a longitude off the globe": replaceAll(s, ",-9.099999999988768\n", ",180.1\n"),
		"Windows line endings":      replaceAll(s, "\n", "\r\n"),
		"a blank line":              replace(s, "\n"+second, "\n\n"+second),
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			m, err := parseMember(strings.NewReader(doc))
			if err == nil {
				t.Errorf("no error; parsed %+v", m)
			}
			t.Log(err)
		})
	}
}

func TestParseMemberErrorsNameTheLineAndTheSea(t *testing.T) {
	doc := strings.Replace(sampleT2M, "1991-01-06 02:00:00", "1991-01-06 03:00:00", 1)
	if _, err := parseMember(strings.NewReader(doc)); err == nil || !strings.Contains(err.Error(), "line 4") {
		t.Errorf("error = %v, want one naming line 4", err)
	}
	// A grid point over the ocean comes back with every value empty.
	sea := strings.ReplaceAll(sampleT2M, "285.07925", "")
	if _, err := parseMember(strings.NewReader(sea)); err == nil || !strings.Contains(err.Error(), "sea") {
		t.Errorf("error = %v, want one saying the grid point is sea", err)
	}
}

// FuzzParseERA5 checks what parseMember accepts: at least one value, hours
// one after another, plausible values with no precipitation below zero,
// and a grid point on the globe.
func FuzzParseERA5(f *testing.F) {
	f.Add(readFixture(f, "era5-land-lisbon-t2m.csv"))
	f.Add(readFixture(f, "era5-land-lisbon-tp.csv"))
	f.Add([]byte(sampleT2M))
	f.Add([]byte(sampleTP))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := parseMember(bytes.NewReader(data))
		if err != nil {
			return
		}
		if len(m.values) == 0 {
			t.Fatal("accepted no values")
		}
		if !m.first.Truncate(time.Hour).Equal(m.first) || m.first.Location() != time.UTC {
			t.Fatalf("first hour %v", m.first)
		}
		if m.latTenths < -900 || m.latTenths > 900 || m.lonTenths < -1800 || m.lonTenths > 1800 {
			t.Fatalf("grid point %d, %d tenths", m.latTenths, m.lonTenths)
		}
		lo, hi := 170.0, 340.0
		switch m.variable {
		case "t2m":
		case "tp":
			lo, hi = 0, 0.5
		default:
			t.Fatalf("variable %q", m.variable)
		}
		for i, v := range m.values {
			if !(v >= lo && v <= hi) {
				t.Fatalf("value %d is %v", i, v)
			}
		}
	})
}
