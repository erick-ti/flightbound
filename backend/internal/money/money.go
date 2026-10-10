// Package money handles exact amounts in a currency's minor units.
//
// An amount is an int64 count of the currency's smallest unit (cents for
// USD, yen for JPY), so arithmetic never rounds. Parsing rejects input that
// would need rounding instead of silently changing the value.
package money

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Currency is an ISO 4217 currency and its number of minor-unit digits.
type Currency struct {
	Code   string
	Digits int
}

// supported lists the comparison currencies: the euro plus the currencies in
// the European Central Bank's daily euro reference rates. Minor-unit digits
// come from ISO 4217 List One.
var supported = map[string]int{
	"AUD": 2, "BRL": 2, "CAD": 2, "CHF": 2, "CNY": 2, "CZK": 2,
	"DKK": 2, "EUR": 2, "GBP": 2, "HKD": 2, "HUF": 2, "IDR": 2,
	"ILS": 2, "INR": 2, "ISK": 0, "JPY": 0, "KRW": 0, "MXN": 2,
	"MYR": 2, "NOK": 2, "NZD": 2, "PHP": 2, "PLN": 2, "RON": 2,
	"SEK": 2, "SGD": 2, "THB": 2, "TRY": 2, "USD": 2, "ZAR": 2,
}

// LookupCurrency returns the supported currency for an uppercase ISO 4217
// code.
func LookupCurrency(code string) (Currency, bool) {
	digits, ok := supported[code]
	if !ok {
		return Currency{}, false
	}
	return Currency{Code: code, Digits: digits}, true
}

// MaxIntegerDigits limits the whole-unit part of an amount. It keeps an
// amount multiplied by a stay length of a few hundred nights, plus another
// amount, far inside the int64 range; Add and MulInt still check.
const MaxIntegerDigits = 12

var (
	// ErrSyntax reports input that is not ASCII digits with an optional
	// decimal point, such as "1,000", "-5", "1e3", ".5", or "5.".
	ErrSyntax = errors.New("amount must be digits with an optional decimal point")
	// ErrPrecision reports more decimal places than the currency allows.
	ErrPrecision = errors.New("amount has more decimal places than the currency allows")
	// ErrTooLarge reports more than MaxIntegerDigits whole-unit digits.
	ErrTooLarge = errors.New("amount is too large")
)

// Parse converts a decimal string such as "125.25" into minor units. It
// accepts only ASCII digits, optionally followed by a decimal point and at
// least one more digit. Leading zeros are allowed. Input with more decimal
// places than the currency allows is rejected, never rounded.
func (c Currency) Parse(s string) (int64, error) {
	whole, frac, hasPoint := strings.Cut(s, ".")
	if whole == "" || (hasPoint && frac == "") || !isDigits(whole) || !isDigits(frac) {
		return 0, ErrSyntax
	}
	if len(frac) > c.Digits {
		return 0, ErrPrecision
	}
	whole = strings.TrimLeft(whole, "0")
	if len(whole) > MaxIntegerDigits {
		return 0, ErrTooLarge
	}
	digits := whole + frac + strings.Repeat("0", c.Digits-len(frac))
	if digits == "" {
		return 0, nil
	}
	minor, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, ErrTooLarge
	}
	return minor, nil
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Format renders minor units as a decimal string with exactly the currency's
// number of decimal places, such as "975.85" for USD or "74000" for JPY.
func (c Currency) Format(minor int64) string {
	digits := strconv.FormatInt(minor, 10)
	sign := ""
	if minor < 0 {
		sign, digits = "-", digits[1:]
	}
	if c.Digits <= 0 {
		return sign + digits
	}
	if pad := c.Digits + 1 - len(digits); pad > 0 {
		digits = strings.Repeat("0", pad) + digits
	}
	cut := len(digits) - c.Digits
	return sign + digits[:cut] + "." + digits[cut:]
}

// Add returns a+b for non-negative amounts. It reports false if either
// amount is negative or the sum would overflow.
func Add(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}

// Sub returns a-b for non-negative amounts. It reports false if either
// amount is negative or b is larger than a.
func Sub(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || b > a {
		return 0, false
	}
	return a - b, true
}

// MulInt returns a*n for a non-negative amount and count. It reports false
// if either is negative or the product would overflow.
func MulInt(a, n int64) (int64, bool) {
	if a < 0 || n < 0 || (n != 0 && a > math.MaxInt64/n) {
		return 0, false
	}
	return a * n, true
}
