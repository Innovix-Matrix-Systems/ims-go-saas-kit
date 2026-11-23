package i18n

import "testing"

func TestParseLocale(t *testing.T) {
	supported := []string{"en", "bn"}
	cases := []struct {
		header string
		want   string
	}{
		{"en-US,en;q=0.9", "en"},
		{"bn-BD,bn;q=0.9", "bn"},
		{"fr-FR", "en"},
		{"*", "en"},
		{"", "en"},
		{"en", "en"},
	}
	for _, tc := range cases {
		if got := ParseLocale(tc.header, supported, "en"); got != tc.want {
			t.Errorf("ParseLocale(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}
