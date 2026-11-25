// Package maintenance is the application's maintenance mode: one
// switch that takes the API down and brings it back (`ims down` / `ims up`,
// or the developer dashboard). The state lives in the database so every
// process — API servers, the worker, the scheduler — sees it; each process
// reads it through a short-lived in-memory copy (Checker), so a request costs
// no query.
package maintenance

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

const (
	// DefaultMessage is shown to API clients when `down` gives none.
	DefaultMessage = "The service is down for maintenance. Please try again shortly."
	// DefaultRetryAfter is the Retry-After hint (seconds) when `down` gives none.
	DefaultRetryAfter = 60
	// MaxRetryAfter caps the Retry-After hint (one day).
	MaxRetryAfter = 86400
	// MaxMessageLength caps the message shown to clients.
	MaxMessageLength = 500
	// MinSecretLength is the shortest bypass secret `down --secret` accepts.
	MinSecretLength = 8
)

// State is the maintenance state at one moment.
type State struct {
	Down       bool       `json:"down"`
	Since      *time.Time `json:"since"`
	Message    string     `json:"message"`
	RetryAfter int        `json:"retry_after"`
	// ByUserID is who took the app down (nil: the CLI).
	ByUserID  *int64 `json:"by_user_id"`
	HasSecret bool   `json:"has_secret"`

	secretHash string
}

// Allows reports whether secret is the bypass secret of this down period.
// Comparison is in constant time over the hashes.
func (s State) Allows(secret string) bool {
	if !s.Down || s.secretHash == "" || secret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashSecret(secret)), []byte(s.secretHash)) == 1
}

// Store reads and writes the maintenance row. No HTTP concepts.
type Store interface {
	Get(ctx context.Context) (State, error)
	Down(ctx context.Context, message string, retryAfter int, secretHash string, byUserID int64) (State, error)
	// Up returns false when the app was not down.
	Up(ctx context.Context, byUserID int64) (bool, error)
}

// NewStore returns the sqlc-backed store.
func NewStore(queries *dbgen.Queries) Store { return &sqlStore{queries: queries} }

type sqlStore struct{ queries *dbgen.Queries }

func (s *sqlStore) Get(ctx context.Context) (State, error) {
	row, err := s.queries.GetMaintenance(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, nil
	}
	if err != nil {
		return State{}, database.MapError("maintenance.get", err)
	}
	return toState(row), nil
}

func (s *sqlStore) Down(ctx context.Context, message string, retryAfter int, secretHash string, byUserID int64) (State, error) {
	params := dbgen.SetMaintenanceDownParams{Message: message, RetryAfter: int32(retryAfter)}
	if secretHash != "" {
		params.SecretHash = &secretHash
	}
	if byUserID > 0 {
		params.ByUserID = &byUserID
	}
	row, err := s.queries.SetMaintenanceDown(ctx, params)
	if err != nil {
		return State{}, database.MapError("maintenance.down", err)
	}
	return toState(row), nil
}

func (s *sqlStore) Up(ctx context.Context, byUserID int64) (bool, error) {
	var by *int64
	if byUserID > 0 {
		by = &byUserID
	}
	rows, err := s.queries.SetMaintenanceUp(ctx, by)
	if err != nil {
		return false, database.MapError("maintenance.up", err)
	}
	return rows > 0, nil
}

func toState(row dbgen.Maintenance) State {
	if !row.DownSince.Valid {
		return State{}
	}
	since := row.DownSince.Time
	state := State{
		Down:       true,
		Since:      &since,
		Message:    row.Message,
		RetryAfter: int(row.RetryAfter),
		ByUserID:   row.ByUserID,
	}
	if row.SecretHash != nil && *row.SecretHash != "" {
		state.HasSecret, state.secretHash = true, *row.SecretHash
	}
	return state
}

// Event is one finished toggle, reported to Audit.
type Event struct {
	Down     bool  // true: taken down; false: brought back up
	ByUserID int64 // 0: the CLI (the system)
	// Down only: what clients see and whether a bypass secret was set.
	Message    string
	RetryAfter int
	WithBypass bool
	// Up only: when the down period started.
	DownSince *time.Time
}

// Audit records toggles in the system activity log. The activity module
// implements it; maintenance only decides when to call it.
type Audit interface {
	Maintenance(ctx context.Context, event Event)
}

// DownInput is what `down` accepts. Zero values take the defaults.
type DownInput struct {
	Message    string
	RetryAfter int
	// Secret is a bypass secret chosen by the caller; GenerateSecret makes
	// one instead. The secret is returned once and stored only as a hash.
	Secret         string
	GenerateSecret bool
	ByUserID       int64
}

// Service takes the app down and brings it back: the one entry point for the
// CLI, the dashboard endpoints and the dashboard commands.
type Service struct {
	store   Store
	checker *Checker
	audit   Audit
}

// NewService creates the service; the checker is updated at once on every
// toggle so this process never serves a stale state.
func NewService(store Store, checker *Checker) *Service {
	return &Service{store: store, checker: checker}
}

// WithAudit records every toggle (maintenance.down / maintenance.up).
func (s *Service) WithAudit(audit Audit) *Service {
	s.audit = audit
	return s
}

// Checker returns the cached reader the middleware and the scheduler use.
func (s *Service) Checker() *Checker { return s.checker }

// Status reads the state from the database (not the cached copy).
func (s *Service) Status(ctx context.Context) (State, error) {
	state, err := s.store.Get(ctx)
	if err == nil {
		s.checker.set(state)
	}
	return state, err
}

// Down takes the app down. It returns the new state and, when one was
// generated or given, the bypass secret (shown once).
func (s *Service) Down(ctx context.Context, in DownInput) (_ State, _ string, err error) {
	ctx, end := tracing.Begin(ctx, "maintenance.down", attribute.Int("maintenance.retry_after", in.RetryAfter))
	defer end(&err)
	message := strings.TrimSpace(in.Message)
	if message == "" {
		message = DefaultMessage
	}
	retry := in.RetryAfter
	if retry == 0 {
		retry = DefaultRetryAfter
	}
	fields := map[string][]string{}
	if utf8.RuneCountInString(message) > MaxMessageLength {
		fields["message"] = []string{"The message may not be longer than 500 characters."}
	}
	if retry < 1 || retry > MaxRetryAfter {
		fields["retry_after"] = []string{"The retry must be between 1 and 86400 seconds."}
	}
	secret := strings.TrimSpace(in.Secret)
	if secret != "" && in.GenerateSecret {
		fields["secret"] = []string{"Give a secret or generate one, not both."}
	} else if secret != "" && len(secret) < MinSecretLength {
		fields["secret"] = []string{"The secret must be at least 8 characters."}
	}
	if len(fields) > 0 {
		return State{}, "", apperr.Validation(fields)
	}
	if in.GenerateSecret {
		generated, err := GenerateSecret()
		if err != nil {
			return State{}, "", apperr.Wrap(apperr.InternalServerError, "The bypass secret could not be generated.", err)
		}
		secret = generated
	}
	hash := ""
	if secret != "" {
		hash = hashSecret(secret)
	}

	state, err := s.store.Down(ctx, message, retry, hash, in.ByUserID)
	if err != nil {
		return State{}, "", err
	}
	s.checker.set(state)
	if s.audit != nil {
		s.audit.Maintenance(ctx, Event{
			Down: true, ByUserID: in.ByUserID, Message: message, RetryAfter: retry, WithBypass: secret != "",
		})
	}
	return state, secret, nil
}

// Up brings the app back. It returns false when the app was not down
// (nothing is recorded then).
func (s *Service) Up(ctx context.Context, byUserID int64) (_ bool, err error) {
	ctx, end := tracing.Begin(ctx, "maintenance.up")
	defer end(&err)
	before, _ := s.store.Get(ctx)
	changed, err := s.store.Up(ctx, byUserID)
	if err != nil {
		return false, err
	}
	s.checker.set(State{})
	if changed && s.audit != nil {
		s.audit.Maintenance(ctx, Event{ByUserID: byUserID, DownSince: before.Since})
	}
	return changed, nil
}

// GenerateSecret returns a random bypass secret (32 hex characters).
func GenerateSecret() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
