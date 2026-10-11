package era5

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// Normals is the document the climate normals converter writes: monthly
// normals for each place, with where they came from.
type Normals struct {
	Dataset     string
	Variables   []string
	Period      Period
	Attribution string
	Places      []PlaceNormals
}

// PlaceNormals is one place's monthly normals. The grid point is the one
// the series came from, and SourceSHA256 is the hash of the downloaded file.
type PlaceNormals struct {
	ID               string
	GridLatitude     float64
	GridLongitude    float64
	SolarOffsetHours int
	SourceSHA256     string
	Months           [12]Month
}

// Pointers tell a missing member from a zero, so a month without a high
// cannot load as 0 °C.
type normalsDoc struct {
	Dataset     *string      `json:"dataset"`
	Variables   *[]string    `json:"variables"`
	Period      *periodDoc   `json:"period"`
	Attribution *string      `json:"attribution"`
	Places      *[]*placeDoc `json:"places"`
}

type periodDoc struct {
	First *int `json:"first"`
	Last  *int `json:"last"`
}

type placeDoc struct {
	ID               *string      `json:"id"`
	GridLatitude     *float64     `json:"grid_latitude"`
	GridLongitude    *float64     `json:"grid_longitude"`
	SolarOffsetHours *int         `json:"solar_offset_hours"`
	SourceSHA256     *string      `json:"source_sha256"`
	Months           *[]*monthDoc `json:"months"`
}

type monthDoc struct {
	Month         *int `json:"month"`
	High          *int `json:"high_tenths_c"`
	Low           *int `json:"low_tenths_c"`
	Precipitation *int `json:"precipitation_tenths_mm"`
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// UnmarshalNormals reads a normals document. The document's whole shape is
// ours, so encoding/json/v2 rejects unknown, repeated and differently cased
// members, and every member is required.
func UnmarshalNormals(data []byte) (Normals, error) {
	var doc *normalsDoc
	if err := json.Unmarshal(data, &doc, json.RejectUnknownMembers(true)); err != nil {
		return Normals{}, err
	}
	if doc == nil || doc.Dataset == nil || doc.Variables == nil || doc.Period == nil || doc.Attribution == nil || doc.Places == nil ||
		doc.Period.First == nil || doc.Period.Last == nil {
		return Normals{}, errors.New("normals document is missing a member")
	}
	n := Normals{
		Dataset:     *doc.Dataset,
		Variables:   *doc.Variables,
		Period:      Period{First: *doc.Period.First, Last: *doc.Period.Last},
		Attribution: *doc.Attribution,
	}
	if n.Period.First > n.Period.Last {
		return Normals{}, fmt.Errorf("period %v ends before it starts", n.Period)
	}
	seen := map[string]bool{}
	for i, pd := range *doc.Places {
		p, err := placeFromDoc(pd)
		if err != nil {
			return Normals{}, fmt.Errorf("place %d: %w", i, err)
		}
		if seen[p.ID] {
			return Normals{}, fmt.Errorf("place %q is listed twice", p.ID)
		}
		seen[p.ID] = true
		n.Places = append(n.Places, p)
	}
	return n, nil
}

func placeFromDoc(d *placeDoc) (PlaceNormals, error) {
	if d == nil || d.ID == nil || d.GridLatitude == nil || d.GridLongitude == nil || d.SolarOffsetHours == nil ||
		d.SourceSHA256 == nil || d.Months == nil {
		return PlaceNormals{}, errors.New("missing a member")
	}
	p := PlaceNormals{
		ID:               *d.ID,
		GridLatitude:     *d.GridLatitude,
		GridLongitude:    *d.GridLongitude,
		SolarOffsetHours: *d.SolarOffsetHours,
		SourceSHA256:     *d.SourceSHA256,
	}
	switch {
	case p.ID == "":
		return p, errors.New("empty ID")
	case p.GridLatitude < -90 || p.GridLatitude > 90 || p.GridLongitude < -180 || p.GridLongitude > 180:
		return p, fmt.Errorf("%s: grid point %v, %v is not on the globe", p.ID, p.GridLatitude, p.GridLongitude)
	case p.SolarOffsetHours < -12 || p.SolarOffsetHours > 12:
		return p, fmt.Errorf("%s: solar offset %d is outside -12 to 12 hours", p.ID, p.SolarOffsetHours)
	case !sha256Hex.MatchString(p.SourceSHA256):
		return p, fmt.Errorf("%s: source hash %q is not a SHA-256 in hex", p.ID, p.SourceSHA256)
	case len(*d.Months) != 12:
		return p, fmt.Errorf("%s: %d months, want 12", p.ID, len(*d.Months))
	}
	for i, md := range *d.Months {
		if md == nil || md.Month == nil || md.High == nil || md.Low == nil || md.Precipitation == nil {
			return p, fmt.Errorf("%s: month %d is missing a member", p.ID, i+1)
		}
		m := Month{Month: *md.Month, HighTenthsC: *md.High, LowTenthsC: *md.Low, PrecipitationTenthsMM: *md.Precipitation}
		switch {
		case m.Month != i+1:
			return p, fmt.Errorf("%s: month %d is listed as %d", p.ID, i+1, m.Month)
		case m.LowTenthsC < -900 || m.HighTenthsC > 600:
			return p, fmt.Errorf("%s: month %d's temperatures are outside -90 to 60 °C", p.ID, m.Month)
		case m.HighTenthsC < m.LowTenthsC:
			return p, fmt.Errorf("%s: month %d's high is below its low", p.ID, m.Month)
		case m.PrecipitationTenthsMM < 0:
			return p, fmt.Errorf("%s: month %d's precipitation is negative", p.ID, m.Month)
		}
		p.Months[i] = m
	}
	return p, nil
}

// MarshalNormals writes a normals document with one month to a line, so a
// regenerated file diffs month by month. The same value always gives the
// same bytes.
func MarshalNormals(n Normals) []byte {
	var b bytes.Buffer
	str := func(s string) string {
		out, err := json.Marshal(s)
		if err != nil {
			panic(err) // a Go string always encodes
		}
		return string(out)
	}
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"dataset\": %s,\n", str(n.Dataset))
	b.WriteString("  \"variables\": [")
	for i, v := range n.Variables {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(str(v))
	}
	b.WriteString("],\n")
	fmt.Fprintf(&b, "  \"period\": {\"first\": %d, \"last\": %d},\n", n.Period.First, n.Period.Last)
	fmt.Fprintf(&b, "  \"attribution\": %s,\n", str(n.Attribution))
	if len(n.Places) == 0 {
		b.WriteString("  \"places\": []\n}\n")
		return b.Bytes()
	}
	b.WriteString("  \"places\": [\n")
	for i, p := range n.Places {
		b.WriteString("    {\n")
		fmt.Fprintf(&b, "      \"id\": %s,\n", str(p.ID))
		fmt.Fprintf(&b, "      \"grid_latitude\": %s,\n", num(p.GridLatitude))
		fmt.Fprintf(&b, "      \"grid_longitude\": %s,\n", num(p.GridLongitude))
		fmt.Fprintf(&b, "      \"solar_offset_hours\": %d,\n", p.SolarOffsetHours)
		fmt.Fprintf(&b, "      \"source_sha256\": %s,\n", str(p.SourceSHA256))
		b.WriteString("      \"months\": [\n")
		for j, m := range p.Months {
			fmt.Fprintf(&b, "        {\"month\": %d, \"high_tenths_c\": %d, \"low_tenths_c\": %d, \"precipitation_tenths_mm\": %d}",
				m.Month, m.HighTenthsC, m.LowTenthsC, m.PrecipitationTenthsMM)
			if j < len(p.Months)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("      ]\n    }")
		if i < len(n.Places)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ]\n}\n")
	return b.Bytes()
}
