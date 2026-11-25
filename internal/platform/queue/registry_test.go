package queue

import (
	"context"
	"errors"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

func TestRegistryValidation(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(Definition{Kind: "", Handle: noopHandle}); err == nil {
		t.Fatal("empty kind accepted")
	}
	if err := registry.Register(Definition{Kind: "a", Handle: nil}); err == nil {
		t.Fatal("missing handler accepted")
	}
	if err := registry.Register(Definition{Kind: "a", Handle: noopHandle}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(Definition{Kind: "a", Handle: noopHandle}); err == nil {
		t.Fatal("duplicate kind accepted")
	}

	def, ok := registry.Definition("a")
	if !ok || def.Queue != QueueDefault {
		t.Fatalf("definition = %+v, ok = %v", def, ok)
	}
}

func TestSyncEngineDispatch(t *testing.T) {
	registry := NewRegistry()
	var handled []byte
	var validated bool
	if err := registry.Register(Definition{
		Kind: "test.echo",
		Validate: func(payload []byte) error {
			validated = true
			return nil
		},
		Handle: func(ctx context.Context, payload []byte) error {
			handled = payload
			return nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	engine := NewSyncEngine(registry)
	ctx := tenancy.WithScope(context.Background(), tenancy.Scope{TenantID: 1, BranchID: 2})
	if err := engine.Enqueue(ctx, "test.echo", map[string]string{"hello": "world"}, EnqueueOptions{}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if !validated || string(handled) != `{"hello":"world"}` {
		t.Fatalf("validated=%v handled=%s", validated, handled)
	}

	err := engine.Enqueue(context.Background(), "missing.kind", nil, EnqueueOptions{})
	if !apperr.Is(err, apperr.InvalidInput) {
		t.Fatalf("unknown kind error = %v, want INVALID_INPUT", err)
	}
}

// TestEnqueueRefusesUnscopedScope proves the M48 fix: a tenant-less envelope
// is refused unless the definition is explicitly a system job — a job would
// otherwise execute with "no filter" across all tenants.
func TestEnqueueRefusesUnscopedScope(t *testing.T) {
	registry := NewRegistry()
	ran := false
	mustRegister := func(def Definition) {
		t.Helper()
		if err := registry.Register(def); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}
	mustRegister(Definition{
		Kind:   "test.scoped",
		Handle: func(context.Context, []byte) error { ran = true; return nil },
	})
	mustRegister(Definition{
		Kind:      "test.system",
		SystemJob: true,
		Handle:    func(context.Context, []byte) error { ran = true; return nil },
	})

	engine := NewSyncEngine(registry)
	unscoped := context.Background()

	if err := engine.Enqueue(unscoped, "test.scoped", nil, EnqueueOptions{}); !apperr.Is(err, apperr.InvalidInput) {
		t.Fatalf("unscoped enqueue error = %v, want INVALID_INPUT", err)
	}
	if ran {
		t.Fatal("handler ran for an unscoped enqueue")
	}
	if err := engine.Enqueue(unscoped, "test.system", nil, EnqueueOptions{}); err != nil {
		t.Fatalf("system job enqueue error = %v", err)
	}
	if !ran {
		t.Fatal("system job did not run")
	}
}

func TestSyncEngineValidateRejects(t *testing.T) {
	registry := NewRegistry()
	ran := false
	if err := registry.Register(Definition{
		Kind:     "test.strict",
		Validate: func(payload []byte) error { return errors.New("bad payload") },
		Handle: func(ctx context.Context, payload []byte) error {
			ran = true
			return nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	engine := NewSyncEngine(registry)
	if err := engine.Enqueue(context.Background(), "test.strict", nil, EnqueueOptions{}); err == nil {
		t.Fatal("Enqueue() error = nil, want validation error")
	}
	if ran {
		t.Fatal("handler ran despite invalid payload")
	}
}

func noopHandle(ctx context.Context, payload []byte) error { return nil }
