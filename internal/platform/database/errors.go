package database

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
)

// MapError converts driver errors into application errors. Constraint
// violations become client errors; everything else becomes a generic database
// error. The original error is always wrapped for logs.
func MapError(op string, err error) *apperr.Error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return apperr.Wrap(apperr.ResourceAlreadyExists, "A record with the same unique value already exists.", err).WithOp(op)
		case "23503": // foreign_key_violation
			return apperr.Wrap(apperr.Conflict, "The operation references a record that does not exist.", err).WithOp(op)
		case "23502": // not_null_violation
			return apperr.Wrap(apperr.ValidationFailed, "A required value is missing.", err).WithOp(op)
		}
	}
	return apperr.Wrap(apperr.DatabaseError, "database operation failed", err).WithOp(op)
}
