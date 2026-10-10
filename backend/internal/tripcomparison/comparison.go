// Package tripcomparison compares flight-plus-stay estimates for two or
// three destinations that share dates, a party size, and a currency.
//
// Every amount is a traveler-entered estimate for the whole party. This
// package validates the estimates and does all of the arithmetic; clients
// display the results as returned.
package tripcomparison

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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

// Request is the body of a trip comparison request.
type Request struct {
	CheckIn      string             `json:"check_in"`
	CheckOut     string             `json:"check_out"`
	Travelers    int                `json:"travelers"`
	Currency     string             `json:"currency"`
	Destinations []DestinationInput `json:"destinations"`
}

// DestinationInput holds one destination's estimates as decimal strings. A
// nil estimate is unknown, which is different from "0".
type DestinationInput struct {
	Label string `json:"label"`
	// FlightEstimate is the round-trip flight cost for the whole party.
	FlightEstimate *string `json:"flight_estimate"`
	// NightlyStayEstimate is the accommodation cost per night for the whole
	// party.
	NightlyStayEstimate *string `json:"nightly_stay_estimate"`
}

// Comparison is the result for a valid request. Amounts are decimal strings
// in the request currency with its exact number of decimal places; nil means
// unknown.
type Comparison struct {
	CheckIn   string `json:"check_in"`
	CheckOut  string `json:"check_out"`
	Nights    int    `json:"nights"`
	Travelers int    `json:"travelers"`
	Currency  string `json:"currency"`
	// AllComplete reports whether every destination has a total.
	AllComplete  bool                  `json:"all_complete"`
	Destinations []DestinationEstimate `json:"destinations"`
}

// DestinationEstimate is one destination's calculated estimate.
type DestinationEstimate struct {
	Label               string  `json:"label"`
	FlightEstimate      *string `json:"flight_estimate"`
	NightlyStayEstimate *string `json:"nightly_stay_estimate"`
	// StayEstimate is the nightly estimate multiplied by the number of
	// nights.
	StayEstimate *string `json:"stay_estimate"`
	// FlightAndStayEstimate is set only when both estimates are known.
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
// field error it finds, or the comparison when there are none.
func Compare(req Request) (Comparison, []FieldError) {
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
	return calculate(req, cur, nights, parsed)
}

// destination is a validated destination with estimates in minor units.
type destination struct {
	label   string
	flight  *int64
	nightly *int64
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

// destination validates one destination. Amounts are checked only when the
// currency is valid, since their precision depends on it.
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
	if curOK {
		parsed.flight = v.amount(prefix+"flight_estimate", d.FlightEstimate, cur)
		parsed.nightly = v.amount(prefix+"nightly_stay_estimate", d.NightlyStayEstimate, cur)
	}
	return parsed
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

// calculate builds the comparison from validated input. Whole-party amounts
// are never multiplied by the number of travelers.
func calculate(req Request, cur money.Currency, nights int, parsed []destination) (Comparison, []FieldError) {
	result := Comparison{
		CheckIn:      req.CheckIn,
		CheckOut:     req.CheckOut,
		Nights:       nights,
		Travelers:    req.Travelers,
		Currency:     cur.Code,
		AllComplete:  true,
		Destinations: make([]DestinationEstimate, len(parsed)),
	}
	totals := make([]int64, len(parsed))
	for i, d := range parsed {
		estimate := DestinationEstimate{
			Label:               d.label,
			FlightEstimate:      format(cur, d.flight),
			NightlyStayEstimate: format(cur, d.nightly),
		}
		var stay *int64
		if d.nightly != nil {
			s, ok := money.MulInt(*d.nightly, int64(nights))
			if !ok {
				return Comparison{}, tooLarge(i, "nightly_stay_estimate")
			}
			stay = &s
			estimate.StayEstimate = format(cur, stay)
		}
		if d.flight != nil && stay != nil {
			total, ok := money.Add(*d.flight, *stay)
			if !ok {
				return Comparison{}, tooLarge(i, "flight_estimate")
			}
			totals[i] = total
			estimate.FlightAndStayEstimate = format(cur, &total)
			estimate.Complete = true
		} else {
			result.AllComplete = false
		}
		result.Destinations[i] = estimate
	}
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

// tooLarge reports an overflow. The amount and stay limits make it
// unreachable; it exists so that arithmetic can never wrap silently.
func tooLarge(i int, field string) []FieldError {
	return []FieldError{{
		Field:   fmt.Sprintf("destinations[%d].%s", i, field),
		Message: "This estimate is too large to calculate.",
	}}
}

func format(cur money.Currency, minor *int64) *string {
	if minor == nil {
		return nil
	}
	s := cur.Format(*minor)
	return &s
}
