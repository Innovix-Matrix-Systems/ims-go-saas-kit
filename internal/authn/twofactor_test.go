package authn

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestRecoveryCodes(t *testing.T) {
	codes, encoded, err := newRecoveryCodes()
	if err != nil {
		t.Fatalf("newRecoveryCodes() error = %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("codes = %v, want %d", codes, recoveryCodeCount)
	}
	shape := regexp.MustCompile(`^[a-hj-km-np-z2-9]{5}-[a-hj-km-np-z2-9]{5}$`)
	seen := map[string]bool{}
	for _, code := range codes {
		if !shape.MatchString(code) || seen[code] {
			t.Fatalf("code %q is malformed or repeated", code)
		}
		seen[code] = true
		if !looksLikeRecoveryCode(code) {
			t.Errorf("looksLikeRecoveryCode(%q) = false", code)
		}
	}
	var stored []recoveryCode
	if err := json.Unmarshal(encoded, &stored); err != nil || len(stored) != len(codes) {
		t.Fatalf("stored = %s, %v", encoded, err)
	}
	if stored[0].Hash != hashRecoveryCode(codes[0]) || stored[0].Hash == codes[0] {
		t.Fatal("codes are not stored as their hash")
	}
	// Typed in capitals, without the dash or with spaces: the same code.
	compact := codes[0][:5] + codes[0][6:]
	if hashRecoveryCode(strings.ToUpper(codes[0])) != stored[0].Hash {
		t.Error("a code typed in capitals does not match")
	}
	if hashRecoveryCode(compact) != stored[0].Hash || hashRecoveryCode(" "+codes[0]+" ") != stored[0].Hash {
		t.Error("a code typed without the dash or with spaces does not match")
	}
	if looksLikeRecoveryCode("123456") {
		t.Error("a six-digit code looks like a recovery code")
	}
}
