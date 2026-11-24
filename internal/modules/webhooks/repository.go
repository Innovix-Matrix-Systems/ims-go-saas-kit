package webhooks

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Repository persists endpoints and deliveries. Every read and write of a
// tenant's rows takes a tenancy.Filter.
type Repository interface {
	CreateEndpoint(ctx context.Context, input EndpointInput) (*Endpoint, error)
	GetEndpoint(ctx context.Context, tenant tenancy.Filter, id int64) (*Endpoint, error)
	ListEndpoints(ctx context.Context, tenant tenancy.Filter) ([]Endpoint, error)
	UpdateEndpoint(ctx context.Context, tenant tenancy.Filter, id int64, input EndpointInput) (*Endpoint, error)
	RotateSecret(ctx context.Context, tenant tenancy.Filter, id int64, secret string, previousExpires time.Time, actorID int64) (*Endpoint, error)
	DeleteEndpoint(ctx context.Context, tenant tenancy.Filter, id int64) error
	SubscribedEndpoints(ctx context.Context, tenantID int64, eventType string) ([]Endpoint, error)

	CreateDelivery(ctx context.Context, endpoint Endpoint, eventID, eventType string, payload []byte) (*Delivery, error)
	GetDelivery(ctx context.Context, tenant tenancy.Filter, id int64) (*Delivery, error)
	ListDeliveries(ctx context.Context, tenant tenancy.Filter, endpointID int64, filter DeliveryFilter) ([]Delivery, error)
	RecordAttempt(ctx context.Context, id int64, attempt Attempt) (*Delivery, error)
	ResetFailures(ctx context.Context, endpointID int64) error
	CountFailure(ctx context.Context, endpointID int64, disableAfter int, reason string) (*Endpoint, error)
	PruneDeliveries(ctx context.Context, before time.Time) (int64, error)
}

// EndpointInput is a new or changed endpoint (Secret only on create).
type EndpointInput struct {
	TenantID    int64
	URL         string
	Description string
	Events      []string
	Secret      string
	IsActive    bool
	ActorID     int64
}

// DeliveryFilter narrows an endpoint's delivery log (newest first).
type DeliveryFilter struct {
	Status   string
	BeforeID int64
	Limit    int32
}

// Attempt is the outcome of one delivery attempt.
type Attempt struct {
	Status         string
	ResponseStatus int
	ResponseBody   string
	Duration       time.Duration
	Error          string
	NextAttemptAt  time.Time
}

// NewRepository returns the sqlc-backed repository.
func NewRepository(queries *dbgen.Queries) Repository { return &repository{queries: queries} }

type repository struct{ queries *dbgen.Queries }

func (r *repository) CreateEndpoint(ctx context.Context, input EndpointInput) (*Endpoint, error) {
	row, err := r.queries.CreateWebhookEndpoint(ctx, dbgen.CreateWebhookEndpointParams{
		TenantID: input.TenantID, Url: input.URL, Description: input.Description,
		Events: input.Events, Secret: input.Secret, ActorID: &input.ActorID,
	})
	if err != nil {
		return nil, database.MapError("webhooks.create_endpoint", err)
	}
	endpoint := mapEndpoint(row)
	return &endpoint, nil
}

func (r *repository) GetEndpoint(ctx context.Context, tenant tenancy.Filter, id int64) (*Endpoint, error) {
	row, err := r.queries.GetWebhookEndpoint(ctx, dbgen.GetWebhookEndpointParams{ID: id, AllTenants: tenant.All, TenantID: tenant.TenantID})
	if err != nil {
		return nil, notFound(err, "webhooks.get_endpoint", "Webhook endpoint", id)
	}
	endpoint := mapEndpoint(row)
	return &endpoint, nil
}

func (r *repository) ListEndpoints(ctx context.Context, tenant tenancy.Filter) ([]Endpoint, error) {
	rows, err := r.queries.ListWebhookEndpoints(ctx, dbgen.ListWebhookEndpointsParams{AllTenants: tenant.All, TenantID: tenant.TenantID})
	if err != nil {
		return nil, database.MapError("webhooks.list_endpoints", err)
	}
	endpoints := make([]Endpoint, 0, len(rows))
	for _, row := range rows {
		endpoints = append(endpoints, mapEndpoint(row))
	}
	return endpoints, nil
}

func (r *repository) UpdateEndpoint(ctx context.Context, tenant tenancy.Filter, id int64, input EndpointInput) (*Endpoint, error) {
	row, err := r.queries.UpdateWebhookEndpoint(ctx, dbgen.UpdateWebhookEndpointParams{
		ID: id, AllTenants: tenant.All, TenantID: tenant.TenantID,
		Url: input.URL, Description: input.Description, Events: input.Events,
		IsActive: input.IsActive, ActorID: &input.ActorID,
	})
	if err != nil {
		return nil, notFound(err, "webhooks.update_endpoint", "Webhook endpoint", id)
	}
	endpoint := mapEndpoint(row)
	return &endpoint, nil
}

func (r *repository) RotateSecret(ctx context.Context, tenant tenancy.Filter, id int64, secret string, previousExpires time.Time, actorID int64) (*Endpoint, error) {
	row, err := r.queries.RotateWebhookSecret(ctx, dbgen.RotateWebhookSecretParams{
		ID: id, AllTenants: tenant.All, TenantID: tenant.TenantID,
		Secret: secret, PreviousExpiresAt: timestamptz(previousExpires), ActorID: &actorID,
	})
	if err != nil {
		return nil, notFound(err, "webhooks.rotate_secret", "Webhook endpoint", id)
	}
	endpoint := mapEndpoint(row)
	return &endpoint, nil
}

func (r *repository) DeleteEndpoint(ctx context.Context, tenant tenancy.Filter, id int64) error {
	affected, err := r.queries.DeleteWebhookEndpoint(ctx, dbgen.DeleteWebhookEndpointParams{ID: id, AllTenants: tenant.All, TenantID: tenant.TenantID})
	if err != nil {
		return database.MapError("webhooks.delete_endpoint", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.ResourceNotFound, "Webhook endpoint with ID '%d' was not found.", id)
	}
	return nil
}

func (r *repository) SubscribedEndpoints(ctx context.Context, tenantID int64, eventType string) ([]Endpoint, error) {
	rows, err := r.queries.ListSubscribedWebhookEndpoints(ctx, dbgen.ListSubscribedWebhookEndpointsParams{TenantID: tenantID, EventType: eventType})
	if err != nil {
		return nil, database.MapError("webhooks.subscribed_endpoints", err)
	}
	endpoints := make([]Endpoint, 0, len(rows))
	for _, row := range rows {
		endpoints = append(endpoints, mapEndpoint(row))
	}
	return endpoints, nil
}

func (r *repository) CreateDelivery(ctx context.Context, endpoint Endpoint, eventID, eventType string, payload []byte) (*Delivery, error) {
	row, err := r.queries.CreateWebhookDelivery(ctx, dbgen.CreateWebhookDeliveryParams{
		TenantID: endpoint.TenantID, EndpointID: endpoint.ID, EventID: eventID, EventType: eventType, Payload: payload,
	})
	if err != nil {
		return nil, database.MapError("webhooks.create_delivery", err)
	}
	delivery := mapDelivery(row)
	return &delivery, nil
}

func (r *repository) GetDelivery(ctx context.Context, tenant tenancy.Filter, id int64) (*Delivery, error) {
	row, err := r.queries.GetWebhookDelivery(ctx, dbgen.GetWebhookDeliveryParams{ID: id, AllTenants: tenant.All, TenantID: tenant.TenantID})
	if err != nil {
		return nil, notFound(err, "webhooks.get_delivery", "Webhook delivery", id)
	}
	delivery := mapDelivery(row)
	return &delivery, nil
}

func (r *repository) ListDeliveries(ctx context.Context, tenant tenancy.Filter, endpointID int64, filter DeliveryFilter) ([]Delivery, error) {
	params := dbgen.ListWebhookDeliveriesParams{
		EndpointID: endpointID, AllTenants: tenant.All, TenantID: tenant.TenantID,
		Status: filter.Status, RowLimit: filter.Limit,
	}
	if filter.BeforeID > 0 {
		params.BeforeID = &filter.BeforeID
	}
	rows, err := r.queries.ListWebhookDeliveries(ctx, params)
	if err != nil {
		return nil, database.MapError("webhooks.list_deliveries", err)
	}
	deliveries := make([]Delivery, 0, len(rows))
	for _, row := range rows {
		deliveries = append(deliveries, mapDelivery(row))
	}
	return deliveries, nil
}

func (r *repository) RecordAttempt(ctx context.Context, id int64, attempt Attempt) (*Delivery, error) {
	params := dbgen.RecordWebhookAttemptParams{
		ID: id, Status: attempt.Status, NextAttemptAt: timestamptz(attempt.NextAttemptAt),
	}
	duration := int32(attempt.Duration.Milliseconds()) // #nosec G115 -- bounded by the delivery timeout
	params.DurationMs = &duration
	if attempt.ResponseStatus > 0 {
		status := int32(attempt.ResponseStatus) // #nosec G115 -- an HTTP status code
		params.ResponseStatus = &status
	}
	if attempt.ResponseBody != "" {
		params.ResponseBody = &attempt.ResponseBody
	}
	if attempt.Error != "" {
		params.Error = &attempt.Error
	}
	row, err := r.queries.RecordWebhookAttempt(ctx, params)
	if err != nil {
		return nil, database.MapError("webhooks.record_attempt", err)
	}
	delivery := mapDelivery(row)
	return &delivery, nil
}

func (r *repository) ResetFailures(ctx context.Context, endpointID int64) error {
	if err := r.queries.ResetWebhookFailures(ctx, endpointID); err != nil {
		return database.MapError("webhooks.reset_failures", err)
	}
	return nil
}

func (r *repository) CountFailure(ctx context.Context, endpointID int64, disableAfter int, reason string) (*Endpoint, error) {
	row, err := r.queries.CountWebhookFailure(ctx, dbgen.CountWebhookFailureParams{
		ID: endpointID, DisableAfter: int32(disableAfter), Reason: reason, // #nosec G115 -- a small configured count
	})
	if err != nil {
		return nil, database.MapError("webhooks.count_failure", err)
	}
	endpoint := mapEndpoint(row)
	return &endpoint, nil
}

func (r *repository) PruneDeliveries(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.queries.PruneWebhookDeliveries(ctx, timestamptz(before))
	if err != nil {
		return 0, database.MapError("webhooks.prune_deliveries", err)
	}
	return n, nil
}

func notFound(err error, op, what string, id int64) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.Newf(apperr.ResourceNotFound, "%s with ID '%d' was not found.", what, id)
	}
	return database.MapError(op, err)
}
