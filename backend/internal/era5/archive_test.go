package era5

import (
	"archive/zip"
	"bytes"
	"hash/crc32"
	"io"
	"strings"
	"testing"
	"time"
)

type zipMember struct{ name, body string }

// The two files' names in a real download.
const (
	t2mName = "reanalysis-era5-land-timeseries-sfc-2m-temperature1i1bcu4f.csv"
	tpName  = "reanalysis-era5-land-timeseries-sfc-pressure-precipitationhneqiyxg.csv"
)

// zipOf builds a zip archive of the members, deflated.
func zipOf(t *testing.T, members ...zipMember) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, m := range members {
		f, err := w.Create(m.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withStored adds to a zip of the members one uncompressed member whose
// header gives the size and checksum passed in, whatever the body.
func withStored(t *testing.T, members []zipMember, name, body string, size uint64, crc uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, m := range members {
		f, err := w.Create(m.name)
		if err == nil {
			_, err = f.Write([]byte(m.body))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	f, err := w.CreateRaw(&zip.FileHeader{
		Name: name, Method: zip.Store, CRC32: crc,
		CompressedSize64: uint64(len(body)), UncompressedSize64: size,
	})
	if err == nil {
		_, err = f.Write([]byte(body))
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseArchive(t *testing.T) {
	t2m := zipMember{t2mName, string(readFixture(t, "era5-land-lisbon-t2m.csv"))}
	tp := zipMember{tpName, string(readFixture(t, "era5-land-lisbon-tp.csv"))}
	for name, data := range map[string][]byte{
		"as downloaded":       zipOf(t, t2m, tp),
		"in the other order":  zipOf(t, tp, t2m),
		"precipitation saved": withStored(t, []zipMember{t2m}, "p.csv", tp.body, uint64(len(tp.body)), crc32.ChecksumIEEE([]byte(tp.body))),
	} {
		t.Run(name, func(t *testing.T) {
			s, err := ParseArchive(data)
			if err != nil {
				t.Fatal(err)
			}
			if len(s.Hours) != 48 || s.Latitude != 38.7 || s.Longitude != -9.1 {
				t.Fatalf("parsed %d hours at %v, %v", len(s.Hours), s.Latitude, s.Longitude)
			}
			first := time.Date(1991, 1, 6, 0, 0, 0, 0, time.UTC)
			for i, want := range map[int]Hour{
				0:  {Time: first, Temperature: 285.07925, Precipitation: 0},
				38: {Time: first.Add(38 * time.Hour), Temperature: 286.93774, Precipitation: 0.0017636367},
				47: {Time: first.Add(47 * time.Hour), Temperature: 284.50476, Precipitation: 4.656613e-09},
			} {
				if got := s.Hours[i]; !got.Time.Equal(want.Time) || got.Temperature != want.Temperature || got.Precipitation != want.Precipitation {
					t.Errorf("hour %d = %+v, want %+v", i, got, want)
				}
			}
		})
	}
}

func TestParseArchiveRefusesOtherArchives(t *testing.T) {
	t2m := zipMember{t2mName, sampleT2M}
	tp := zipMember{tpName, sampleTP}
	// A digit changed after the checksum was taken: still a valid CSV file.
	damaged := strings.Replace(sampleTP, "2.7567148e-07", "2.7567149e-07", 1)
	tests := map[string][]byte{
		"not a zip archive":           []byte(sampleT2M),
		"an empty archive":            zipOf(t),
		"temperature alone":           zipOf(t, t2m),
		"three files":                 zipOf(t, t2m, tp, zipMember{"more.csv", sampleTP}),
		"temperature twice":           zipOf(t, t2m, zipMember{tpName, sampleT2M}),
		"a file not ending .csv":      zipOf(t, t2m, zipMember{"tp.txt", sampleTP}),
		"a file in a folder":          zipOf(t, t2m, zipMember{"dir/tp.csv", sampleTP}),
		"a path out of the archive":   zipOf(t, t2m, zipMember{"../tp.csv", sampleTP}),
		"a CSV file it refuses":       zipOf(t, t2m, zipMember{tpName, "hello\n"}),
		"files for different hours":   zipOf(t, t2m, zipMember{tpName, strings.ReplaceAll(sampleTP, "1991-01-06 0", "1991-01-07 0")}),
		"files of different lengths":  zipOf(t, t2m, zipMember{tpName, strings.TrimSuffix(sampleTP, "1991-01-06 02:00:00,2.7567148e-07,38.70000000000112,-9.099999999988768\n")}),
		"files at different points":   zipOf(t, t2m, zipMember{tpName, strings.ReplaceAll(sampleTP, "38.70000000000112,", "38.8,")}),
		"a damaged file":              withStored(t, []zipMember{t2m}, tpName, damaged, uint64(len(damaged)), crc32.ChecksumIEEE([]byte(sampleTP))),
		"a file over the size limit":  withStored(t, []zipMember{t2m}, tpName, sampleTP, uint64(maxArchiveCSV)+1, crc32.ChecksumIEEE([]byte(sampleTP))),
		"a sea point with no values":  zipOf(t, zipMember{t2mName, strings.NewReplacer("285.07925", "", "285.05774", "", "284.94308", "").Replace(sampleT2M)}, tp),
		"a single-file ERA5 download": zipOf(t, zipMember{"era5.csv", "valid_time,t2m,tp,latitude,longitude\n1991-01-06 00:00:00,285.1,0.0,38.75,-9.25\n"}),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			s, err := ParseArchive(data)
			if err == nil {
				t.Errorf("no error; parsed %d hours", len(s.Hours))
			}
			t.Log(err)
		})
	}
}

// A file exactly at the size limit must still reach the end of the zip
// reader, which is where its CRC-32 is checked.
func TestParseArchiveChecksTheCRCAtTheSizeLimit(t *testing.T) {
	saved := maxArchiveCSV
	maxArchiveCSV = int64(len(sampleTP))
	t.Cleanup(func() { maxArchiveCSV = saved })
	t2m := zipMember{t2mName, sampleT2M}
	if len(sampleT2M) > len(sampleTP) {
		t.Fatal("the temperature sample must fit under the limit")
	}
	crc := crc32.ChecksumIEEE([]byte(sampleTP))
	whole := withStored(t, []zipMember{t2m}, tpName, sampleTP, uint64(len(sampleTP)), crc)
	if _, err := ParseArchive(whole); err != nil {
		t.Errorf("an intact file at the limit: %v", err)
	}
	damaged := strings.Replace(sampleTP, "2.7567148e-07", "2.7567149e-07", 1)
	_, err := ParseArchive(withStored(t, []zipMember{t2m}, tpName, damaged, uint64(len(damaged)), crc))
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("a damaged file at the limit: error %v, want a checksum error", err)
	}
}

// capped is a backstop in case the zip reader ever returned more than the
// size it declares.
func TestCappedFailsPastTheLimit(t *testing.T) {
	if _, err := io.ReadAll(&capped{r: strings.NewReader("0123456789"), left: 5}); err == nil {
		t.Error("10 bytes under a limit of 5: no error")
	}
	if b, err := io.ReadAll(&capped{r: strings.NewReader("01234"), left: 5}); err != nil || string(b) != "01234" {
		t.Errorf("exactly the limit: %q, %v", b, err)
	}
}
