package era5

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// headers maps the first line of each CSV file in a download to the
// variable the file holds.
var headers = map[string]string{
	"valid_time,t2m,latitude,longitude": "t2m",
	"valid_time,tp,latitude,longitude":  "tp",
}

// timeLayout is how valid_time is written, in UTC.
const timeLayout = "2006-01-02 15:04:05"

// minPrecipitation is the lowest hourly precipitation accepted, in metres.
// ERA5-Land's hourly values come from subtracting running totals, which
// leaves rounding residue just below zero (down to about -4e-8 m); it
// counts as no precipitation.
const minPrecipitation = -1e-6

// number is a decimal number, optionally with an exponent, as downloads
// write them. strconv.ParseFloat alone also takes NaN, Inf, hexadecimal and
// digit separators.
var number = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][-+]?[0-9]+)?$`)

// member is one CSV file of a download: one variable's hourly values from
// first, at one grid point given in tenths of a degree.
type member struct {
	variable             string
	first                time.Time
	values               []float64
	latTenths, lonTenths int
}

// parseMember reads one CSV file of an ERA5-Land time-series download: one
// of the headers above, then one row per hour, in order and without gaps,
// with times in UTC, temperatures in kelvin (170 to 340) and precipitation
// in metres (0 to 0.5 in an hour), at one point of the 0.1 degree grid. An
// empty value means the grid point is ocean, which ERA5-Land does not
// cover. Errors name the line.
func parseMember(r io.Reader) (member, error) {
	var m member
	sc := bufio.NewScanner(r)
	sc.Split(lines)
	n := 0
	for sc.Scan() {
		n++
		text := sc.Text()
		if n == 1 {
			v, ok := headers[text]
			if !ok {
				return member{}, fmt.Errorf("line 1: header %q is not a 2 m temperature or a precipitation file", text)
			}
			m.variable = v
			continue
		}
		t, v, lat, lon, err := parseRow(text, m.variable)
		if err != nil {
			return member{}, fmt.Errorf("line %d: %w", n, err)
		}
		if len(m.values) == 0 {
			m.first, m.latTenths, m.lonTenths = t, lat, lon
		} else {
			if lat != m.latTenths || lon != m.lonTenths {
				return member{}, fmt.Errorf("line %d: grid point %s; earlier rows have %s", n, point(lat, lon), point(m.latTenths, m.lonTenths))
			}
			if want := m.first.Add(time.Duration(len(m.values)) * time.Hour); !t.Equal(want) {
				return member{}, fmt.Errorf("line %d: time %s, want %s", n, t.Format(timeLayout), want.Format(timeLayout))
			}
		}
		m.values = append(m.values, v)
	}
	if err := sc.Err(); err != nil {
		return member{}, fmt.Errorf("line %d: %w", n+1, err)
	}
	if n == 0 {
		return member{}, errors.New("no header")
	}
	if len(m.values) == 0 {
		return member{}, errors.New("no rows after the header")
	}
	return m, nil
}

// parseRow reads one row: its time, its value and its grid point in tenths
// of a degree.
func parseRow(text, variable string) (time.Time, float64, int, int, error) {
	f := strings.Split(text, ",")
	if len(f) != 4 {
		return time.Time{}, 0, 0, 0, fmt.Errorf("%d fields, want 4", len(f))
	}
	// time.Parse also takes one-digit hours and fractional seconds, so the
	// time must read back the same.
	t, err := time.Parse(timeLayout, f[0])
	if err != nil || t.Format(timeLayout) != f[0] {
		return time.Time{}, 0, 0, 0, fmt.Errorf("time %q is not YYYY-MM-DD HH:MM:SS", f[0])
	}
	if t.Minute() != 0 || t.Second() != 0 {
		return time.Time{}, 0, 0, 0, fmt.Errorf("time %s is not on the hour", f[0])
	}
	if f[1] == "" {
		return time.Time{}, 0, 0, 0, errors.New("no data: the grid point is sea in ERA5-Land")
	}
	var v float64
	switch variable {
	case "t2m":
		if v, err = value(f[1], 170, 340); err != nil {
			return time.Time{}, 0, 0, 0, fmt.Errorf("t2m %w", err)
		}
	case "tp":
		if v, err = value(f[1], minPrecipitation, 0.5); err != nil {
			return time.Time{}, 0, 0, 0, fmt.Errorf("tp %w", err)
		}
		v = max(v, 0)
	}
	lat, err := gridTenths(f[2], 90)
	if err != nil {
		return time.Time{}, 0, 0, 0, fmt.Errorf("latitude %w", err)
	}
	lon, err := gridTenths(f[3], 180)
	if err != nil {
		return time.Time{}, 0, 0, 0, fmt.Errorf("longitude %w", err)
	}
	return t, v, lat, lon, nil
}

// value reads a decimal number from lo to hi.
func value(s string, lo, hi float64) (float64, error) {
	if !number.MatchString(s) {
		return 0, fmt.Errorf("%q is not a decimal number", s)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", s, err)
	}
	if v < lo || v > hi {
		return 0, fmt.Errorf("%s is outside %v to %v", s, lo, hi)
	}
	return v, nil
}

// gridTenths reads a coordinate of the 0.1 degree grid, written with a
// little float noise such as 38.70000000000112, as tenths of a degree.
func gridTenths(s string, limit float64) (int, error) {
	v, err := value(s, -limit, limit)
	if err != nil {
		return 0, err
	}
	tenths := math.Round(v * 10)
	if math.Abs(v*10-tenths) > 1e-5 {
		return 0, fmt.Errorf("%s is not on the 0.1 degree grid", s)
	}
	return int(tenths), nil
}

// point formats a grid point given in tenths of a degree.
func point(lat, lon int) string {
	return fmt.Sprintf("%v, %v", float64(lat)/10, float64(lon)/10)
}

// lines splits on "\n" alone. bufio.ScanLines would also drop a carriage
// return before it and so accept Windows line endings.
func lines(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
