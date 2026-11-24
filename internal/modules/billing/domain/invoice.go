package domain

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
)

// GenerateInvoiceNumber builds a unique invoice number. A crypto/rand failure
// is an error, never a silent fallback to a predictable number (M16).
func GenerateInvoiceNumber(now time.Time) (string, error) {
	token, err := randomHex(8)
	if err != nil {
		return "", err
	}
	return "INV-" + token + "-" + now.Format("20060102150405"), nil
}

// ManualReference builds an unguessable reference for manual (cash / bank)
// payments, e.g. CASH-3F9A0C1B7D2E4A6F8B0C1D2E (H9).
func ManualReference(prefix string) (string, error) {
	token, err := randomHex(12)
	if err != nil {
		return "", err
	}
	return prefix + "-" + token, nil
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", apperr.Wrap(apperr.InternalServerError, "generate random reference", err)
	}
	return strings.ToUpper(hex.EncodeToString(buf)), nil
}
