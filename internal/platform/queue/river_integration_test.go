package queue

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

func TestRiverEngineEnqueueAndWork(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Clean(t, pool, "river_job")

	registry := NewRegistry()
	handled := make(chan []byte, 1)
	scopeSeen := make(chan tenancy.Scope, 1)
	if err := registry.Register(Definition{
		Kind:  "test.river.echo",
		Queue: QueueDefault,
		Handle: func(ctx context.Context, payload []byte) error {
			scope, _ := tenancy.FromContext(ctx)
			scopeSeen <- scope
			handled <- payload
			return nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	cfg := testQueueConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine, err := NewRiverEngine(pool, registry, cfg, logger, nil)
	if err != nil {
		t.Fatalf("NewRiverEngine() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scoped := tenancy.WithScope(ctx, tenancy.Scope{TenantID: 1, BranchID: 2})
	if err := engine.Enqueue(scoped, "test.river.echo", map[string]string{"msg": "hi"}, EnqueueOptions{}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	if err := engine.Once(ctx, 20*time.Second); err != nil {
		t.Fatalf("Once() error = %v", err)
	}

	select {
	case payload := <-handled:
		var decoded map[string]string
		if err := json.Unmarshal(payload, &decoded); err != nil || decoded["msg"] != "hi" {
			t.Fatalf("payload = %s, err = %v", payload, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not run")
	}

	select {
	case scope := <-scopeSeen:
		if scope.TenantID != 1 || scope.BranchID != 2 {
			t.Fatalf("restored scope = %+v", scope)
		}
	case <-time.After(time.Second):
		t.Fatal("scope not captured")
	}
}

func TestRiverEngineTransactionalEnqueue(t *testing.T) {
	pool := dbtest.Pool(t)

	registry := NewRegistry()
	if err := registry.Register(Definition{
		Kind:   "test.river.tx",
		Handle: func(ctx context.Context, payload []byte) error { return nil },
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine, err := NewRiverEngine(pool, registry, testQueueConfig(), logger, nil)
	if err != nil {
		t.Fatalf("NewRiverEngine() error = %v", err)
	}

	ctx := tenancy.WithScope(context.Background(), tenancy.Scope{TenantID: 1, BranchID: 2})
	var count int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind = 'ims.job'").Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := engine.EnqueueTx(ctx, tx, "test.river.tx", nil, EnqueueOptions{}); err != nil {
		t.Fatalf("EnqueueTx() error = %v", err)
	}
	// Not visible until the transaction commits.
	_ = tx.Rollback(ctx)

	var after int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind = 'ims.job'").Scan(&after); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if after != count {
		t.Fatalf("rolled-back job became visible: before=%d after=%d", count, after)
	}

	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := engine.EnqueueTx(ctx, tx2, "test.river.tx", nil, EnqueueOptions{}); err != nil {
		t.Fatalf("EnqueueTx() error = %v", err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var committed int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind = 'ims.job'").Scan(&committed); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if committed != count+1 {
		t.Fatalf("committed job count = %d, want %d", committed, count+1)
	}
}

func testQueueConfig() config.Queue {
	return config.Queue{
		Driver:            "river",
		WorkerConcurrency: 2,
		Critical:          config.Channel{Tries: 3, Timeout: 30 * time.Second},
		Default:           config.Channel{Tries: 3, Timeout: 30 * time.Second},
		Heavy:             config.Channel{Tries: 1, Timeout: time.Minute},
	}
}
