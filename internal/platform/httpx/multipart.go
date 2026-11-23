package httpx

import (
	"errors"
	"mime/multipart"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
)

// multipartMemory caps the in-memory part of a parsed multipart body (H22).
const multipartMemory = 8 << 20

// Multipart parses a multipart/form-data request body with a per-route byte
// limit (the global JSON limit does not apply to multipart routes).
func Multipart(w http.ResponseWriter, r *http.Request, maxBytes int64) (*multipart.Form, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	// Keep at most 8 MB of parts in memory; larger files spill to temp files
	// (removed by net/http after the handler) instead of pinning the heap.
	if err := r.ParseMultipartForm(min(maxBytes, multipartMemory)); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, apperr.New(apperr.RequestEntityTooLarge, "The uploaded payload is too large.")
		}
		if errors.Is(err, http.ErrNotMultipart) {
			return nil, apperr.New(apperr.UnsupportedMediaType, "The request must be multipart/form-data.")
		}
		return nil, apperr.New(apperr.MalformedRequest, "The multipart payload is malformed.")
	}
	return r.MultipartForm, nil
}

// FormFile returns the single uploaded file for a form field, or a validation
// error when the field is missing.
func FormFile(form *multipart.Form, field string) (*multipart.FileHeader, error) {
	if form == nil || len(form.File[field]) == 0 {
		return nil, apperr.Validation(map[string][]string{
			field: {"The " + field + " field is required."},
		})
	}
	return form.File[field][0], nil
}
