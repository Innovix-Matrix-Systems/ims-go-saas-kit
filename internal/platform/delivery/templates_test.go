package delivery

import (
	"strings"
	"testing"
	"testing/fstest"
)

var testBrand = Brand{Name: "Acme", Color: "#112233", Footer: "Acme Ltd, Dhaka"}

func TestBuiltinTemplatesRender(t *testing.T) {
	registry := NewRegistry()
	cases := []struct {
		template string
		channel  Channel
		locale   string
		data     map[string]any
		want     []string
	}{
		{"otp-code", Mail, "en", map[string]any{"code": "482913", "minutes": 5}, []string{"482913", "5 minutes"}},
		{"otp-code", Mail, "bn", map[string]any{"code": "482913", "minutes": 5}, []string{"482913", "৫ মিনিট"}},
		{"otp-code", SMS, "en", map[string]any{"code": "482913", "minutes": 5}, []string{"482913 is your Acme sign-in code"}},
		{"password-reset", Mail, "en", map[string]any{"link": "https://app.test/r?token=abc", "minutes": 60}, []string{"https://app.test/r?token=abc", "1 hour"}},
		{"email-verify", Mail, "bn", map[string]any{"link": "https://app.test/v?token=abc", "minutes": 1440}, []string{"https://app.test/v?token=abc", "১ দিন"}},
	}
	for _, tc := range cases {
		t.Run(tc.template+"/"+string(tc.channel)+"/"+tc.locale, func(t *testing.T) {
			content, err := registry.Render(tc.template, tc.channel, []string{tc.locale, "en"},
				View{To: Recipient{Name: "Jane"}, Data: tc.data, Brand: testBrand})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(content.Text, want) {
					t.Errorf("text %q does not contain %q", content.Text, want)
				}
			}
			if tc.channel == Mail {
				if content.Subject == "" || strings.Contains(content.Subject, "\n") {
					t.Errorf("subject = %q, want one line", content.Subject)
				}
				if !strings.Contains(content.HTML, "Acme Ltd, Dhaka") || !strings.Contains(content.HTML, "#112233") {
					t.Errorf("HTML is not wrapped in the branded layout:\n%s", content.HTML)
				}
			}
		})
	}
}

func TestBuiltinTemplatesRenderTheirSamples(t *testing.T) {
	registry := NewRegistry()
	for _, name := range registry.Names() {
		sample, err := registry.Sample(name)
		if err != nil {
			t.Fatalf("Sample(%q) error = %v", name, err)
		}
		for _, channel := range registry.Channels(name) {
			for _, locale := range []string{"en", "bn"} {
				if _, err := registry.Render(name, channel, []string{locale}, View{Data: sample, Brand: testBrand}); err != nil {
					t.Errorf("Render(%s, %s, %s) error = %v", name, channel, locale, err)
				}
			}
		}
	}
}

func TestSMSTemplatesFitTwoSegments(t *testing.T) {
	registry := NewRegistry()
	for _, name := range registry.Names() {
		sample, _ := registry.Sample(name)
		for _, channel := range registry.Channels(name) {
			if channel != SMS {
				continue
			}
			for _, locale := range []string{"en", "bn"} {
				content, err := registry.Render(name, SMS, []string{locale}, View{Data: sample, Brand: testBrand})
				if err != nil {
					t.Fatalf("Render() error = %v", err)
				}
				if n := len([]rune(content.Text)); n > 320 {
					t.Errorf("%s (%s) SMS is %d characters, want at most 320", name, locale, n)
				}
			}
		}
	}
}

func TestPlainTextTemplateWorksAlone(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fstest.MapFS{
		"welcome/subject.txt": {Data: []byte("Welcome, {{.To.Name}}")},
		"welcome/mail.txt":    {Data: []byte("Hello {{.To.Name}}, your plan is {{.Data.plan}}.")},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	content, err := registry.Render("welcome", Mail, []string{"en"},
		View{To: Recipient{Name: "Jane"}, Data: map[string]any{"plan": "Pro"}, Brand: testBrand})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if content.Subject != "Welcome, Jane" || content.Text != "Hello Jane, your plan is Pro." || content.HTML != "" {
		t.Fatalf("content = %+v, want a text-only mail", content)
	}
	if got := registry.Channels("welcome"); len(got) != 1 || got[0] != Mail {
		t.Fatalf("Channels() = %v, want [mail]", got)
	}
}

func TestTemplateErrors(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.Render("otp-code", Mail, []string{"en"}, View{Data: map[string]any{"minutes": 5}}); err == nil {
		t.Error("a missing data key rendered without an error")
	}
	if _, err := registry.Render("password-reset", SMS, []string{"en"}, View{Data: map[string]any{"link": "x", "minutes": 1}}); err == nil {
		t.Error("a channel the template has no file for rendered without an error")
	}
	if _, err := registry.Render("nope", Mail, nil, View{}); err == nil {
		t.Error("an unknown template rendered without an error")
	}
}

func TestHTMLEscapesData(t *testing.T) {
	registry := NewRegistry()
	content, err := registry.Render("otp-code", Mail, []string{"en"},
		View{To: Recipient{Name: "<script>alert(1)</script>"}, Data: map[string]any{"code": "1", "minutes": 1}, Brand: testBrand})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(content.HTML, "<script>") {
		t.Fatalf("HTML contains unescaped data:\n%s", content.HTML)
	}
}

func TestRegisteredTemplateOverridesBuiltin(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fstest.MapFS{
		"otp-code/subject.txt": {Data: []byte("Code")},
		"otp-code/mail.txt":    {Data: []byte("Use {{.Data.code}}")},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	content, err := registry.Render("otp-code", Mail, []string{"bn"}, View{Data: map[string]any{"code": "9"}})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if content.Text != "Use 9" {
		t.Fatalf("text = %q, want the project's template", content.Text)
	}
}

func TestHumanMinutes(t *testing.T) {
	cases := []struct {
		minutes int
		locale  string
		want    string
	}{
		{1, "en", "1 minute"},
		{5, "en", "5 minutes"},
		{90, "en", "1 hour 30 minutes"},
		{1440, "en", "1 day"},
		{2880, "en", "2 days"},
		{0, "en", "0 minutes"},
		{90, "bn", "১ ঘণ্টা ৩০ মিনিট"},
		{1440, "bn", "১ দিন"},
	}
	for _, tc := range cases {
		if got := humanMinutes(tc.minutes, tc.locale); got != tc.want {
			t.Errorf("humanMinutes(%d, %q) = %q, want %q", tc.minutes, tc.locale, got, tc.want)
		}
	}
}

func TestMaskAddress(t *testing.T) {
	cases := []struct {
		channel Channel
		address string
		want    string
	}{
		{Mail, "jane@acme.test", "j***@acme.test"},
		{Mail, "broken", "***"},
		{SMS, "+8801712345678", "+88017******78"},
		{SMS, "123", "***"},
	}
	for _, tc := range cases {
		if got := MaskAddress(tc.channel, tc.address); got != tc.want {
			t.Errorf("MaskAddress(%s, %q) = %q, want %q", tc.channel, tc.address, got, tc.want)
		}
	}
}
