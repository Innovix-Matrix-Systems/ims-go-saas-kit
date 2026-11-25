package dev

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database/dbtest"
)

func TestDeveloperRepository(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	ctx := context.Background()
	dbtest.Clean(t, pool, "personal_access_tokens", "model_has_roles", "roles", "users")

	var roleID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO roles (name, guard_name, is_system, hidden)
		 VALUES ('Developer', 'sanctum', true, true) RETURNING id`,
	).Scan(&roleID); err != nil {
		t.Fatalf("insert developer role: %v", err)
	}

	insertUser := func(name, email string, active bool) int64 {
		t.Helper()
		activeValue := 0
		if active {
			activeValue = 1
		}
		var id int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (name, email, password, is_active)
			 VALUES ($1, $2, 'hash', $3) RETURNING id`,
			name, email, activeValue,
		).Scan(&id); err != nil {
			t.Fatalf("insert user %s: %v", email, err)
		}
		return id
	}
	assignDeveloper := func(userID int64) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO model_has_roles (role_id, model_type, model_id)
			 VALUES ($1, 'user', $2)`, roleID, userID,
		); err != nil {
			t.Fatalf("assign developer role to %d: %v", userID, err)
		}
	}
	insertToken := func(userID int64, seed int, expires time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO personal_access_tokens
			 (tokenable_type, tokenable_id, name, device, token, expires_at)
			 VALUES ('user', $1, $2, $2, $3, $4)`,
			userID, fmt.Sprintf("token-%d", seed), fmt.Sprintf("%064x", seed), expires,
		); err != nil {
			t.Fatalf("insert token %d: %v", seed, err)
		}
	}

	alice := insertUser("Alice Developer", "alice-dev@example.test", true)
	bob := insertUser("Bob Internal", "bob-internal@example.test", true)
	inactive := insertUser("Inactive Developer", "inactive-dev@example.test", false)
	assignDeveloper(alice)
	assignDeveloper(inactive)
	insertToken(alice, 1, time.Now().Add(time.Hour))
	insertToken(alice, 2, time.Now().Add(-time.Hour))

	repo := NewDeveloperRepository(queries)

	developers, total, err := repo.List(ctx, DeveloperFilter{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 2 || len(developers) != 2 {
		t.Fatalf("List() = %+v total=%d, want 2 developers", developers, total)
	}
	byID := map[int64]Developer{}
	for _, developer := range developers {
		byID[developer.UserID] = developer
	}
	if byID[alice].ActiveTokens != 1 {
		t.Fatalf("Alice active tokens = %d, want 1", byID[alice].ActiveTokens)
	}
	if !byID[alice].IsActive || byID[inactive].IsActive {
		t.Fatalf("active flags = alice:%v inactive:%v", byID[alice].IsActive, byID[inactive].IsActive)
	}
	if _, ok := byID[bob]; ok {
		t.Fatalf("Bob does not hold the Developer role but was listed")
	}

	searched, total, err := repo.List(ctx, DeveloperFilter{Search: "alice", Limit: 10})
	if err != nil {
		t.Fatalf("List(search) error = %v", err)
	}
	if total != 1 || len(searched) != 1 || searched[0].UserID != alice {
		t.Fatalf("List(search) = %+v total=%d, want Alice only", searched, total)
	}

	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("Count() = %d, want 2", count)
	}

	page, total, err := repo.List(ctx, DeveloperFilter{Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("List(page 1) error = %v", err)
	}
	if total != 2 || len(page) != 1 {
		t.Fatalf("List(page 1) = %+v total=%d, want 1 of 2", page, total)
	}
}
