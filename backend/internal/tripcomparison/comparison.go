// Package tripcomparison compares flight-plus-stay estimates for two or
// three destinations that share dates and a party size.
//
// Every amount is a traveler-entered estimate for the whole party, in a
// supported currency of its own. This package validates the estimates,
// converts them to the comparison currency, and does all of the
// arithmetic; clients display the results as returned.
package tripcomparison

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/erick-ti/flightbound/backend/internal/fxrates"
	"github.com/erick-ti/flightbound/backend/internal/money"
)

// Limits on a comparison request.
const (
	MinDestinations = 2
	MaxDestinations = 3
	MinTravelers    = 1
	MaxTravelers    = 20
	MaxNights       = 365
	// MaxLabelLength counts characters after surrounding spaces are trimmed.
	MaxLabelLength = 80
)

// Request is the body of a trip comparison request. Currency is the
// comparison currency, which every total uses.
type Request struct {
	CheckIn      string             `json:"check_in"`
	CheckOut     string             `json:"check_out"`
	Travelers    int                `json:"travelers"`
	Currency     string             `json:"currency"`
	Destinations []DestinationInput `json:"destinations"`
}

// DestinationInput holds one destination's estimates as decimal strings. A
// nil estimate is unknown, which is different from "0". A nil estimate
// currency means the comparison currency.
type DestinationInput struct {
	Label string `json:"label"`
	// FlightEstimate is the round-trip flight cost for the whole party.
	FlightEstimate         *string `json:"flight_estimate"`
	FlightEstimateCurrency *string `json:"flight_estimate_currency"`
	// NightlyStayEstimate is the accommodation cost per night for the whole
	// party.
	NightlyStayEstimate         *string `json:"nightly_stay_estimate"`
	NightlyStayEstimateCurrency *string `json:"nightly_stay_estimate_currency"`
}

// Comparison is the result for a valid request. Amounts are decimal strings
// with their currency's exact number of decimal places; nil means unknown.
type Comparison struct {
	CheckIn   string `json:"check_in"`
	CheckOut  string `json:"check_out"`
	Nights    int    `json:"nights"`
	Travelers int    `json:"travelers"`
	Currency  string `json:"currency"`
	// AllComplete reports whether every destination has a total.
	AllComplete bool `json:"all_complete"`
	// ExchangeRates describes the rates used, or is nil when no known
	// estimate needed converting.
	ExchangeRates *ExchangeRates        `json:"exchange_rates"`
	Destinations  []DestinationEstimate `json:"destinations"`
}

// ExchangeRates describes the ECB reference rates behind a comparison's
// conversions.
type ExchangeRates struct {
	// Available is false when no usable rates could be loaded. Estimates that
	// needed converting are then unknown in the comparison currency.
	Available bool `json:"available"`
	// Date is the rates' reference date, or nil when they are unavailable.
	Date *string `json:"date"`
	// PerEuro maps each currency other than the euro that a conversion used
	// to its rate as published, such as "USD": "1.1206".
	PerEuro map[string]string `json:"per_euro"`
}

// DestinationEstimate is one destination's calculated estimate.
type DestinationEstimate struct {
	Label                       string  `json:"label"`
	FlightEstimate              *string `json:"flight_estimate"`
	FlightEstimateCurrency      string  `json:"flight_estimate_currency"`
	NightlyStayEstimate         *string `json:"nightly_stay_estimate"`
	NightlyStayEstimateCurrency string  `json:"nightly_stay_estimate_currency"`
	// StayEstimate is the nightly estimate multiplied by the number of
	// nights, in the nightly estimate's currency.
	StayEstimate *string `json:"stay_estimate"`
	// ConvertedFlightEstimate and ConvertedStayEstimate are the estimates in
	// the comparison currency: the same amounts when already in it, nil when
	// unknown or when no rate is available.
	ConvertedFlightEstimate *string `json:"converted_flight_estimate"`
	ConvertedStayEstimate   *string `json:"converted_stay_estimate"`
	// FlightAndStayEstimate is the sum of the converted estimates, set only
	// when both are known.
	FlightAndStayEstimate *string `json:"flight_and_stay_estimate"`
	Complete              bool    `json:"complete"`
	// LowestEstimate is true only when every destination is complete and
	// this total equals the lowest one. Tied destinations are all marked.
	LowestEstimate bool `json:"lowest_estimate"`
}

// FieldError describes one invalid request field. Field is the JSON path,
// such as "check_out" or "destinations[1].flight_estimate".
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Compare validates req and calculates its comparison. It returns every
// field error it finds, or the comparison when there are none. It asks
// rates for exchange rates at most once, and only when a known estimate is
// in a currency other than the comparison currency.
func Compare(req Request, rates fxrates.Source) (Comparison, []FieldError) {
	var v validator
	nights := v.nights(req.CheckIn, req.CheckOut)
	if req.Travelers < MinTravelers || req.Travelers > MaxTravelers {
		v.add("travelers", fmt.Sprintf("Choose from %d to %d travelers.", MinTravelers, MaxTravelers))
	}
	cur, curOK := money.LookupCurrency(req.Currency)
	if !curOK {
		v.add("currency", "Choose a supported currency.")
	}
	if n := len(req.Destinations); n < MinDestinations || n > MaxDestinations {
		v.add("destinations", "Compare two or three destinations.")
	}
	// Validate at most MaxDestinations entries, so an oversized list still
	// gets a bounded set of errors.
	parsed := make([]destination, min(len(req.Destinations), MaxDestinations))
	for i := range parsed {
		parsed[i] = v.destination(i, req.Destinations[i], cur, curOK)
	}
	if len(v.errs) > 0 {
		return Comparison{}, v.errs
	}
	return calculate(req, cur, nights, parsed, rates)
}

// destination is a validated destination with estimates in minor units of
// their own currencies.
type destination struct {
	label      string
	flight     *int64
	flightCur  money.Currency
	nightly    *int64
	nightlyCur money.Currency
}

type validator struct {
	errs []FieldError
}

func (v *validator) add(field, message string) {
	v.errs = append(v.errs, FieldError{Field: field, Message: message})
}

// nights returns the number of calendar nights between the two dates, or 0
// after recording an error.
func (v *validator) nights(checkIn, checkOut string) int {
	in, inOK := v.date("check_in", "check-in", checkIn)
	out, outOK := v.date("check_out", "check-out", checkOut)
	if !inOK || !outOK {
		return 0
	}
	// Dates without a time parse as midnight UTC, so the difference is a
	// whole number of days whatever time zone the traveler is in.
	nights := (out.Unix() - in.Unix()) / (24 * 60 * 60)
	switch {
	case nights < 1:
		v.add("check_out", "Check-out must be after check-in.")
		return 0
	case nights > MaxNights:
		v.add("check_out", fmt.Sprintf("Stays can be at most %d nights.", MaxNights))
		return 0
	}
	return int(nights)
}

func (v *validator) date(field, name, value string) (time.Time, bool) {
	if value == "" {
		v.add(field, fmt.Sprintf("Enter a %s date.", name))
		return time.Time{}, false
	}
	t, err := time.Parse(time.DateOnly, value)
	if err != nil {
		v.add(field, fmt.Sprintf("Enter a real %s date as YYYY-MM-DD.", name))
		return time.Time{}, false
	}
	return t, true
}

// destination validates one destination. cur is the comparison currency.
func (v *validator) destination(i int, d DestinationInput, cur money.Currency, curOK bool) destination {
	prefix := fmt.Sprintf("destinations[%d].", i)
	label := strings.TrimSpace(d.Label)
	switch {
	case label == "":
		v.add(prefix+"label", "Enter a destination name.")
	case utf8.RuneCountInString(label) > MaxLabelLength:
		v.add(prefix+"label", fmt.Sprintf("Use at most %d characters.", MaxLabelLength))
	}
	parsed := destination{label: label}
	parsed.flightCur, parsed.flight = v.estimate(prefix+"flight_estimate", d.FlightEstimate, d.FlightEstimateCurrency, cur, curOK)
	parsed.nightlyCur, parsed.nightly = v.estimate(prefix+"nightly_stay_estimate", d.NightlyStayEstimate, d.NightlyStayEstimateCurrency, cur, curOK)
	return parsed
}

// estimate resolves an estimate's currency, which defaults to the
// comparison currency, then parses the estimate in it. The amount is
// checked only when its currency is valid, since its precision depends on
// it.
func (v *validator) estimate(field string, value, code *string, cur money.Currency, curOK bool) (money.Currency, *int64) {
	if code != nil {
		cur, curOK = money.LookupCurrency(*code)
		if !curOK {
			v.add(field+"_currency", "Choose a supported currency.")
		}
	}
	if !curOK {
		return cur, nil
	}
	return cur, v.amount(field, value, cur)
}

// amount parses an optional estimate. A nil estimate stays nil (unknown).
func (v *validator) amount(field string, value *string, cur money.Currency) *int64 {
	if value == nil {
		return nil
	}
	minor, err := cur.Parse(*value)
	if err != nil {
		v.add(field, amountMessage(err, cur))
		return nil
	}
	return &minor
}

func amountMessage(err error, cur money.Currency) string {
	switch {
	case errors.Is(err, money.ErrPrecision) && cur.Digits == 0:
		return fmt.Sprintf("%s amounts must be whole numbers.", cur.Code)
	case errors.Is(err, money.ErrPrecision):
		return fmt.Sprintf("%s amounts can have at most %d decimal places.", cur.Code, cur.Digits)
	case errors.Is(err, money.ErrTooLarge):
		return "This amount is too large."
	default:
		example := "1250"
		if cur.Digits > 0 {
			example += ".5" + strings.Repeat("0", cur.Digits-1)
		}
		return fmt.Sprintf("Use digits with an optional decimal point, like %s.", example)
	}
}

// Overflow messages. The amount and stay limits keep a stay's
// multiplication in range, but converting to a currency with a much larger
// rate can overflow, and so can adding the two converted estimates.
const (
	tooLargeToCalculate = "This estimate is too large to calculate."
	tooLargeToConvert   = "This estimate is too large to convert."
)

// calculate builds the comparison from validated input. Whole-party amounts
// are never multiplied by the number of travelers. The flight estimate and
// the stay estimate are each converted once, then added, so the converted
// parts always sum to the total. It reports every overflow it finds.
func calculate(req Request, cur money.Currency, nights int, parsed []destination, rates fxrates.Source) (Comparison, []FieldError) {
	result := Comparison{
		CheckIn:      req.CheckIn,
		CheckOut:     req.CheckOut,
		Nights:       nights,
		Travelers:    req.Travelers,
		Currency:     cur.Code,
		AllComplete:  true,
		Destinations: make([]DestinationEstimate, len(parsed)),
	}
	conv := converter{source: rates, to: cur}
	var errs []FieldError
	totals := make([]int64, len(parsed))
	for i, d := range parsed {
		field := func(name string) string { return fmt.Sprintf("destinations[%d].%s", i, name) }
		estimate := DestinationEstimate{
			Label:                       d.label,
			FlightEstimate:              format(d.flightCur, d.flight),
			FlightEstimateCurrency:      d.flightCur.Code,
			NightlyStayEstimate:         format(d.nightlyCur, d.nightly),
			NightlyStayEstimateCurrency: d.nightlyCur.Code,
		}
		var flight, stay *int64
		if d.flight != nil {
			var ok bool
			if flight, ok = conv.convert(*d.flight, d.flightCur); !ok {
				errs = append(errs, FieldError{Field: field("flight_estimate"), Message: tooLargeToConvert})
			}
		}
		if d.nightly != nil {
			s, ok := money.MulInt(*d.nightly, int64(nights))
			if !ok {
				errs = append(errs, FieldError{Field: field("nightly_stay_estimate"), Message: tooLargeToCalculate})
			} else {
				estimate.StayEstimate = format(d.nightlyCur, &s)
				if stay, ok = conv.convert(s, d.nightlyCur); !ok {
					errs = append(errs, FieldError{Field: field("nightly_stay_estimate"), Message: tooLargeToConvert})
				}
			}
		}
		estimate.ConvertedFlightEstimate = format(cur, flight)
		estimate.ConvertedStayEstimate = format(cur, stay)
		if flight != nil && stay != nil {
			if total, ok := money.Add(*flight, *stay); ok {
				totals[i] = total
				estimate.FlightAndStayEstimate = format(cur, &total)
				estimate.Complete = true
			} else {
				errs = append(errs, FieldError{Field: field("flight_estimate"), Message: tooLargeToCalculate})
			}
		}
		if !estimate.Complete {
			result.AllComplete = false
		}
		result.Destinations[i] = estimate
	}
	if len(errs) > 0 {
		return Comparison{}, errs
	}
	result.ExchangeRates = conv.exchangeRates()
	if result.AllComplete {
		lowest := totals[0]
		for _, t := range totals[1:] {
			lowest = min(lowest, t)
		}
		for i := range result.Destinations {
			result.Destinations[i].LowestEstimate = totals[i] == lowest
		}
	}
	return result, nil
}

// converter converts estimates to the comparison currency. It asks its
// source for rates the first time an estimate needs converting.
type converter struct {
	source  fxrates.Source
	to      money.Currency
	loaded  bool
	rates   fxrates.Rates
	usable  bool
	perEuro map[string]string
}

// convert returns minor in the comparison currency, or nil when no rate is
// available. It reports false when the converted amount would overflow.
func (c *converter) convert(minor int64, from money.Currency) (*int64, bool) {
	if from.Code == c.to.Code {
		return &minor, true
	}
	if !c.loaded {
		c.rates, c.usable = c.source.Rates()
		c.loaded = true
		c.perEuro = map[string]string{}
	}
	if !c.usable {
		return nil, true
	}
	converted, err := c.rates.Convert(minor, from, c.to)
	if errors.Is(err, fxrates.ErrNoRate) {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	for _, code := range []string{from.Code, c.to.Code} {
		if rate, ok := c.rates.PerEuro(code); ok {
			c.perEuro[code] = rate
		}
	}
	return &converted, true
}

// exchangeRates describes the rates used, or returns nil when nothing
// needed converting.
func (c *converter) exchangeRates() *ExchangeRates {
	if !c.loaded {
		return nil
	}
	if !c.usable {
		return &ExchangeRates{PerEuro: map[string]string{}}
	}
	date := c.rates.Date
	return &ExchangeRates{Available: true, Date: &date, PerEuro: c.perEuro}
}

func format(cur money.Currency, minor *int64) *string {
	if minor == nil {
		return nil
	}
	s := cur.Format(*minor)
	return &s
}
