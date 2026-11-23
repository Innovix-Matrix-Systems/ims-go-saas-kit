package delivery

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

// Repository is the delivery log. No HTTP concepts.
type Repository interface {
	Create(ctx context.Context, entry Entry) (int64, error)
	MarkSent(ctx context.Context, id int64, providerMessageID string) error
	MarkFailed(ctx context.Context, id int64, reason string) error
	Get(ctx context.Context, id int64) (*Record, error)
	List(ctx context.Context, filter ListFilter) ([]Record, error)
	Prune(ctx context.Context, before time.Time) (int64, error)
}

// Entry is a new delivery log row.
type Entry struct {
	TenantID int64
	UserID   int64
	Channel  Channel
	Address  string // masked
	Template string
	Status   string
	Error    string
}

// ListFilter narrows the delivery log (newest first, keyset by id).
type ListFilter struct {
	Channel  string
	Status   string
	Template string
	UserID   int64
	BeforeID int64
	Limit    int32
}

// NewRepository returns the sqlc-backed delivery log.
func NewRepository(queries *dbgen.Queries) Repository { return &sqlRepository{queries: queries} }

type sqlRepository struct{ queries *dbgen.Queries }

func (r *sqlRepository) Create(ctx context.Context, entry Entry) (int64, error) {
	row, err := r.queries.CreateDelivery(ctx, dbgen.CreateDeliveryParams{
		TenantID: optionalID(entry.TenantID),
		UserID:   optionalID(entry.UserID),
		Channel:  string(entry.Channel),
		Address:  entry.Address,
		Template: entry.Template,
		Status:   entry.Status,
		Error:    optionalText(entry.Error),
	})
	if err != nil {
		return 0, database.MapError("delivery.create", err)
	}
	return row.ID, nil
}

func (r *sqlRepository) MarkSent(ctx context.Context, id int64, providerMessageID string) error {
	err := r.queries.MarkDeliverySent(ctx, dbgen.MarkDeliverySentParams{
		ID: id, ProviderMessageID: optionalText(providerMessageID),
	})
	if err != nil {
		return database.MapError("delivery.mark_sent", err)
	}
	return nil
}

func (r *sqlRepository) MarkFailed(ctx context.Context, id int64, reason string) error {
	err := r.queries.MarkDeliveryFailed(ctx, dbgen.MarkDeliveryFailedParams{ID: id, Error: optionalText(truncate(reason, 1000))})
	if err != nil {
		return database.MapError("delivery.mark_failed", err)
	}
	return nil
}

func (r *sqlRepository) Get(ctx context.Context, id int64) (*Record, error) {
	row, err := r.queries.GetDelivery(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Delivery with ID '%d' was not found.", id)
	}
	if err != nil {
		return nil, database.MapError("delivery.get", err)
	}
	record := toRecord(row)
	return &record, nil
}

func (r *sqlRepository) List(ctx context.Context, filter ListFilter) ([]Record, error) {
	rows, err := r.queries.ListDeliveries(ctx, dbgen.ListDeliveriesParams{
		Channel:  filter.Channel,
		Status:   filter.Status,
		Template: filter.Template,
		UserID:   optionalID(filter.UserID),
		BeforeID: optionalID(filter.BeforeID),
		RowLimit: filter.Limit,
	})
	if err != nil {
		return nil, database.MapError("delivery.list", err)
	}
	records := make([]Record, 0, len(rows))
	for _, row := range rows {
		records = append(records, toRecord(row))
	}
	return records, nil
}

func (r *sqlRepository) Prune(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.queries.PruneDeliveries(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return 0, database.MapError("delivery.prune", err)
	}
	return n, nil
}

func toRecord(row dbgen.Delivery) Record {
	record := Record{
		ID:                row.ID,
		TenantID:          row.TenantID,
		UserID:            row.UserID,
		Channel:           row.Channel,
		Address:           row.Address,
		Template:          row.Template,
		Status:            row.Status,
		Attempts:          row.Attempts,
		Error:             row.Error,
		ProviderMessageID: row.ProviderMessageID,
		CreatedAt:         row.CreatedAt.Time,
	}
	if row.SentAt.Valid {
		sent := row.SentAt.Time
		record.SentAt = &sent
	}
	return record
}

func optionalID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

func optionalText(text string) *string {
	if text == "" {
		return nil
	}
	return &text
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

// MaskAddress hides most of an address for the delivery log:
// a***@acme.test, +88017*****12.
func MaskAddress(channel Channel, address string) string {
	switch channel {
	case Mail:
		local, domain, ok := strings.Cut(address, "@")
		if !ok || local == "" {
			return "***"
		}
		return string([]rune(local)[0]) + "***@" + domain
	case SMS:
		digits := []rune(address)
		if len(digits) <= 7 {
			return "***"
		}
		return string(digits[:6]) + strings.Repeat("*", len(digits)-8) + string(digits[len(digits)-2:])
	default:
		return address
	}
}
