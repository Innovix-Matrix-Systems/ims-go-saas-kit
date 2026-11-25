package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

// TestDeveloperSettingsEndToEnd proves the settings panel delegates to the
// settings service: groups are created/assigned/deleted, global and tenant
// values resolve with the right source, and every mutation is audited.
func TestDeveloperSettingsEndToEnd(t *testing.T) {
	router, token, deps := settingsRouter(t)
	ctx := context.Background()
	if _, err := deps.Platform.Settings.Sync(ctx); err != nil {
		t.Fatalf("settings sync: %v", err)
	}
	tenantID := strconv.FormatInt(deps.Config.Tenancy.DefaultTenantID, 10)

	type groupItem struct {
		ID        int64  `json:"id"`
		Slug      string `json:"slug"`
		Name      string `json:"name"`
		SortOrder int    `json:"sort_order"`
		IsSystem  bool   `json:"is_system"`
	}
	getGroups := func() []groupItem {
		t.Helper()
		rec := apiRequest(t, router, http.MethodGet, "/dev/api/settings/groups", "", token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("settings groups status = %d body = %s", rec.Code, rec.Body.String())
		}
		var response struct {
			Data []groupItem `json:"data"`
		}
		decodeBody(t, rec, &response)
		return response.Data
	}
	groups := getGroups()
	if len(groups) != len(settings.Groups) {
		t.Fatalf("settings groups = %+v, want the %d catalog groups", groups, len(settings.Groups))
	}
	var generalID, ungroupedID int64
	for _, group := range groups {
		if group.Slug == "general" && group.IsSystem {
			generalID = group.ID
		}
		if group.Slug == "ungrouped" && group.IsSystem {
			ungroupedID = group.ID
		}
	}
	if generalID == 0 || ungroupedID == 0 {
		t.Fatalf("catalog groups = %+v, want general and ungrouped", groups)
	}

	// Renaming a system/catalog group keeps its slug; the next catalog sync
	// restores the catalog metadata.
	renameRec := apiRequest(t, router, http.MethodPatch, "/dev/api/settings/groups/"+strconv.FormatInt(generalID, 10),
		`{"name":"Core Settings","description":"Renamed","sort_order":1}`, token, nil)
	if renameRec.Code != http.StatusOK {
		t.Fatalf("rename system group status = %d body = %s", renameRec.Code, renameRec.Body.String())
	}
	var renamed struct {
		Data groupItem `json:"data"`
	}
	decodeBody(t, renameRec, &renamed)
	if renamed.Data.Name != "Core Settings" || renamed.Data.Slug != "general" {
		t.Fatalf("renamed system group = %+v, want the stable general slug", renamed.Data)
	}
	renameRec = apiRequest(t, router, http.MethodPost, "/dev/api/settings/sync", "", token, nil)
	if renameRec.Code != http.StatusOK {
		t.Fatalf("restore system group status = %d body = %s", renameRec.Code, renameRec.Body.String())
	}
	groups = getGroups()
	restored := false
	for _, group := range groups {
		if group.ID == generalID && group.Name == "General" && group.Slug == "general" {
			restored = true
		}
	}
	if !restored {
		t.Fatalf("system group was not restored by sync: %+v", groups)
	}

	type settingItem struct {
		ID         int64  `json:"id"`
		Key        string `json:"key"`
		Type       string `json:"type"`
		Value      any    `json:"value"`
		Default    any    `json:"default"`
		Source     string `json:"source"`
		Scope      string `json:"scope"`
		Editable   bool   `json:"editable"`
		Overridden bool   `json:"overridden"`
		Group      struct {
			ID   int64  `json:"id"`
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"group"`
	}
	getSettings := func(target string) []settingItem {
		t.Helper()
		rec := apiRequest(t, router, http.MethodGet, "/dev/api/settings"+target, "", token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("settings %s status = %d body = %s", target, rec.Code, rec.Body.String())
		}
		var response struct {
			Data []settingItem `json:"data"`
		}
		decodeBody(t, rec, &response)
		return response.Data
	}
	find := func(items []settingItem, key string) settingItem {
		t.Helper()
		for _, item := range items {
			if item.Key == key {
				return item
			}
		}
		t.Fatalf("setting %q missing from %+v", key, items)
		return settingItem{}
	}

	global := getSettings("")
	name := find(global, "system.name")
	if name.Source != "global" || !name.Overridden || name.Group.Slug != "general" || !name.Editable {
		t.Fatalf("global system.name = %+v", name)
	}
	notifications := find(global, "notification.enabled")
	if notifications.Type != "bool" || notifications.Scope != "tenant" || notifications.Source != "global" {
		t.Fatalf("global notification.enabled = %+v", notifications)
	}

	rec := apiRequest(t, router, http.MethodPatch, "/dev/api/settings/notification.enabled",
		`{"target_type":"global","value":false}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("set global setting status = %d body = %s", rec.Code, rec.Body.String())
	}
	var updated struct {
		Data settingItem `json:"data"`
	}
	decodeBody(t, rec, &updated)
	if updated.Data.Value != false || updated.Data.Source != "global" || !updated.Data.Overridden {
		t.Fatalf("global notification.enabled after set = %+v", updated.Data)
	}

	// Park a setting in the Ungrouped bucket and move it back.
	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/settings/system.name/group",
		`{"group_id":`+strconv.FormatInt(ungroupedID, 10)+`}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("move to ungrouped status = %d body = %s", rec.Code, rec.Body.String())
	}
	global = getSettings("")
	if got := find(global, "system.name"); got.Group.Slug != "ungrouped" {
		t.Fatalf("system.name after moving to ungrouped = %+v", got.Group)
	}
	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/settings/system.name/group",
		`{"group_id":`+strconv.FormatInt(generalID, 10)+`}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("move back from ungrouped status = %d body = %s", rec.Code, rec.Body.String())
	}
	global = getSettings("")
	if got := find(global, "system.name"); got.Group.Slug != "general" {
		t.Fatalf("system.name after moving back = %+v", got.Group)
	}

	tenant := getSettings("?target_type=tenant&target_id=" + tenantID)
	tenantName := find(tenant, "system.name")
	if tenantName.Editable {
		t.Fatalf("global-only system.name is editable for a tenant: %+v", tenantName)
	}
	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/settings/system.name",
		`{"target_type":"tenant","target_id":`+tenantID+`,"value":"Tenant name"}`, token, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("tenant override of global-only key status = %d, want 422", rec.Code)
	}

	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/settings/notification.enabled",
		`{"target_type":"tenant","target_id":`+tenantID+`,"value":false}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("set tenant setting status = %d body = %s", rec.Code, rec.Body.String())
	}
	tenant = getSettings("?target_type=tenant&target_id=" + tenantID)
	tenantNotification := find(tenant, "notification.enabled")
	if tenantNotification.Value != false || tenantNotification.Source != "tenant" || !tenantNotification.Overridden {
		t.Fatalf("tenant notification.enabled = %+v", tenantNotification)
	}

	rec = apiRequest(t, router, http.MethodDelete,
		"/dev/api/settings/notification.enabled?target_type=tenant&target_id="+tenantID, "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset tenant setting status = %d body = %s", rec.Code, rec.Body.String())
	}
	tenant = getSettings("?target_type=tenant&target_id=" + tenantID)
	tenantNotification = find(tenant, "notification.enabled")
	if tenantNotification.Source != "global" || tenantNotification.Overridden {
		t.Fatalf("tenant notification.enabled after reset = %+v", tenantNotification)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/settings/groups",
		`{"name":"Experimental","description":"Drag-and-drop target","sort_order":9}`, token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create settings group status = %d body = %s", rec.Code, rec.Body.String())
	}
	var createdGroup struct {
		Data groupItem `json:"data"`
	}
	decodeBody(t, rec, &createdGroup)
	if createdGroup.Data.ID < 1 || createdGroup.Data.Slug != "experimental" || createdGroup.Data.IsSystem {
		t.Fatalf("created group = %+v", createdGroup.Data)
	}
	groupID := createdGroup.Data.ID

	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/settings/groups/"+strconv.FormatInt(groupID, 10),
		`{"name":"Experimental Lab","description":"Updated","sort_order":4}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("update settings group status = %d body = %s", rec.Code, rec.Body.String())
	}
	var updatedGroup struct {
		Data groupItem `json:"data"`
	}
	decodeBody(t, rec, &updatedGroup)
	if updatedGroup.Data.Name != "Experimental Lab" || updatedGroup.Data.Slug != "experimental-lab" || updatedGroup.Data.SortOrder != 4 {
		t.Fatalf("updated group = %+v", updatedGroup.Data)
	}

	rec = apiRequest(t, router, http.MethodPatch, "/dev/api/settings/system.name/group",
		`{"group_id":`+strconv.FormatInt(groupID, 10)+`}`, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign setting group status = %d body = %s", rec.Code, rec.Body.String())
	}
	global = getSettings("")
	if got := find(global, "system.name"); got.Group.ID != groupID || got.Group.Name != "Experimental Lab" {
		t.Fatalf("system.name after assignment = %+v", got.Group)
	}

	rec = apiRequest(t, router, http.MethodDelete,
		"/dev/api/settings/groups/"+strconv.FormatInt(groupID, 10), "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete settings group status = %d body = %s", rec.Code, rec.Body.String())
	}
	global = getSettings("")
	if got := find(global, "system.name"); got.Group.Slug != "general" {
		t.Fatalf("system.name after group delete = %+v, want the catalog group", got.Group)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/settings/sync", "", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings sync status = %d body = %s", rec.Code, rec.Body.String())
	}
	var synced struct {
		Data struct {
			Created int `json:"created"`
		} `json:"data"`
	}
	decodeBody(t, rec, &synced)
	if synced.Data.Created != 0 {
		t.Fatalf("second settings sync created = %d, want 0", synced.Data.Created)
	}

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
	for _, want := range []string{
		"settings.set", "settings.reset", "settings.group.create", "settings.group.update",
		"settings.group.assign", "settings.group.delete", "settings.sync",
	} {
		if !seen[want] {
			t.Fatalf("action %q missing from %+v", want, actions.Data)
		}
	}
}

func settingsRouter(t *testing.T) (http.Handler, string, *app.Dependencies) {
	t.Helper()
	t.Setenv("APP_ENV", "local")
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
		`{"email":"developer@ims.com","password":"secret123","device":"settings-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("developer login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &login)
	if login.Data.Token == "" {
		t.Fatal("developer token missing")
	}
	return router, login.Data.Token, deps
}
