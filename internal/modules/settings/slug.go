package settings

import "strings"

// slugify derives a URL-safe slug from a name: lowercase ASCII alphanumerics
// with runs of other characters collapsed to single hyphens (Str::slug parity).
func slugify(name string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		default:
			pendingDash = true
		}
	}
	return b.String()
}
