-- Currencies: how each one is written. One row is the default (the currency
-- of new payments and plan prices); amounts are never converted.

-- name: ListCurrencies :many
-- The default first, then by code; in_use says whether payments use it.
SELECT sqlc.embed(c),
       EXISTS (SELECT 1 FROM payments p WHERE p.currency = c.code) AS in_use
FROM currencies c
ORDER BY c.is_default DESC, c.code;

-- name: GetCurrency :one
SELECT sqlc.embed(c),
       EXISTS (SELECT 1 FROM payments p WHERE p.currency = c.code) AS in_use
FROM currencies c
WHERE c.id = @id;

-- name: CreateCurrency :one
INSERT INTO currencies (
    code, name, symbol, symbol_first, precision, decimal_mark, thousands_separator, is_enabled,
    created_by, updated_by
)
VALUES (
    @code, @name, @symbol, @symbol_first, @precision, @decimal_mark, @thousands_separator, @is_enabled,
    sqlc.narg('actor_id'), sqlc.narg('actor_id')
)
RETURNING *;

-- name: EnsureCurrency :exec
-- Seeds a currency if its code is missing (the seeder; idempotent). The
-- default is chosen separately, only when there is none.
INSERT INTO currencies (code, name, symbol, symbol_first, precision, decimal_mark, thousands_separator)
VALUES (@code, @name, @symbol, @symbol_first, @precision, @decimal_mark, @thousands_separator)
ON CONFLICT (code) DO NOTHING;

-- name: UpdateCurrency :one
UPDATE currencies
SET name = @name,
    symbol = @symbol,
    symbol_first = @symbol_first,
    precision = @precision,
    decimal_mark = @decimal_mark,
    thousands_separator = @thousands_separator,
    is_enabled = @is_enabled,
    updated_by = sqlc.narg('actor_id'),
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ClearDefaultCurrency :exec
UPDATE currencies SET is_default = false, updated_at = now() WHERE is_default;

-- name: SetDefaultCurrency :execrows
UPDATE currencies
SET is_default = true, updated_by = sqlc.narg('actor_id'), updated_at = now()
WHERE id = @id AND is_enabled;

-- name: DeleteCurrency :execrows
DELETE FROM currencies WHERE id = @id;
