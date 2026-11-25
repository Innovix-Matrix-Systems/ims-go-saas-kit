package currencies

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/money"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// cacheKey holds every row (formatting reads it on each amount); writes
// clear it.
const (
	cacheKey = "currencies:all"
	cacheTTL = time.Hour
)

// Service implements the currency rules: the default is enabled and can't
// be disabled or deleted, a currency with payments keeps its decimals and
// can't be deleted.
type Service struct {
	repo  Repository
	cache cache.Store
	tx    *database.TxManager
}

// NewService creates the currency service.
func NewService(repo Repository, store cache.Store) *Service {
	return &Service{repo: repo, cache: store}
}

// WithTx makes moving the default one transaction.
func (s *Service) WithTx(tx *database.TxManager) *Service {
	s.tx = tx
	return s
}

// List returns every currency with whether payments use it (staff screen).
func (s *Service) List(ctx context.Context) ([]Currency, error) {
	return s.repo.List(ctx)
}

// Get returns one currency.
func (s *Service) Get(ctx context.Context, id int64) (*Currency, error) {
	return s.repo.Get(ctx, id)
}

// all is every row, cached.
func (s *Service) all(ctx context.Context) ([]Currency, error) {
	if s.cache == nil {
		return s.repo.List(ctx)
	}
	return cache.Remember(ctx, s.cache, cacheKey, cacheTTL, s.repo.List)
}

// Enabled returns the currencies offered to clients, the default first.
func (s *Service) Enabled(ctx context.Context) ([]Currency, error) {
	rows, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	enabled := make([]Currency, 0, len(rows))
	for _, row := range rows {
		if row.IsEnabled {
			enabled = append(enabled, row)
		}
	}
	return enabled, nil
}

// Default returns the default currency: the currency of new payments and of
// plan prices.
func (s *Service) Default(ctx context.Context) (*Currency, error) {
	rows, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.IsDefault {
			return &row, nil
		}
	}
	return nil, apperr.New(apperr.ServiceUnavailable, "No default currency is set.")
}

// DefaultCode is the default currency's code (billing's CurrencySource).
func (s *Service) DefaultCode(ctx context.Context) (string, error) {
	currency, err := s.Default(ctx)
	if err != nil {
		return "", err
	}
	return currency.Code, nil
}

// Format returns how code is written; a code without a row (deleted, or
// recorded before it was added) gets money.Fallback.
func (s *Service) Format(ctx context.Context, code string) money.Currency {
	rows, err := s.all(ctx)
	if err == nil {
		for _, row := range rows {
			if strings.EqualFold(row.Code, code) {
				return row.Money()
			}
		}
	}
	return money.Fallback(code)
}

// Create adds a currency (never the default; see SetDefault).
func (s *Service) Create(ctx context.Context, actorID int64, input Input) (_ *Currency, err error) {
	ctx, end := tracing.Begin(ctx, "currencies.create", attribute.String("currency.code", input.Code))
	defer end(&err)
	currency, err := s.repo.Create(ctx, input, actor(actorID))
	if err != nil {
		return nil, err
	}
	s.forget(ctx)
	return currency, nil
}

// Update changes how a currency is written. Its decimals can't change once
// payments use it (every stored amount would be read 10× off per digit).
func (s *Service) Update(ctx context.Context, actorID, id int64, input Input) (_ *Currency, err error) {
	ctx, end := tracing.Begin(ctx, "currencies.update", attribute.Int64("currency.id", id))
	defer end(&err)
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	problems := map[string][]string{}
	if existing.InUse && input.Precision != existing.Precision {
		problems["precision"] = []string{"The decimals can't change while payments are recorded in this currency."}
	}
	if existing.IsDefault && !input.IsEnabled {
		problems["is_enabled"] = []string{"The default currency can't be disabled. Make another currency the default first."}
	}
	if len(problems) > 0 {
		return nil, apperr.Validation(problems)
	}
	currency, err := s.repo.Update(ctx, id, input, actor(actorID))
	if err != nil {
		return nil, err
	}
	currency.InUse = existing.InUse
	s.forget(ctx)
	return currency, nil
}

// SetDefault makes an enabled currency the default. Amounts are not
// converted: plan prices and new payments are read in the new currency.
func (s *Service) SetDefault(ctx context.Context, actorID, id int64) (_ *Currency, err error) {
	ctx, end := tracing.Begin(ctx, "currencies.set_default", attribute.Int64("currency.id", id))
	defer end(&err)
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !existing.IsEnabled {
		return nil, apperr.Validation(map[string][]string{"is_enabled": {"Enable the currency before making it the default."}})
	}
	move := func(repo Repository) error { return repo.SetDefault(ctx, id, actor(actorID)) }
	if s.tx != nil {
		err = s.tx.WithTx(ctx, func(q *dbgen.Queries) error { return move(NewRepository(q)) })
	} else {
		err = move(s.repo)
	}
	if err != nil {
		return nil, err
	}
	s.forget(ctx)
	return s.repo.Get(ctx, id)
}

// Delete removes a currency that is neither the default nor used by a
// payment (disable those instead).
func (s *Service) Delete(ctx context.Context, id int64) (err error) {
	ctx, end := tracing.Begin(ctx, "currencies.delete", attribute.Int64("currency.id", id))
	defer end(&err)
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	switch {
	case existing.IsDefault:
		return apperr.New(apperr.Conflict, "The default currency can't be deleted. Make another currency the default first.")
	case existing.InUse:
		return apperr.New(apperr.Conflict, "Payments are recorded in this currency, so it can't be deleted. Disable it instead.")
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.forget(ctx)
	return nil
}

// EnsureSeeds adds the seeded currencies that are missing and, when no
// currency is the default, makes the first seed the default (db:seed).
func (s *Service) EnsureSeeds(ctx context.Context) (err error) {
	ctx, end := tracing.Begin(ctx, "currencies.ensure_seeds")
	defer end(&err)
	for _, seed := range Seeds {
		if err := s.repo.Ensure(ctx, seed); err != nil {
			return err
		}
	}
	s.forget(ctx)
	if _, err := s.Default(ctx); err == nil {
		return nil
	}
	rows, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Code == Seeds[0].Code {
			if err := s.repo.SetDefault(ctx, row.ID, nil); err != nil {
				return err
			}
		}
	}
	s.forget(ctx)
	return nil
}

func (s *Service) forget(ctx context.Context) {
	if s.cache != nil {
		_ = s.cache.Delete(ctx, cacheKey)
	}
}

func actor(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}
