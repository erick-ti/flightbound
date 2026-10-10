// Package fxrates converts amounts between supported currencies with the
// European Central Bank's euro foreign exchange reference rates.
//
// The ECB publishes each rate as the amount of a currency that one euro
// buys. Conversions go through the euro with exact rational arithmetic and
// round once, to the target currency's minor unit.
package fxrates

import (
	"errors"
	"math/big"

	"github.com/erick-ti/flightbound/backend/internal/money"
)

// Rates is one day of reference rates. A Rates value is safe for concurrent
// use; nothing modifies it after parsing.
type Rates struct {
	// Date is the ECB reference date, as YYYY-MM-DD.
	Date string
	// perEuro holds each currency's exact rate. The euro is implied as 1.
	perEuro map[string]*big.Rat
	// published holds each rate exactly as the ECB wrote it.
	published map[string]string
}

var (
	// ErrNoRate reports a currency without a rate on the reference date.
	ErrNoRate = errors.New("no reference rate for the currency")
	// ErrTooLarge reports a converted amount outside the int64 range.
	ErrTooLarge = errors.New("the converted amount is too large")

	errNegative = errors.New("cannot convert a negative amount")
)

// PerEuro returns the published rate of a currency other than the euro, such
// as "1.1206" for USD.
func (r Rates) PerEuro(code string) (string, bool) {
	s, ok := r.published[code]
	return s, ok
}

func (r Rates) rate(code string) (*big.Rat, bool) {
	if code == "EUR" {
		return big.NewRat(1, 1), true
	}
	rate, ok := r.perEuro[code]
	return rate, ok
}

// Convert converts a non-negative amount from one currency's minor units to
// another's. The exact result is rounded to the nearest minor unit, with
// halves rounded up. Converting to the same currency needs no rate and
// returns the amount unchanged.
func (r Rates) Convert(minor int64, from, to money.Currency) (int64, error) {
	if minor < 0 {
		return 0, errNegative
	}
	if from.Code == to.Code {
		return minor, nil
	}
	fromRate, ok := r.rate(from.Code)
	if !ok {
		return 0, ErrNoRate
	}
	toRate, ok := r.rate(to.Code)
	if !ok {
		return 0, ErrNoRate
	}
	// minor / 10^from.Digits / fromRate * toRate * 10^to.Digits, written as
	// one fraction of integers, num/den.
	num := big.NewInt(minor)
	num.Mul(num, toRate.Num())
	num.Mul(num, fromRate.Denom())
	num.Mul(num, pow10(to.Digits))
	den := new(big.Int).Mul(toRate.Denom(), fromRate.Num())
	den.Mul(den, pow10(from.Digits))
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	if rem.Lsh(rem, 1).Cmp(den) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, ErrTooLarge
	}
	return q.Int64(), nil
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}
