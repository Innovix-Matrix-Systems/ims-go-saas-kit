package i18n

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// Locale is a normalized language code (a registered Language.Code).
type Locale string

type contextKey string

const (
	localeKey     contextKey = "locale"
	localizerKey  contextKey = "localizer"
	fromHeaderKey contextKey = "locale_from_header"
)

// FromContext returns the request locale, or "" when unset.
func FromContext(ctx context.Context) Locale {
	if loc, ok := ctx.Value(localeKey).(Locale); ok {
		return loc
	}
	return ""
}

// WithLocale stores a locale in the context.
func WithLocale(ctx context.Context, locale Locale) context.Context {
	return context.WithValue(ctx, localeKey, locale)
}

// ParseLocale picks the best supported locale from an Accept-Language header,
// honoring q-values (higher wins; q=0 excludes a tag). Tags are normalized to
// their primary subtag (en-US → en) so region variants match their base
// language.
func ParseLocale(header string, supported []string, fallback string) string {
	if len(supported) == 0 || strings.TrimSpace(header) == "" {
		return fallback
	}

	type candidate struct {
		tag   string
		q     float64
		order int
	}
	candidates := make([]candidate, 0, 4)
	for i, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		tag := strings.TrimSpace(fields[0])
		if tag == "" {
			continue
		}
		q := 1.0
		for _, parameter := range fields[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "q") {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					q = parsed
				}
			}
		}
		if q <= 0 {
			continue
		}
		candidates = append(candidates, candidate{tag: tag, q: q, order: i})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].q > candidates[j].q })

	for _, chosen := range candidates {
		normalized := strings.ToLower(chosen.tag)
		if idx := strings.IndexAny(normalized, "-_"); idx > 0 {
			normalized = normalized[:idx]
		}
		if normalized == "*" {
			return fallback
		}
		for _, allowed := range supported {
			if strings.EqualFold(allowed, normalized) {
				return allowed
			}
		}
	}
	return fallback
}
