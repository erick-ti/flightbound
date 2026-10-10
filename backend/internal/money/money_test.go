package money

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"testing"
)

var (
	usd = Currency{Code: "USD", Digits: 2}
	jpy = Currency{Code: "JPY", Digits: 0}
	// threeDigit stands in for currencies with three minor-unit digits,
	// which the supported list does not include today.
	threeDigit = Currency{Code: "XTD", Digits: 3}
)

func TestLookupCurrency(t *testing.T) {
	tests := []struct {
		code   string
		ok     bool
		digits int
	}{
		{"USD", true, 2},
		{"EUR", true, 2},
		{"GBP", true, 2},
		{"JPY", true, 0},
		{"ISK", true, 0},
		{"KRW", true, 0},
		{"usd", false, 0},
		{"XXX", false, 0},
		{"", false, 0},
	}
	for _, tt := range tests {
		got, ok := LookupCurrency(tt.code)
		if ok != tt.ok {
			t.Errorf("LookupCurrency(%q) ok = %v, want %v", tt.code, ok, tt.ok)
			continue
		}
		if ok && (got.Code != tt.code || got.Digits != tt.digits) {
			t.Errorf("LookupCurrency(%q) = %+v, want code %s with %d digits", tt.code, got, tt.code, tt.digits)
		}
	}
}

func TestParseValid(t *testing.T) {
	tests := []struct {
		cur  Currency
		in   string
		want int64
	}{
		{usd, "0", 0},
		{usd, "600.10", 60010},
		{usd, "125.25", 12525},
		{usd, "90", 9000},
		{usd, "0.5", 50},
		{usd, "0.05", 5},
		{usd, "007.50", 750},
		{usd, "0000000000001", 100},
		{usd, "999999999999.99", 99999999999999},
		{jpy, "74000", 74000},
		{jpy, "0", 0},
		{threeDigit, "1.234", 1234},
		{threeDigit, "1.2", 1200},
	}
	for _, tt := range tests {
		got, err := tt.cur.Parse(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("%s Parse(%q) = %d, %v; want %d, nil", tt.cur.Code, tt.in, got, err, tt.want)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	tests := []struct {
		cur  Currency
		in   string
		want error
	}{
		{usd, "", ErrSyntax},
		{usd, " 1", ErrSyntax},
		{usd, "1 ", ErrSyntax},
		{usd, "-1", ErrSyntax},
		{usd, "+1", ErrSyntax},
		{usd, "1.", ErrSyntax},
		{usd, ".5", ErrSyntax},
		{usd, "1,000", ErrSyntax},
		{usd, "12,50", ErrSyntax},
		{usd, "1e3", ErrSyntax},
		{usd, "1_000", ErrSyntax},
		{usd, "0x10", ErrSyntax},
		{usd, "1.2.3", ErrSyntax},
		{usd, "١٢٣", ErrSyntax},
		{usd, "NaN", ErrSyntax},
		{usd, "12.345", ErrPrecision},
		{usd, "12.340", ErrPrecision},
		{jpy, "100.5", ErrPrecision},
		{jpy, "100.0", ErrPrecision},
		{threeDigit, "1.2345", ErrPrecision},
		{usd, "1000000000000", ErrTooLarge},
		{usd, "1000000000000.00", ErrTooLarge},
		{jpy, "99999999999999999999999", ErrTooLarge},
	}
	for _, tt := range tests {
		got, err := tt.cur.Parse(tt.in)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s Parse(%q) = %d, %v; want error %v", tt.cur.Code, tt.in, got, err, tt.want)
		}
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		cur   Currency
		minor int64
		want  string
	}{
		{usd, 97585, "975.85"},
		{usd, 110110, "1101.10"},
		{usd, 27000, "270.00"},
		{usd, 5, "0.05"},
		{usd, 0, "0.00"},
		{jpy, 74000, "74000"},
		{jpy, 0, "0"},
		{threeDigit, 1234, "1.234"},
		{threeDigit, 7, "0.007"},
	}
	for _, tt := range tests {
		if got := tt.cur.Format(tt.minor); got != tt.want {
			t.Errorf("%s Format(%d) = %q, want %q", tt.cur.Code, tt.minor, got, tt.want)
		}
	}
}

func TestAdd(t *testing.T) {
	tests := []struct {
		a, b int64
		want int64
		ok   bool
	}{
		{60010, 37575, 97585, true},
		{0, 0, 0, true},
		{math.MaxInt64 - 1, 1, math.MaxInt64, true},
		{math.MaxInt64, 1, 0, false},
		{-1, 1, 0, false},
		{1, -1, 0, false},
	}
	for _, tt := range tests {
		got, ok := Add(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Add(%d, %d) = %d, %v; want %d, %v", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}

func TestMulInt(t *testing.T) {
	tests := []struct {
		a, n int64
		want int64
		ok   bool
	}{
		{12525, 3, 37575, true},
		{0, 365, 0, true},
		{12525, 0, 0, true},
		{math.MaxInt64, 1, math.MaxInt64, true},
		{math.MaxInt64/2 + 1, 2, 0, false},
		{-1, 3, 0, false},
		{3, -1, 0, false},
	}
	for _, tt := range tests {
		got, ok := MulInt(tt.a, tt.n)
		if got != tt.want || ok != tt.ok {
			t.Errorf("MulInt(%d, %d) = %d, %v; want %d, %v", tt.a, tt.n, got, ok, tt.want, tt.ok)
		}
	}
}

var plainDecimal = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// FuzzParse checks that Parse never panics, accepts only plain decimal
// strings, agrees exactly with an independent big.Rat conversion, and
// round-trips through Format.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"0", "600.10", "125.25", "007.50", "0.05", "1.234", "999999999999.99",
		"1000000000000", "1.", ".5", "-1", "+1", "1e3", "1,000", "١٢٣", "", " 1",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, cur := range []Currency{jpy, usd, threeDigit} {
			minor, err := cur.Parse(s)
			if err != nil {
				continue
			}
			if !plainDecimal.MatchString(s) {
				t.Fatalf("%s accepted %q, which is not a plain decimal", cur.Code, s)
			}
			want, ok := new(big.Rat).SetString(s)
			if !ok {
				t.Fatalf("big.Rat rejected %q", s)
			}
			want.Mul(want, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(cur.Digits)), nil)))
			if !want.IsInt() || want.Num().Cmp(big.NewInt(minor)) != 0 {
				t.Fatalf("%s Parse(%q) = %d, want %s", cur.Code, s, minor, want.RatString())
			}
			back, err := cur.Parse(cur.Format(minor))
			if err != nil || back != minor {
				t.Fatalf("%s round trip of %q: Format gave %q, Parse gave %d, %v", cur.Code, s, cur.Format(minor), back, err)
			}
		}
	})
}
