package delivery

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	texttemplate "text/template"
)

// A template is a folder; each file is optional except what its channels
// need:
//
//	<name>/subject.txt   mail subject (one line)
//	<name>/mail.txt      plain-text mail body: enough on its own
//	<name>/mail.html     optional HTML body, wrapped in the layout
//	<name>/sms.txt       SMS body
//	<name>/push.json     {"title": …, "body": …}
//
// A locale variant sits next to its file: mail.bn.txt is used for "bn" and
// mail.txt for every other locale. Templates see .To (the Recipient), .Data
// (the message data) and .Brand. A missing data key is an error, so a typo
// fails at Send instead of mailing "<no value>". The layout is
// _layout/mail.html; a registered FS can override it.

//go:embed all:templates
var builtinFS embed.FS

// Brand is what the mail layout shows: name, logo, colour and footer.
type Brand struct {
	Name    string `json:"name"`
	LogoURL string `json:"logo_url"`
	Color   string `json:"color"`
	Footer  string `json:"footer"`
}

// View is what a template is executed with.
type View struct {
	To    Recipient
	Data  map[string]any
	Brand Brand
}

// layoutView is what the layout is executed with.
type layoutView struct {
	Brand   Brand
	Subject string
	Body    htmltemplate.HTML
}

const layoutDir = "_layout"

// funcs are the template functions; they speak the variant's locale.
//
//	{{duration .Data.minutes}}   90 → "1 hour 30 minutes" / "১ ঘণ্টা ৩০ মিনিট"
//	{{json .Data.title}}         a JSON string, for push.json
func funcs(locale string) map[string]any {
	return map[string]any{
		"duration": func(minutes any) string { return humanMinutes(toInt(minutes), locale) },
		// {{json .Data.title}} quotes a value for push.json.
		"json": func(value any) (string, error) {
			encoded, err := json.Marshal(value)
			return string(encoded), err
		},
	}
}

// Registry holds the templates: the built-in ones and those registered by
// modules. A later registration of the same name replaces the earlier one,
// so a project overrides a built-in template by registering its own.
type Registry struct {
	mu        sync.RWMutex
	templates map[string]fs.FS
	layout    fs.FS
}

// NewRegistry returns a registry with the built-in templates.
func NewRegistry() *Registry {
	registry := &Registry{templates: map[string]fs.FS{}}
	builtin, err := fs.Sub(builtinFS, "templates")
	if err == nil {
		err = registry.Register(builtin)
	}
	if err != nil {
		panic(fmt.Sprintf("delivery: built-in templates: %v", err)) // embedded at build time
	}
	return registry
}

// Register adds every template folder of fsys (and its _layout, if any).
func (r *Registry) Register(fsys fs.FS) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("delivery: read templates: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sub, err := fs.Sub(fsys, entry.Name())
		if err != nil {
			return fmt.Errorf("delivery: template %q: %w", entry.Name(), err)
		}
		if entry.Name() == layoutDir {
			r.layout = sub
			continue
		}
		r.templates[entry.Name()] = sub
	}
	return nil
}

// Names lists the registered templates, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.templates))
	for name := range r.templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Has reports whether a template is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.templates[name]
	return ok
}

// Channels lists the channels a template can render.
func (r *Registry) Channels(name string) []Channel {
	fsys, ok := r.template(name)
	if !ok {
		return nil
	}
	var channels []Channel
	for _, probe := range []struct {
		channel Channel
		file    string
	}{{Mail, "mail.txt"}, {SMS, "sms.txt"}, {Push, "push.json"}} {
		if _, err := fs.Stat(fsys, probe.file); err == nil {
			channels = append(channels, probe.channel)
		}
	}
	return channels
}

// Render renders a template for one channel. locales are tried in order
// (the recipient's, then the fallback), then the file without a locale.
func (r *Registry) Render(name string, channel Channel, locales []string, view View) (Content, error) {
	fsys, ok := r.template(name)
	if !ok {
		return Content{}, fmt.Errorf("delivery: template %q is not registered", name)
	}
	content := Content{Channel: channel}
	var err error
	switch channel {
	case Mail:
		if content.Subject, err = renderText(fsys, "subject.txt", locales, view); err != nil {
			return Content{}, err
		}
		content.Subject = firstLine(content.Subject)
		if content.Text, err = renderText(fsys, "mail.txt", locales, view); err != nil {
			return Content{}, err
		}
		body, err := renderHTML(fsys, "mail.html", locales, view)
		if err != nil {
			return Content{}, err
		}
		if body != "" {
			if content.HTML, err = r.Layout(view.Brand, content.Subject, body); err != nil {
				return Content{}, err
			}
		}
	case SMS:
		if content.Text, err = renderText(fsys, "sms.txt", locales, view); err != nil {
			return Content{}, err
		}
	case Push:
		raw, err := renderText(fsys, "push.json", locales, view)
		if err != nil {
			return Content{}, err
		}
		var push struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if err := json.Unmarshal([]byte(raw), &push); err != nil {
			return Content{}, fmt.Errorf("delivery: template %q push.json: %w", name, err)
		}
		content.Title, content.Text = push.Title, push.Body
	default:
		return Content{}, fmt.Errorf("delivery: unknown channel %q", channel)
	}
	return content, nil
}

// Layout wraps an HTML body (already rendered and escaped) in the layout.
func (r *Registry) Layout(brand Brand, subject string, body htmltemplate.HTML) (string, error) {
	r.mu.RLock()
	layout := r.layout
	r.mu.RUnlock()
	if layout == nil {
		return string(body), nil
	}
	source, err := fs.ReadFile(layout, "mail.html")
	if err != nil {
		return "", fmt.Errorf("delivery: layout: %w", err)
	}
	tmpl, err := htmltemplate.New("layout").Option("missingkey=error").Parse(string(source))
	if err != nil {
		return "", fmt.Errorf("delivery: layout: %w", err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, layoutView{Brand: brand, Subject: subject, Body: body}); err != nil {
		return "", fmt.Errorf("delivery: layout: %w", err)
	}
	return out.String(), nil
}

func (r *Registry) template(name string) (fs.FS, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fsys, ok := r.templates[name]
	return fsys, ok
}

// Sample returns a template's sample.json (empty when it has none).
func (r *Registry) Sample(name string) (map[string]any, error) {
	fsys, ok := r.template(name)
	if !ok {
		return nil, fmt.Errorf("delivery: template %q is not registered", name)
	}
	raw, err := fs.ReadFile(fsys, "sample.json")
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("delivery: template %q sample.json: %w", name, err)
	}
	return data, nil
}

// trustedHTML marks a raw message's HTML as safe. It comes from code (a
// caller of Send), never from user input: callers must escape user values.
func trustedHTML(html string) htmltemplate.HTML {
	return htmltemplate.HTML(html) // #nosec G203 -- raw HTML is written by the calling code
}

// templateFile is one template file found for a locale.
type templateFile struct {
	source string
	locale string // the variant's locale; "" for the file without one
}

// findFile returns file in the first matching locale variant, then without
// a locale. ok is false when neither exists.
func findFile(fsys fs.FS, file string, locales []string) (templateFile, bool, error) {
	ext := path.Ext(file)
	base := strings.TrimSuffix(file, ext)
	candidates := make([]string, 0, len(locales)+1)
	for _, locale := range locales {
		if locale != "" {
			candidates = append(candidates, locale)
		}
	}
	for _, locale := range append(candidates, "") { // "" = the file without a locale
		candidate := file
		if locale != "" {
			candidate = base + "." + locale + ext
		}
		source, err := fs.ReadFile(fsys, candidate)
		if err == nil {
			return templateFile{source: string(source), locale: locale}, true, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return templateFile{}, false, err
		}
	}
	return templateFile{}, false, nil
}

func renderText(fsys fs.FS, file string, locales []string, view View) (string, error) {
	found, ok, err := findFile(fsys, file, locales)
	if err != nil {
		return "", fmt.Errorf("delivery: read %s: %w", file, err)
	}
	if !ok {
		return "", fmt.Errorf("delivery: the template has no %s", file)
	}
	tmpl, err := texttemplate.New(file).Option("missingkey=error").Funcs(funcs(found.locale)).Parse(found.source)
	if err != nil {
		return "", fmt.Errorf("delivery: parse %s: %w", file, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, view); err != nil {
		return "", fmt.Errorf("delivery: render %s: %w", file, err)
	}
	return strings.TrimSpace(out.String()), nil
}

func renderHTML(fsys fs.FS, file string, locales []string, view View) (htmltemplate.HTML, error) {
	found, ok, err := findFile(fsys, file, locales)
	if err != nil || !ok {
		return "", err
	}
	tmpl, err := htmltemplate.New(file).Option("missingkey=error").Funcs(funcs(found.locale)).Parse(found.source)
	if err != nil {
		return "", fmt.Errorf("delivery: parse %s: %w", file, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, view); err != nil {
		return "", fmt.Errorf("delivery: render %s: %w", file, err)
	}
	return htmltemplate.HTML(out.String()), nil // #nosec G203 -- produced by html/template, already escaped
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

// humanMinutes spells a duration in days, hours and minutes, in English or
// (locale "bn") Bangla with Bangla digits.
func humanMinutes(minutes int, locale string) string {
	type unit struct {
		size             int
		singular, plural string
		bangla           string
	}
	units := []unit{{1440, "day", "days", "দিন"}, {60, "hour", "hours", "ঘণ্টা"}, {1, "minute", "minutes", "মিনিট"}}
	var parts []string
	for _, u := range units {
		n := minutes / u.size
		if n == 0 {
			continue
		}
		minutes -= n * u.size
		switch {
		case locale == "bn":
			parts = append(parts, banglaDigits(strconv.Itoa(n))+" "+u.bangla)
		case n == 1:
			parts = append(parts, "1 "+u.singular)
		default:
			parts = append(parts, strconv.Itoa(n)+" "+u.plural)
		}
	}
	if len(parts) == 0 {
		if locale == "bn" {
			return "০ মিনিট"
		}
		return "0 minutes"
	}
	return strings.Join(parts, " ")
}

func banglaDigits(ascii string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return '০' + (r - '0')
		}
		return r
	}, ascii)
}

// toInt reads a number that may have come back from JSON (float64).
func toInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}
