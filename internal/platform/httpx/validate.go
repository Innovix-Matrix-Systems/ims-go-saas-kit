package httpx

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/i18n"
)

// Validator validates request structs and maps failures to readable,
// translated field errors (dot paths, translated messages, no raw tags).
type Validator struct {
	v *validator.Validate
}

// NewValidator creates a validator that reports JSON field names.
func NewValidator() *Validator {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		if name == "" {
			return fld.Name
		}
		return name
	})
	return &Validator{v: v}
}

// Struct validates dst and returns an apperr.Validation error on failure.
// Messages are translated for the context locale via the validation.* catalog
// keys (e.g. validation.min.string with the :attribute label from
// validation.attributes.*).
func (val *Validator) Struct(ctx context.Context, dst any) error {
	err := val.v.Struct(dst)
	if err == nil {
		return nil
	}
	var fieldErrors validator.ValidationErrors
	if !errors.As(err, &fieldErrors) {
		return apperr.Wrap(apperr.InternalServerError, "Validation failed unexpectedly.", err)
	}

	rootName := rootTypeName(dst)
	fields := make(map[string][]string, len(fieldErrors))
	for _, fe := range fieldErrors {
		key := normalizeFieldNamespace(fe.Namespace(), rootName)
		fields[key] = append(fields[key], messageFor(ctx, fe, key))
	}
	return apperr.Validation(fields)
}

// messageFor maps a validator failure to a translated message.
func messageFor(ctx context.Context, fe validator.FieldError, field string) string {
	key, params := messageSpec(fe, attributeLabel(ctx, field))
	return i18n.TOr(ctx, key, fallbackMessage(fe, field), params)
}

// messageSpec maps validator tags to message keys and template params.
func messageSpec(fe validator.FieldError, attribute string) (string, map[string]any) {
	params := map[string]any{"Attribute": attribute}
	switch fe.Tag() {
	case "required", "email", "url", "uuid", "date", "boolean", "numeric", "integer":
		return "validation." + fe.Tag(), params
	case "min":
		params["Min"] = fe.Param()
		return "validation.min." + kindSuffix(fe.Kind()), params
	case "max":
		params["Max"] = fe.Param()
		return "validation.max." + kindSuffix(fe.Kind()), params
	case "len":
		params["Size"] = fe.Param()
		return "validation.size." + kindSuffix(fe.Kind()), params
	case "gt", "gte", "lt", "lte":
		params["Value"] = fe.Param()
		return "validation." + fe.Tag() + "." + kindSuffix(fe.Kind()), params
	case "oneof":
		return "validation.in", params
	default:
		return "validation.invalid", params
	}
}

func kindSuffix(kind reflect.Kind) string {
	switch kind {
	case reflect.Slice, reflect.Array, reflect.Map:
		return "array"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "numeric"
	default:
		return "string"
	}
}

// attributeLabel resolves the :attribute display name: validation.attributes.*
// for the full field path or its last segment, otherwise the humanized field.
func attributeLabel(ctx context.Context, field string) string {
	candidates := []string{field}
	if idx := strings.LastIndex(field, "."); idx >= 0 {
		candidates = append(candidates, field[idx+1:])
	}
	for _, candidate := range candidates {
		if label := i18n.T(ctx, "validation.attributes."+candidate, nil); label != "" {
			return label
		}
	}
	return displayField(field)
}

// fallbackMessage keeps readable English text if a catalog key is missing.
func fallbackMessage(fe validator.FieldError, field string) string {
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("The %s field is required.", displayField(field))
	default:
		return fmt.Sprintf("The %s field is invalid.", displayField(field))
	}
}

func rootTypeName(dst any) string {
	t := reflect.TypeOf(dst)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

func normalizeFieldNamespace(namespace, rootName string) string {
	if rootName != "" {
		namespace = strings.TrimPrefix(namespace, rootName+".")
	}
	namespace = strings.ReplaceAll(namespace, "[", ".")
	namespace = strings.ReplaceAll(namespace, "]", "")
	return namespace
}

func displayField(field string) string {
	field = strings.ReplaceAll(field, "_", " ")
	field = strings.ReplaceAll(field, "-", " ")
	return field
}
