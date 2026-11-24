package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/seeder"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// TestDeveloperDashboardEndToEnd proves the access model: the seeded
// developer account logs in, reaches the gated /dev API, and the hidden
// Developer role never appears in role responses.
func TestDeveloperDashboardEndToEnd(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")

	kit := testkit.New(t)
	cfg := kit.Config

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps, pool := kit.Deps, kit.Pool

	ctx := context.Background()
	runSeed := func() {
		t.Helper()
		if err := seeder.Run(ctx, cfg, deps.Tx.Queries(), deps.Tx, deps.Checker, logger); err != nil {
			t.Fatalf("seeder.Run() error = %v", err)
		}
	}
	runSeed()
	runSeed() // idempotency

	router := deps.Router()

	// ── developer login: the hidden role is not exposed in the payload ──
	rec := apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"developer@ims.com","password":"secret123","device":"dev-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("developer login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Data struct {
			Status string `json:"status"`
			Token  string `json:"token"`
			User   struct {
				Roles []struct {
					Name string `json:"name"`
				} `json:"roles"`
			} `json:"user"`
		} `json:"data"`
	}
	decodeBody(t, rec, &login)
	if login.Data.Status != "AUTH_SUCCESS" || login.Data.Token == "" {
		t.Fatalf("developer login payload = %+v", login.Data)
	}
	for _, role := range login.Data.User.Roles {
		if role.Name == authz.DeveloperRole || role.Name == authz.SuperAdminRole {
			t.Fatalf("hidden/privileged role leaked in login payload: %+v", login.Data.User.Roles)
		}
	}
	devToken := login.Data.Token

	// ── /dev/api/me works for the developer account ─────────────────────
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/me", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev me status = %d body = %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Data struct {
			UserID       int64    `json:"user_id"`
			IsSuperAdmin bool     `json:"is_super_admin"`
			Permissions  []string `json:"permissions"`
		} `json:"data"`
	}
	decodeBody(t, rec, &me)
	if me.Data.UserID < 1 || me.Data.IsSuperAdmin {
		t.Fatalf("dev me payload = %+v", me.Data)
	}
	permissions := map[string]bool{}
	for _, name := range me.Data.Permissions {
		permissions[name] = true
	}
	if !permissions[authz.PermDevView] || !permissions[authz.PermDevManage] {
		t.Fatalf("developer permissions missing dev.*: %v", me.Data.Permissions)
	}
	developerUserID := me.Data.UserID

	// �"?�"? sessions: stats, directory, current-token flag and safe force logout �"?�?
	tokenParts := strings.SplitN(devToken, "|", 2)
	if len(tokenParts) != 2 {
		t.Fatalf("developer token %q is not in id|secret form", devToken)
	}
	currentTokenID, err := strconv.ParseInt(tokenParts[0], 10, 64)
	if err != nil {
		t.Fatalf("developer token id %q: %v", tokenParts[0], err)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/sessions/stats", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions stats status = %d body = %s", rec.Code, rec.Body.String())
	}
	var stats struct {
		Data struct {
			Users        int64 `json:"users"`
			ActiveTokens int64 `json:"active_tokens"`
			Used24h      int64 `json:"used_24h"`
		} `json:"data"`
	}
	decodeBody(t, rec, &stats)
	if stats.Data.Users < 1 || stats.Data.ActiveTokens < 1 || stats.Data.Used24h < 1 {
		t.Fatalf("sessions stats = %+v, want the developer token counted", stats.Data)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/sessions/users?search=developer", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions users status = %d body = %s", rec.Code, rec.Body.String())
	}
	var sessionUsers struct {
		Data []struct {
			UserID       int64 `json:"user_id"`
			ActiveTokens int64 `json:"active_tokens"`
		} `json:"data"`
	}
	decodeBody(t, rec, &sessionUsers)
	foundDeveloper := false
	for _, user := range sessionUsers.Data {
		if user.UserID == me.Data.UserID && user.ActiveTokens > 0 {
			foundDeveloper = true
		}
	}
	if !foundDeveloper {
		t.Fatalf("sessions users = %+v, want the developer user", sessionUsers.Data)
	}

	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/sessions/users/"+strconv.FormatInt(me.Data.UserID, 10)+"/tokens", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions tokens status = %d body = %s", rec.Code, rec.Body.String())
	}
	var sessionTokens struct {
		Data []struct {
			ID      int64 `json:"id"`
			Current bool  `json:"current"`
		} `json:"data"`
	}
	decodeBody(t, rec, &sessionTokens)
	foundCurrent := false
	for _, token := range sessionTokens.Data {
		if token.ID == currentTokenID {
			if !token.Current {
				t.Fatalf("token %d is not flagged current: %+v", currentTokenID, sessionTokens.Data)
			}
			foundCurrent = true
		}
	}
	if !foundCurrent {
		t.Fatalf("sessions tokens = %+v, want token %d", sessionTokens.Data, currentTokenID)
	}

	rec = apiRequest(t, router, http.MethodGet, "/dev/api/sessions/users/99999999/tokens", "", devToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session user status = %d, want 404", rec.Code)
	}
	rec = apiRequest(t, router, http.MethodDelete, "/dev/api/sessions/tokens/99999999", "", devToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session token status = %d, want 404", rec.Code)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/sessions/prune",
		`{"idle_days":1,"dry_run":true}`, devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("session prune dry-run status = %d body = %s", rec.Code, rec.Body.String())
	}
	var prune struct {
		Data struct {
			Affected int64 `json:"affected"`
		} `json:"data"`
	}
	decodeBody(t, rec, &prune)
	if prune.Data.Affected != 0 {
		t.Fatalf("session prune dry-run affected = %d, want 0 (current token is in use)", prune.Data.Affected)
	}

	// �"?�"? revoke one extra device token and one user's sessions (non-caller) �"?�?
	var extraTokenID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO personal_access_tokens (tokenable_type, tokenable_id, name, device, token)
		 VALUES ('user', $1, 'extra', 'extra-device', md5('extra') || md5('extra-token')) RETURNING id`,
		me.Data.UserID,
	).Scan(&extraTokenID); err != nil {
		t.Fatalf("insert extra session token: %v", err)
	}
	rec = apiRequest(t, router, http.MethodDelete,
		"/dev/api/sessions/tokens/"+strconv.FormatInt(extraTokenID, 10), "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke extra token status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/sessions/users/"+strconv.FormatInt(me.Data.UserID, 10)+"/tokens", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("tokens after revoke status = %d body = %s", rec.Code, rec.Body.String())
	}
	var afterRevoke struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &afterRevoke)
	for _, token := range afterRevoke.Data {
		if token.ID == extraTokenID {
			t.Fatalf("revoked token %d still listed", extraTokenID)
		}
	}

	var targetUserID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (name, email, password, is_active)
		 VALUES ('Session Target', 'session-target@example.test', 'hash', 1) RETURNING id`,
	).Scan(&targetUserID); err != nil {
		t.Fatalf("insert session target user: %v", err)
	}
	var targetTokenID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO personal_access_tokens (tokenable_type, tokenable_id, name, device, token)
		 VALUES ('user', $1, 'target', 'target-device', md5('target') || md5('target-token')) RETURNING id`,
		targetUserID,
	).Scan(&targetTokenID); err != nil {
		t.Fatalf("insert target session token: %v", err)
	}
	rec = apiRequest(t, router, http.MethodDelete,
		"/dev/api/sessions/users/"+strconv.FormatInt(targetUserID, 10), "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke target user sessions status = %d body = %s", rec.Code, rec.Body.String())
	}
	var targetRevoked struct {
		Data struct {
			Revoked int64 `json:"revoked"`
		} `json:"data"`
	}
	decodeBody(t, rec, &targetRevoked)
	if targetRevoked.Data.Revoked != 1 {
		t.Fatalf("revoke target user sessions = %+v, want 1", targetRevoked.Data)
	}

	rec = apiRequest(t, router, http.MethodDelete, "/dev/api/sessions", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("force logout all status = %d body = %s", rec.Code, rec.Body.String())
	}
	var revoked struct {
		Data struct {
			Revoked         int64 `json:"revoked"`
			ExcludedTokenID int64 `json:"excluded_token_id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &revoked)
	if revoked.Data.Revoked != 0 || revoked.Data.ExcludedTokenID != currentTokenID {
		t.Fatalf("force logout result = %+v, want 0 revoked and token %d preserved", revoked.Data, currentTokenID)
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/me", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("caller token did not survive force logout: %d %s", rec.Code, rec.Body.String())
	}

	// ── super admin also reaches the surface and is flagged ─────────────
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"superadmin@ims.com","password":"secret123","device":"dev-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("super admin login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var superLogin struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &superLogin)
	superToken := superLogin.Data.Token
	if superToken == "" {
		t.Fatal("super admin token missing")
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/me", "", superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("super admin dev me status = %d body = %s", rec.Code, rec.Body.String())
	}
	me = struct {
		Data struct {
			UserID       int64    `json:"user_id"`
			IsSuperAdmin bool     `json:"is_super_admin"`
			Permissions  []string `json:"permissions"`
		} `json:"data"`
	}{}
	decodeBody(t, rec, &me)
	if !me.Data.IsSuperAdmin {
		t.Fatalf("super admin me payload = %+v", me.Data)
	}

	// ── developers panel: stats, create internal account, self-unassign, revoke ──
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/developers/stats", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("developer stats status = %d body = %s", rec.Code, rec.Body.String())
	}
	var devStats struct {
		Data struct {
			Developers       int64 `json:"developers"`
			RoleExists       bool  `json:"role_exists"`
			RolePermissions  int64 `json:"role_permissions"`
			TotalPermissions int64 `json:"total_permissions"`
		} `json:"data"`
	}
	decodeBody(t, rec, &devStats)
	if !devStats.Data.RoleExists || devStats.Data.TotalPermissions != int64(len(authz.DeveloperPermissions())) {
		t.Fatalf("developer stats = %+v, want an existing role with %d permissions", devStats.Data, len(authz.DeveloperPermissions()))
	}
	if devStats.Data.Developers < 1 || devStats.Data.RolePermissions != int64(len(authz.DeveloperPermissions())) {
		t.Fatalf("developer stats counts = %+v", devStats.Data)
	}

	rec = apiRequest(t, router, http.MethodPost, "/dev/api/developers",
		`{"name":"Dev Created","email":"dev-created@example.test","password":"secret123","is_active":true}`,
		devToken, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create developer status = %d body = %s", rec.Code, rec.Body.String())
	}
	var createdDeveloper struct {
		Data struct {
			UserID       int64 `json:"user_id"`
			IsSelf       bool  `json:"is_self"`
			ActiveTokens int64 `json:"active_tokens"`
		} `json:"data"`
	}
	decodeBody(t, rec, &createdDeveloper)
	createdID := createdDeveloper.Data.UserID
	if createdID < 1 || createdDeveloper.Data.IsSelf || createdDeveloper.Data.ActiveTokens != 0 {
		t.Fatalf("created developer = %+v", createdDeveloper.Data)
	}

	type developerPage struct {
		Data []struct {
			UserID       int64 `json:"user_id"`
			IsSelf       bool  `json:"is_self"`
			ActiveTokens int64 `json:"active_tokens"`
		} `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/developers?search=dev-created", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("developers list status = %d body = %s", rec.Code, rec.Body.String())
	}
	var developers developerPage
	decodeBody(t, rec, &developers)
	foundCreated := false
	for _, item := range developers.Data {
		if item.UserID == createdID {
			foundCreated = true
			if item.ActiveTokens != 0 {
				t.Fatalf("created developer tokens = %d, want 0", item.ActiveTokens)
			}
		}
	}
	if !foundCreated || developers.Meta.Total != 1 {
		t.Fatalf("developers = %+v total=%d, want created user %d", developers.Data, developers.Meta.Total, createdID)
	}

	rec = apiRequest(t, router, http.MethodPost, "/api/v1/login",
		`{"email":"dev-created@example.test","password":"secret123","device":"dev-created-suite"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("created developer login status = %d body = %s", rec.Code, rec.Body.String())
	}
	var createdLogin struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, rec, &createdLogin)
	createdToken := createdLogin.Data.Token
	if createdToken == "" {
		t.Fatal("created developer login token missing")
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/me", "", createdToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("created developer me status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Self-unassign is refused for the seeded developer and for a newly created one.
	rec = apiRequest(t, router, http.MethodDelete,
		"/dev/api/developers/"+strconv.FormatInt(developerUserID, 10), "", devToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("seeded self-unassign status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodDelete,
		"/dev/api/developers/"+strconv.FormatInt(createdID, 10), "", createdToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("created self-unassign status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, router, http.MethodDelete,
		"/dev/api/developers/"+strconv.FormatInt(createdID, 10), "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove developer status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/me", "", createdToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("created developer access after revoke = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// ── the hidden role is absent from role listings ────────────────────
	acting := map[string]string{"X-Acting-Tenant": "1"}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/role", "", superToken, acting)
	if rec.Code != http.StatusOK {
		t.Fatalf("role list status = %d body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), authz.DeveloperRole) {
		t.Fatalf("hidden developer role leaked in role list: %s", rec.Body.String())
	}

	// ── direct tenant-surface fetch of the hidden role 404s ─────────────
	one := int64(1)
	role, err := deps.Tx.Queries().GetRoleByName(ctx, dbgen.GetRoleByNameParams{
		Name:     authz.DeveloperRole,
		TenantID: &one,
	})
	if err != nil {
		t.Fatalf("lookup developer role: %v", err)
	}
	if !role.Hidden || !role.IsSystem {
		t.Fatalf("developer role flags = hidden:%v system:%v", role.Hidden, role.IsSystem)
	}
	rec = apiRequest(t, router, http.MethodGet, "/api/v1/role/"+strconv.FormatInt(role.ID, 10), "", superToken, acting)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("hidden role fetch status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}

	// ── the hidden role holds only the developer-dashboard permissions ─────────
	var assigned int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM role_has_permissions WHERE role_id = $1", role.ID,
	).Scan(&assigned); err != nil {
		t.Fatalf("count role permissions: %v", err)
	}
	if assigned != len(authz.DeveloperPermissions()) {
		t.Fatalf("developer role permissions = %d, want %d", assigned, len(authz.DeveloperPermissions()))
	}

	// ── the action audit log is reachable and starts empty ────────────────────
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/actions", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev actions status = %d body = %s", rec.Code, rec.Body.String())
	}
	var actions struct {
		Data []struct {
			ID     int64  `json:"id"`
			Action string `json:"action"`
		} `json:"data"`
	}
	decodeBody(t, rec, &actions)
	auditedLogout := false
	for _, action := range actions.Data {
		if action.Action == "sessions.logout_all" {
			auditedLogout = true
		}
	}
	if !auditedLogout {
		t.Fatalf("actions = %+v, want the audited sessions.logout_all action", actions.Data)
	}

	// ── captured request detail returns arrays, never null ───────────────────
	time.Sleep(1200 * time.Millisecond) // let the telemetry collector flush
	rec = apiRequest(t, router, http.MethodGet, "/dev/api/requests?limit=1", "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev requests status = %d body = %s", rec.Code, rec.Body.String())
	}
	var requests struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &requests)
	if len(requests.Data) == 0 {
		t.Fatal("no captured requests after login")
	}
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/requests/"+strconv.FormatInt(requests.Data[0].ID, 10), "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev request detail status = %d body = %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Data struct {
			Queries     []json.RawMessage `json:"queries"`
			CacheEvents []json.RawMessage `json:"cache_events"`
			QueueEvents []json.RawMessage `json:"queue_events"`
			Logs        []json.RawMessage `json:"logs"`
			Exceptions  []json.RawMessage `json:"exceptions"`
		} `json:"data"`
	}
	decodeBody(t, rec, &detail)
	if detail.Data.Queries == nil || detail.Data.CacheEvents == nil ||
		detail.Data.QueueEvents == nil || detail.Data.Logs == nil || detail.Data.Exceptions == nil {
		t.Fatalf("correlated arrays must be [] not null: %+v", detail.Data)
	}

	// ── a background export records its queue dispatch on the request ────────
	rec = apiRequest(t, router, http.MethodPost, "/api/v1/internal/user/export-background",
		`{"order_by":"created_at","is_active":true,"order_direction":"desc"}`, superToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export-background status = %d body = %s", rec.Code, rec.Body.String())
	}
	exportRequestID := rec.Header().Get("X-Request-ID")
	if exportRequestID == "" {
		t.Fatal("export-background response is missing X-Request-ID")
	}
	time.Sleep(1200 * time.Millisecond) // let the telemetry collector flush
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/requests?limit=1&request_id="+exportRequestID, "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export request lookup status = %d body = %s", rec.Code, rec.Body.String())
	}
	var exportRequests struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &exportRequests)
	if len(exportRequests.Data) != 1 {
		t.Fatalf("export request lookup = %+v, want one captured request", exportRequests.Data)
	}
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/requests/"+strconv.FormatInt(exportRequests.Data[0].ID, 10), "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export detail status = %d body = %s", rec.Code, rec.Body.String())
	}
	var exportDetail struct {
		Data struct {
			QueueEvents []struct {
				Event string `json:"event"`
				Kind  string `json:"kind"`
				JobID *int64 `json:"job_id"`
			} `json:"queue_events"`
		} `json:"data"`
	}
	decodeBody(t, rec, &exportDetail)
	if len(exportDetail.Data.QueueEvents) == 0 {
		t.Fatal("background export did not record a queue dispatch")
	}
	found := false
	for _, event := range exportDetail.Data.QueueEvents {
		if event.Kind == "user.export" && event.Event == "dispatched" {
			if event.JobID == nil || *event.JobID < 1 {
				t.Fatalf("dispatched user.export event has no job id: %+v", event)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queue events = %+v, want a dispatched user.export", exportDetail.Data.QueueEvents)
	}

	// ── telemetry simulator produces correlated logs and an exception ────────
	rec = apiRequest(t, router, http.MethodPost, "/dev/api/simulate-events",
		`{"logs":2,"level":"error","exception":true}`, devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("simulate-events status = %d body = %s", rec.Code, rec.Body.String())
	}
	var simulated struct {
		Data struct {
			RequestID string `json:"request_id"`
		} `json:"data"`
	}
	decodeBody(t, rec, &simulated)
	if simulated.Data.RequestID == "" {
		t.Fatal("simulate-events did not return a request id")
	}
	time.Sleep(1300 * time.Millisecond) // let the telemetry collector flush
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/logs?limit=10&request_id="+simulated.Data.RequestID, "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("simulated logs status = %d body = %s", rec.Code, rec.Body.String())
	}
	var simulatedLogs struct {
		Data []struct {
			Level string `json:"level"`
		} `json:"data"`
	}
	decodeBody(t, rec, &simulatedLogs)
	if len(simulatedLogs.Data) < 2 {
		t.Fatalf("simulated logs = %+v, want at least 2", simulatedLogs.Data)
	}
	rec = apiRequest(t, router, http.MethodGet,
		"/dev/api/exceptions?limit=10&request_id="+simulated.Data.RequestID, "", devToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("simulated exceptions status = %d body = %s", rec.Code, rec.Body.String())
	}
	var simulatedExceptions struct {
		Data []struct {
			Class string `json:"class"`
		} `json:"data"`
	}
	decodeBody(t, rec, &simulatedExceptions)
	foundException := false
	for _, item := range simulatedExceptions.Data {
		if item.Class == "SimulatedException" {
			foundException = true
			break
		}
	}
	if !foundException {
		t.Fatalf("simulated exceptions = %+v, want SimulatedException", simulatedExceptions.Data)
	}

	// ── per-entity detail endpoints exist and 404 on unknown ids ─────────────
	for _, path := range []string{
		"/dev/api/logs/99999999",
		"/dev/api/exceptions/99999999",
		"/dev/api/queue/jobs/99999999",
	} {
		rec = apiRequest(t, router, http.MethodGet, path, "", devToken, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 (body %s)", path, rec.Code, rec.Body.String())
		}
	}
}
