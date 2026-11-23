package webhooks

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Signing follows Standard Webhooks (standardwebhooks.com), so receivers can
// verify with an existing library:
//
//	webhook-id:        evt_…
//	webhook-timestamp: <unix seconds>
//	webhook-signature: v1,<base64 HMAC-SHA256 of "id.timestamp.body">
//
// The secret is "whsec_" + base64 of 24 random bytes; the HMAC key is the
// decoded bytes. During a rotation both secrets sign, space-separated.

const secretPrefix = "whsec_"

// Header names.
const (
	HeaderID        = "webhook-id"
	HeaderTimestamp = "webhook-timestamp"
	HeaderSignature = "webhook-signature"
)

// newSecret returns a new signing secret.
func newSecret() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("webhooks: secret: %w", err)
	}
	return secretPrefix + base64.StdEncoding.EncodeToString(raw), nil
}

// sign returns the v1 signature of a message.
func sign(secret, id string, timestamp time.Time, body []byte) (string, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, secretPrefix))
	if err != nil {
		return "", fmt.Errorf("webhooks: invalid secret: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + strconv.FormatInt(timestamp.Unix(), 10) + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// signatures signs with every given secret ("" skipped), space-separated.
func signatures(id string, timestamp time.Time, body []byte, secrets ...string) (string, error) {
	var all []string
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		signature, err := sign(secret, id, timestamp, body)
		if err != nil {
			return "", err
		}
		all = append(all, signature)
	}
	return strings.Join(all, " "), nil
}
