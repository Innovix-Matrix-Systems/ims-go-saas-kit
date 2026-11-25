package money

import (
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

var (
	usd = Currency{Code: "USD", Symbol: "$", SymbolFirst: true, Precision: 2, DecimalMark: ".", ThousandsSeparator: ","}
	bdt = Currency{Code: "BDT", Symbol: "৳", SymbolFirst: true, Precision: 2, DecimalMark: ".", ThousandsSeparator: ","}
	eur = Currency{Code: "EUR", Symbol: "€", SymbolFirst: true, Precision: 2, DecimalMark: ",", ThousandsSeparator: "."}
	sek = Currency{Code: "SEK", Symbol: "kr", SymbolFirst: false, Precision: 2, DecimalMark: ",", ThousandsSeparator: " "}
	jpy = Currency{Code: "JPY", Symbol: "¥", SymbolFirst: true, Precision: 0, DecimalMark: ".", ThousandsSeparator: ","}
	kwd = Currency{Code: "KWD", Symbol: "KD", SymbolFirst: true, Precision: 3, DecimalMark: ".", ThousandsSeparator: ","}
)

func language(t *testing.T, code string) i18n.Language {
	t.Helper()
	for _, lang := range i18n.Builtin() {
		if lang.Code == code {
			return lang
		}
	}
	t.Fatalf("no language %q", code)
	return i18n.Language{}
}

func TestFormat(t *testing.T) {
	en, bn, ar := language(t, "en"), language(t, "bn"), language(t, "ar")
	cases := []struct {
		amount Amount
		lang   i18n.Language
		want   string
	}{
		{Of(123450, usd), en, "$1,234.50"},
		{Of(5, usd), en, "$0.05"},
		{Of(0, usd), en, "$0.00"},
		{Of(-123450, usd), en, "-$1,234.50"},
		{Of(123456789, bdt), en, "৳1,234,567.89"},
		{Of(123450, bdt), bn, "৳১,২৩৪.৫০"},
		{Of(123450, eur), en, "€1.234,50"},
		{Of(123450, sek), en, "1 234,50 kr"},
		{Of(1234, jpy), en, "¥1,234"},
		{Of(1234567, kwd), en, "KD 1,234.567"},
		{Of(123450, usd), ar, "$1,234.50"}, // Arabic uses Western digits by default
	}
	for _, tc := range cases {
		if got := tc.amount.Format(tc.lang); got != tc.want {
			t.Errorf("%s %d in %s = %q, want %q", tc.amount.Currency.Code, tc.amount.Minor, tc.lang.Code, got, tc.want)
		}
	}
}

func TestCodeAndDecimal(t *testing.T) {
	cases := []struct {
		amount        Amount
		code, decimal string
	}{
		{Of(123450, bdt), "BDT 1,234.50", "1234.50"},
		{Of(123450, eur), "EUR 1.234,50", "1234.50"},
		{Of(1234, jpy), "JPY 1,234", "1234"},
		{Of(7, kwd), "KWD 0.007", "0.007"},
		{Of(-250, usd), "-USD 2.50", "-2.50"},
	}
	for _, tc := range cases {
		if got := tc.amount.Code(); got != tc.code {
			t.Errorf("Code() = %q, want %q", got, tc.code)
		}
		if got := tc.amount.Decimal(); got != tc.decimal {
			t.Errorf("Decimal() = %q, want %q", got, tc.decimal)
		}
	}
}

func TestFallback(t *testing.T) {
	if got := Of(150000, Fallback("gbp")).Format(language(t, "en")); got != "GBP 1,500.00" {
		t.Fatalf("fallback = %q", got)
	}
}
