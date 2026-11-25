// Package money writes amounts the way their currency says (Akaunting
// style): the symbol and where it goes, the decimals and the separators come
// from the currency's row; only the digits follow the reader's language.
// Amounts are integers in the currency's minor units (cents, paisa) and are
// never converted between currencies.
package money

import (
	"strconv"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

// Currency is how one currency is written (a row of the currencies table).
type Currency struct {
	Code               string `json:"code"`
	Name               string `json:"name"`
	Symbol             string `json:"symbol"`
	SymbolFirst        bool   `json:"symbol_first"`
	Precision          int    `json:"precision"`
	DecimalMark        string `json:"decimal_mark"`
	ThousandsSeparator string `json:"thousands_separator"`
}

// Fallback is a currency with no row (deleted, or never added): its code as
// the symbol, two decimals, "1,234.00".
func Fallback(code string) Currency {
	code = strings.ToUpper(strings.TrimSpace(code))
	return Currency{Code: code, Name: code, Symbol: code, SymbolFirst: true, Precision: 2, DecimalMark: ".", ThousandsSeparator: ","}
}

// Amount is minor units in a currency.
type Amount struct {
	Minor    int64    `json:"minor"`
	Currency Currency `json:"currency"`
}

// Of pairs minor units with their currency.
func Of(minor int64, currency Currency) Amount { return Amount{Minor: minor, Currency: currency} }

// Format writes the amount with its currency's symbol, position, decimals
// and separators, in the language's digits: "$1,234.50", "€1.234,50",
// "৳১,২৩৪.৫০".
func (a Amount) Format(lang i18n.Language) string {
	return localDigits(a.compose(a.Currency.Symbol), lang)
}

// Code writes the amount with the ISO code instead of the symbol, in Latin
// digits ("BDT 1,234.50"): for documents whose fonts can't draw every symbol
// (the PDF invoice).
func (a Amount) Code() string {
	c := a.Currency
	c.Symbol, c.SymbolFirst = c.Code, true
	return Amount{Minor: a.Minor, Currency: c}.compose(c.Symbol)
}

// Decimal is the plain number with a dot and no grouping ("1234.50"), for
// gateway payloads.
func (a Amount) Decimal() string {
	whole, fraction, negative := a.split()
	out := whole
	if fraction != "" {
		out += "." + fraction
	}
	if negative {
		out = "-" + out
	}
	return out
}

func (a Amount) compose(symbol string) string {
	whole, fraction, negative := a.split()
	number := group(whole, a.Currency.ThousandsSeparator)
	if fraction != "" {
		number += a.Currency.DecimalMark + fraction
	}
	// A symbol after the number is always spaced ("1.234,50 €"); before it,
	// only a symbol of several characters is ("CHF 1,234.50", "$1,234.50").
	out := number + " " + symbol
	if a.Currency.SymbolFirst {
		out = symbol + number
		if len([]rune(symbol)) > 1 {
			out = symbol + " " + number
		}
	}
	if negative {
		out = "-" + out
	}
	return out
}

// split returns the whole and fractional digits of the absolute amount.
func (a Amount) split() (whole, fraction string, negative bool) {
	minor := a.Minor
	if minor < 0 {
		negative, minor = true, -minor
	}
	precision := a.Currency.Precision
	if precision <= 0 {
		return strconv.FormatInt(minor, 10), "", negative
	}
	digits := strconv.FormatInt(minor, 10)
	if len(digits) <= precision {
		digits = strings.Repeat("0", precision-len(digits)+1) + digits
	}
	cut := len(digits) - precision
	return digits[:cut], digits[cut:], negative
}

func group(digits, separator string) string {
	if separator == "" || len(digits) <= 3 {
		return digits
	}
	var b strings.Builder
	head := len(digits) % 3
	if head > 0 {
		b.WriteString(digits[:head])
	}
	for i := head; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteString(separator)
		}
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}

// localDigits writes ASCII digits in the language's numbering system.
func localDigits(s string, lang i18n.Language) string {
	if lang.Digits == "" || lang.Digits == "latn" {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteString(lang.FormatNumber(int64(r - '0')))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
