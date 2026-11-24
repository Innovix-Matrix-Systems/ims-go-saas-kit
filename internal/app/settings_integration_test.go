package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	billingdomain "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

type settingItemJSON struct {
	ID       int64  `json:"id"`
	Key      string `json:"key"`
	Value    any    `json:"value"`
	Source   string `json:"source"`
	Scope    string `json:"scope"`
	Editable bool   `json:"editable"`
}

type settingsGroupJSON struct {
	Group struct {
		ID        int64  `json:"id"`
		Slug      string `json:"slug"`
		SortOrder int    `json:"sort_order"`
	} `json:"group"`
	Settings []settingItemJSON `json:"settings"`
}

// TestSettingsAndFeatureFlagsEndToEnd exercises the settings and feature-flags REST surface
// through the real router: grouped reads, tenant-admin writes, internal
// target-aware writes, permission boundaries and validation.
func TestSettingsAndFeatureFlagsEndToEnd(t *testing.T) {

	kit := testkit.New(t)
	cfg := kit.Config

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := kit.Deps

	ctx := context.Background()
	if err := seeder.Run(ctx, cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
		t.Fatalf("seeder.Run() error = %v", err)
	}
	if _, err := deps.Platform.Settings.Sync(ctx); err != nil {
		t.Fatalf("settings sync: %v", err)
	}
	if _, err := deps.Platform.FeatureFlags.Sync(ctx); err != nil {
		t.Fatalf("feature flags sync: %v", err)
	}

	// Unlimited trial so tenant resource creation passes the billing guard.
	unlimited, err := deps.Platform.BillingPackages.Create(ctx, billingdomain.PackageInput{
		Name: "Test Unlimited", Price: 0, BillingCycle: billingdomain.CycleMonthly, TrialDays: 30, IsActive: true,
	}, []billingdomain.FeatureInput{
		{FeatureKey: string(billingdomain.FeatureUsersMax), IsEnabled: true, LimitValue: -1},
		{FeatureKey: string(billingdomain.FeatureBranchesMax), IsEnabled: true, LimitValue: -1},
	})
	if err != nil {
		t.Fatalf("create test package: %v", err)
	}
	if _, err := deps.Platform.BillingSubs.StartTrial(ctx, 1, unlimited.ID); err != nil {
		t.Fatalf("start tenant trial: %v", err)
	}

	router := deps.Router()
	acting := map[string]string{"X-Acting-Tenant": "1", "X-Acting-Branch": "1"}

	superToken := loginToken(t, router, "superadmin@ims.com", "secret123", "super-device")

	// Create a tenant admin under tenant 1 (holds setting.update).
	one := int64(1)
	adminRole, err := deps.Tx.Queries().GetRoleByName(ctx, dbgen.GetRoleByNameParams{Name: "Admin", TenantID: &one})
	if err != nil {
		t.Fatalf("lookup Admin role: %v", err)
	}
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/user",
		`{"name":"Acme Admin","email":"settings-admin@acme.test","password":"secret123","branch_id":1,"roles":[`+strconv.FormatInt(adminRole.ID, 10)+`]}`,
		superToken, acting)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tenant admin status = %d body = %s", rec.Code, rec.Body.String())
	}
	adminToken := loginToken(t, router, "settings-admin@acme.test", "secret123", "admin-device")

	// ── grouped effective settings for the tenant admin ─────────────────
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/settings", "", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings status = %d body = %s", rec.Code, rec.Body.String())
	}
	var grouped struct {
		Data []settingsGroupJSON `json:"data"`
	}
	decodeBody(t, rec, &grouped)
	if len(grouped.Data) == 0 {
		t.Fatal("GET /settings returned no groups")
	}
	notification := findSetting(t, grouped.Data, "notification.enabled")
	if !notification.Editable || notification.Scope != "tenant" {
		t.Fatalf("notification.enabled = editable %v scope %q; want editable tenant", notification.Editable, notification.Scope)
	}
	if notification.ID == 0 {
		t.Fatal("grouped setting is missing its id")
	}

	// ── tenant admin writes their own override ──────────────────────────
	rec = apiRequest(t, router, http.MethodPatch, "/api/v1/settings/tenant/notification.enabled",
		`{"value":false}`, adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH tenant setting status = %d body = %s", rec.Code, rec.Body.String())
	}
	var patched struct {
		Data struct {
			Value  any    `json:"value"`
			Source string `json:"source"`
		} `json:"data"`
	}
	decodeBody(t, rec, &patched)
	if patched.Data.Value != false || patched.Data.Source != "tenant" {
		t.Fatalf("patched setting = %+v; want value false source tenant", patched.Data)
	}

	// A GlobalOnly key rejects the tenant write.
	rec = apiRequest(t, router, http.MethodPatch, "/api/v1/settings/tenant/system.name",
		`{"value":"hijack"}`, adminToken, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("GlobalOnly tenant write status = %d body = %s", rec.Code, rec.Body.String())
	}

	// ── tenant admins cannot touch feature flags ────────────────────────
	rec = apiRequest(t, router, http.MethodPatch, "/api/v1/internal/feature-flags",
		`{"flags":{"feature.audit":true}}`, adminToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant admin flag write status = %d body = %s", rec.Code, rec.Body.String())
	}

	// ── internal settings: read + target-aware write ────────────────────
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/internal/settings", "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /internal/settings status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Unknown key -> 404.
	rec = apiRequest(t, router, http.MethodPatch, "/api/v1/internal/settings/nope.key",
		`{"value":true}`, superToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown key status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Global target with a target_id -> 422.
	rec = apiRequest(t, router, http.MethodPatch, "/api/v1/internal/settings/system.name",
		`{"value":"x","target_id":1}`, superToken, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("global+target_id status = %d body = %s", rec.Code, rec.Body.String())
	}

	// user target is not enabled for settings -> 422.
	rec = apiRequest(t, router, http.MethodPatch, "/api/v1/internal/settings/system.name",
		`{"value":"x","target_type":"user","target_id":1}`, superToken, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("user target settings status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Explicit target_type "global" is accepted (normalized to the global row).
	rec = apiRequest(t, router, http.MethodPatch, "/api/v1/internal/settings/system.name",
		`{"value":"Global Name","target_type":"global"}`, superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("explicit global status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/internal/settings?target_type=global", "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("internal settings target_type=global status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Internal tenant override for a tenant-scoped key.
	rec = apiRequest(t, router, http.MethodPatch, "/api/v1/internal/settings/notification.enabled",
		`{"value":true,"target_type":"tenant","target_id":1}`, superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("internal tenant override status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/internal/settings?target_type=tenant&target_id=1", "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("internal settings tenant target status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Deleting the tenant override falls back to the global value.
	rec = apiRequest(t, router, http.MethodDelete,
		"/api/v1/internal/settings/notification.enabled?target_type=tenant&target_id=1", "", superToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete tenant override status = %d body = %s", rec.Code, rec.Body.String())
	}

	// ── feature flags: effective read + internal writes ─────────────────
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/feature-flags", "", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /feature-flags status = %d body = %s", rec.Code, rec.Body.String())
	}
	var flags struct {
		Data map[string]bool `json:"data"`
	}
	decodeBody(t, rec, &flags)
	if _, ok := flags.Data["feature.audit"]; !ok {
		t.Fatalf("feature flags payload = %+v", flags.Data)
	}

	rec = apiRequest(t, router, http.MethodPatch, "/api/v1/internal/feature-flags",
		`{"flags":{"feature.audit":true}}`, superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("internal flag write status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, router, http.MethodGet, "/api/v1/internal/feature-flags?target_type=tenant&target_id=1",
		"", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("internal flag read status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/internal/feature-flags?target_type=global",
		"", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("internal flag target_type=global status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, router, http.MethodDelete, "/api/v1/internal/feature-flags/feature.audit", "", superToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("internal flag delete status = %d body = %s", rec.Code, rec.Body.String())
	}

	// ── settings groups: CRUD + key assignment ──────────────────────────
	// Tenant admins cannot manage groups.
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/internal/settings-groups",
		`{"name":"Reports"}`, adminToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant admin group create status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, router, http.MethodPost, "/api/v1/internal/settings-groups",
		`{"name":"Reports","description":"Reporting settings","sort_order":9}`, superToken, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group status = %d body = %s", rec.Code, rec.Body.String())
	}
	var groupResp struct {
		Data struct {
			ID       int64  `json:"id"`
			Slug     string `json:"slug"`
			IsSystem bool   `json:"is_system"`
		} `json:"data"`
	}
	decodeBody(t, rec, &groupResp)
	if groupResp.Data.ID == 0 || groupResp.Data.Slug != "reports" || groupResp.Data.IsSystem {
		t.Fatalf("created group = %+v", groupResp.Data)
	}
	groupID := strconv.FormatInt(groupResp.Data.ID, 10)

	// Duplicate name -> 422.
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/internal/settings-groups",
		`{"name":"reports"}`, superToken, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate group status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Show + paginated list + lightweight list.
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/internal/settings-groups/"+groupID, "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("show group status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/internal/settings-groups?per_page=50", "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list groups status = %d body = %s", rec.Code, rec.Body.String())
	}
	var groupList struct {
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	decodeBody(t, rec, &groupList)
	if groupList.Meta.Total < 4 {
		t.Fatalf("group list total = %d", groupList.Meta.Total)
	}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/internal/settings-groups?is_light_weight=1", "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("light list status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Assign system.name to the new group and confirm the grouped read.
	rec = apiRequest(t, router, http.MethodPut, "/api/v1/internal/settings/system.name/group",
		`{"group_id":`+groupID+`}`, superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign group status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/settings", "", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings after assign status = %d body = %s", rec.Code, rec.Body.String())
	}
	var regrouped struct {
		Data []settingsGroupJSON `json:"data"`
	}
	decodeBody(t, rec, &regrouped)
	assigned := false
	for _, g := range regrouped.Data {
		if g.Group.Slug != "reports" {
			continue
		}
		if g.Group.ID == 0 {
			t.Fatalf("group id missing from grouped response: %+v", g.Group)
		}
		for _, s := range g.Settings {
			if s.Key == "system.name" {
				assigned = true
			}
		}
	}
	if !assigned {
		t.Fatalf("system.name did not move under the reports group; body=%s", rec.Body.String())
	}

	// Assigning an unknown key / group -> 404.
	rec = apiRequest(t, router, http.MethodPut, "/api/v1/internal/settings/nope.key/group",
		`{"group_id":null}`, superToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("assign unknown key status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodPut, "/api/v1/internal/settings/system.name/group",
		`{"group_id":999999}`, superToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("assign unknown group status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Update the group name (slug re-derived).
	rec = apiRequest(t, router, http.MethodPatch, "/api/v1/internal/settings-groups/"+groupID,
		`{"name":"Reports & Exports"}`, superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("update group status = %d body = %s", rec.Code, rec.Body.String())
	}
	var updatedGroup struct {
		Data struct {
			Slug string `json:"slug"`
		} `json:"data"`
	}
	decodeBody(t, rec, &updatedGroup)
	if updatedGroup.Data.Slug != "reports-exports" {
		t.Fatalf("updated slug = %q", updatedGroup.Data.Slug)
	}

	// Delete the group -> system.name falls back to its catalog group.
	rec = apiRequest(t, router, http.MethodDelete, "/api/v1/internal/settings-groups/"+groupID, "", superToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete group status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/settings", "", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings after delete status = %d body = %s", rec.Code, rec.Body.String())
	}
	var afterDelete struct {
		Data []settingsGroupJSON `json:"data"`
	}
	decodeBody(t, rec, &afterDelete)
	fallback := false
	for _, g := range afterDelete.Data {
		if g.Group.Slug == "reports" || g.Group.Slug == "reports-exports" {
			t.Fatal("deleted group still present in grouped settings")
		}
		if g.Group.Slug == "general" {
			for _, s := range g.Settings {
				if s.Key == "system.name" {
					fallback = true
				}
			}
		}
	}
	if !fallback {
		t.Fatal("system.name did not fall back to the general group")
	}
}

func findSetting(t *testing.T, groups []settingsGroupJSON, key string) settingItemJSON {
	t.Helper()
	for _, group := range groups {
		for _, setting := range group.Settings {
			if setting.Key == key {
				return setting
			}
		}
	}
	t.Fatalf("setting %q not found in response", key)
	return settingItemJSON{}
}
