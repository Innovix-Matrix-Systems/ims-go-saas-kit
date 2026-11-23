package authn

import (
	"strings"
	"unicode/utf8"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
)

// Password rules for every place a password is chosen: sign-up, accepting an
// invitation, a reset, changing one's own password, and an admin setting one.
// Signing in only checks that a password was sent, so accounts created under
// older rules keep working.
const (
	// PasswordMinLength is the shortest password accepted.
	PasswordMinLength = 8
	// PasswordMaxBytes is bcrypt's limit: longer input would be truncated.
	PasswordMaxBytes = 72
)

// commonPasswords are refused outright: they are the first guesses of any
// attack. Lower-case; compared case-insensitively.
var commonPasswords = map[string]struct{}{
	"password": {}, "password1": {}, "password12": {}, "password123": {}, "passw0rd": {},
	"12345678": {}, "123456789": {}, "1234567890": {}, "12341234": {}, "11111111": {},
	"00000000": {}, "87654321": {}, "11223344": {}, "123123123": {}, "qwertyui": {},
	"qwerty123": {}, "qwertyuiop": {}, "1q2w3e4r": {}, "1qaz2wsx": {}, "asdfghjk": {},
	"iloveyou": {}, "sunshine": {}, "football": {}, "baseball": {}, "superman": {},
	"princess": {}, "welcome1": {}, "welcome123": {}, "letmein1": {}, "admin123": {},
	"administrator": {}, "changeme": {}, "trustno1": {}, "abc12345": {}, "abcd1234": {},
	"bangladesh": {}, "dhaka1234": {},
}

// ValidateNewPassword checks a password someone is about to set. The error is
// a 422 on the "password" field.
func ValidateNewPassword(password string) error {
	return ValidateNewPasswordField("password", password)
}

// ValidateNewPasswordField is ValidateNewPassword for a request whose field has
// another name.
func ValidateNewPasswordField(field, password string) error {
	invalid := func(message string) error {
		return apperr.Validation(map[string][]string{field: {message}})
	}
	if utf8.RuneCountInString(password) < PasswordMinLength {
		return invalid("The password must be at least 8 characters.")
	}
	if len(password) > PasswordMaxBytes {
		return invalid("The password may not be longer than 72 bytes.")
	}
	if strings.TrimSpace(password) == "" {
		return invalid("The password may not be only spaces.")
	}
	if _, common := commonPasswords[strings.ToLower(password)]; common {
		return invalid("This password is too common. Choose one that is harder to guess.")
	}
	return nil
}
