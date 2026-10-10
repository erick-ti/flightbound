package fxrates

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/money"
)

// Element names in the ECB's daily reference rates document.
var (
	envelopeName = xml.Name{Space: "http://www.gesmes.org/xml/2002-08-01", Local: "Envelope"}
	cubeName     = xml.Name{Space: "http://www.ecb.int/vocabulary/2002-08-01/eurofxref", Local: "Cube"}
)

// plainRate matches a published rate: digits with an optional decimal part.
var plainRate = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// level is where an open element sits in the document.
type level int

const (
	unrelated level = iota // an element the parser skips
	root                   // the Envelope
	outer                  // the Cube in the Envelope
	day                    // the dated Cube in the outer Cube
	rate                   // a Cube with one currency's rate, in the dated Cube
)

// ParseECB reads the ECB's daily reference rates document:
//
//	<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01"
//	    xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
//	  <Cube>
//	    <Cube time="2026-10-09">
//	      <Cube currency="USD" rate="1.1206"/>
//
// The root must be that Envelope, holding exactly one outer Cube, which
// holds exactly one dated Cube; rate Cubes may appear only in the dated
// Cube, and no Cube may appear anywhere else. Only whitespace, comments,
// and the XML declaration may surround the root. The time, currency, and
// rate attributes must be unprefixed and appear at most once. Elements and
// attributes the producer adds are ignored. Currencies that money does not
// support are ignored, and at least one supported currency must have a
// rate.
func ParseECB(r io.Reader) (Rates, error) {
	dec := xml.NewDecoder(r)
	rates := Rates{perEuro: map[string]*big.Rat{}, published: map[string]string{}}
	seen := map[string]bool{}
	var open []level
	var started, ended bool
	var outers, days int
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Rates{}, fmt.Errorf("reading ECB rates: %w", err)
		}
		inside := len(open) > 0
		switch tok := tok.(type) {
		case xml.StartElement:
			if ended {
				return Rates{}, errors.New("ECB rates have data after the document")
			}
			if !started {
				if tok.Name != envelopeName {
					return Rates{}, fmt.Errorf("ECB rates have the root element %q, not the published Envelope", tok.Name.Local)
				}
				started = true
				open = append(open, root)
				continue
			}
			if tok.Name != cubeName {
				open = append(open, unrelated)
				continue
			}
			attrs, err := cubeAttrs(tok.Attr)
			if err != nil {
				return Rates{}, err
			}
			_, hasTime := attrs["time"]
			_, hasCurrency := attrs["currency"]
			_, hasRate := attrs["rate"]
			switch open[len(open)-1] {
			case root:
				outers++
				if outers > 1 || hasTime || hasCurrency || hasRate {
					return Rates{}, errors.New("ECB rates need exactly one outer Cube, without attributes")
				}
				open = append(open, outer)
			case outer:
				days++
				if days > 1 || !hasTime || hasCurrency || hasRate {
					return Rates{}, errors.New("ECB rates need exactly one dated Cube")
				}
				if _, err := time.Parse(time.DateOnly, attrs["time"]); err != nil {
					return Rates{}, fmt.Errorf("ECB rates have the date %q, not YYYY-MM-DD", attrs["time"])
				}
				rates.Date = attrs["time"]
				open = append(open, day)
			case day:
				if hasTime || !hasCurrency || !hasRate {
					return Rates{}, errors.New("ECB rates have a malformed currency entry")
				}
				if err := rates.add(attrs["currency"], attrs["rate"], seen); err != nil {
					return Rates{}, err
				}
				open = append(open, rate)
			default:
				return Rates{}, errors.New("ECB rates have a Cube outside the dated Cube")
			}
		case xml.EndElement:
			open = open[:len(open)-1]
			ended = len(open) == 0
		case xml.CharData:
			if !inside && strings.TrimSpace(string(tok)) != "" {
				return Rates{}, errors.New("ECB rates have text outside the document")
			}
		case xml.Comment:
		case xml.ProcInst:
			if !inside && (started || tok.Target != "xml") {
				return Rates{}, errors.New("ECB rates have an unexpected processing instruction")
			}
		case xml.Directive:
			if !inside {
				return Rates{}, errors.New("ECB rates have an unexpected declaration")
			}
		}
	}
	switch {
	case !ended:
		return Rates{}, errors.New("ECB rates have no document")
	case days == 0:
		return Rates{}, errors.New("ECB rates need exactly one dated Cube")
	case len(rates.perEuro) == 0:
		return Rates{}, errors.New("ECB rates have no supported currency")
	}
	return rates, nil
}

// cubeAttrs returns a Cube's unprefixed time, currency, and rate
// attributes. Prefixed attributes belong to other vocabularies and are
// ignored. A repeated attribute is an error.
func cubeAttrs(attrs []xml.Attr) (map[string]string, error) {
	got := map[string]string{}
	for _, a := range attrs {
		if a.Name.Space != "" {
			continue
		}
		switch a.Name.Local {
		case "time", "currency", "rate":
			if _, ok := got[a.Name.Local]; ok {
				return nil, fmt.Errorf("ECB rates repeat the %s attribute", a.Name.Local)
			}
			got[a.Name.Local] = a.Value
		}
	}
	return got, nil
}

// add records one currency's published rate. Each currency may appear once;
// currencies that money does not support are skipped.
func (r *Rates) add(code, published string, seen map[string]bool) error {
	if seen[code] {
		return fmt.Errorf("ECB rates list %s twice", code)
	}
	seen[code] = true
	if _, ok := money.LookupCurrency(code); !ok || code == "EUR" {
		return nil
	}
	// Check the plain decimal form first: SetString also accepts exponents,
	// which can expand to very large numbers.
	if !plainRate.MatchString(published) {
		return fmt.Errorf("ECB rates have the %s rate %q", code, published)
	}
	rate, ok := new(big.Rat).SetString(published)
	if !ok || rate.Sign() <= 0 {
		return fmt.Errorf("ECB rates have the %s rate %q", code, published)
	}
	r.perEuro[code] = rate
	r.published[code] = published
	return nil
}

// LoadFile reads rates from a saved copy of the ECB's daily document.
func LoadFile(path string) (Rates, error) {
	f, err := os.Open(path)
	if err != nil {
		return Rates{}, err
	}
	defer f.Close()
	rates, err := ParseECB(f)
	if err != nil {
		return Rates{}, fmt.Errorf("%s: %w", path, err)
	}
	return rates, nil
}
