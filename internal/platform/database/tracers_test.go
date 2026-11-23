package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

type orderTracer struct {
	name string
	log  *[]string
}

type orderKey string

func (o orderTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	*o.log = append(*o.log, "start "+o.name)
	return context.WithValue(ctx, orderKey(o.name), true)
}

func (o orderTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(orderKey(o.name)) == nil {
		*o.log = append(*o.log, "lost context "+o.name)
	}
	*o.log = append(*o.log, "end "+o.name)
}

func TestTracersChainInOrder(t *testing.T) {
	var log []string
	tracer := Tracers(orderTracer{"a", &log}, orderTracer{"b", &log})
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	want := []string{"start a", "start b", "end b", "end a"}
	if len(log) != len(want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("log = %v, want %v", log, want)
		}
	}
}
