package crypto

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func key(fill byte) []byte { return bytes.Repeat([]byte{fill}, 32) }

func TestEncryptDecrypt(t *testing.T) {
	enc, err := New(key(1))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sealed, err := enc.EncryptString("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if !strings.HasPrefix(sealed, "v1:") || strings.Contains(sealed, "JBSWY3DPEHPK3PXP") {
		t.Fatalf("sealed = %q", sealed)
	}
	again, _ := enc.EncryptString("JBSWY3DPEHPK3PXP")
	if again == sealed {
		t.Fatal("two encryptions of the same value are identical (nonce reuse)")
	}
	plain, err := enc.DecryptString(sealed)
	if err != nil || plain != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("Decrypt() = %q, %v", plain, err)
	}
}

func TestRotation(t *testing.T) {
	old, _ := New(key(1))
	sealed, _ := old.EncryptString("secret")

	rotated, err := New(key(2), key(1))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if plain, err := rotated.DecryptString(sealed); err != nil || plain != "secret" {
		t.Fatalf("Decrypt(old value) = %q, %v", plain, err)
	}
	if !rotated.NeedsRotation(sealed) {
		t.Error("a value sealed with the previous key does not need rotation")
	}
	fresh, _ := rotated.EncryptString("secret")
	if rotated.NeedsRotation(fresh) {
		t.Error("a value sealed with the current key needs rotation")
	}

	stranger, _ := New(key(3))
	if _, err := stranger.Decrypt(sealed); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("Decrypt(other key) error = %v, want ErrUndecryptable", err)
	}
}

func TestTamperedValuesAreRefused(t *testing.T) {
	enc, _ := New(key(1))
	sealed, _ := enc.EncryptString("secret")
	// Change the last two characters; a value that already ends in "AA" would
	// otherwise stay as it was and decrypt fine.
	suffix := "AA"
	if strings.HasSuffix(sealed, suffix) {
		suffix = "BB"
	}
	tampered := sealed[:len(sealed)-2] + suffix
	for _, value := range []string{tampered, "", "v1:abc", "v2:" + sealed[3:], "plain text"} {
		if _, err := enc.Decrypt(value); !errors.Is(err, ErrUndecryptable) {
			t.Errorf("Decrypt(%q) error = %v, want ErrUndecryptable", value, err)
		}
	}
	if _, err := New([]byte("short")); err == nil {
		t.Error("New(short key) error = nil")
	}
}
