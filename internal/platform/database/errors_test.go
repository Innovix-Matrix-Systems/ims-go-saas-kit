package database

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
)

func TestMapErrorTellsTheForeignKeySidesApart(t *testing.T) {
	cases := []struct {
		name, message, want string
	}{
		{"delete of a row still in use",
			`update or delete on table "packages" violates foreign key constraint "subscriptions_package_id_fkey" on table "subscriptions"`,
			"The record is still in use by other records."},
		{"write pointing at a missing row",
			`insert or update on table "subscriptions" violates foreign key constraint "subscriptions_package_id_fkey"`,
			"The operation references a record that does not exist."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mapped := MapError("test.op", &pgconn.PgError{Code: "23503", Message: tc.message})
			if mapped.Code != apperr.Conflict || mapped.Message != tc.want {
				t.Fatalf("MapError() = %s %q, want %s %q", mapped.Code, mapped.Message, apperr.Conflict, tc.want)
			}
		})
	}
}
