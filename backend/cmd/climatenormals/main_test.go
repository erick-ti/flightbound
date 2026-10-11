package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/era5"
	"github.com/erick-ti/flightbound/backend/internal/places"
)

var (
	firstHour = time.Date(1990, 12, 31, 0, 0, 0, 0, time.UTC)
	lastHour  = time.Date(2021, 1, 1, 23, 0, 0, 0, time.UTC)
)

// buildDownload makes a download as the Climate Data Store sends it, a zip
// archive with a 2 m temperature file and a precipitation file, for a grid
// point: every hour from first to last at the temperature given (empty for
// a sea point) with 0.1 mm of precipitation.
func buildDownload(lat, lon, t2m string, first, last time.Time) []byte {
	var temperature, precipitation strings.Builder
	temperature.WriteString("valid_time,t2m,latitude,longitude\n")
	precipitation.WriteString("valid_time,tp,latitude,longitude\n")
	tp := "0.0001"
	if t2m == "" {
		tp = ""
	}
	for h := first; !h.After(last); h = h.Add(time.Hour) {
		at := h.Format("2006-01-02 15:04:05")
		fmt.Fprintf(&temperature, "%s,%s,%s,%s\n", at, t2m, lat, lon)
		fmt.Fprintf(&precipitation, "%s,%s,%s,%s\n", at, tp, lat, lon)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{
		{"reanalysis-era5-land-timeseries-sfc-2m-temperature-test.csv", temperature.String()},
		{"reanalysis-era5-land-timeseries-sfc-pressure-precipitation-test.csv", precipitation.String()},
	} {
		w2, err := w.Create(f.name)
		if err == nil {
			_, err = io.WriteString(w2, f.body)
		}
		if err != nil {
			panic(err)
		}
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// tokyo is a whole download at Tokyo's climate point.
var tokyo = sync.OnceValue(func() []byte { return buildDownload("35.7", "139.7", "283.15", firstHour, lastHour) })

func dirWith(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunWritesNormals(t *testing.T) {
	in := dirWith(t, map[string][]byte{
		"tokyo.zip":        tokyo(),
		"requests.log":     []byte("2026-10-11T17:43:10Z tokyo\n"),
		".lisbon.zip.part": []byte("unfinished"),
	})
	out := filepath.Join(t.TempDir(), "normals.json")
	var log bytes.Buffer
	if err := run(in, out, &log); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// The same downloads give the same file.
	again := filepath.Join(t.TempDir(), "normals.json")
	if err := run(in, again, io.Discard); err != nil {
		t.Fatal(err)
	}
	if data2, _ := os.ReadFile(again); !bytes.Equal(data, data2) {
		t.Errorf("a second run wrote different bytes:\n%s\n%s", data, data2)
	}

	n, err := era5.UnmarshalNormals(data)
	if err != nil {
		t.Fatal(err)
	}
	if n.Dataset != "reanalysis-era5-land-timeseries" || n.Period != (era5.Period{First: 1991, Last: 2020}) ||
		!slices.Equal(n.Variables, []string{"2m_temperature", "total_precipitation"}) ||
		!strings.Contains(n.Attribution, "Copernicus Climate Change Service information") {
		t.Errorf("header %+v", n)
	}
	if len(n.Places) != 1 {
		t.Fatalf("%d places, want 1", len(n.Places))
	}
	p := n.Places[0]
	sum := sha256.Sum256(tokyo())
	if p.ID != "tokyo" || p.GridLatitude != 35.7 || p.GridLongitude != 139.7 || p.SolarOffsetHours != 9 ||
		p.SourceSHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("place %+v", p)
	}
	for _, m := range p.Months {
		if m.HighTenthsC != 100 || m.LowTenthsC != 100 {
			t.Errorf("month %d: high %d, low %d; want 100", m.Month, m.HighTenthsC, m.LowTenthsC)
		}
	}
	// 2.4 mm a day: 31 days in January, and 28 or 29 in February.
	if jan, feb := p.Months[0].PrecipitationTenthsMM, p.Months[1].PrecipitationTenthsMM; jan != 744 || feb != 678 {
		t.Errorf("precipitation %d and %d tenths of a mm, want 744 and 678", jan, feb)
	}
	if want := fmt.Sprintf("no download for %d places", len(places.All())-1); !strings.Contains(log.String(), want) {
		t.Errorf("log does not report the missing places:\n%s", log.String())
	}
}

func TestRunWritesNothingOnFailure(t *testing.T) {
	tests := map[string]map[string][]byte{
		"no downloads":       {"requests.log": []byte("2026-10-11T17:43:10Z tokyo\n")},
		"an unknown place":   {"tokyo.zip": tokyo(), "atlantis.zip": tokyo()},
		"a damaged download": {"tokyo.zip": []byte("not a zip archive")},
		"another grid point": {"tokyo.zip": buildDownload("35.8", "139.7", "283.15", firstHour, lastHour)},
		"a sea point":        {"tokyo.zip": buildDownload("35.7", "139.7", "", firstHour, lastHour)},
		// At UTC+9 the first local day starts at 15:00 UTC on 31 December.
		"too few hours": {"tokyo.zip": buildDownload("35.7", "139.7", "283.15", firstHour.Add(16*time.Hour), lastHour)},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			in := dirWith(t, files)
			outDir := t.TempDir()
			out := filepath.Join(outDir, "normals.json")
			if err := os.WriteFile(out, []byte("previous\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			err := run(in, out, io.Discard)
			if err == nil {
				t.Fatal("no error")
			}
			t.Log(err)
			if data, _ := os.ReadFile(out); string(data) != "previous\n" {
				t.Errorf("output changed to %q", data)
			}
			if entries, _ := os.ReadDir(outDir); len(entries) != 1 {
				t.Errorf("output folder holds %v", entries)
			}
		})
	}
}

func TestCheckPoint(t *testing.T) {
	ny, ok := places.Lookup("new-york") // climate point 40.7, -74.0
	if !ok {
		t.Fatal("no new-york")
	}
	for _, tt := range []struct {
		lat, lon float64
		ok       bool
	}{
		{40.7, -74.0, true},
		{40.8, -74.0, false},
		{40.6, -74.0, false},
		{40.7, -73.9, false},
		{40.7, -74.1, false},
	} {
		if err := checkPoint(tt.lat, tt.lon, ny); (err == nil) != tt.ok {
			t.Errorf("checkPoint(%v, %v) = %v, want ok %v", tt.lat, tt.lon, err, tt.ok)
		}
	}
}
