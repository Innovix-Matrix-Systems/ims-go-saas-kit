package webhooks

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/crypto"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/queue"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/tracing"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// KindDeliver is the delivery job: one attempt of one delivery.
const KindDeliver = "webhook.deliver"

// retrySchedule is the wait before each retry after a failed attempt: eight
// attempts over about a day, then the delivery is abandoned.
var retrySchedule = []time.Duration{
	time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 5 * time.Hour, 10 * time.Hour, 10 * time.Hour,
}

// secretGrace is how long the previous secret keeps signing after a rotation.
const secretGrace = 24 * time.Hour

// maxResponseBody is how much of an endpoint's answer the log keeps.
const maxResponseBody = 4 << 10

// FeatureGate tells whether a tenant's plan has a feature (api.access).
type FeatureGate interface {
	Allows(ctx context.Context, tenantID int64, feature string) bool
}

// Notifier tells a tenant an endpoint was disabled after failing repeatedly.
type Notifier interface {
	EndpointDisabled(ctx context.Context, endpoint Endpoint)
}

// Service manages endpoints and delivers events.
type Service struct {
	repo      Repository
	encrypter *crypto.Encrypter
	queue     queue.Engine
	client    *http.Client
	cfg       config.Webhooks
	features  FeatureGate
	notifier  Notifier
	activity  *activity.Recorder
	userAgent string
	logger    *slog.Logger
	now       func() time.Time
}

// NewService creates the webhook service. userAgent names the sender
// ("ims-go-admin-starter-Webhooks/0.1.7").
func NewService(repo Repository, encrypter *crypto.Encrypter, engine queue.Engine, cfg config.Webhooks, userAgent string, logger *slog.Logger) *Service {
	return &Service{
		repo: repo, encrypter: encrypter, queue: engine, cfg: cfg, userAgent: userAgent, logger: logger,
		client: newClient(cfg.Timeout, cfg.AllowPrivate), now: time.Now,
	}
}

// WithFeatures requires the plan's api.access to manage endpoints.
func (s *Service) WithFeatures(features FeatureGate) *Service {
	s.features = features
	return s
}

// WithNotifier tells tenants about disabled endpoints.
func (s *Service) WithNotifier(notifier Notifier) *Service {
	s.notifier = notifier
	return s
}

// WithActivity records endpoint changes in the activity log.
func (s *Service) WithActivity(recorder *activity.Recorder) *Service {
	s.activity = recorder
	return s
}

// WithClient replaces the delivery HTTP client (tests).
func (s *Service) WithClient(client *http.Client) *Service {
	s.client = client
	return s
}

// ── endpoints ──────────────────────────────────────────────────────────

// CreateInput is a new endpoint.
type CreateInput struct {
	URL         string
	Description string
	Events      []string
}

// UpdateInput changes an endpoint; nil fields keep their value.
type UpdateInput struct {
	URL         *string
	Description *string
	Events      []string
	IsActive    *bool
}

// Create adds an endpoint for the scope's tenant and returns it with its
// signing secret, shown this once.
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, actorID int64, input CreateInput) (_ *Endpoint, _ string, err error) {
	ctx, end := tracing.Begin(ctx, "webhooks.create", attribute.Int64("tenant.id", scope.EffectiveTenantID()))
	defer end(&err)
	tenantID := scope.EffectiveTenantID()
	if tenantID == 0 {
		return nil, "", apperr.New(apperr.TenantContextNotResolved, "A tenant context is required.")
	}
	if err := s.requirePlan(ctx, tenantID); err != nil {
		return nil, "", err
	}
	if err := s.validate(input.URL, input.Events); err != nil {
		return nil, "", err
	}
	secret, sealed, err := s.newSealedSecret()
	if err != nil {
		return nil, "", err
	}
	endpoint, err := s.repo.CreateEndpoint(ctx, EndpointInput{
		TenantID: tenantID, URL: strings.TrimSpace(input.URL), Description: input.Description,
		Events: input.Events, Secret: sealed, IsActive: true, ActorID: actorID,
	})
	if err != nil {
		return nil, "", err
	}
	s.record(ctx, actorID, "webhook.created", endpoint)
	return endpoint, secret, nil
}

// List returns the scope's endpoints.
func (s *Service) List(ctx context.Context, scope tenancy.Scope) ([]Endpoint, error) {
	return s.repo.ListEndpoints(ctx, scope.Filter())
}

// Get returns one endpoint of the scope.
func (s *Service) Get(ctx context.Context, scope tenancy.Scope, id int64) (*Endpoint, error) {
	return s.repo.GetEndpoint(ctx, scope.Filter(), id)
}

// Update changes an endpoint; turning it back on resets its failure count.
func (s *Service) Update(ctx context.Context, scope tenancy.Scope, actorID, id int64, input UpdateInput) (_ *Endpoint, err error) {
	ctx, end := tracing.Begin(ctx, "webhooks.update",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
		attribute.Int64("webhook.id", id),
	)
	defer end(&err)
	current, err := s.repo.GetEndpoint(ctx, scope.Filter(), id)
	if err != nil {
		return nil, err
	}
	next := EndpointInput{URL: current.URL, Description: current.Description, Events: current.Events, IsActive: current.IsActive, ActorID: actorID}
	if input.URL != nil {
		next.URL = strings.TrimSpace(*input.URL)
	}
	if input.Description != nil {
		next.Description = *input.Description
	}
	if input.Events != nil {
		next.Events = input.Events
	}
	if input.IsActive != nil {
		next.IsActive = *input.IsActive
	}
	if err := s.validate(next.URL, next.Events); err != nil {
		return nil, err
	}
	endpoint, err := s.repo.UpdateEndpoint(ctx, scope.Filter(), id, next)
	if err != nil {
		return nil, err
	}
	s.record(ctx, actorID, "webhook.updated", endpoint)
	return endpoint, nil
}

// Delete removes an endpoint and its delivery log.
func (s *Service) Delete(ctx context.Context, scope tenancy.Scope, actorID, id int64) (err error) {
	ctx, end := tracing.Begin(ctx, "webhooks.delete",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
		attribute.Int64("webhook.id", id),
	)
	defer end(&err)
	endpoint, err := s.repo.GetEndpoint(ctx, scope.Filter(), id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteEndpoint(ctx, scope.Filter(), id); err != nil {
		return err
	}
	s.record(ctx, actorID, "webhook.deleted", endpoint)
	return nil
}

// RotateSecret gives an endpoint a new signing secret, returned this once.
// The previous one keeps signing (a second signature) for a day, so the
// receiver can switch without dropping events.
func (s *Service) RotateSecret(ctx context.Context, scope tenancy.Scope, actorID, id int64) (_ *Endpoint, _ string, err error) {
	ctx, end := tracing.Begin(ctx, "webhooks.rotate_secret",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
		attribute.Int64("webhook.id", id),
	)
	defer end(&err)
	secret, sealed, err := s.newSealedSecret()
	if err != nil {
		return nil, "", err
	}
	endpoint, err := s.repo.RotateSecret(ctx, scope.Filter(), id, sealed, s.now().Add(secretGrace), actorID)
	if err != nil {
		return nil, "", err
	}
	s.record(ctx, actorID, "webhook.secret_rotated", endpoint)
	return endpoint, secret, nil
}

// ── deliveries ─────────────────────────────────────────────────────────

// Test sends a webhook.test event to an endpoint now and returns the
// delivery with the endpoint's answer (no retries).
func (s *Service) Test(ctx context.Context, scope tenancy.Scope, id int64) (_ *Delivery, err error) {
	ctx, end := tracing.Begin(ctx, "webhooks.test",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
		attribute.Int64("webhook.id", id),
	)
	defer end(&err)
	endpoint, err := s.repo.GetEndpoint(ctx, scope.Filter(), id)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(map[string]any{"message": "This is a test event."})
	delivery, err := s.createDelivery(ctx, *endpoint, newEventID(), EventTest, data)
	if err != nil {
		return nil, err
	}
	return s.attemptNow(ctx, *endpoint, *delivery)
}

// Deliveries lists an endpoint's deliveries, newest first.
func (s *Service) Deliveries(ctx context.Context, scope tenancy.Scope, endpointID int64, filter DeliveryFilter) ([]Delivery, error) {
	if _, err := s.repo.GetEndpoint(ctx, scope.Filter(), endpointID); err != nil {
		return nil, err
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 25
	}
	return s.repo.ListDeliveries(ctx, scope.Filter(), endpointID, filter)
}

// Redeliver sends a delivery again now (same event id, so the receiver can
// tell it is the same event) and returns the outcome.
func (s *Service) Redeliver(ctx context.Context, scope tenancy.Scope, deliveryID int64) (_ *Delivery, err error) {
	ctx, end := tracing.Begin(ctx, "webhooks.redeliver",
		attribute.Int64("tenant.id", scope.EffectiveTenantID()),
		attribute.Int64("delivery.id", deliveryID),
	)
	defer end(&err)
	delivery, err := s.repo.GetDelivery(ctx, scope.Filter(), deliveryID)
	if err != nil {
		return nil, err
	}
	endpoint, err := s.repo.GetEndpoint(ctx, scope.Filter(), delivery.EndpointID)
	if err != nil {
		return nil, err
	}
	return s.attemptNow(ctx, *endpoint, *delivery)
}

// Recorded implements activity.Sink: a deliverable entry of a tenant is
// queued once for every endpoint subscribed to it.
func (s *Service) Recorded(ctx context.Context, entry activity.Recorded) {
	if entry.TenantID == 0 || !deliverable(entry.Action) {
		return
	}
	endpoints, err := s.repo.SubscribedEndpoints(ctx, entry.TenantID, entry.Action)
	if err != nil {
		s.logger.Warn("webhooks: endpoints unavailable", "action", entry.Action, "error", err)
		return
	}
	if len(endpoints) == 0 {
		return
	}
	data, _ := json.Marshal(map[string]any{
		"subject": entry.Subject, "subject_id": entry.SubjectID, "actor_id": entry.ActorID, "changes": entry.Changes,
	})
	eventID := newEventID()
	for _, endpoint := range endpoints {
		delivery, err := s.createDelivery(ctx, endpoint, eventID, entry.Action, data)
		if err == nil {
			err = s.enqueue(ctx, delivery.ID, time.Time{})
		}
		if err != nil {
			s.logger.Warn("webhooks: delivery not queued", "endpoint_id", endpoint.ID, "action", entry.Action, "error", err)
		}
	}
}

// RegisterJob adds the webhook.deliver job.
func (s *Service) RegisterJob(registry *queue.Registry) error {
	return registry.Register(queue.Definition{
		Kind:        KindDeliver,
		Description: "Sends one webhook delivery to its endpoint and schedules the next attempt when it fails.",
		Queue:       queue.QueueDefault,
		Timeout:     s.cfg.Timeout + 30*time.Second,
		// The delivery row names its tenant. A failed HTTP attempt is not a
		// job error: the job records it and queues the next attempt itself
		// (retrySchedule). Only infrastructure errors (the database) reach
		// the queue, which retries them with the channel's default tries.
		SystemJob: true,
		Validate: func(payload []byte) error {
			var input deliverPayload
			if err := json.Unmarshal(payload, &input); err != nil || input.DeliveryID <= 0 {
				return apperr.New(apperr.InvalidInput, "webhook.deliver payload requires delivery_id")
			}
			return nil
		},
		Handle: func(ctx context.Context, payload []byte) error {
			var input deliverPayload
			if err := json.Unmarshal(payload, &input); err != nil {
				return err
			}
			return s.deliver(ctx, input.DeliveryID)
		},
	})
}

// Prune deletes delivery logs older than WEBHOOK_RETENTION_DAYS.
func (s *Service) Prune(ctx context.Context) (_ int64, err error) {
	ctx, end := tracing.Begin(ctx, "webhooks.prune")
	defer end(&err)
	if s.cfg.RetentionDays <= 0 {
		return 0, nil
	}
	return s.repo.PruneDeliveries(ctx, s.now().AddDate(0, 0, -s.cfg.RetentionDays))
}

type deliverPayload struct {
	DeliveryID int64 `json:"delivery_id"`
}

// deliver makes one attempt of a pending delivery: success, the next retry
// queued, or — after the last attempt — abandoned, which may disable the
// endpoint.
func (s *Service) deliver(ctx context.Context, deliveryID int64) error {
	delivery, err := s.repo.GetDelivery(ctx, tenancy.Unscoped(), deliveryID)
	if err != nil {
		if apperr.Is(err, apperr.ResourceNotFound) {
			return nil // the endpoint was deleted with its log
		}
		return err
	}
	if delivery.Status != StatusPending {
		return nil
	}
	endpoint, err := s.repo.GetEndpoint(ctx, tenancy.Unscoped(), delivery.EndpointID)
	if err != nil {
		return err
	}
	if !endpoint.IsActive {
		_, err := s.repo.RecordAttempt(ctx, delivery.ID, Attempt{Status: StatusAbandoned, Error: "the endpoint is disabled"})
		return err
	}
	attempt := s.attempt(ctx, *endpoint, *delivery)
	if attempt.Status == StatusSucceeded {
		if _, err := s.repo.RecordAttempt(ctx, delivery.ID, attempt); err != nil {
			return err
		}
		return s.repo.ResetFailures(ctx, endpoint.ID)
	}
	made := int(delivery.Attempts) + 1
	if made <= len(retrySchedule) {
		attempt.Status, attempt.NextAttemptAt = StatusPending, s.now().Add(retrySchedule[made-1])
		if _, err := s.repo.RecordAttempt(ctx, delivery.ID, attempt); err != nil {
			return err
		}
		return s.enqueue(ctx, delivery.ID, attempt.NextAttemptAt)
	}
	attempt.Status = StatusAbandoned
	if _, err := s.repo.RecordAttempt(ctx, delivery.ID, attempt); err != nil {
		return err
	}
	updated, err := s.repo.CountFailure(ctx, endpoint.ID, s.cfg.DisableAfter,
		fmt.Sprintf("Disabled after %d deliveries in a row failed every attempt.", s.cfg.DisableAfter))
	if err != nil {
		return err
	}
	if endpoint.IsActive && !updated.IsActive && s.notifier != nil {
		s.notifier.EndpointDisabled(ctx, *updated)
	}
	return nil
}

// attemptNow makes one attempt outside the retry schedule (test, redeliver)
// and records it.
func (s *Service) attemptNow(ctx context.Context, endpoint Endpoint, delivery Delivery) (*Delivery, error) {
	attempt := s.attempt(ctx, endpoint, delivery)
	if attempt.Status != StatusSucceeded {
		attempt.Status = StatusAbandoned
	}
	return s.repo.RecordAttempt(ctx, delivery.ID, attempt)
}

// attempt POSTs a delivery to its endpoint, signed, and reports the answer.
func (s *Service) attempt(ctx context.Context, endpoint Endpoint, delivery Delivery) Attempt {
	started := s.now()
	result := Attempt{}
	secrets, err := s.signingSecrets(endpoint)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	signature, err := signatures(delivery.EventID, started, delivery.Payload, secrets...)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(delivery.Payload))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set(HeaderID, delivery.EventID)
	req.Header.Set(HeaderTimestamp, fmt.Sprint(started.Unix()))
	req.Header.Set(HeaderSignature, signature)

	res, err := s.client.Do(req)
	result.Duration = s.now().Sub(started)
	if err != nil {
		result.Error = describeError(err)
		return result
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxResponseBody))
	result.ResponseStatus, result.ResponseBody = res.StatusCode, string(body)
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		result.Status = StatusSucceeded
	} else {
		result.Error = fmt.Sprintf("the endpoint answered %d", res.StatusCode)
	}
	return result
}

func (s *Service) createDelivery(ctx context.Context, endpoint Endpoint, eventID, eventType string, data []byte) (*Delivery, error) {
	payload, err := json.Marshal(Payload{
		ID: eventID, Type: eventType, Timestamp: s.now().UTC().Truncate(time.Second), TenantID: endpoint.TenantID, Data: data,
	})
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "encode webhook payload", err)
	}
	return s.repo.CreateDelivery(ctx, endpoint, eventID, eventType, payload)
}

func (s *Service) enqueue(ctx context.Context, deliveryID int64, at time.Time) error {
	opts := queue.EnqueueOptions{}
	if !at.IsZero() {
		opts.RunAt = &at
	}
	return s.queue.Enqueue(ctx, KindDeliver, deliverPayload{DeliveryID: deliveryID}, opts)
}

// signingSecrets are the current secret and, within a day of a rotation,
// the previous one.
func (s *Service) signingSecrets(endpoint Endpoint) ([]string, error) {
	current, err := s.encrypter.DecryptString(endpoint.Secret)
	if err != nil {
		return nil, errors.New("the signing secret cannot be read (APP_KEY changed?)")
	}
	secrets := []string{current}
	if endpoint.PreviousSecret != nil && endpoint.PreviousSecretExpiresAt != nil && s.now().Before(*endpoint.PreviousSecretExpiresAt) {
		if previous, err := s.encrypter.DecryptString(*endpoint.PreviousSecret); err == nil {
			secrets = append(secrets, previous)
		}
	}
	return secrets, nil
}

func (s *Service) newSealedSecret() (string, string, error) {
	secret, err := newSecret()
	if err != nil {
		return "", "", apperr.Wrap(apperr.InternalServerError, "generate webhook secret", err)
	}
	sealed, err := s.encrypter.EncryptString(secret)
	if err != nil {
		return "", "", apperr.Wrap(apperr.InternalServerError, "encrypt webhook secret", err)
	}
	return secret, sealed, nil
}

func (s *Service) validate(url string, subscribed []string) error {
	fields := map[string][]string{}
	if problem := urlProblem(url, s.cfg.AllowPrivate); problem != "" {
		fields["url"] = []string{problem}
	}
	if len(subscribed) == 0 {
		fields["events"] = []string{"Subscribe to at least one event (or \"*\" for every event)."}
	}
	for _, event := range subscribed {
		if event != "*" && !deliverable(event) {
			fields["events"] = append(fields["events"], fmt.Sprintf("%q is not a webhook event.", event))
		}
	}
	if slices.Contains(subscribed, "*") && len(subscribed) > 1 {
		fields["events"] = append(fields["events"], "\"*\" already covers every event.")
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	return nil
}

func (s *Service) requirePlan(ctx context.Context, tenantID int64) error {
	if s.features != nil && !s.features.Allows(ctx, tenantID, "api.access") {
		return apperr.New(apperr.FeatureNotAvailable, "Webhooks need a plan with API access.")
	}
	return nil
}

// record writes an endpoint change to the activity log (not deliverable as
// a webhook itself).
func (s *Service) record(ctx context.Context, actorID int64, action string, endpoint *Endpoint) {
	s.activity.Record(ctx, activity.Entry{
		TenantID: endpoint.TenantID, ActorID: actorID, Action: action, Subject: "webhook", SubjectID: endpoint.ID,
		Changes: map[string]any{"url": endpoint.URL, "events": endpoint.Events, "is_active": endpoint.IsActive},
	})
}

func newEventID() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return "evt_" + hex.EncodeToString(raw)
}

// describeError keeps a delivery error readable (no Go wrapping noise).
func describeError(err error) string {
	if errors.Is(err, errBlockedAddress) {
		return errBlockedAddress.Error()
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "the endpoint did not answer in time"
	}
	return err.Error()
}
