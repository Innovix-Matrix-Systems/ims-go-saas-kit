package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
)

// Binder decodes and constrains JSON request bodies.
type Binder struct {
	maxBodyBytes int64
	strictFields bool
}

// NewBinder creates a binder with the given body-size limit. When strictFields
// is true, unknown JSON properties are rejected with a 422 validation error.
func NewBinder(maxBodyBytes int64, strictFields bool) *Binder {
	return &Binder{maxBodyBytes: maxBodyBytes, strictFields: strictFields}
}

// JSON decodes the request body into dst and maps transport-level failures to
// application errors: 415 for unsupported content types, 413 for oversized
// bodies, 400 for malformed JSON and 422 for unknown fields.
func (b *Binder) JSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if err := checkContentType(r); err != nil {
		return err
	}

	reader := http.MaxBytesReader(w, r.Body, b.maxBodyBytes)
	defer func() { _ = reader.Close() }()

	decoder := json.NewDecoder(reader)
	if b.strictFields {
		decoder.DisallowUnknownFields()
	}

	if err := decoder.Decode(dst); err != nil {
		return decodeError(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperr.New(apperr.MalformedJSON, "The request body must contain a single JSON document.")
	}
	return nil
}

func checkContentType(r *http.Request) error {
	raw := r.Header.Get("Content-Type")
	if raw == "" {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return apperr.New(apperr.UnsupportedMediaType, "The Content-Type header is invalid.")
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
		return nil
	}
	return apperr.New(apperr.UnsupportedMediaType, "The request content type must be application/json.")
}

func decodeError(err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return apperr.New(apperr.RequestEntityTooLarge, "The request body is too large.")
	}
	if errors.Is(err, io.EOF) {
		return apperr.New(apperr.MalformedRequest, "A request body is required.")
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return apperr.Wrap(apperr.MalformedJSON, fmt.Sprintf("Malformed JSON at byte %d.", syntaxErr.Offset), err)
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return apperr.Newf(apperr.MalformedJSON, "The field %q contains an invalid value.", typeErr.Field)
	}
	if field, ok := unknownField(err); ok {
		return apperr.Validation(map[string][]string{
			field: {fmt.Sprintf("The %s field is not allowed.", field)},
		})
	}
	return apperr.Wrap(apperr.MalformedJSON, "The request body is not valid JSON.", err)
}

func unknownField(err error) (string, bool) {
	const prefix = "json: unknown field "
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		return "", false
	}
	name := strings.Trim(strings.TrimPrefix(msg, prefix), `"`)
	if name == "" {
		return "", false
	}
	return name, true
}
