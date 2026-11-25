package i18n

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

// A Bundle is a set of catalogs owned by one module: a folder per namespace
// with a file per language, <namespace>/<code>.json. Each key starts with its
// namespace ("oauth.signin_title"). A value is a string or, for text that
// depends on a number, an object of CLDR plural forms
// ({"one": "…", "other": "…"}, plus zero/two/few/many where the language
// has them). A key ending in "._context" is a note for translators, not a
// message.
//
// Bundles are registered in order: the platform, the kit's modules, the
// project's modules, then the project's overrides. A key may only be defined
// in a namespace its owner registered; the overrides bundle is the one
// exception and may replace any existing key (never add one).
type Bundle struct {
	// Owner names the module ("oauth"). Two owners can't share a namespace.
	Owner string
	// Namespaces are the folders this bundle owns.
	Namespaces []string
	// FS holds the namespace folders.
	FS fs.FS
	// Overrides marks the project's overrides bundle: every folder in FS,
	// replacing keys other bundles defined.
	Overrides bool
}

// ContextSuffix marks a translator note next to a key.
const ContextSuffix = "._context"

//go:embed all:locales
var platformFS embed.FS

// Platform is the platform's own bundle: shared words, HTTP status titles
// and validation messages.
func Platform() Bundle {
	sub, err := fs.Sub(platformFS, "locales")
	if err != nil {
		panic(err) // embedded at build time
	}
	return Bundle{Owner: "platform", Namespaces: []string{"common", "http-statuses", "validation"}, FS: sub}
}

// Overrides returns the overrides bundle for fsys (nil: none).
func Overrides(fsys fs.FS) Bundle {
	return Bundle{Owner: "overrides", FS: fsys, Overrides: true}
}

// entry is one catalog message with where it came from.
type entry struct {
	msg        *goi18n.Message
	namespace  string
	owner      string
	overridden bool
}

// catalog is every language's messages, merged in registration order.
type catalog struct {
	messages map[string]map[string]*entry // code -> key -> entry
	owners   map[string]string            // namespace -> owner
	contexts map[string]string            // key -> translator note
}

func newCatalog() *catalog {
	return &catalog{messages: map[string]map[string]*entry{}, owners: map[string]string{}, contexts: map[string]string{}}
}

// register merges a bundle into the catalog for the given language codes.
func (c *catalog) register(b Bundle, codes []string) error {
	if b.FS == nil {
		return nil
	}
	if b.Overrides {
		return c.registerOverrides(b, codes)
	}
	if b.Owner == "" {
		return errors.New("i18n: a bundle needs an owner")
	}
	for _, namespace := range b.Namespaces {
		if owner, taken := c.owners[namespace]; taken {
			return fmt.Errorf("i18n: namespace %q is owned by %q; %q can't register it (use the overrides bundle to change its text)", namespace, owner, b.Owner)
		}
		c.owners[namespace] = b.Owner
		for _, code := range codes {
			entries, err := readCatalogFile(b.FS, namespace, code)
			if err != nil {
				return err
			}
			for key, value := range entries {
				if !strings.HasPrefix(key, namespace+".") {
					return fmt.Errorf("i18n: %s/%s.json (%s): key %q is outside the namespace %q", namespace, code, b.Owner, key, namespace)
				}
				if strings.HasSuffix(key, ContextSuffix) {
					if code == Reference {
						note, _ := value.(string)
						c.contexts[strings.TrimSuffix(key, ContextSuffix)] = note
					}
					continue
				}
				msg, err := parseMessage(key, value)
				if err != nil {
					return fmt.Errorf("i18n: %s/%s.json: %w", namespace, code, err)
				}
				c.set(code, key, &entry{msg: msg, namespace: namespace, owner: b.Owner})
			}
		}
	}
	return nil
}

// registerOverrides replaces existing keys with the project's wording.
func (c *catalog) registerOverrides(b Bundle, codes []string) error {
	dirs, err := fs.ReadDir(b.FS, ".")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("i18n: read overrides: %w", err)
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		namespace := dir.Name()
		if _, ok := c.owners[namespace]; !ok {
			return fmt.Errorf("i18n: overrides/%s: no bundle registers the namespace %q", namespace, namespace)
		}
		for _, code := range codes {
			entries, err := readCatalogFile(b.FS, namespace, code)
			if err != nil {
				return err
			}
			for key, value := range entries {
				if strings.HasSuffix(key, ContextSuffix) {
					continue
				}
				existing, ok := c.messages[Reference][key]
				if !ok {
					return fmt.Errorf("i18n: overrides/%s/%s.json: %q is not an existing key (overrides change text, they don't add keys)", namespace, code, key)
				}
				msg, err := parseMessage(key, value)
				if err != nil {
					return fmt.Errorf("i18n: overrides/%s/%s.json: %w", namespace, code, err)
				}
				c.set(code, key, &entry{msg: msg, namespace: namespace, owner: existing.owner, overridden: true})
			}
		}
	}
	return nil
}

func (c *catalog) set(code, key string, m *entry) {
	if c.messages[code] == nil {
		c.messages[code] = map[string]*entry{}
	}
	c.messages[code][key] = m
}

// readCatalogFile reads <namespace>/<code>.json; a missing file is an empty
// catalog (the coverage check reports the missing keys).
func readCatalogFile(fsys fs.FS, namespace, code string) (map[string]any, error) {
	name := path.Join(namespace, code+".json")
	raw, err := fs.ReadFile(fsys, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("i18n: read %s: %w", name, err)
	}
	var entries map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("i18n: parse %s: %w", name, err)
	}
	return entries, nil
}

// pluralForms are the keys a plural value may have.
var pluralForms = []string{"zero", "one", "two", "few", "many", "other"}

// parseMessage turns a catalog value into a go-i18n message.
func parseMessage(key string, value any) (*goi18n.Message, error) {
	msg := &goi18n.Message{ID: key}
	switch typed := value.(type) {
	case string:
		msg.Other = typed
	case map[string]any:
		for form, text := range typed {
			str, ok := text.(string)
			if !ok || !slices.Contains(pluralForms, form) {
				return nil, fmt.Errorf("key %q: plural forms are strings named %s", key, strings.Join(pluralForms, ", "))
			}
			switch form {
			case "zero":
				msg.Zero = str
			case "one":
				msg.One = str
			case "two":
				msg.Two = str
			case "few":
				msg.Few = str
			case "many":
				msg.Many = str
			case "other":
				msg.Other = str
			}
		}
		if msg.Other == "" {
			return nil, fmt.Errorf("key %q: a plural value needs an \"other\" form", key)
		}
	default:
		return nil, fmt.Errorf("key %q: a value is a string or an object of plural forms", key)
	}
	return msg, nil
}

// forms lists the plural forms a message defines.
func forms(msg *goi18n.Message) []string {
	var out []string
	for _, form := range pluralForms {
		if text := formText(msg, form); text != "" {
			out = append(out, form)
		}
	}
	return out
}

func formText(msg *goi18n.Message, form string) string {
	switch form {
	case "zero":
		return msg.Zero
	case "one":
		return msg.One
	case "two":
		return msg.Two
	case "few":
		return msg.Few
	case "many":
		return msg.Many
	default:
		return msg.Other
	}
}

// isPlural reports whether a message has forms beyond "other".
func isPlural(msg *goi18n.Message) bool { return len(forms(msg)) > 1 }

// Namespace describes one registered namespace.
type Namespace struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
	Keys  int    `json:"keys"`
}

// Namespaces lists the registered namespaces, sorted, with their key counts
// in the reference language.
func (t *Translator) Namespaces() []Namespace {
	counts := map[string]int{}
	for _, m := range t.catalog.messages[Reference] {
		counts[m.namespace]++
	}
	out := make([]Namespace, 0, len(t.catalog.owners))
	for name, owner := range t.catalog.owners {
		out = append(out, Namespace{Name: name, Owner: owner, Keys: counts[name]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Overridden lists, per language, the keys the overrides bundle replaces.
func (t *Translator) Overridden() map[string][]string {
	out := map[string][]string{}
	for code, messages := range t.catalog.messages {
		for key, m := range messages {
			if m.overridden {
				out[code] = append(out[code], key)
			}
		}
		sort.Strings(out[code])
	}
	return out
}
