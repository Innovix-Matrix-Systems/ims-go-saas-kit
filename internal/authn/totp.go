package authn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6238 TOTP is HMAC-SHA1; authenticator apps expect it
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238) as authenticator apps implement it: HMAC-SHA1, 6 digits,
// 30-second steps. Built on the standard library: the starter adds no
// dependency for it.

const (
	totpDigits = 6
	totpPeriod = 30
	// totpDrift accepts codes one step either side of now (clock skew).
	totpDrift = 1
	// totpSecretBytes is the secret size (160 bits, RFC 4226's recommendation).
	totpSecretBytes = 20
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a random secret, base32 (what authenticator apps
// type or scan).
func newTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("totp secret: %w", err)
	}
	return totpEncoding.EncodeToString(raw), nil
}

// totpStep is the time step a moment falls in.
func totpStep(at time.Time) int64 { return at.Unix() / totpPeriod }

// totpCode is the code for a secret at a time step.
func totpCode(secret string, step int64) (string, error) {
	key, err := totpEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("totp: invalid secret: %w", err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step)) // #nosec G115 -- a Unix time step is never negative
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, value%1_000_000), nil
}

// totpMatch returns the time step a code belongs to (within the drift
// window around now), or 0 when it matches none.
func totpMatch(secret, code string, now time.Time) int64 {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0
	}
	current := totpStep(now)
	var matched int64
	for step := current - totpDrift; step <= current+totpDrift; step++ {
		expected, err := totpCode(secret, step)
		if err != nil {
			return 0
		}
		// Compare every step in constant time: no early exit.
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			matched = step
		}
	}
	return matched
}

// totpURL is the otpauth:// URL authenticator apps import (as a QR code or a
// link): otpauth://totp/Issuer:account?secret=…&issuer=Issuer.
func totpURL(issuer, account, secret string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	query := url.Values{
		"secret":    {secret},
		"issuer":    {issuer},
		"algorithm": {"SHA1"},
		"digits":    {fmt.Sprint(totpDigits)},
		"period":    {fmt.Sprint(totpPeriod)},
	}
	return "otpauth://totp/" + label + "?" + query.Encode()
}

// TOTPCodeAt is the code an authenticator app shows for secret at a moment:
// for tests and tools (testkit.TOTP), never for verifying sign-ins.
func TOTPCodeAt(secret string, at time.Time) (string, error) {
	return totpCode(secret, totpStep(at))
}
