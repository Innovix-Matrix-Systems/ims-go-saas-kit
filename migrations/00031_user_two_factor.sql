-- +goose Up
-- Two-factor authentication: one row per user who started or
-- finished enrolment. method is totp (an authenticator app; the secret is
-- encrypted with APP_KEY) or email (a code mailed at sign-in; no secret).
-- recovery_codes holds SHA-256 hashes, each with the time it was used.
-- last_step is the last accepted TOTP time step, so a code works once.
CREATE TABLE user_two_factor (
    user_id        bigint PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    method         text NOT NULL CHECK (method IN ('totp', 'email')),
    secret         text,
    confirmed_at   timestamptz,
    recovery_codes jsonb NOT NULL DEFAULT '[]'::jsonb,
    last_step      bigint NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS user_two_factor;
