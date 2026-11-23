package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/crypto"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/delivery"
)

// Notification channels (phase 26.6). Every notification is in the in-app
// feed (ChannelDatabase); it can also be delivered by mail, SMS or push
// through the delivery system. Fan-out delivers to the users it newly
// addresses, so a retried job never mails twice. Users turn a channel off
// per event type (preferences) unless the event is mandatory.

// ChannelDatabase is the in-app feed: always on.
const ChannelDatabase = "database"

// TemplateNotification is the delivery template of a notification.
const TemplateNotification = "notification"

// Event is one notification kind: what it is, which channels it may use
// (the preferences screen offers these) and whether users may opt out.
type Event struct {
	Description string
	Channels    []delivery.Channel
	// Mandatory events (security, billing failures) ignore opt-outs.
	Mandatory bool
}

// Events is the event-type catalog; RegisterEvent adds a module's events.
var Events = map[string]Event{
	EventRenewalReminder: {
		Description: "A subscription is about to renew or expire.",
		Channels:    []delivery.Channel{delivery.Mail, delivery.SMS},
	},
}

// RegisterEvent adds a notification event type (call it from init or wiring).
func RegisterEvent(name string, event Event) { Events[name] = event }

// FeatureGate tells whether a tenant's plan has a feature (sms.notifications);
// implemented by billing's Guard.
type FeatureGate interface {
	Allows(ctx context.Context, tenantID int64, feature string) bool
}

// channelOptions configures delivery outside the feed.
type channelOptions struct {
	delivery      *delivery.Service
	features      FeatureGate
	encrypter     *crypto.Encrypter
	unsubscribe   string // base URL of the one-click unsubscribe endpoint
	smsDailyLimit int64
}

// WithDelivery delivers notifications by mail, SMS and push. unsubscribeURL
// is the public endpoint one-click unsubscribe links point at;
// smsDailyLimit caps SMS per tenant per day (0: no cap).
func (s *Service) WithDelivery(service *delivery.Service, features FeatureGate, encrypter *crypto.Encrypter, unsubscribeURL string, smsDailyLimit int64) *Service {
	s.channels = channelOptions{delivery: service, features: features, encrypter: encrypter, unsubscribe: unsubscribeURL, smsDailyLimit: smsDailyLimit}
	return s
}

// normalizeChannels validates requested channels: the feed is always
// first, each channel once.
func normalizeChannels(eventType string, requested []string) ([]string, error) {
	event := Events[eventType]
	out := []string{ChannelDatabase}
	for _, channel := range requested {
		channel = strings.TrimSpace(channel)
		if channel == ChannelDatabase || slices.Contains(out, channel) {
			continue
		}
		if !slices.Contains(event.Channels, delivery.Channel(channel)) {
			return nil, apperr.Validation(map[string][]string{
				"channels": {fmt.Sprintf("The %s event cannot be sent by %q.", eventType, channel)},
			})
		}
		out = append(out, channel)
	}
	return out, nil
}

// deliver sends a notification on its extra channels to newly addressed
// users, honouring preferences, verified addresses, the plan (SMS) and the
// daily SMS cap. Problems are logged: the feed already has the notification.
func (s *Service) deliver(ctx context.Context, notification Notification, userIDs []int64) {
	opts := s.channels
	if opts.delivery == nil || len(notification.Channels) <= 1 || len(userIDs) == 0 {
		return
	}
	event := Events[notification.EventType]
	recipients, err := s.repo.Recipients(ctx, userIDs)
	if err != nil {
		s.logWarn(ctx, "notification delivery: recipients unavailable", "notification_id", notification.ID, "error", err)
		return
	}
	disabled := map[int64]map[string]bool{}
	if !event.Mandatory {
		if disabled, err = s.repo.DisabledChannels(ctx, notification.EventType, userIDs); err != nil {
			s.logWarn(ctx, "notification delivery: preferences unavailable", "notification_id", notification.ID, "error", err)
			return
		}
	}
	tenantID := tenantValue(notification.TenantID)
	smsAllowed := opts.features == nil || tenantID == 0 || opts.features.Allows(ctx, tenantID, "sms.notifications")

	for _, recipient := range recipients {
		var channels []delivery.Channel
		for _, name := range notification.Channels[1:] {
			channel := delivery.Channel(name)
			switch {
			case disabled[recipient.UserID][name]:
				continue
			case channel == delivery.Mail && recipient.Email == "",
				channel == delivery.SMS && (recipient.Phone == "" || !smsAllowed || !s.takeSMS(ctx, tenantID)):
				continue
			}
			channels = append(channels, channel)
		}
		if len(channels) == 0 {
			continue
		}
		message := delivery.Message{
			To:       recipient,
			Template: TemplateNotification,
			Data:     s.messageData(notification, recipient.UserID, event.Mandatory),
			Channels: channels,
		}
		if link, _ := message.Data["unsubscribe"].(string); link != "" {
			message.Headers = map[string]string{
				"List-Unsubscribe":      "<" + link + ">",
				"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
			}
		}
		if err := opts.delivery.Send(ctx, message); err != nil {
			s.logWarn(ctx, "notification delivery failed", "notification_id", notification.ID, "user_id", recipient.UserID, "error", err)
		}
	}
}

// messageData is what the notification template sees: title, body, the
// action URL from the notification's data ("url") and the unsubscribe link.
func (s *Service) messageData(notification Notification, userID int64, mandatory bool) map[string]any {
	data := map[string]any{"title": notification.Title, "body": "", "url": "", "unsubscribe": ""}
	if notification.Body != nil {
		data["body"] = *notification.Body
	}
	var extra map[string]any
	if json.Unmarshal(notification.Data, &extra) == nil {
		if link, ok := extra["url"].(string); ok {
			data["url"] = link
		}
	}
	if !mandatory {
		if token, err := s.unsubscribeToken(userID, notification.EventType); err == nil && token != "" {
			data["unsubscribe"] = s.channels.unsubscribe + "?token=" + url.QueryEscape(token)
		}
	}
	return data
}

// takeSMS counts one SMS against the tenant's daily cap; false when the cap
// is reached. A cache failure allows the SMS (the cap is a guard rail).
func (s *Service) takeSMS(ctx context.Context, tenantID int64) bool {
	limit := s.channels.smsDailyLimit
	if limit <= 0 {
		return true
	}
	key := fmt.Sprintf("notifications:sms:%d:%s", tenantID, time.Now().UTC().Format("20060102"))
	_, _ = s.cache.SetNX(ctx, key, 0, 48*time.Hour)
	used, err := s.cache.Increment(ctx, key, 1)
	return err != nil || used <= limit
}

// unsubscribeClaims is what an unsubscribe token carries (sealed with
// APP_KEY, so it can be neither read nor forged).
type unsubscribeClaims struct {
	UserID    int64  `json:"u"`
	EventType string `json:"e"`
}

func (s *Service) unsubscribeToken(userID int64, eventType string) (string, error) {
	if s.channels.encrypter == nil || s.channels.unsubscribe == "" {
		return "", nil
	}
	raw, err := json.Marshal(unsubscribeClaims{UserID: userID, EventType: eventType})
	if err != nil {
		return "", err
	}
	return s.channels.encrypter.Encrypt(raw)
}

// Unsubscribe turns off every extra channel of an event type for the user a
// token names (the one-click link in notification mails).
func (s *Service) Unsubscribe(ctx context.Context, token string) error {
	invalid := apperr.Validation(map[string][]string{"token": {"This unsubscribe link is invalid."}})
	if s.channels.encrypter == nil {
		return invalid
	}
	raw, err := s.channels.encrypter.Decrypt(token)
	if err != nil {
		return invalid
	}
	var claims unsubscribeClaims
	if json.Unmarshal(raw, &claims) != nil || claims.UserID <= 0 {
		return invalid
	}
	event, ok := Events[claims.EventType]
	if !ok || event.Mandatory {
		return invalid
	}
	for _, channel := range event.Channels {
		if err := s.repo.SetPreference(ctx, claims.UserID, claims.EventType, string(channel), false); err != nil {
			return err
		}
	}
	return nil
}

// Preference is one event type × channel of a user, with the user's choice.
type Preference struct {
	EventType   string `json:"event_type"`
	Description string `json:"description"`
	Channel     string `json:"channel"`
	Enabled     bool   `json:"enabled"`
	Mandatory   bool   `json:"mandatory"`
}

// PreferenceInput changes one preference.
type PreferenceInput struct {
	EventType string
	Channel   string
	Enabled   bool
}

// Preferences lists every registered event type × channel for a user.
func (s *Service) Preferences(ctx context.Context, userID int64) ([]Preference, error) {
	stored, err := s.repo.Preferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(Events))
	for name := range Events {
		names = append(names, name)
	}
	slices.Sort(names)
	var out []Preference
	for _, name := range names {
		event := Events[name]
		for _, channel := range event.Channels {
			enabled, found := stored[name+"|"+string(channel)]
			out = append(out, Preference{
				EventType: name, Description: event.Description, Channel: string(channel),
				Enabled: !found || enabled || event.Mandatory, Mandatory: event.Mandatory,
			})
		}
	}
	return out, nil
}

// SetPreferences stores a user's choices; mandatory events and channels an
// event does not use are refused.
func (s *Service) SetPreferences(ctx context.Context, userID int64, inputs []PreferenceInput) error {
	for i, input := range inputs {
		event, ok := Events[input.EventType]
		field := fmt.Sprintf("preferences.%d", i)
		switch {
		case !ok:
			return apperr.Validation(map[string][]string{field: {"The event type is invalid."}})
		case event.Mandatory:
			return apperr.Validation(map[string][]string{field: {"This notification cannot be turned off."}})
		case !slices.Contains(event.Channels, delivery.Channel(input.Channel)):
			return apperr.Validation(map[string][]string{field: {"The event does not use this channel."}})
		}
	}
	for _, input := range inputs {
		if err := s.repo.SetPreference(ctx, userID, input.EventType, input.Channel, input.Enabled); err != nil {
			return err
		}
	}
	return nil
}
