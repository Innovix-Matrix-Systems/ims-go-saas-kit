package httpx

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
)

// Kit is what every module handler embeds: the Responder methods (Data,
// Error, NoContent, Paginated, ...) plus Bind and PathID. It turns the usual
// "decode, validate, write the 4xx" dance into one line:
//
//	var req StoreRequest
//	if !h.Bind(w, r, &req) {
//		return // the 400/415/422 problem is already written
//	}
type Kit struct {
	*Responder
	binder    *Binder
	validator *Validator
}

// NewKit bundles the shared HTTP helpers for a handler.
func NewKit(binder *Binder, validator *Validator, responder *Responder) Kit {
	return Kit{Responder: responder, binder: binder, validator: validator}
}

// Bind decodes the JSON body into dst and validates its `validate` tags. On
// failure it writes the problem response and returns false.
func (k Kit) Bind(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := k.binder.JSON(w, r, dst); err != nil {
		k.Error(w, r, err)
		return false
	}
	return k.Validate(w, r, dst)
}

// Validate checks the `validate` tags of an already-filled struct (query or
// multipart input). On failure it writes the 422 and returns false.
func (k Kit) Validate(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := k.validator.Struct(r.Context(), dst); err != nil {
		k.Error(w, r, err)
		return false
	}
	return true
}

// PathID reads a positive integer route parameter ({id}). On a bad value it
// writes a 400 and returns false.
func (k Kit) PathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := ParseID(chi.URLParam(r, name))
	if err != nil {
		k.Error(w, r, err)
		return 0, false
	}
	return id, true
}

// ParseID parses a positive integer identifier.
func ParseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.InvalidInput, "The provided identifier is invalid.")
	}
	return id, nil
}
