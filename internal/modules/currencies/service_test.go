package currencies

import (
	"context"
	"sort"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
)

// fakeRepo keeps currencies in memory; inUse marks codes that payments use.
type fakeRepo struct {
	rows   map[int64]*Currency
	inUse  map[string]bool
	nextID int64
	writes int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: map[int64]*Currency{}, inUse: map[string]bool{}}
}

func (f *fakeRepo) add(c Currency) Currency {
	f.nextID++
	c.ID = f.nextID
	f.rows[c.ID] = &c
	return c
}

func (f *fakeRepo) List(context.Context) ([]Currency, error) {
	out := make([]Currency, 0, len(f.rows))
	for _, row := range f.rows {
		item := *row
		item.InUse = f.inUse[row.Code]
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeRepo) Get(_ context.Context, id int64) (*Currency, error) {
	row, ok := f.rows[id]
	if !ok {
		return nil, notFound(id)
	}
	item := *row
	item.InUse = f.inUse[row.Code]
	return &item, nil
}

func (f *fakeRepo) Create(_ context.Context, input Input, _ *int64) (*Currency, error) {
	f.writes++
	c := f.add(Currency{Code: input.Code, Name: input.Name, Symbol: input.Symbol, SymbolFirst: input.SymbolFirst,
		Precision: input.Precision, DecimalMark: input.DecimalMark, ThousandsSeparator: input.ThousandsSeparator, IsEnabled: input.IsEnabled})
	return &c, nil
}

func (f *fakeRepo) Update(_ context.Context, id int64, input Input, _ *int64) (*Currency, error) {
	f.writes++
	row, ok := f.rows[id]
	if !ok {
		return nil, notFound(id)
	}
	row.Name, row.Symbol, row.SymbolFirst, row.Precision = input.Name, input.Symbol, input.SymbolFirst, input.Precision
	row.DecimalMark, row.ThousandsSeparator, row.IsEnabled = input.DecimalMark, input.ThousandsSeparator, input.IsEnabled
	item := *row
	return &item, nil
}

func (f *fakeRepo) SetDefault(_ context.Context, id int64, _ *int64) error {
	f.writes++
	if _, ok := f.rows[id]; !ok {
		return notFound(id)
	}
	for _, row := range f.rows {
		row.IsDefault = row.ID == id
	}
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, id int64) error {
	f.writes++
	if _, ok := f.rows[id]; !ok {
		return notFound(id)
	}
	delete(f.rows, id)
	return nil
}

func (f *fakeRepo) Ensure(_ context.Context, c Currency) error {
	for _, row := range f.rows {
		if row.Code == c.Code {
			return nil
		}
	}
	c.IsDefault, c.IsEnabled = false, true
	f.add(c)
	return nil
}

var usd = Input{Code: "USD", Name: "US Dollar", Symbol: "$", SymbolFirst: true, Precision: 2, DecimalMark: ".", ThousandsSeparator: ",", IsEnabled: true}

func wantCode(t *testing.T, err error, code apperr.Code) {
	t.Helper()
	if !apperr.Is(err, code) {
		t.Fatalf("err = %v, want %s", err, code)
	}
}

func TestFormatMustBeReadable(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, nil)
	bad := usd
	bad.ThousandsSeparator = "."
	_, err := service.Create(context.Background(), 1, bad)
	wantCode(t, err, apperr.ValidationFailed)
	if repo.writes != 0 {
		t.Fatalf("a refused currency was written")
	}
	bad.ThousandsSeparator = ""
	if _, err := service.Create(context.Background(), 1, bad); err != nil {
		t.Fatalf("no thousands separator is allowed: %v", err)
	}
}

func TestDefaultAndInUseRules(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepo()
	service := NewService(repo, nil)
	def := repo.add(Currency{Code: "USD", Precision: 2, DecimalMark: ".", IsEnabled: true, IsDefault: true})
	used := repo.add(Currency{Code: "BDT", Precision: 2, DecimalMark: ".", IsEnabled: true})
	off := repo.add(Currency{Code: "EUR", Precision: 2, DecimalMark: ",", ThousandsSeparator: ".", IsEnabled: false})
	repo.inUse["BDT"] = true

	input := func(c Currency, enabled bool, precision int) Input {
		return Input{Name: c.Code, Symbol: c.Code, Precision: precision, DecimalMark: c.DecimalMark, ThousandsSeparator: c.ThousandsSeparator, IsEnabled: enabled}
	}

	_, err := service.Update(ctx, 1, def.ID, input(def, false, 2))
	wantCode(t, err, apperr.ValidationFailed) // the default stays enabled
	_, err = service.Update(ctx, 1, used.ID, input(used, true, 0))
	wantCode(t, err, apperr.ValidationFailed) // decimals locked by payments
	if _, err := service.Update(ctx, 1, used.ID, input(used, true, 2)); err != nil {
		t.Fatalf("editing a used currency without touching its decimals: %v", err)
	}

	_, err = service.SetDefault(ctx, 1, off.ID)
	wantCode(t, err, apperr.ValidationFailed) // enable it first
	if moved, err := service.SetDefault(ctx, 1, used.ID); err != nil || !moved.IsDefault {
		t.Fatalf("SetDefault(BDT) = %+v, %v", moved, err)
	}
	if code, _ := service.DefaultCode(ctx); code != "BDT" {
		t.Fatalf("DefaultCode = %q, want BDT", code)
	}

	wantCode(t, service.Delete(ctx, 1, used.ID), apperr.Conflict) // the default (and used)
	if _, err := service.SetDefault(ctx, 1, def.ID); err != nil {
		t.Fatal(err)
	}
	wantCode(t, service.Delete(ctx, 1, used.ID), apperr.Conflict) // payments use it
	if err := service.Delete(ctx, 1, off.ID); err != nil {
		t.Fatalf("deleting an unused currency: %v", err)
	}
}

func TestEnsureSeeds(t *testing.T) {
	ctx := context.Background()

	repo := newFakeRepo()
	service := NewService(repo, nil)
	for range 2 { // idempotent
		if err := service.EnsureSeeds(ctx); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := repo.List(ctx)
	if len(rows) != len(Seeds) {
		t.Fatalf("seeded %d currencies, want %d", len(rows), len(Seeds))
	}
	if code, _ := service.DefaultCode(ctx); code != Seeds[0].Code {
		t.Fatalf("new install default = %q, want %s", code, Seeds[0].Code)
	}

	// An install that already has a default keeps it.
	repo = newFakeRepo()
	repo.add(Currency{Code: "BDT", IsEnabled: true, IsDefault: true})
	service = NewService(repo, nil)
	if err := service.EnsureSeeds(ctx); err != nil {
		t.Fatal(err)
	}
	if code, _ := service.DefaultCode(ctx); code != "BDT" {
		t.Fatalf("default after seeding = %q, want BDT kept", code)
	}
}

func TestFormatFallsBackForUnknownCodes(t *testing.T) {
	service := NewService(newFakeRepo(), nil)
	if got := service.Format(context.Background(), "gbp"); got.Code != "GBP" || got.Precision != 2 {
		t.Fatalf("Format(gbp) = %+v, want the fallback", got)
	}
}
