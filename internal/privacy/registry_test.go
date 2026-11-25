package privacy

import (
	"context"
	"testing"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
)

type fakeContributor struct {
	name    string
	columns []string
}

func (f fakeContributor) Name() string      { return f.name }
func (f fakeContributor) Columns() []string { return f.columns }
func (fakeContributor) Export(context.Context, *dbgen.Queries, Subject, Writer) error {
	return nil
}
func (fakeContributor) Erase(context.Context, *dbgen.Queries, Subject) (Report, error) {
	return Report{}, nil
}

type fakeGuard struct{ fakeContributor }

func (fakeGuard) CheckErasure(context.Context, *dbgen.Queries, Subject) ([]string, error) {
	return nil, nil
}

func TestRegistryCover(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fakeContributor{name: "auth", columns: []string{"tokens.user_id"}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(fakeGuard{fakeContributor{name: "identity", columns: []string{"*.created_by"}}}); err != nil {
		t.Fatal(err)
	}
	registry.Exempt("ledger.user_id", "proof of erasure")

	got := registry.Cover([]string{"tokens.user_id", "branches.created_by", "ledger.user_id", "orders.customer_id"})
	want := map[string]Coverage{
		"branches.created_by": {Column: "branches.created_by", Contributor: "identity"},
		"ledger.user_id":      {Column: "ledger.user_id", Exemption: "proof of erasure"},
		"orders.customer_id":  {Column: "orders.customer_id"},
		"tokens.user_id":      {Column: "tokens.user_id", Contributor: "auth"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i, entry := range got {
		if i > 0 && got[i-1].Column > entry.Column {
			t.Errorf("not sorted: %q before %q", got[i-1].Column, entry.Column)
		}
		if entry != want[entry.Column] {
			t.Errorf("%s = %+v, want %+v", entry.Column, entry, want[entry.Column])
		}
	}
	if got[2].Covered() {
		t.Errorf("%s should not be covered", got[2].Column)
	}
	if len(registry.Guards()) != 1 {
		t.Errorf("guards = %d, want the contributor that is also a guard", len(registry.Guards()))
	}
}

func TestRegistryRejectsDuplicates(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fakeContributor{name: "auth"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(fakeContributor{name: "auth"}); err == nil {
		t.Error("registering a name twice should fail")
	}
	if err := registry.Register(fakeContributor{}); err == nil {
		t.Error("a contributor without a name should fail")
	}
}

func TestReportAdd(t *testing.T) {
	report := Report{Deleted: 1}
	report.Add(Report{Deleted: 2, Anonymized: 3, Objects: []Object{{Disk: "local", Path: "a"}}})
	if report.Deleted != 3 || report.Anonymized != 3 || len(report.Objects) != 1 {
		t.Errorf("report = %+v", report)
	}
}
