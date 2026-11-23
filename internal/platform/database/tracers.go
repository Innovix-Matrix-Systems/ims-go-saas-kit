package database

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Tracers chains pgx query tracers (the developer dashboard's capture and
// OpenTelemetry): each sees the query, in order, on the context the previous
// one returned.
func Tracers(tracers ...pgx.QueryTracer) pgx.QueryTracer { return chain(tracers) }

type chain []pgx.QueryTracer

func (c chain) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	for _, tracer := range c {
		ctx = tracer.TraceQueryStart(ctx, conn, data)
	}
	return ctx
}

func (c chain) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	for i := len(c) - 1; i >= 0; i-- {
		c[i].TraceQueryEnd(ctx, conn, data)
	}
}
