-- +goose Up
-- Phase 14 (H27/M17): the public payment callback resolves payments by
-- gateway_ref, so it must be indexed and unique. Older manual references
-- (CASH-<unix seconds>) could collide; de-duplicate them first by suffixing
-- the later rows with their id.
UPDATE payments p
SET gateway_ref = p.gateway_ref || '-' || p.id
WHERE p.gateway_ref IS NOT NULL
  AND EXISTS (
      SELECT 1 FROM payments q
      WHERE q.gateway_ref = p.gateway_ref AND q.id < p.id
  );

CREATE UNIQUE INDEX payments_gateway_ref_uidx ON payments (gateway_ref) WHERE gateway_ref IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS payments_gateway_ref_uidx;
