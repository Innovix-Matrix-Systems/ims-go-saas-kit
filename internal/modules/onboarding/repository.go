// Package onboarding lets people create an organization themselves (public
// sign-up) and tracks each new organization's first-run setup.
package onboarding

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// Progress is an organization's first-run setup.
type Progress struct {
	TenantID     int64
	OwnerUserID  *int64
	Source       string
	CompletedAt  *time.Time
	SkippedSteps []string
	CreatedAt    time.Time
}

// Agreement is one accepted document version.
type Agreement struct {
	ID         int64
	UserID     int64
	TenantID   *int64
	Document   string
	Version    string
	AcceptedAt time.Time
	IPAddress  *string
	UserAgent  *string
}

// Repository persists onboarding progress and accepted agreements.
type Repository interface {
	CreateProgress(ctx context.Context, tenantID int64, ownerUserID *int64, source string) (*Progress, error)
	GetProgress(ctx context.Context, tenantID int64) (*Progress, error)
	CompleteProgress(ctx context.Context, tenantID int64, skipped []string) (*Progress, error)
	RecordAgreement(ctx context.Context, input Agreement) (*Agreement, error)
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func (r *repository) CreateProgress(ctx context.Context, tenantID int64, ownerUserID *int64, source string) (*Progress, error) {
	row, err := r.queries.CreateTenantOnboarding(ctx, dbgen.CreateTenantOnboardingParams{
		TenantID: tenantID, OwnerUserID: ownerUserID, Source: source,
	})
	if err != nil {
		return nil, database.MapError("onboarding.create", err)
	}
	return mapProgress(row), nil
}

func (r *repository) GetProgress(ctx context.Context, tenantID int64) (*Progress, error) {
	row, err := r.queries.GetTenantOnboarding(ctx, tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.New(apperr.ResourceNotFound, "This organization has no onboarding record.")
	}
	if err != nil {
		return nil, database.MapError("onboarding.get", err)
	}
	progress := &Progress{
		TenantID: row.TenantID, Source: row.Source, CompletedAt: timePtr(row.CompletedAt),
		SkippedSteps: row.SkippedSteps, CreatedAt: row.CreatedAt.Time,
	}
	if progress.SkippedSteps == nil {
		progress.SkippedSteps = []string{}
	}
	if row.HasOwner {
		owner := row.OwnerUserID
		progress.OwnerUserID = &owner
	}
	return progress, nil
}

func (r *repository) CompleteProgress(ctx context.Context, tenantID int64, skipped []string) (*Progress, error) {
	if skipped == nil {
		skipped = []string{}
	}
	affected, err := r.queries.CompleteTenantOnboarding(ctx, dbgen.CompleteTenantOnboardingParams{
		TenantID: tenantID, SkippedSteps: skipped,
	})
	if err != nil {
		return nil, database.MapError("onboarding.complete", err)
	}
	if affected == 0 {
		return nil, apperr.New(apperr.ResourceNotFound, "This organization has no onboarding record.")
	}
	return r.GetProgress(ctx, tenantID)
}

func (r *repository) RecordAgreement(ctx context.Context, input Agreement) (*Agreement, error) {
	row, err := r.queries.CreateUserAgreement(ctx, dbgen.CreateUserAgreementParams{
		UserID: input.UserID, TenantID: input.TenantID, Document: input.Document, Version: input.Version,
		IpAddress: input.IPAddress, UserAgent: input.UserAgent,
	})
	if err != nil {
		return nil, database.MapError("onboarding.record_agreement", err)
	}
	return mapAgreement(row), nil
}

func mapProgress(row dbgen.TenantOnboarding) *Progress {
	skipped := row.SkippedSteps
	if skipped == nil {
		skipped = []string{}
	}
	return &Progress{
		TenantID: row.TenantID, OwnerUserID: row.OwnerUserID, Source: row.Source,
		CompletedAt: timePtr(row.CompletedAt), SkippedSteps: skipped, CreatedAt: row.CreatedAt.Time,
	}
}

func mapAgreement(row dbgen.UserAgreement) *Agreement {
	return &Agreement{
		ID: row.ID, UserID: row.UserID, TenantID: row.TenantID, Document: row.Document, Version: row.Version,
		AcceptedAt: row.AcceptedAt.Time, IPAddress: row.IpAddress, UserAgent: row.UserAgent,
	}
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}
