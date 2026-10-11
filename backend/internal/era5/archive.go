package era5

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

// maxArchiveCSV caps the size of each CSV file in a download. A 1991-2020
// download for one place holds about 17 MB per file. Tests lower it to
// check the limit with small files.
var maxArchiveCSV int64 = 64 << 20

// csvName is a file name ending in .csv, outside any folder.
var csvName = regexp.MustCompile(`^[A-Za-z0-9._-]+\.csv$`)

// ParseArchive reads a Climate Data Store download of the ERA5-Land time
// series: a zip archive holding two CSV files, one of 2 m temperature and
// one of precipitation, for the same hours at the same grid point. Nothing
// is extracted to disk.
func ParseArchive(data []byte) (Series, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Series{}, fmt.Errorf("not a zip archive: %w", err)
	}
	if len(z.File) != 2 {
		return Series{}, fmt.Errorf("the archive holds %d files, want 2", len(z.File))
	}
	found := map[string]member{}
	for _, f := range z.File {
		m, err := readMember(f)
		if err != nil {
			return Series{}, err
		}
		if _, ok := found[m.variable]; ok {
			return Series{}, fmt.Errorf("the archive holds two %s files", m.variable)
		}
		found[m.variable] = m
	}
	t2m, tp := found["t2m"], found["tp"]
	if !t2m.first.Equal(tp.first) || len(t2m.values) != len(tp.values) {
		return Series{}, fmt.Errorf("the files cover different hours: %d from %s and %d from %s",
			len(t2m.values), t2m.first.Format(timeLayout), len(tp.values), tp.first.Format(timeLayout))
	}
	if t2m.latTenths != tp.latTenths || t2m.lonTenths != tp.lonTenths {
		return Series{}, fmt.Errorf("the files are for different grid points: %s and %s",
			point(t2m.latTenths, t2m.lonTenths), point(tp.latTenths, tp.lonTenths))
	}
	s := Series{
		Latitude:  float64(t2m.latTenths) / 10,
		Longitude: float64(t2m.lonTenths) / 10,
		Hours:     make([]Hour, len(t2m.values)),
	}
	for i := range s.Hours {
		s.Hours[i] = Hour{
			Time:          t2m.first.Add(time.Duration(i) * time.Hour),
			Temperature:   t2m.values[i],
			Precipitation: tp.values[i],
		}
	}
	return s, nil
}

// readMember checks one file of the archive and parses it.
func readMember(f *zip.File) (member, error) {
	if !csvName.MatchString(f.Name) {
		return member{}, fmt.Errorf("the archive holds %q, want CSV files", f.Name)
	}
	if f.UncompressedSize64 > uint64(maxArchiveCSV) {
		return member{}, fmt.Errorf("%s is %d bytes, more than %d", f.Name, f.UncompressedSize64, maxArchiveCSV)
	}
	rc, err := f.Open()
	if err != nil {
		return member{}, fmt.Errorf("%s: %w", f.Name, err)
	}
	defer rc.Close()
	// Read through to the zip reader's own end, where it checks the CRC-32;
	// io.LimitReader would stop at the limit without that check. The zip
	// reader also stops at the declared size, which is within the limit.
	m, err := parseMember(&capped{r: rc, left: maxArchiveCSV})
	if err != nil {
		return member{}, fmt.Errorf("%s: %w", f.Name, err)
	}
	return m, nil
}

// capped passes reads through and fails, rather than stopping quietly,
// once more than left bytes have come.
type capped struct {
	r    io.Reader
	left int64
}

func (c *capped) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return n, errors.New("the file is larger than the size limit")
	}
	return n, err
}
