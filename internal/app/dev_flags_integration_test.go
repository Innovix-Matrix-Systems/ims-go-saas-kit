package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// TestDeveloperFlagsEndToEnd proves the flags panel: target search, global and
// tenant/user overrides, reset-to-inherit, sync and audit coverage.
func TestDeveloperFlagsEndToEnd(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")

	kit := testkit.New(t)
	cfg := kit.Config

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := kit.Deps
	if err := seeder.Run(context.Background(), cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	router := deps.Router()

	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"developer@ims.com","password":"secret123","device":"flags-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("developer login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Data struct {
			Token string `json:"token"`
			User  struct {
				ID int64 `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	decodeBody(t, rec, &login)
	token := login.Data.Token
	if token == "" {
		t.Fatal("developer token missing")
	}

	type flagItem struct {
		Key         string `json:"key"`
		Value       bool   `json:"value"`
		Default     bool   `json:"default"`
		Source      string `json:"source"`
		Overridden  bool   `json:"overridden"`
		Description string `json:"description"`
		Group       string `json:"group"`
	}
	type flagList struct {
		Data []flagItem `json:"data"`
	}
	getFlags := func(path string) flagList {
		t.Helper()
		rec := apiRequest(t, router, http.MethodGet, path, "", token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d body = %s", path, rec.Code, rec.Body.String())
		}
		var list flagList
		decodeBody(t, rec, &list)
		return list
	}
	item := func(list flagList, key string) flagItem {
		t.Helper()
		for _, flag := range list.Data {
			if flag.Key == key {
				return flag
			}
		}
		t.Fatalf("flag %q missing from %+v", key, list.Data)
		return flagItem{}
	}

	tenantID := strconv.FormatInt(cfg.Tenancy.DefaultTenantID, 10)

	// ── initial state: catalog flags resolve to their code defaults ──────
	initial := getFlags("/dev/api/flags")
	if len(initial.Data) != len(featureflags.Catalog) {
		t.Fatalf("flags = %d, want %d", len(initial.Data), len(featureflags.Catalog))
	}
	audit := item(initial, "feature.audit")
	if audit.Value || audit.Default || audit.Source != "default" || audit.Overridden {
		t.Fatalf("initial feature.audit = %+v", audit)
	}
	if audit.Description == "" || audit.Group == "" {
		t.Fatalf("flag metadata missing: %+v", audit)
	}

	// ── global override and tenant inheritance ───────────────────────────
	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/flags",
		`{"target_type":"global","flags":{"feature.audit":true}}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("global flag set status = %d body = %s", rec.Code, rec.Body.String())
	}
	var updated flagList
	decodeBody(t, rec, &updated)
	if got := item(updated, "feature.audit"); !got.Value || got.Source != "global" || !got.Overridden {
		t.Fatalf("global feature.audit = %+v", got)
	}

	tenantFlags := getFlags("/dev/api/flags?target_type=tenant&target_id=" + tenantID)
	if got := item(tenantFlags, "feature.audit"); !got.Value || got.Source != "global" || got.Overridden {
		t.Fatalf("tenant inherited feature.audit = %+v", got)
	}

	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/flags",
		`{"target_type":"tenant","target_id":`+tenantID+`,"flags":{"feature.audit":false}}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant flag set status = %d body = %s", rec.Code, rec.Body.String())
	}
	tenantFlags = getFlags("/dev/api/flags?target_type=tenant&target_id=" + tenantID)
	if got := item(tenantFlags, "feature.audit"); got.Value || got.Source != "tenant" || !got.Overridden {
		t.Fatalf("tenant feature.audit = %+v", got)
	}
	if got := item(getFlags("/dev/api/flags"), "feature.audit"); !got.Value || got.Source != "global" {
		t.Fatalf("global changed by tenant override: %+v", got)
	}

	// ── reset asks the next level again; a second reset 404s ────────────
	rec = apiRequest(t, router, http.MethodDelete,
		"/dev/api/flags/feature.audit?target_type=tenant&target_id="+tenantID, "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant flag reset status = %d body = %s", rec.Code, rec.Body.String())
	}
	tenantFlags = getFlags("/dev/api/flags?target_type=tenant&target_id=" + tenantID)
	if got := item(tenantFlags, "feature.audit"); !got.Value || got.Source != "global" {
		t.Fatalf("tenant feature.audit after reset = %+v", got)
	}
	rec = apiRequest(t, router, http.MethodDelete,
		"/dev/api/flags/feature.audit?target_type=tenant&target_id="+tenantID, "", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second tenant reset status = %d, want 404", rec.Code)
	}

	// ── user override, then a null value clears it in the batch path ────
	userID := strconv.FormatInt(login.Data.User.ID, 10)
	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/flags",
		`{"target_type":"user","target_id":`+userID+`,"flags":{"feature.audit":true}}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user flag set status = %d body = %s", rec.Code, rec.Body.String())
	}
	userFlags := getFlags("/dev/api/flags?target_type=user&target_id=" + userID)
	if got := item(userFlags, "feature.audit"); !got.Value || got.Source != "user" || !got.Overridden {
		t.Fatalf("user feature.audit = %+v", got)
	}
	type targetStats struct {
		Data struct {
			Total         int64 `json:"total"`
			WithOverrides int64 `json:"with_overrides"`
		} `json:"data"`
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/targets/stats?type=user", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user target stats status = %d body = %s", rec.Code, rec.Body.String())
	}
	var stats targetStats
	decodeBody(t, rec, &stats)
	if stats.Data.Total < 1 || stats.Data.WithOverrides != 1 {
		t.Fatalf("user target stats = %+v, want at least one user with an override", stats.Data)
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/targets?type=user&search=developer", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user target override count status = %d body = %s", rec.Code, rec.Body.String())
	}
	var overriddenTargets struct {
		Data []struct {
			ID        int64 `json:"id"`
			Overrides int64 `json:"overrides"`
		} `json:"data"`
	}
	decodeBody(t, rec, &overriddenTargets)
	foundOverrideCount := false
	for _, target := range overriddenTargets.Data {
		if target.ID == login.Data.User.ID {
			if target.Overrides != 1 {
				t.Fatalf("user target overrides = %d, want 1", target.Overrides)
			}
			foundOverrideCount = true
		}
	}
	if !foundOverrideCount {
		t.Fatalf("user targets = %+v, want user %d", overriddenTargets.Data, login.Data.User.ID)
	}
	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/flags",
		`{"target_type":"user","target_id":`+userID+`,"flags":{"feature.audit":null}}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user flag clear status = %d body = %s", rec.Code, rec.Body.String())
	}
	userFlags = getFlags("/dev/api/flags?target_type=user&target_id=" + userID)
	if got := item(userFlags, "feature.audit"); !got.Value || got.Source != "global" {
		t.Fatalf("user feature.audit after clear = %+v", got)
	}

	// ── validation and unknown keys ──────────────────────────────────────
	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/flags",
		`{"target_type":"branch","flags":{"feature.audit":true}}`, token, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported target status = %d, want 422 (body %s)", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/flags",
		`{"flags":{"feature.unknown":true}}`, token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown flag status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}

	// ── shared target search (tenants and users) ────────────────────────
	type targetList struct {
		Data []struct {
			ID         int64   `json:"id"`
			Type       string  `json:"type"`
			Name       string  `json:"name"`
			Code       *string `json:"code"`
			Email      *string `json:"email"`
			TenantName *string `json:"tenant_name"`
			Overrides  int64   `json:"overrides"`
		} `json:"data"`
		Meta struct {
			CurrentPage int   `json:"current_page"`
			LastPage    int   `json:"last_page"`
			PerPage     int   `json:"per_page"`
			Total       int64 `json:"total"`
		} `json:"meta"`
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/targets?type=tenant&search=Main", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant target search status = %d body = %s", rec.Code, rec.Body.String())
	}
	var tenants targetList
	decodeBody(t, rec, &tenants)
	foundTenant := false
	for _, target := range tenants.Data {
		if strconv.FormatInt(target.ID, 10) == tenantID && target.Type == "tenant" && target.Code != nil {
			foundTenant = true
		}
	}
	if !foundTenant {
		t.Fatalf("tenant targets = %+v, want id %s", tenants.Data, tenantID)
	}
	if tenants.Meta.Total < 1 || tenants.Meta.CurrentPage != 1 || tenants.Meta.PerPage != 10 {
		t.Fatalf("tenant target meta = %+v", tenants.Meta)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/targets?type=user&search=developer", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user target search status = %d body = %s", rec.Code, rec.Body.String())
	}
	var users targetList
	decodeBody(t, rec, &users)
	foundUser := false
	for _, target := range users.Data {
		if target.ID == login.Data.User.ID && target.Type == "user" && target.Email != nil {
			foundUser = true
		}
	}
	if !foundUser {
		t.Fatalf("user targets = %+v, want user %d", users.Data, login.Data.User.ID)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/targets?type=user&per_page=1&page=1", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("paged user targets status = %d body = %s", rec.Code, rec.Body.String())
	}
	var paged targetList
	decodeBody(t, rec, &paged)
	if len(paged.Data) != 1 || paged.Meta.PerPage != 1 || paged.Meta.Total < 2 || paged.Meta.LastPage < 2 {
		t.Fatalf("paged user targets = %d rows meta %+v, want one row and multiple pages", len(paged.Data), paged.Meta)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/targets?type=role", "", token, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported target type search status = %d, want 422", rec.Code)
	}

	// ── sync inserts missing globals and stays idempotent ───────────────
	rec = apiRequest(t, router, http.MethodPost, "/dev/api/flags/sync", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("flags sync status = %d body = %s", rec.Code, rec.Body.String())
	}
	var synced struct {
		Data struct {
			Created int `json:"created"`
		} `json:"data"`
	}
	decodeBody(t, rec, &synced)
	rec = apiRequest(t, router, http.MethodPost, "/dev/api/flags/sync", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("second flags sync status = %d body = %s", rec.Code, rec.Body.String())
	}
	decodeBody(t, rec, &synced)
	if synced.Data.Created != 0 {
		t.Fatalf("second sync created = %d, want 0", synced.Data.Created)
	}

	// ── every mutation is audited ───────────────────────────────────────
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/actions", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("actions status = %d body = %s", rec.Code, rec.Body.String())
	}
	var actions struct {
		Data []struct {
			Action string `json:"action"`
		} `json:"data"`
	}
	decodeBody(t, rec, &actions)
	seen := map[string]bool{}
	for _, action := range actions.Data {
		seen[action.Action] = true
	}
	for _, want := range []string{"flags.set", "flags.reset", "flags.sync"} {
		if !seen[want] {
			t.Fatalf("action %q missing from %+v", want, actions.Data)
		}
	}
}
