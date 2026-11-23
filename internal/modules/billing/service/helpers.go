package service

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func strPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
