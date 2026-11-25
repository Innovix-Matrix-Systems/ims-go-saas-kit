package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// TestDeveloperDatabasePools proves GET /dev/api/inspector/database lists
// every pool with its counts, and that the public health report never
// carries those counts.
func TestDeveloperDatabasePools(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")

	kit := testkit.New(t)
	deps := kit.Deps
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := seeder.Run(context.Background(), kit.Config, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	router := deps.Router()

	rec := apiRequest(t, router, http.MethodGet, "/dev/api/inspector/database", "", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", rec.Code)
	}

	rec = apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"developer@ims.com","password":"secret123","device":"pools-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("developer login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &login)

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/inspector/database", "", login.Data.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("pools status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			BusyShare float64 `json:"busy_share"`
			Pools     []struct {
				Name     string `json:"name"`
				Active   int32  `json:"active"`
				Total    int32  `json:"total"`
				Max      int32  `json:"max"`
				Acquires int64  `json:"acquires"`
			} `json:"pools"`
		} `json:"data"`
	}
	decodeBody(t, rec, &body)
	if body.Data.BusyShare != 0.8 {
		t.Errorf("busy_share = %v, want 0.8", body.Data.BusyShare)
	}
	if len(body.Data.Pools) == 0 || body.Data.Pools[0].Name != "primary" {
		t.Fatalf("pools = %+v, want primary first", body.Data.Pools)
	}
	primary := body.Data.Pools[0]
	if primary.Max < 1 || primary.Total > primary.Max || primary.Active > primary.Total {
		t.Errorf("primary = %+v, want 0 <= active <= total <= max, max >= 1", primary)
	}
	if primary.Acquires < 1 {
		t.Errorf("primary acquires = %d, want the login's queries counted", primary.Acquires)
	}

	rec = apiRequest(t, router, http.MethodGet, "/api/health", "", "", nil)
	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status = %d", rec.Code)
	}
	var report struct {
		CheckResults []struct {
			Name string          `json:"name"`
			Meta json.RawMessage `json:"meta"`
		} `json:"checkResults"`
	}
	decodeBody(t, rec, &report)
	found := false
	for _, check := range report.CheckResults {
		if check.Name != "Database" {
			continue
		}
		found = true
		var pools map[string]map[string]any
		if err := json.Unmarshal(check.Meta, &pools); err != nil {
			t.Fatalf("Database meta: %v", err)
		}
		for pool, meta := range pools {
			for _, key := range []string{"active", "idle", "total", "max", "acquires"} {
				if _, ok := meta[key]; ok {
					t.Errorf("public Database check carries %q for %s: %v", key, pool, meta)
				}
			}
		}
	}
	if !found {
		t.Fatal("public health report has no Database check")
	}
}
