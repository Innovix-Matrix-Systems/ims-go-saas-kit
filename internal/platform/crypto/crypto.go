// Package crypto encrypts values the application must read back (two-factor
// secrets, sensitive job payloads, webhook signing secrets) with the
// application key (APP_KEY). AES-256-GCM with a random nonce; each value
// names the key that sealed it, so APP_PREVIOUS_KEYS keeps old values
// readable while the key is rotated. Values that only need comparing
// (passwords, tokens) are hashed instead, never encrypted.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// version prefixes every sealed value: "v1:<key id>:<base64 nonce+ciphertext>".
const version = "v1"

// ErrUndecryptable is returned for a value no configured key can open
// (wrong key, tampered, or not produced by Encrypter).
var ErrUndecryptable = errors.New("crypto: the value cannot be decrypted with the configured keys")

// Encrypter seals and opens values with the application key.
type Encrypter struct {
	current string // key id
	keys    map[string]cipher.AEAD
}

// New creates an encrypter: key seals, key and previous open.
func New(key []byte, previous ...[]byte) (*Encrypter, error) {
	e := &Encrypter{keys: map[string]cipher.AEAD{}}
	for i, k := range append([][]byte{key}, previous...) {
		aead, err := newAEAD(k)
		if err != nil {
			return nil, err
		}
		id := keyID(k)
		if i == 0 {
			e.current = id
		}
		if _, exists := e.keys[id]; !exists {
			e.keys[id] = aead
		}
	}
	return e, nil
}

// Encrypt seals plaintext with the current key.
func (e *Encrypter) Encrypt(plaintext []byte) (string, error) {
	aead := e.keys[e.current]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypto: nonce: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, plaintext, []byte(e.current))
	return version + ":" + e.current + ":" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// EncryptString seals a string.
func (e *Encrypter) EncryptString(plaintext string) (string, error) {
	return e.Encrypt([]byte(plaintext))
}

// Decrypt opens a value sealed by Encrypt with the current or a previous key.
func (e *Encrypter) Decrypt(value string) ([]byte, error) {
	parts := strings.SplitN(value, ":", 3)
	if len(parts) != 3 || parts[0] != version {
		return nil, ErrUndecryptable
	}
	aead, ok := e.keys[parts[1]]
	if !ok {
		return nil, ErrUndecryptable
	}
	sealed, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(sealed) < aead.NonceSize() {
		return nil, ErrUndecryptable
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(parts[1]))
	if err != nil {
		return nil, ErrUndecryptable
	}
	return plaintext, nil
}

// DecryptString opens a value as a string.
func (e *Encrypter) DecryptString(value string) (string, error) {
	plaintext, err := e.Decrypt(value)
	return string(plaintext), err
}

// NeedsRotation reports whether a value was sealed with a previous key, so
// the caller can re-encrypt it with the current one on its next write.
func (e *Encrypter) NeedsRotation(value string) bool {
	parts := strings.SplitN(value, ":", 3)
	return len(parts) == 3 && parts[1] != e.current
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: the key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}
	return cipher.NewGCM(block)
}

// keyID names a key without revealing it: the first 8 hex digits of its
// SHA-256.
func keyID(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:4])
}
