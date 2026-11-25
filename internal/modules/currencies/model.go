// Package currencies manages how amounts are written (Akaunting style): one
// row per currency with its symbol, position, decimals and separators, one of
// them the default. Amounts are stored in minor units and never converted;
// changing the default changes the label, not the numbers.
package currencies

import (
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/money"
)

// Currency is a row of the currencies table.
type Currency struct {
	ID                 int64
	Code               string
	Name               string
	Symbol             string
	SymbolFirst        bool
	Precision          int
	DecimalMark        string
	ThousandsSeparator string
	IsEnabled          bool
	IsDefault          bool
	// InUse is true when payments are recorded in this currency (only
	// filled by List and Get).
	InUse     bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Money is the currency's format for the money package.
func (c Currency) Money() money.Currency {
	return money.Currency{
		Code: c.Code, Name: c.Name, Symbol: c.Symbol, SymbolFirst: c.SymbolFirst, Precision: c.Precision,
		DecimalMark: c.DecimalMark, ThousandsSeparator: c.ThousandsSeparator,
	}
}

func mapCurrency(row dbgen.Currency) Currency {
	return Currency{
		ID:                 row.ID,
		Code:               row.Code,
		Name:               row.Name,
		Symbol:             row.Symbol,
		SymbolFirst:        row.SymbolFirst,
		Precision:          int(row.Precision),
		DecimalMark:        row.DecimalMark,
		ThousandsSeparator: row.ThousandsSeparator,
		IsEnabled:          row.IsEnabled,
		IsDefault:          row.IsDefault,
		CreatedAt:          row.CreatedAt.Time,
		UpdatedAt:          row.UpdatedAt.Time,
	}
}

// Seeds are the currencies every install starts with; the first is the
// default of a new install (the migration seeds the same rows).
var Seeds = []Currency{
	{Code: "USD", Name: "US Dollar", Symbol: "$", SymbolFirst: true, Precision: 2, DecimalMark: ".", ThousandsSeparator: ",", IsDefault: true},
	{Code: "BDT", Name: "Bangladeshi Taka", Symbol: "৳", SymbolFirst: true, Precision: 2, DecimalMark: ".", ThousandsSeparator: ","},
	{Code: "EUR", Name: "Euro", Symbol: "€", SymbolFirst: true, Precision: 2, DecimalMark: ",", ThousandsSeparator: "."},
}
