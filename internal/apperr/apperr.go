package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a stable, machine-readable application error code.
type Code string

const (
	BadRequest               Code = "BAD_REQUEST"
	InvalidRequest           Code = "INVALID_REQUEST"
	MalformedRequest         Code = "MALFORMED_REQUEST"
	MalformedJSON            Code = "MALFORMED_JSON"
	InvalidInput             Code = "INVALID_INPUT"
	InvalidFormat            Code = "INVALID_FORMAT"
	InvalidDataType          Code = "INVALID_DATA_TYPE"
	MissingRequiredField     Code = "MISSING_REQUIRED_FIELD"
	Unauthorized             Code = "UNAUTHORIZED"
	InvalidCredentials       Code = "INVALID_CREDENTIALS"
	TokenExpired             Code = "TOKEN_EXPIRED"
	TokenInvalid             Code = "TOKEN_INVALID"
	TokenMissing             Code = "TOKEN_MISSING"
	SessionExpired           Code = "SESSION_EXPIRED"
	AccountUnverified        Code = "ACCOUNT_UNVERIFIED"
	AccountDeactivated       Code = "ACCOUNT_DEACTIVATED"
	TenantSuspended          Code = "TENANT_SUSPENDED"
	TwoFactorSetupRequired   Code = "TWO_FACTOR_SETUP_REQUIRED"
	AccountLocked            Code = "ACCOUNT_LOCKED"
	SocialAccountNotFound    Code = "SOCIAL_ACCOUNT_NOT_FOUND"
	OTPInvalid               Code = "OTP_INVALID"
	OTPExpired               Code = "OTP_EXPIRED"
	Forbidden                Code = "FORBIDDEN"
	InsufficientPermissions  Code = "INSUFFICIENT_PERMISSIONS"
	ImpersonationForbidden   Code = "IMPERSONATION_FORBIDDEN"
	ResourceNotFound         Code = "RESOURCE_NOT_FOUND"
	ResourceDeleted          Code = "RESOURCE_DELETED"
	MethodNotAllowed         Code = "METHOD_NOT_ALLOWED"
	Conflict                 Code = "CONFLICT"
	DuplicateRequest         Code = "DUPLICATE_REQUEST"
	IdempotencyInProgress    Code = "IDEMPOTENCY_IN_PROGRESS"
	IdempotencyKeyReused     Code = "IDEMPOTENCY_KEY_REUSED"
	ResourceAlreadyExists    Code = "RESOURCE_ALREADY_EXISTS"
	ResourceLocked           Code = "RESOURCE_LOCKED"
	InvalidState             Code = "INVALID_STATE"
	PreconditionFailed       Code = "PRECONDITION_FAILED"
	OperationNotAllowed      Code = "OPERATION_NOT_ALLOWED"
	RequestEntityTooLarge    Code = "REQUEST_ENTITY_TOO_LARGE"
	UnsupportedMediaType     Code = "UNSUPPORTED_MEDIA_TYPE"
	ValidationFailed         Code = "VALIDATION_FAILED"
	BusinessLogicError       Code = "BUSINESS_LOGIC_ERROR"
	TenantContextNotResolved Code = "TENANT_CONTEXT_NOT_RESOLVED"
	RateLimitExceeded        Code = "RATE_LIMIT_EXCEEDED"
	QuotaExceeded            Code = "QUOTA_EXCEEDED"
	FeatureNotAvailable      Code = "FEATURE_NOT_AVAILABLE"
	UsageLimitExceeded       Code = "USAGE_LIMIT_EXCEEDED"
	SubscriptionRequired     Code = "SUBSCRIPTION_REQUIRED"
	InternalServerError      Code = "INTERNAL_SERVER_ERROR"
	DatabaseError            Code = "DATABASE_ERROR"
	ServiceUnavailable       Code = "SERVICE_UNAVAILABLE"
	MaintenanceMode          Code = "MAINTENANCE_MODE"
)

var defaultStatuses = map[Code]int{
	BadRequest:               http.StatusBadRequest,
	InvalidRequest:           http.StatusBadRequest,
	MalformedRequest:         http.StatusBadRequest,
	MalformedJSON:            http.StatusBadRequest,
	InvalidInput:             http.StatusBadRequest,
	InvalidFormat:            http.StatusBadRequest,
	InvalidDataType:          http.StatusBadRequest,
	MissingRequiredField:     http.StatusBadRequest,
	Unauthorized:             http.StatusUnauthorized,
	InvalidCredentials:       http.StatusUnauthorized,
	TokenExpired:             http.StatusUnauthorized,
	TokenInvalid:             http.StatusUnauthorized,
	TokenMissing:             http.StatusUnauthorized,
	SessionExpired:           http.StatusUnauthorized,
	AccountUnverified:        http.StatusForbidden,
	AccountDeactivated:       http.StatusForbidden,
	TenantSuspended:          http.StatusForbidden,
	TwoFactorSetupRequired:   http.StatusForbidden,
	AccountLocked:            http.StatusTooManyRequests,
	SocialAccountNotFound:    http.StatusForbidden,
	OTPInvalid:               http.StatusBadRequest,
	OTPExpired:               http.StatusBadRequest,
	Forbidden:                http.StatusForbidden,
	InsufficientPermissions:  http.StatusForbidden,
	ImpersonationForbidden:   http.StatusForbidden,
	ResourceNotFound:         http.StatusNotFound,
	ResourceDeleted:          http.StatusNotFound,
	MethodNotAllowed:         http.StatusMethodNotAllowed,
	Conflict:                 http.StatusConflict,
	DuplicateRequest:         http.StatusConflict,
	IdempotencyInProgress:    http.StatusConflict,
	IdempotencyKeyReused:     http.StatusUnprocessableEntity,
	ResourceAlreadyExists:    http.StatusConflict,
	ResourceLocked:           http.StatusConflict,
	InvalidState:             http.StatusConflict,
	PreconditionFailed:       http.StatusConflict,
	OperationNotAllowed:      http.StatusConflict,
	RequestEntityTooLarge:    http.StatusRequestEntityTooLarge,
	UnsupportedMediaType:     http.StatusUnsupportedMediaType,
	ValidationFailed:         http.StatusUnprocessableEntity,
	BusinessLogicError:       http.StatusUnprocessableEntity,
	TenantContextNotResolved: http.StatusUnprocessableEntity,
	RateLimitExceeded:        http.StatusTooManyRequests,
	QuotaExceeded:            http.StatusTooManyRequests,
	FeatureNotAvailable:      http.StatusPaymentRequired,
	UsageLimitExceeded:       http.StatusPaymentRequired,
	SubscriptionRequired:     http.StatusPaymentRequired,
	InternalServerError:      http.StatusInternalServerError,
	DatabaseError:            http.StatusInternalServerError,
	ServiceUnavailable:       http.StatusServiceUnavailable,
	MaintenanceMode:          http.StatusServiceUnavailable,
}

// Error is the application-wide error type. Services return *Error values;
// HTTP, CLI and queue entry points render them appropriately.
type Error struct {
	Code       Code
	Message    string
	Status     int
	Fields     map[string][]string
	Meta       map[string]any
	RetryAfter int
	Op         string
	Err        error

	// MessageKey is an optional i18n message ID (e.g. "messages.billing.
	// feature_not_available") rendered by the HTTP responder for the request
	// locale; Message stays the English fallback.
	MessageKey string
	Params     map[string]any
}

// New creates an application error with the default status for the code.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message, Status: StatusFor(code)}
}

// Newf creates an application error with a formatted message.
func Newf(code Code, format string, args ...any) *Error {
	return New(code, fmt.Sprintf(format, args...))
}

// Wrap creates an application error that wraps a cause for logging.
func Wrap(code Code, message string, err error) *Error {
	return &Error{Code: code, Message: message, Status: StatusFor(code), Err: err}
}

// Validation creates a 422 validation error carrying field
// errors (dot paths → message list). The problem detail is translated from
// validation.failed.
func Validation(fields map[string][]string) *Error {
	if fields == nil {
		fields = map[string][]string{}
	}
	return &Error{
		Code:       ValidationFailed,
		Message:    "The given data was invalid.",
		Status:     http.StatusUnprocessableEntity,
		Fields:     fields,
		MessageKey: "validation.failed",
	}
}

// WithMessageKey sets the i18n message ID and template params used to render
// the error message for the request locale.
func (e *Error) WithMessageKey(key string, params map[string]any) *Error {
	e.MessageKey = key
	e.Params = params
	return e
}

// WithStatus overrides the default HTTP status for the error.
func (e *Error) WithStatus(status int) *Error {
	e.Status = status
	return e
}

// WithRetryAfter sets the Retry-After hint in seconds (429 responses).
func (e *Error) WithRetryAfter(seconds int) *Error {
	e.RetryAfter = seconds
	return e
}

// WithMeta attaches RFC 9457 extension data (e.g. feature_key/limit/current).
func (e *Error) WithMeta(meta map[string]any) *Error {
	e.Meta = meta
	return e
}

// WithOp annotates the operation for logs.
func (e *Error) WithOp(op string) *Error {
	e.Op = op
	return e
}

// WithField adds a single field error.
func (e *Error) WithField(field, message string) *Error {
	if e.Fields == nil {
		e.Fields = map[string][]string{}
	}
	e.Fields[field] = append(e.Fields[field], message)
	return e
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the wrapped cause, if any.
func (e *Error) Unwrap() error {
	return e.Err
}

// StatusFor returns the default HTTP status for a code (500 when unknown).
func StatusFor(code Code) int {
	if status, ok := defaultStatuses[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// Is reports whether err is or wraps an application error with the given code.
func Is(err error, code Code) bool {
	var target *Error
	if errors.As(err, &target) {
		return target.Code == code
	}
	return false
}

// CodeOf returns the code of the application error err is or wraps, and
// INTERNAL_SERVER_ERROR for any other error.
func CodeOf(err error) Code {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return InternalServerError
}
