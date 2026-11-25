package currencies

import (
	"strings"
	"time"
)

// UpdateRequest is how a currency is written (PATCH /currencies/{id}).
type UpdateRequest struct {
	Name               string `json:"name" validate:"required,max=100"`
	Symbol             string `json:"symbol" validate:"required,max=10"`
	SymbolFirst        bool   `json:"symbol_first"`
	Precision          int    `json:"precision" validate:"gte=0,lte=4"`
	DecimalMark        string `json:"decimal_mark" validate:"required,max=3"`
	ThousandsSeparator string `json:"thousands_separator" validate:"max=3"`
	IsEnabled          *bool  `json:"is_enabled"`
}

// CreateRequest adds a currency (POST /currencies); the code is set once.
type CreateRequest struct {
	Code string `json:"code" validate:"required,len=3,alpha,uppercase"`
	UpdateRequest
}

func (req UpdateRequest) toInput() Input {
	enabled := true
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}
	return Input{
		Name:               strings.TrimSpace(req.Name),
		Symbol:             strings.TrimSpace(req.Symbol),
		SymbolFirst:        req.SymbolFirst,
		Precision:          req.Precision,
		DecimalMark:        req.DecimalMark,
		ThousandsSeparator: req.ThousandsSeparator,
		IsEnabled:          enabled,
	}
}

func (req CreateRequest) toInput() Input {
	input := req.UpdateRequest.toInput()
	input.Code = req.Code
	return input
}

// Response is a currency for the staff screen.
type Response struct {
	ID                 int64     `json:"id"`
	Code               string    `json:"code"`
	Name               string    `json:"name"`
	Symbol             string    `json:"symbol"`
	SymbolFirst        bool      `json:"symbol_first"`
	Precision          int       `json:"precision"`
	DecimalMark        string    `json:"decimal_mark"`
	ThousandsSeparator string    `json:"thousands_separator"`
	IsEnabled          bool      `json:"is_enabled"`
	IsDefault          bool      `json:"is_default"`
	InUse              bool      `json:"in_use"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// NewResponse maps a currency to its staff representation.
func NewResponse(c Currency) Response {
	return Response{
		ID: c.ID, Code: c.Code, Name: c.Name, Symbol: c.Symbol, SymbolFirst: c.SymbolFirst,
		Precision: c.Precision, DecimalMark: c.DecimalMark, ThousandsSeparator: c.ThousandsSeparator,
		IsEnabled: c.IsEnabled, IsDefault: c.IsDefault, InUse: c.InUse, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// PublicCurrency is how a client writes amounts in one currency.
type PublicCurrency struct {
	Code               string `json:"code"`
	Name               string `json:"name"`
	Symbol             string `json:"symbol"`
	SymbolFirst        bool   `json:"symbol_first"`
	Precision          int    `json:"precision"`
	DecimalMark        string `json:"decimal_mark"`
	ThousandsSeparator string `json:"thousands_separator"`
}

// PublicResponse is GET /public/currencies: the enabled currencies and the
// default (the currency of plan prices and new payments).
type PublicResponse struct {
	Default    string           `json:"default"`
	Currencies []PublicCurrency `json:"currencies"`
}

// NewPublicResponse maps the enabled currencies.
func NewPublicResponse(items []Currency) PublicResponse {
	out := PublicResponse{Currencies: make([]PublicCurrency, 0, len(items))}
	for _, c := range items {
		if c.IsDefault {
			out.Default = c.Code
		}
		out.Currencies = append(out.Currencies, PublicCurrency{
			Code: c.Code, Name: c.Name, Symbol: c.Symbol, SymbolFirst: c.SymbolFirst, Precision: c.Precision,
			DecimalMark: c.DecimalMark, ThousandsSeparator: c.ThousandsSeparator,
		})
	}
	return out
}
