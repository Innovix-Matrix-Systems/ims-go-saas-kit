package dev

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
)

func TestSessionRepository(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	ctx := context.Background()
	dbtest.Clean(t, pool, "personal_access_tokens", "users")

	now := time.Now()
	insertUser := func(name, email string, deleted bool) int64 {
		t.Helper()
		var id int64
		var deletedAt *time.Time
		if deleted {
			deletedAt = &now
		}
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (name, email, password, is_active, deleted_at)
			 VALUES ($1, $2, 'hash', 1, $3) RETURNING id`,
			name, email, deletedAt,
		).Scan(&id); err != nil {
			t.Fatalf("insert user %s: %v", email, err)
		}
		return id
	}
	insertToken := func(userID int64, token string, lastUsed, expires *time.Time, createdAt time.Time) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO personal_access_tokens
			 (tokenable_type, tokenable_id, name, device, token, last_used_at, expires_at, created_at)
			 VALUES ('user', $1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			userID, "user_"+token, "device-"+token, token, lastUsed, expires, createdAt,
		).Scan(&id); err != nil {
			t.Fatalf("insert token %s: %v", token, err)
		}
		return id
	}
	hash := func(seed int) string { return fmt.Sprintf("%064x", seed) }
	ago := func(d time.Duration) *time.Time { value := now.Add(-d); return &value }

	userA := insertUser("Alice", "alice@example.test", false)
	userB := insertUser("Bob", "bob@example.test", false)
	deleted := insertUser("Deleted", "deleted@example.test", true)

	tokenA1 := insertToken(userA, hash(1), ago(time.Hour), nil, now.Add(-time.Hour))
	insertToken(userA, hash(2), ago(10*24*time.Hour), nil, now.Add(-20*24*time.Hour))
	tokenA3 := insertToken(userA, hash(3), nil, ago(24*time.Hour), now.Add(-48*time.Hour))
	insertToken(userB, hash(4), ago(40*24*time.Hour), nil, now.Add(-60*24*time.Hour))
	insertToken(deleted, hash(5), ago(time.Hour), nil, now.Add(-time.Hour))

	repo := NewSessionRepository(queries)

	stats, err := repo.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	want := SessionStats{Users: 2, ActiveTokens: 3, Used24h: 1, IdleTokens: 2, Inactive30d: 1}
	if stats != want {
		t.Fatalf("Stats() = %+v, want %+v", stats, want)
	}

	users, err := repo.ListUsers(ctx, SessionUserFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 2 || users[0].UserID != userB || users[1].UserID != userA {
		t.Fatalf("ListUsers() = %+v, want [B, A]", users)
	}
	if users[1].ActiveTokens != 2 || users[1].TotalTokens != 3 {
		t.Fatalf("user A counts = active:%d total:%d, want 2/3", users[1].ActiveTokens, users[1].TotalTokens)
	}

	searched, err := repo.ListUsers(ctx, SessionUserFilter{Search: "alice", Limit: 10})
	if err != nil {
		t.Fatalf("ListUsers(search) error = %v", err)
	}
	if len(searched) != 1 || searched[0].UserID != userA {
		t.Fatalf("ListUsers(search) = %+v, want Alice only", searched)
	}

	page, err := repo.ListUsers(ctx, SessionUserFilter{BeforeID: userB, Limit: 10})
	if err != nil {
		t.Fatalf("ListUsers(before) error = %v", err)
	}
	if len(page) != 1 || page[0].UserID != userA {
		t.Fatalf("ListUsers(before) = %+v, want Alice only", page)
	}

	exists, err := repo.UserExists(ctx, userA)
	if err != nil || !exists {
		t.Fatalf("UserExists(A) = %v, %v", exists, err)
	}
	exists, err = repo.UserExists(ctx, deleted)
	if err != nil || exists {
		t.Fatalf("UserExists(deleted) = %v, %v, want false", exists, err)
	}

	tokens, err := repo.ListTokens(ctx, userA)
	if err != nil {
		t.Fatalf("ListTokens() error = %v", err)
	}
	if len(tokens) != 3 {
		t.Fatalf("ListTokens() returned %d tokens, want 3", len(tokens))
	}
	for _, token := range tokens {
		if token.ID == tokenA3 && !token.Expired {
			t.Fatalf("token %d should be expired", tokenA3)
		}
		if token.ID != tokenA3 && token.Expired {
			t.Fatalf("token %d should not be expired", token.ID)
		}
	}

	deletedToken, err := repo.DeleteToken(ctx, tokenA3)
	if err != nil || !deletedToken {
		t.Fatalf("DeleteToken(expired) = %v, %v", deletedToken, err)
	}
	deletedToken, err = repo.DeleteToken(ctx, tokenA3)
	if err != nil || deletedToken {
		t.Fatalf("DeleteToken(again) = %v, %v", deletedToken, err)
	}

	pruned, err := repo.Prune(ctx, 5, true)
	if err != nil {
		t.Fatalf("Prune(dry run) error = %v", err)
	}
	if pruned != 2 {
		t.Fatalf("Prune(dry run) = %d, want 2 (A2, B1)", pruned)
	}
	pruned, err = repo.Prune(ctx, 5, false)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if pruned != 2 {
		t.Fatalf("Prune() = %d, want 2", pruned)
	}
	stats, err = repo.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() after prune error = %v", err)
	}
	if stats.ActiveTokens != 1 || stats.Users != 1 {
		t.Fatalf("Stats() after prune = %+v, want 1 active for 1 user", stats)
	}

	insertToken(userA, hash(6), ago(time.Minute), nil, now)

	kept, err := repo.DeleteAllExcept(ctx, tokenA1)
	if err != nil {
		t.Fatalf("DeleteAllExcept() error = %v", err)
	}
	if kept != 2 {
		t.Fatalf("DeleteAllExcept() = %d, want 2 (extra token plus the deleted user's token)", kept)
	}
	tokens, err = repo.ListTokens(ctx, userA)
	if err != nil {
		t.Fatalf("ListTokens() after revoke-all error = %v", err)
	}
	if len(tokens) != 1 || tokens[0].ID != tokenA1 {
		t.Fatalf("kept tokens = %+v, want only A1", tokens)
	}

	revoked, err := repo.DeleteUserTokens(ctx, userA)
	if err != nil {
		t.Fatalf("DeleteUserTokens() error = %v", err)
	}
	if revoked != 1 {
		t.Fatalf("DeleteUserTokens() = %d, want 1", revoked)
	}
}
