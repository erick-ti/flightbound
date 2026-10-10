package fxrates

import (
	"errors"
	"math"
	"math/big"
	"sync"
	"testing"

	"github.com/erick-ti/flightbound/backend/internal/money"
)

func currency(t testing.TB, code string) money.Currency {
	t.Helper()
	c, ok := money.LookupCurrency(code)
	if !ok {
		t.Fatalf("unsupported currency %s", code)
	}
	return c
}

func TestConvert(t *testing.T) {
	rates := fixtureRates(t)
	tests := []struct {
		amount, from, to, want string
	}{
		{"330.00", "EUR", "USD", "369.80"}, // 369.798
		{"600.10", "USD", "EUR", "535.52"}, // 535.51668...
		{"54000", "JPY", "USD", "341.22"},  // 341.22251...
		{"600.10", "USD", "JPY", "94969"},  // 94968.529...
		{"250.00", "GBP", "USD", "330.51"}, // 330.50977...
		{"0", "EUR", "USD", "0.00"},
		{"0", "USD", "JPY", "0"},
	}
	for _, tt := range tests {
		from, to := currency(t, tt.from), currency(t, tt.to)
		minor, err := from.Parse(tt.amount)
		if err != nil {
			t.Fatal(err)
		}
		got, err := rates.Convert(minor, from, to)
		if err != nil || to.Format(got) != tt.want {
			t.Errorf("%s %s to %s = %s, %v; want %s", tt.from, tt.amount, tt.to, to.Format(got), err, tt.want)
		}
	}
}

func TestConvertRoundsHalfUp(t *testing.T) {
	rates := mustParse(t, ecbDoc("2026-10-09", "USD", "1.5"))
	eur, usd := currency(t, "EUR"), currency(t, "USD")
	tests := []struct {
		cents, want int64
	}{
		{1, 2}, // 1.5 cents
		{2, 3}, // exactly 3 cents
		{3, 5}, // 4.5 cents rounds up; half to even would give 4
		{5, 8}, // 7.5 cents
	}
	for _, tt := range tests {
		if got, err := rates.Convert(tt.cents, eur, usd); err != nil || got != tt.want {
			t.Errorf("EUR %d cents = %d USD cents, %v; want %d", tt.cents, got, err, tt.want)
		}
	}
}

func TestConvertSameCurrencyNeedsNoRates(t *testing.T) {
	var none Rates
	for _, code := range []string{"USD", "EUR", "JPY"} {
		c := currency(t, code)
		if got, err := none.Convert(12345, c, c); err != nil || got != 12345 {
			t.Errorf("%s to %s = %d, %v; want 12345", code, code, got, err)
		}
	}
}

func TestConvertWithoutARate(t *testing.T) {
	rates := mustParse(t, ecbDoc("2026-10-09", "USD", "1.1206"))
	eur, usd, jpy := currency(t, "EUR"), currency(t, "USD"), currency(t, "JPY")
	if got, err := rates.Convert(1000, eur, usd); err != nil || got != 1121 {
		t.Errorf("EUR 10.00 to USD = %d, %v; want 1121", got, err)
	}
	for _, pair := range [][2]money.Currency{{jpy, usd}, {usd, jpy}, {eur, jpy}} {
		if _, err := rates.Convert(1000, pair[0], pair[1]); !errors.Is(err, ErrNoRate) {
			t.Errorf("%s to %s: err = %v, want ErrNoRate", pair[0].Code, pair[1].Code, err)
		}
	}
}

func TestConvertTooLarge(t *testing.T) {
	rates := fixtureRates(t)
	eur, idr, krw := currency(t, "EUR"), currency(t, "IDR"), currency(t, "KRW")
	// The largest stay a comparison accepts: a 12-digit nightly amount for
	// 365 nights.
	stay, ok := money.MulInt(99999999999999, 365)
	if !ok {
		t.Fatal("largest stay overflowed")
	}
	if _, err := rates.Convert(stay, eur, idr); !errors.Is(err, ErrTooLarge) {
		t.Errorf("largest stay in IDR: err = %v, want ErrTooLarge", err)
	}
	if _, err := rates.Convert(stay, eur, krw); err != nil {
		t.Errorf("largest stay in KRW: err = %v, want it to fit", err)
	}
}

func TestConvertRejectsNegativeAmounts(t *testing.T) {
	rates := fixtureRates(t)
	eur, usd := currency(t, "EUR"), currency(t, "USD")
	if _, err := rates.Convert(1, eur, usd); err != nil {
		t.Fatalf("EUR 0.01 to USD: %v", err)
	}
	if _, err := rates.Convert(-1, eur, usd); err == nil {
		t.Error("converted a negative amount")
	}
}

func TestConvertIsSafeForConcurrentUse(t *testing.T) {
	rates := fixtureRates(t)
	jpy, usd := currency(t, "JPY"), currency(t, "USD")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if got, err := rates.Convert(54000, jpy, usd); err != nil || got != 34122 {
				t.Errorf("JPY 54000 to USD = %d, %v; want 34122", got, err)
			}
		})
	}
	wg.Wait()
}

// exact returns the unrounded conversion of minor, computed with big.Rat.
func exact(t testing.TB, rates Rates, minor int64, from, to money.Currency) *big.Rat {
	t.Helper()
	perEuro := func(c money.Currency) *big.Rat {
		if c.Code == "EUR" {
			return big.NewRat(1, 1)
		}
		published, ok := rates.PerEuro(c.Code)
		if !ok {
			t.Fatalf("no rate for %s", c.Code)
		}
		r, ok := new(big.Rat).SetString(published)
		if !ok {
			t.Fatalf("bad rate %q", published)
		}
		return r
	}
	scale := func(digits int) *big.Rat {
		return new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil))
	}
	v := new(big.Rat).SetInt64(minor)
	v.Quo(v, scale(from.Digits))
	v.Quo(v, perEuro(from))
	v.Mul(v, perEuro(to))
	return v.Mul(v, scale(to.Digits))
}

func FuzzConvert(f *testing.F) {
	rates := fixtureRates(f)
	codes := []string{"EUR", "USD", "JPY", "GBP", "IDR", "KRW", "ISK", "CHF", "ZAR"}
	f.Add(int64(33000), uint8(0), uint8(1))
	f.Add(int64(60010), uint8(1), uint8(2))
	f.Add(int64(54000), uint8(2), uint8(1))
	f.Add(int64(99999999999999*365), uint8(0), uint8(4))
	f.Add(int64(math.MaxInt64-1), uint8(4), uint8(2))
	half := big.NewRat(1, 2)
	// Rounding half up gives more than MaxInt64 exactly when the exact value
	// is at least MaxInt64 + 1/2.
	limit := new(big.Rat).Add(new(big.Rat).SetInt64(math.MaxInt64), half)
	f.Fuzz(func(t *testing.T, minor int64, fromIndex, toIndex uint8) {
		if minor < 0 || minor == math.MaxInt64 {
			return
		}
		from := currency(t, codes[int(fromIndex)%len(codes)])
		to := currency(t, codes[int(toIndex)%len(codes)])
		want := exact(t, rates, minor, from, to)
		got, err := rates.Convert(minor, from, to)
		if errors.Is(err, ErrTooLarge) {
			if want.Cmp(limit) < 0 {
				t.Fatalf("%d %s to %s reported too large; exact %s", minor, from.Code, to.Code, want.FloatString(4))
			}
			return
		}
		if err != nil {
			t.Fatalf("%d %s to %s: %v", minor, from.Code, to.Code, err)
		}
		// Half up means got - 1/2 <= exact < got + 1/2.
		g := new(big.Rat).SetInt64(got)
		if want.Cmp(new(big.Rat).Sub(g, half)) < 0 || want.Cmp(new(big.Rat).Add(g, half)) >= 0 {
			t.Fatalf("%d %s to %s = %d; exact %s", minor, from.Code, to.Code, got, want.FloatString(4))
		}
		if next, err := rates.Convert(minor+1, from, to); err == nil && next < got {
			t.Fatalf("%s to %s decreased: %d gives %d, %d gives %d", from.Code, to.Code, minor, got, minor+1, next)
		}
	})
}
