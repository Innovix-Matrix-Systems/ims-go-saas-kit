-- +goose Up
-- Invite a user: an admin creates the user inactive and
-- invites them; the invitee sets a password, which activates the account.
-- invited_at / invited_by: the latest invitation; invitation_accepted_at:
-- when the invitee set their password (an old link cannot reactivate a user
-- an admin deactivated later).
ALTER TABLE users
    ADD COLUMN invited_at             timestamptz,
    ADD COLUMN invited_by             bigint REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN invitation_accepted_at timestamptz;

-- +goose Down
ALTER TABLE users
    DROP COLUMN IF EXISTS invitation_accepted_at,
    DROP COLUMN IF EXISTS invited_by,
    DROP COLUMN IF EXISTS invited_at;
