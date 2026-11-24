package config

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// Application encryption key (APP_KEY, APP_PREVIOUS_KEYS): values the app
// must read back — two-factor secrets, sensitive job payloads, webhook
// signing secrets — are encrypted with it (internal/platform/crypto).

// AppKeyLength is the size of an application key: AES-256.
const AppKeyLength = 32

// developmentKeySeed derives the key used when APP_KEY is empty in a local
// environment, so `make dev` works out of the box and data encrypted in a
// previous run stays readable. It is public, so it protects nothing: every
// other environment must set APP_KEY.
const developmentKeySeed = "ims-go-admin-starter local development key (not secret)"

// AppKey holds the encryption key and the previous keys still accepted for
// decryption (rotation).
type AppKey struct {
	Key      []byte
	Previous [][]byte
	// Development is true when Key was derived because APP_KEY is empty
	// (local environments only).
	Development bool
}

func (c *Config) loadAppKey() error {
	key, err := parseAppKey("APP_KEY", getString("APP_KEY", ""))
	if err != nil {
		return err
	}
	c.AppKey = AppKey{Key: key}
	if key == nil {
		sum := sha256.Sum256([]byte(developmentKeySeed))
		c.AppKey = AppKey{Key: sum[:], Development: true}
	}
	for _, raw := range splitCSV(getString("APP_PREVIOUS_KEYS", "")) {
		previous, err := parseAppKey("APP_PREVIOUS_KEYS", raw)
		if err != nil {
			return err
		}
		c.AppKey.Previous = append(c.AppKey.Previous, previous)
	}
	return nil
}

// parseAppKey reads "base64:<32 bytes>" (Laravel's format) or plain base64.
// Empty returns nil.
func parseAppKey(name, raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(raw, "base64:"))
	if err != nil {
		return nil, fmt.Errorf("%s is not valid base64 (generate one with `ims key:generate`): %w", name, err)
	}
	if len(key) != AppKeyLength {
		return nil, fmt.Errorf("%s must be %d bytes, got %d (generate one with `ims key:generate`)", name, AppKeyLength, len(key))
	}
	return key, nil
}

// EncodeAppKey formats a key for .env: "base64:…".
func EncodeAppKey(key []byte) string {
	return "base64:" + base64.StdEncoding.EncodeToString(key)
}

func (c *Config) validateAppKey() error {
	if c.AppKey.Development && !c.IsLocal() {
		return fmt.Errorf("APP_KEY must be set outside local development (APP_ENV=%s); generate one with `ims key:generate`", c.App.Env)
	}
	return nil
}
