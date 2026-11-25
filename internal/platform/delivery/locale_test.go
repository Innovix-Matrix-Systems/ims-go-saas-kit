package delivery

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

type localeSource map[int64]string

func (s localeSource) Locale(_ context.Context, to Recipient) string { return s[to.UserID] }

// TestLocaleForLookupOrder: the recipient's own language, then the
// LocaleSource (account → organization), then the request's, then the
// fallback.
func TestLocaleForLookupOrder(t *testing.T) {
	service := NewService(NewRegistry(), &memoryRepo{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithLocales(localeSource{1: "bn"}).
		WithFallbackLocale("es")
	request := i18n.WithLocale(context.Background(), i18n.Locale("de"))

	cases := []struct {
		name string
		ctx  context.Context
		to   Recipient
		want string
	}{
		{"recipient", request, Recipient{UserID: 1, Locale: "ar"}, "ar"},
		{"source", request, Recipient{UserID: 1}, "bn"},
		{"request", request, Recipient{UserID: 2}, "de"},
		{"fallback", context.Background(), Recipient{UserID: 2}, "es"},
	}
	for _, tc := range cases {
		if got := service.LocaleFor(tc.ctx, tc.to); got != tc.want {
			t.Errorf("%s: LocaleFor = %q, want %q", tc.name, got, tc.want)
		}
	}

	bare := NewService(NewRegistry(), &memoryRepo{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if got := bare.LocaleFor(context.Background(), Recipient{UserID: 1}); got == "" {
		t.Error("without a source or fallback LocaleFor must still name a language")
	}
}
