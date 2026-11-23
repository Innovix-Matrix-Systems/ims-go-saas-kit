package httpx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
)

type validateItem struct {
	Quantity int `json:"quantity" validate:"required,gt=0"`
}

type validateRequest struct {
	Email string         `json:"email" validate:"required,email"`
	Items []validateItem `json:"items" validate:"required,min=1,dive"`
}

func TestValidatorProducesFieldErrors(t *testing.T) {
	req := validateRequest{Email: "not-an-email", Items: []validateItem{{}}}

	err := NewValidator().Struct(context.Background(), &req)
	if err == nil {
		t.Fatal("Struct() = nil error, want validation error")
	}

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not *apperr.Error: %v", err)
	}
	if appErr.Status != 422 {
		t.Errorf("status = %d, want 422", appErr.Status)
	}
	if _, ok := appErr.Fields["email"]; !ok {
		t.Errorf("Fields missing %q: %v", "email", appErr.Fields)
	}
	if _, ok := appErr.Fields["items.0.quantity"]; !ok {
		t.Errorf("Fields missing %q: %v", "items.0.quantity", appErr.Fields)
	}
	if msgs := appErr.Fields["email"]; len(msgs) == 0 || !strings.Contains(msgs[0], "valid email address") {
		t.Errorf("email messages = %v, want readable message", msgs)
	}
	if strings.Contains(appErr.Fields["email"][0], "validate:") {
		t.Error("raw validator tags leaked into message")
	}
}

func TestValidatorPasses(t *testing.T) {
	req := validateRequest{
		Email: "user@example.com",
		Items: []validateItem{{Quantity: 2}},
	}
	if err := NewValidator().Struct(context.Background(), &req); err != nil {
		t.Fatalf("Struct() error = %v, want nil", err)
	}
}
