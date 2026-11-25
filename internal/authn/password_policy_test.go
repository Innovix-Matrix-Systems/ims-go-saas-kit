package authn

import (
	"errors"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
)

func TestValidateNewPassword(t *testing.T) {
	cases := []struct {
		name     string
		password string
		ok       bool
	}{
		{"long enough", "correct horse", true},
		{"exactly eight", "Ab3$efgh", true},
		{"multi-byte counts runes", "পাসওয়ার্ড১২", true},
		{"too short", "Ab3$efg", false},
		{"seeded default is too short", "123456", false},
		{"only spaces", "          ", false},
		{"common", "Password123", false},
		{"too long for bcrypt", strings.Repeat("a", 73), false},
		{"bcrypt limit", strings.Repeat("a", 72), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNewPassword(tc.password)
			if tc.ok && err != nil {
				t.Fatalf("ValidateNewPassword(%q) = %v, want nil", tc.password, err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatalf("ValidateNewPassword(%q) = nil, want an error", tc.password)
				}
				if !apperr.Is(err, apperr.ValidationFailed) {
					t.Fatalf("error code = %v, want a validation error", apperr.CodeOf(err))
				}
			}
		})
	}
}

func TestValidateNewPasswordFieldName(t *testing.T) {
	err := ValidateNewPasswordField("new_password", "short")
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || len(appErr.Fields["new_password"]) == 0 {
		t.Fatalf("error = %#v, want a message on new_password", err)
	}
}
