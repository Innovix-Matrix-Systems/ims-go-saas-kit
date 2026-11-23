package authn

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// rfc6238Secret is the SHA-1 test key of RFC 6238 appendix B.
var rfc6238Secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))

func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	// RFC 6238 appendix B, SHA-1, truncated to the last 6 digits.
	vectors := map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	}
	for unix, want := range vectors {
		got, err := totpCode(rfc6238Secret, totpStep(time.Unix(unix, 0)))
		if err != nil || got != want {
			t.Errorf("totpCode(t=%d) = %q, %v; want %q", unix, got, err, want)
		}
	}
}

func TestTOTPDriftWindow(t *testing.T) {
	now := time.Unix(1234567890, 0)
	step := totpStep(now)
	for offset, accepted := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		code, _ := totpCode(rfc6238Secret, step+offset)
		if got := totpMatch(rfc6238Secret, code, now) != 0; got != accepted {
			t.Errorf("step offset %d accepted = %v, want %v", offset, got, accepted)
		}
	}
	if totpMatch(rfc6238Secret, "12345", now) != 0 || totpMatch(rfc6238Secret, "abcdef", now) != 0 {
		t.Error("a malformed code matched")
	}
}

func TestTOTPSecretAndURL(t *testing.T) {
	secret, err := newTOTPSecret()
	if err != nil || len(secret) != 32 {
		t.Fatalf("newTOTPSecret() = %q, %v; want 32 base32 characters", secret, err)
	}
	if _, err := totpCode(secret, 1); err != nil {
		t.Fatalf("a generated secret does not produce codes: %v", err)
	}
	link := totpURL("Acme App", "jane@acme.test", secret)
	if !strings.HasPrefix(link, "otpauth://totp/Acme%20App:jane@acme.test?") || !strings.Contains(link, "secret="+secret) || !strings.Contains(link, "issuer=Acme+App") {
		t.Fatalf("totpURL() = %q", link)
	}
}
