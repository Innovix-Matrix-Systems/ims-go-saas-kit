package currencies

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
)

// Repository is the persistence contract for currencies.
type Repository interface {
	List(ctx context.Context) ([]Currency, error)
	Get(ctx context.Context, id int64) (*Currency, error)
	Create(ctx context.Context, input Input, actorID *int64) (*Currency, error)
	Update(ctx context.Context, id int64, input Input, actorID *int64) (*Currency, error)
	SetDefault(ctx context.Context, id int64, actorID *int64) error
	Delete(ctx context.Context, id int64) error
	Ensure(ctx context.Context, currency Currency) error
}

// Input carries a currency's create/update fields (the code is set once).
type Input struct {
	Code               string
	Name               string
	Symbol             string
	SymbolFirst        bool
	Precision          int
	DecimalMark        string
	ThousandsSeparator string
	IsEnabled          bool
}

type repository struct {
	queries *dbgen.Queries
}

// NewRepository creates the sqlc-backed currency repository.
func NewRepository(queries *dbgen.Queries) Repository {
	return &repository{queries: queries}
}

func notFound(id int64) error {
	return apperr.Newf(apperr.ResourceNotFound, "Currency with ID '%d' was not found.", id)
}

func (r *repository) List(ctx context.Context) ([]Currency, error) {
	rows, err := r.queries.ListCurrencies(ctx)
	if err != nil {
		return nil, database.MapError("currencies.list", err)
	}
	items := make([]Currency, 0, len(rows))
	for _, row := range rows {
		item := mapCurrency(row.Currency)
		item.InUse = row.InUse
		items = append(items, item)
	}
	return items, nil
}

func (r *repository) Get(ctx context.Context, id int64) (*Currency, error) {
	row, err := r.queries.GetCurrency(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound(id)
		}
		return nil, database.MapError("currencies.get", err)
	}
	item := mapCurrency(row.Currency)
	item.InUse = row.InUse
	return &item, nil
}

func (r *repository) Create(ctx context.Context, input Input, actorID *int64) (*Currency, error) {
	row, err := r.queries.CreateCurrency(ctx, dbgen.CreateCurrencyParams{
		Code: input.Code, Name: input.Name, Symbol: input.Symbol, SymbolFirst: input.SymbolFirst,
		Precision: int16(input.Precision), DecimalMark: input.DecimalMark,
		ThousandsSeparator: input.ThousandsSeparator, IsEnabled: input.IsEnabled, ActorID: actorID,
	})
	if err != nil {
		return nil, database.MapError("currencies.create", err)
	}
	item := mapCurrency(row)
	return &item, nil
}

func (r *repository) Update(ctx context.Context, id int64, input Input, actorID *int64) (*Currency, error) {
	row, err := r.queries.UpdateCurrency(ctx, dbgen.UpdateCurrencyParams{
		ID: id, Name: input.Name, Symbol: input.Symbol, SymbolFirst: input.SymbolFirst,
		Precision: int16(input.Precision), DecimalMark: input.DecimalMark,
		ThousandsSeparator: input.ThousandsSeparator, IsEnabled: input.IsEnabled, ActorID: actorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound(id)
		}
		return nil, database.MapError("currencies.update", err)
	}
	item := mapCurrency(row)
	return &item, nil
}

// SetDefault moves the default to id; call it inside a transaction.
func (r *repository) SetDefault(ctx context.Context, id int64, actorID *int64) error {
	if err := r.queries.ClearDefaultCurrency(ctx); err != nil {
		return database.MapError("currencies.clear_default", err)
	}
	updated, err := r.queries.SetDefaultCurrency(ctx, dbgen.SetDefaultCurrencyParams{ID: id, ActorID: actorID})
	if err != nil {
		return database.MapError("currencies.set_default", err)
	}
	if updated == 0 {
		return notFound(id)
	}
	return nil
}

func (r *repository) Delete(ctx context.Context, id int64) error {
	deleted, err := r.queries.DeleteCurrency(ctx, id)
	if err != nil {
		return database.MapError("currencies.delete", err)
	}
	if deleted == 0 {
		return notFound(id)
	}
	return nil
}

func (r *repository) Ensure(ctx context.Context, c Currency) error {
	err := r.queries.EnsureCurrency(ctx, dbgen.EnsureCurrencyParams{
		Code: c.Code, Name: c.Name, Symbol: c.Symbol, SymbolFirst: c.SymbolFirst, Precision: int16(c.Precision),
		DecimalMark: c.DecimalMark, ThousandsSeparator: c.ThousandsSeparator,
	})
	if err != nil {
		return database.MapError("currencies.ensure", err)
	}
	return nil
}
