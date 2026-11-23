package httpx

import (
	"net/http"
	"strconv"
	"strings"
)

// Default pagination bounds shared by list endpoints.
const (
	DefaultPerPage = 10
	MaxPerPage     = 100
	// MaxPage caps the page number so the computed offset always fits an int32
	// (100_000_000 here) and can never overflow downstream SQL parameters.
	MaxPage = 1_000_000
)

// PageParams extracts page/per_page query parameters with sane bounds and
// returns the SQL offset for the requested page.
func PageParams(r *http.Request) (page, perPage, offset int) {
	page = queryInt(r, "page", 1)
	if page < 1 {
		page = 1
	}
	if page > MaxPage {
		page = MaxPage
	}
	perPage = queryInt(r, "per_page", DefaultPerPage)
	if perPage < 1 {
		perPage = DefaultPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}
	return page, perPage, (page - 1) * perPage
}

// OrderParams validates order_by/order_direction against a whitelist and
// returns the values passed to SQL order hooks. Invalid input falls back to
// the first allowed column and "desc".
func OrderParams(r *http.Request, allowed ...string) (orderBy, direction string) {
	if len(allowed) == 0 {
		return "id", "desc"
	}
	orderBy = r.URL.Query().Get("order_by")
	valid := false
	for _, candidate := range allowed {
		if strings.EqualFold(orderBy, candidate) {
			orderBy = candidate
			valid = true
			break
		}
	}
	if !valid {
		orderBy = allowed[0]
	}

	direction = strings.ToLower(r.URL.Query().Get("order_direction"))
	if direction != "asc" && direction != "desc" {
		direction = "desc"
	}
	return orderBy, direction
}

func queryInt(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return value
}
