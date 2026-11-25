package notifications

import (
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/money"
)

// useNotificationText makes the process translator one with this module's
// catalogs, for the length of the test.
func useNotificationText(t *testing.T) {
	t.Helper()
	translator, err := i18n.New(i18n.Builtin(), i18n.Platform(), Translations())
	if err != nil {
		t.Fatal(err)
	}
	i18n.SetDefault(translator)
	t.Cleanup(func() { i18n.SetDefault(nil) })
}

func renewalData(t *testing.T, params map[string]any) []byte {
	t.Helper()
	data, err := WithMessage([]byte(`{"url":"/billing"}`), Message{Key: "notifications.renewal_reminder", Params: params})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFeedItemsAreInTheReadersLanguage(t *testing.T) {
	useNotificationText(t)
	stored := "Your subscription renews in 3 days"
	item := FeedItem{Title: stored, Data: renewalData(t, map[string]any{"Count": 3, "Date": "2026-12-01 10:00"})}

	cases := []struct{ locale, title, body string }{
		{"en", "Your subscription renews in 3 days", "The subscription expires on 2026-12-01 10:00."},
		{"bn", "আপনার সাবস্ক্রিপশন ৩ দিনের মধ্যে নবায়ন হবে", "সাবস্ক্রিপশনের মেয়াদ শেষ হবে 2026-12-01 10:00।"},
	}
	for _, tc := range cases {
		got := NewFeedResponse(item, tc.locale)
		if got.Title != tc.title {
			t.Errorf("%s title = %q, want %q", tc.locale, got.Title, tc.title)
		}
		if got.Body == nil || *got.Body != tc.body {
			t.Errorf("%s body = %v, want %q", tc.locale, got.Body, tc.body)
		}
	}

	one := NewFeedResponse(FeedItem{Title: stored, Data: renewalData(t, map[string]any{"Count": 1, "Date": "x"})}, "en")
	if one.Title != "Your subscription renews in 1 day" {
		t.Errorf("singular title = %q", one.Title)
	}
}

func TestTypedNotificationsStayAsWritten(t *testing.T) {
	useNotificationText(t)
	body := "Hand-written"
	got := NewFeedResponse(FeedItem{Title: "Maintenance tonight", Body: &body, Data: []byte(`{"url":"/x"}`)}, "bn")
	if got.Title != "Maintenance tonight" || got.Body == nil || *got.Body != body {
		t.Fatalf("a notification without a message key changed: %q / %v", got.Title, got.Body)
	}
}

func TestTermFallsBackWhenTheKeyIsMissing(t *testing.T) {
	useNotificationText(t)
	data := renewalData(t, map[string]any{"Count": 2, "Date": Term("notices.method.carrier_pigeon", "carrier_pigeon")})
	_, body := localize("de", data, "stored", nil)
	if body == nil || !strings.Contains(*body, "carrier_pigeon") {
		t.Fatalf("body = %v, want the fallback text", body)
	}
}

func TestMailIsInTheRecipientsLanguage(t *testing.T) {
	useNotificationText(t)
	s := &Service{}
	notification := Notification{Title: "stored", Data: renewalData(t, map[string]any{"Count": 5, "Date": "d"})}
	data := s.messageData(notification, delivery.Recipient{UserID: 1, Locale: "de"}, true)
	if data["title"] == "stored" || data["title"] == "" {
		t.Fatalf("mail title = %q, want German text", data["title"])
	}
	if data["url"] != "/billing" {
		t.Fatalf("mail url = %q", data["url"])
	}
}

func TestMoneyIsInTheReadersDigits(t *testing.T) {
	useNotificationText(t)
	bdt := money.Currency{Code: "BDT", Symbol: "৳", SymbolFirst: true, Precision: 2, DecimalMark: ".", ThousandsSeparator: ","}
	data := renewalData(t, map[string]any{"Count": 1, "Date": Money(money.Of(150000, bdt))})
	for locale, want := range map[string]string{"en": "৳1,500.00", "bn": "৳১,৫০০.০০"} {
		_, body := localize(locale, data, "stored", nil)
		if body == nil || !strings.Contains(*body, want) {
			t.Errorf("%s body = %v, want it to contain %q", locale, body, want)
		}
	}
}
