-- name: GetTwoFactor :one
SELECT * FROM user_two_factor WHERE user_id = @user_id;

-- name: StartTwoFactor :one
-- A new enrolment replaces an unconfirmed one; a confirmed one is never
-- overwritten (the service refuses first; the WHERE makes it race-safe).
INSERT INTO user_two_factor (user_id, method, secret)
VALUES (@user_id, @method, sqlc.narg('secret'))
ON CONFLICT (user_id) DO UPDATE
SET method         = EXCLUDED.method,
    secret         = EXCLUDED.secret,
    confirmed_at   = NULL,
    recovery_codes = '[]'::jsonb,
    last_step      = 0,
    updated_at     = now()
WHERE user_two_factor.confirmed_at IS NULL
RETURNING *;

-- name: ConfirmTwoFactor :execrows
UPDATE user_two_factor
SET confirmed_at = now(), recovery_codes = @recovery_codes, updated_at = now()
WHERE user_id = @user_id AND confirmed_at IS NULL;

-- name: SetTwoFactorRecoveryCodes :execrows
UPDATE user_two_factor
SET recovery_codes = @recovery_codes, updated_at = now()
WHERE user_id = @user_id;

-- name: UseTwoFactorRecoveryCode :execrows
-- Writes the codes with one more marked used, only if they are still the ones
-- read: 0 rows means a concurrent sign-in changed them first (the same code
-- used twice, or another code's mark that must not be lost).
UPDATE user_two_factor
SET recovery_codes = @recovery_codes, updated_at = now()
WHERE user_id = @user_id AND recovery_codes = @previous::jsonb;

-- name: SetTwoFactorSecret :exec
UPDATE user_two_factor SET secret = @secret, updated_at = now() WHERE user_id = @user_id;

-- name: AcceptTwoFactorStep :execrows
-- Records a used TOTP time step; 0 rows means the step (or a later one) was
-- already used: a replayed code.
UPDATE user_two_factor
SET last_step = @step, updated_at = now()
WHERE user_id = @user_id AND last_step < @step;

-- name: DeleteTwoFactor :execrows
DELETE FROM user_two_factor WHERE user_id = @user_id;
