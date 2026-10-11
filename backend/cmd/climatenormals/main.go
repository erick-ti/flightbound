// Command climatenormals builds the climate normals that the API serves from
// ERA5-Land hourly downloads made with scripts/climate/download_era5.py. For
// each supported place with a download, <place ID>.zip in -in, it checks
// that the download is for the place's climate point (its nearest mostly
// land grid point, saved in places.json), summarizes the years 1991 to 2020
// into monthly normals, and records them with the download's SHA-256. It
// writes -out only when every download converts.
//
//	cd backend && go run ./cmd/climatenormals -in ~/era5
//
// It reads the place list but not the normals it replaces, so it can always
// rebuild them.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/erick-ti/flightbound/backend/internal/era5"
	"github.com/erick-ti/flightbound/backend/internal/places"
)

const (
	dataset = "reanalysis-era5-land-timeseries"
	// attribution follows ECMWF's guidance for CC BY data; the year is the
	// year of the downloads.
	attribution = "The results contain modified Copernicus Climate Change Service information 2026."
	// maxDownload caps a download's size; one place's download is about
	// 3 MB.
	maxDownload = 64 << 20
)

var (
	variables = []string{"2m_temperature", "total_precipitation"}
	period    = era5.Period{First: 1991, Last: 2020}
)

func main() {
	in := flag.String("in", "", "directory of downloads named <place ID>.zip")
	out := flag.String("out", "internal/climate/normals.json", "normals file to write")
	flag.Parse()
	if *in == "" || flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: climatenormals -in DIR [-out FILE]")
		os.Exit(2)
	}
	if err := run(*in, *out, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "climatenormals:", err)
		os.Exit(1)
	}
}

// run converts the downloads in dir and writes the normals to out,
// reporting each place to log.
func run(dir, out string, log io.Writer) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var ids []string
	for _, e := range entries {
		name := e.Name()
		// Skip requests.log, unfinished .part files and anything else.
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".zip") {
			continue
		}
		id := strings.TrimSuffix(name, ".zip")
		if _, ok := places.Lookup(id); !ok {
			return fmt.Errorf("%s: no supported place has the ID %q", name, id)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return fmt.Errorf("%s holds no downloads", dir)
	}
	slices.Sort(ids)

	doc := era5.Normals{Dataset: dataset, Variables: variables, Period: period, Attribution: attribution}
	for _, id := range ids {
		p, _ := places.Lookup(id)
		n, err := convert(filepath.Join(dir, id+".zip"), p)
		if err != nil {
			return fmt.Errorf("%s.zip: %w", id, err)
		}
		doc.Places = append(doc.Places, n)
		fmt.Fprintf(log, "%s: grid point %v, %v; local solar time UTC%+d\n", id, n.GridLatitude, n.GridLongitude, n.SolarOffsetHours)
	}
	var missing []string
	for _, p := range places.All() {
		if !slices.Contains(ids, p.ID) {
			missing = append(missing, p.ID)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		fmt.Fprintf(log, "no download for %d places: %s\n", len(missing), strings.Join(missing, ", "))
	}

	data := era5.MarshalNormals(doc)
	// Write only what reads back.
	if _, err := era5.UnmarshalNormals(data); err != nil {
		return fmt.Errorf("the normals do not read back: %w", err)
	}
	return writeFile(out, data)
}

// convert reads one place's download and summarizes it.
func convert(path string, p places.Place) (era5.PlaceNormals, error) {
	data, err := readFile(path, maxDownload)
	if err != nil {
		return era5.PlaceNormals{}, err
	}
	s, err := era5.ParseArchive(data)
	if err != nil {
		return era5.PlaceNormals{}, err
	}
	if err := checkPoint(s.Latitude, s.Longitude, p); err != nil {
		return era5.PlaceNormals{}, err
	}
	months, err := era5.Summarize(s, p.Longitude, period)
	if err != nil {
		return era5.PlaceNormals{}, err
	}
	sum := sha256.Sum256(data)
	return era5.PlaceNormals{
		ID:               p.ID,
		GridLatitude:     s.Latitude,
		GridLongitude:    s.Longitude,
		SolarOffsetHours: era5.SolarOffset(p.Longitude),
		SourceSHA256:     hex.EncodeToString(sum[:]),
		Months:           months,
	}, nil
}

// checkPoint fails unless the grid point at lat, lon is the place's climate
// point. Both lie on the 0.1 degree grid, so they are compared in tenths.
func checkPoint(lat, lon float64, p places.Place) error {
	if math.Round(lat*10) != math.Round(p.ClimateLatitude*10) || math.Round(lon*10) != math.Round(p.ClimateLongitude*10) {
		return fmt.Errorf("grid point %v, %v is not %s's climate point %v, %v", lat, lon, p.Name, p.ClimateLatitude, p.ClimateLongitude)
	}
	return nil
}

// readFile reads a file of at most limit bytes.
func readFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	return data, nil
}

// writeFile replaces path with data through a temporary file in the same
// directory, so that path holds either the old or the new contents.
func writeFile(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(tmp.Name())
		}
	}()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	return nil
}
