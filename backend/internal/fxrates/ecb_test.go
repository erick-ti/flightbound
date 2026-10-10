package fxrates

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/erick-ti/flightbound/backend/internal/money"
)

// fixture is the ECB daily euro reference rates file exactly as published,
// downloaded from https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml
// on 2026-10-10 (rates dated 2026-10-09). Source: ECB statistics.
const fixture = "testdata/eurofxref-daily.xml"

func readFixture(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureRates(t testing.TB) Rates {
	t.Helper()
	rates, err := ParseECB(bytes.NewReader(readFixture(t)))
	if err != nil {
		t.Fatalf("ParseECB(fixture): %v", err)
	}
	return rates
}

// ecbDoc returns a document in the published daily format with the given
// date and currency, rate pairs.
func ecbDoc(date string, pairs ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
	<gesmes:subject>Reference rates</gesmes:subject>
	<Cube>
		<Cube time='` + date + `'>
`)
	for i := 0; i+1 < len(pairs); i += 2 {
		fmt.Fprintf(&b, "\t\t\t<Cube currency='%s' rate='%s'/>\n", pairs[i], pairs[i+1])
	}
	b.WriteString("\t\t</Cube>\n\t</Cube>\n</gesmes:Envelope>\n")
	return b.String()
}

func mustParse(t testing.TB, doc string) Rates {
	t.Helper()
	rates, err := ParseECB(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("ParseECB: %v\n%s", err, doc)
	}
	return rates
}

func TestParseECBFixture(t *testing.T) {
	rates := fixtureRates(t)
	if rates.Date != "2026-10-09" {
		t.Errorf("Date = %q, want 2026-10-09", rates.Date)
	}
	for code, want := range map[string]string{
		"USD": "1.1206", "JPY": "177.34", "GBP": "0.84763", "IDR": "20036.94", "ISK": "136.80", "ZAR": "18.5270",
	} {
		if got, ok := rates.PerEuro(code); !ok || got != want {
			t.Errorf("PerEuro(%s) = %q, %v; want %q", code, got, ok, want)
		}
	}
	// The file covers every supported currency except the euro itself.
	if len(rates.perEuro) != 29 {
		t.Errorf("%d rates, want 29", len(rates.perEuro))
	}
	if _, ok := rates.PerEuro("EUR"); ok {
		t.Error("PerEuro(EUR) reported a published rate")
	}
}

func TestParseECBToleratesProducerAdditions(t *testing.T) {
	doc := string(readFixture(t))
	for _, edit := range [][2]string{
		{"<gesmes:subject>Reference rates</gesmes:subject>", "<gesmes:subject>Reference rates</gesmes:subject>\n\t<gesmes:note>added</gesmes:note>"},
		{"<Cube currency='USD' rate='1.1206'/>", "<Cube currency='USD' rate='1.1206' status='final'/>"},
		{"<Cube currency='JPY' rate='177.34'/>", "<Cube currency='JPY' rate='177.34'/>\n\t\t\t<Cube currency='XAU' rate='0.00031'/>"},
		{"<Cube time='2026-10-09'>", "<Cube time='2026-10-09'>\n\t\t\t<Note>added</Note>"},
		// An attribute in another namespace does not replace the published one.
		{"<Cube currency='GBP' rate='0.84763'/>", "<Cube currency='GBP' rate='0.84763' xmlns:extra='urn:extra' extra:rate='9' extra:currency='CHF'/>"},
	} {
		if !strings.Contains(doc, edit[0]) {
			t.Fatalf("fixture lacks %q", edit[0])
		}
		doc = strings.Replace(doc, edit[0], edit[1], 1)
	}
	doc += "<!-- trailing comment -->\n"
	rates := mustParse(t, doc)
	if got, ok := rates.PerEuro("USD"); !ok || got != "1.1206" {
		t.Errorf("PerEuro(USD) = %q, %v", got, ok)
	}
	if got, ok := rates.PerEuro("GBP"); !ok || got != "0.84763" {
		t.Errorf("PerEuro(GBP) = %q, %v; want the published 0.84763", got, ok)
	}
	if got, ok := rates.PerEuro("CHF"); !ok || got != "0.9313" {
		t.Errorf("PerEuro(CHF) = %q, %v; want the published 0.9313", got, ok)
	}
	if _, ok := rates.PerEuro("XAU"); ok {
		t.Error("kept a rate for an unsupported currency")
	}
	if len(rates.perEuro) != 29 {
		t.Errorf("%d rates, want 29", len(rates.perEuro))
	}
}

func TestParseECBRejects(t *testing.T) {
	base := string(readFixture(t))
	// Each case below changes a document that is accepted as published.
	mustParse(t, base)
	edit := func(old, new string) string {
		t.Helper()
		if !strings.Contains(base, old) {
			t.Fatalf("fixture lacks %q", old)
		}
		return strings.ReplaceAll(base, old, new)
	}
	tests := []struct {
		name, doc string
	}{
		{"empty", ""},
		{"not XML", "rates"},
		{"wrong root", edit("gesmes:Envelope", "gesmes:Report")},
		{"wrong root namespace", edit(`xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01"`, `xmlns:gesmes="http://example.com/gesmes"`)},
		{"wrong rates namespace", edit(`xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref"`, `xmlns="http://example.com/eurofxref"`)},
		{"rate beside the dated cube", edit("\t\t<Cube time='2026-10-09'>", "\t\t<Cube currency='CHF' rate='0.9'/>\n\t\t<Cube time='2026-10-09'>")},
		{"rates directly in the outer cube", edit("<Cube time='2026-10-09'>", "<Cube>")},
		{"two outer cubes", edit("</gesmes:Envelope>", "<Cube/>\n</gesmes:Envelope>")},
		{"two dated cubes", edit("\t\t<Cube time='2026-10-09'>", "\t\t<Cube time='2026-10-08'>\n\t\t\t<Cube currency='CHF' rate='0.9'/>\n\t\t</Cube>\n\t\t<Cube time='2026-10-09'>")},
		{"date on the outer cube", edit("\t<Cube>\n", "\t<Cube time='2026-10-09'>\n")},
		{"rate inside a rate", edit("<Cube currency='USD' rate='1.1206'/>", "<Cube currency='USD' rate='1.1206'><Cube currency='CHF' rate='0.9'/></Cube>")},
		{"unpadded date", edit("time='2026-10-09'", "time='2026-10-9'")},
		{"impossible date", edit("time='2026-10-09'", "time='2026-02-30'")},
		{"timestamp date", edit("time='2026-10-09'", "time='2026-10-09T14:00:00Z'")},
		{"duplicate currency", edit("<Cube currency='JPY' rate='177.34'/>", "<Cube currency='USD' rate='1.2'/>")},
		{"missing currency", edit("<Cube currency='JPY' rate='177.34'/>", "<Cube rate='177.34'/>")},
		{"missing rate", edit("<Cube currency='JPY' rate='177.34'/>", "<Cube currency='JPY'/>")},
		{"zero rate", edit("rate='1.1206'", "rate='0'")},
		{"zero rate with decimals", edit("rate='1.1206'", "rate='0.0000'")},
		{"negative rate", edit("rate='1.1206'", "rate='-1.1206'")},
		{"comma rate", edit("rate='1.1206'", "rate='1,1206'")},
		{"exponent rate", edit("rate='1.1206'", "rate='1e3'")},
		{"fraction rate", edit("rate='1.1206'", "rate='11206/10000'")},
		{"spaced rate", edit("rate='1.1206'", "rate=' 1.1206'")},
		{"no supported rates", ecbDoc("2026-10-09", "XAU", "0.00031")},
		{"trailing document", base + "<Envelope/>\n"},
		{"trailing text", base + "rates\n"},
		{"text before the root", "rates\n" + base},
		{"text after the declaration", edit("?>\n", "?>\nrates\n")},
		{"document type declaration", edit("?>\n", "?>\n<!DOCTYPE Envelope>\n")},
		{"rate inside an added element", edit("<gesmes:subject>Reference rates</gesmes:subject>", "<gesmes:subject>Reference rates<Cube currency='CAD' rate='100'/></gesmes:subject>")},
		{"rate inside an added element in the dated cube", edit("<Cube currency='JPY' rate='177.34'/>", "<Note><Cube currency='JPY' rate='177.34'/></Note>")},
		{"rate attribute from another namespace", edit("<Cube currency='USD' rate='1.1206'/>", "<Cube currency='USD' xmlns:extra='urn:extra' extra:rate='1.1206'/>")},
		{"repeated rate attribute", edit("<Cube currency='USD' rate='1.1206'/>", "<Cube currency='USD' rate='1.1206' rate='9'/>")},
		{"repeated currency attribute", edit("<Cube currency='USD' rate='1.1206'/>", "<Cube currency='USD' currency='XAU' rate='1.1206'/>")},
		{"repeated time attribute", edit("<Cube time='2026-10-09'>", "<Cube time='2026-10-09' time='2026-10-08'>")},
		{"huge exponent rate", edit("rate='1.1206'", "rate='1e1000000'")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rates, err := ParseECB(strings.NewReader(tt.doc)); err == nil {
				t.Errorf("accepted, date %q with %d rates", rates.Date, len(rates.perEuro))
			}
		})
	}
}

func TestLoadFile(t *testing.T) {
	rates, err := LoadFile(fixture)
	if err != nil || rates.Date != "2026-10-09" {
		t.Errorf("LoadFile(fixture) = date %q, %v", rates.Date, err)
	}
	if _, err := LoadFile("testdata/missing.xml"); err == nil {
		t.Error("LoadFile of a missing file succeeded")
	}
}

func FuzzParseECB(f *testing.F) {
	f.Add(readFixture(f))
	f.Add([]byte(ecbDoc("2026-10-09", "USD", "1.5", "JPY", "100")))
	f.Add([]byte(ecbDoc("2026-10-09", "USD", "0")))
	f.Add([]byte("<x/>"))
	f.Fuzz(func(t *testing.T, data []byte) {
		rates, err := ParseECB(bytes.NewReader(data))
		if err != nil {
			return
		}
		if _, err := time.Parse(time.DateOnly, rates.Date); err != nil {
			t.Fatalf("accepted date %q: %v", rates.Date, err)
		}
		if len(rates.perEuro) == 0 {
			t.Fatal("accepted a document without supported rates")
		}
		for code, rate := range rates.perEuro {
			if _, ok := money.LookupCurrency(code); !ok || code == "EUR" {
				t.Fatalf("kept a rate for %q", code)
			}
			if rate.Sign() <= 0 {
				t.Fatalf("%s rate %s is not positive", code, rate.RatString())
			}
			if published, ok := rates.PerEuro(code); !ok || !plainRate.MatchString(published) {
				t.Fatalf("%s published rate %q", code, published)
			}
		}
	})
}
