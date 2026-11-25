package notifications

import (
	"encoding/json"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/i18n"
)

// A notification the system writes (a payment notice, a renewal reminder) is
// read by people who use different languages, so it carries a message key
// and params in its data, under "i18n":
//
//	{"i18n": {"key": "notices.payment_received", "params": {"Amount": "BDT 300.00"}}}
//
// and is rendered for each reader: <key>.title and <key>.body from the
// catalogs, in the feed for the request's language and in the e-mail for the
// recipient's. A param written as {"t": "<key>"} is translated first (a word
// such as the payment method). The stored title and body are the reference
// text, kept for exports and as the fallback.

// I18nDataKey is the data entry that holds a notification's message key.
const I18nDataKey = "i18n"

// Message is a translatable notification text.
type Message struct {
	Key    string         `json:"key"`
	Params map[string]any `json:"params,omitempty"`
}

// Term is a param that is itself translated: {"t": "<key>"}.
func Term(key string) map[string]string { return map[string]string{"t": key} }

// WithMessage adds the message to a notification's data (a JSON object, or
// nil) and returns the new data.
func WithMessage(data []byte, message Message) ([]byte, error) {
	fields := map[string]any{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, err
		}
	}
	fields[I18nDataKey] = message
	return json.Marshal(fields)
}

// messageOf reads the message from a notification's data, if any.
func messageOf(data []byte) (Message, bool) {
	if len(data) == 0 {
		return Message{}, false
	}
	var fields struct {
		I18n *Message `json:"i18n"`
	}
	if json.Unmarshal(data, &fields) != nil || fields.I18n == nil || fields.I18n.Key == "" {
		return Message{}, false
	}
	return *fields.I18n, true
}

// localize renders a notification's title and body in a language; without
// a message (a notification someone typed) they stay as stored.
func localize(code string, data []byte, title string, body *string) (string, *string) {
	message, ok := messageOf(data)
	if !ok {
		return title, body
	}
	params := make(map[string]any, len(message.Params))
	for name, value := range message.Params {
		if term, ok := value.(map[string]any); ok {
			if key, _ := term["t"].(string); key != "" {
				value = i18n.Tr(code, key, nil)
			}
		}
		params[name] = value
	}
	// A numeric Count picks the plural form and is shown in the reader's
	// digits.
	if count, ok := params["Count"].(float64); ok {
		params = i18n.Count(code, int64(count), params)
		params["Count"] = i18n.LanguageFor(code).FormatNumber(int64(count))
	}
	if text := i18n.Tr(code, message.Key+".title", params); text != "" {
		title = text
	}
	if text := strings.TrimSpace(i18n.Tr(code, message.Key+".body", params)); text != "" {
		body = &text
	}
	return title, body
}
