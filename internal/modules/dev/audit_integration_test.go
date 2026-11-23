package dev_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/testkit"
)

type auditRow struct {
	ID          int64          `json:"id"`
	ActorUserID int64          `json:"actor_user_id"`
	Action      string         `json:"action"`
	Status      string         `json:"status"`
	Target      *string        `json:"target"`
	IP          *string        `json:"ip"`
	UserAgent   *string        `json:"user_agent"`
	ActorEmail  *string        `json:"actor_email"`
	Params      map[string]any `json:"params"`
}

type auditPage struct {
	Data []auditRow `json:"data"`
	Meta struct {
		Total int64 `json:"total"`
	} `json:"meta"`
}

func TestAuditLogRecordsSignInsAndFilters(t *testing.T) {
	t.Setenv("DEV_DASHBOARD_ENABLED", "true")
	app := testkit.New(t)
	super := app.As(app.SuperAdmin(t))

	// Dashboard sign-ins (device dev-dashboard) are audited, other logins are not.
	const browser = "Mozilla/5.0 (Windows NT 10.0) Chrome/157.0 Edg/157.0"
	app.Guest().Header("User-Agent", browser).
		Post("/api/v1/login", `{"email":"developer@ims.com","password":"`+testkit.Password+`","device":"dev-dashboard"}`).
		Status(t, http.StatusOK)
	app.Guest().Post("/api/v1/login", `{"email":"developer@ims.com","password":"wrong-password","device":"dev-dashboard"}`).
		Status(t, http.StatusUnauthorized)
	super.Post("/dev/api/settings/sync", `{}`).Status(t, http.StatusOK)

	var page auditPage
	super.Get("/dev/api/actions?action=auth.login").Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total != 2 {
		t.Fatalf("dashboard sign-ins = %d, want 2 (the API login by testkit is not one)", page.Meta.Total)
	}
	var ok, failed *auditRow
	for i := range page.Data {
		if page.Data[i].Status == "ok" {
			ok = &page.Data[i]
		} else {
			failed = &page.Data[i]
		}
	}
	if ok == nil || ok.ActorEmail == nil || *ok.ActorEmail != "developer@ims.com" ||
		ok.UserAgent == nil || *ok.UserAgent != browser || ok.IP == nil || *ok.IP == "" {
		t.Fatalf("successful sign-in row = %+v", ok)
	}
	if failed == nil || failed.ActorUserID != 0 || failed.Target == nil || *failed.Target != "developer@ims.com" {
		t.Fatalf("failed sign-in row = %+v", failed)
	}
	for key, value := range failed.Params {
		if strings.Contains(fmt.Sprint(value), "wrong-password") {
			t.Fatalf("failed sign-in stored the password in %s", key)
		}
	}

	var stats struct {
		LastDay       int64 `json:"last_day"`
		FailedSignIns int64 `json:"failed_sign_ins"`
		RetentionDays int   `json:"retention_days"`
	}
	super.Get("/dev/api/actions/stats").Status(t, http.StatusOK).Data(t, &stats)
	if stats.FailedSignIns != 1 || stats.LastDay < 3 || stats.RetentionDays != 180 {
		t.Fatalf("stats = %+v", stats)
	}

	// Filters: result, person, free text, dates.
	super.Get("/dev/api/actions?status=error").Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total != 1 || page.Data[0].Action != "auth.login" {
		t.Fatalf("status=error = %+v", page)
	}
	super.Get("/dev/api/actions?actor_id=1").Status(t, http.StatusOK).JSON(t, &page)
	for _, row := range page.Data {
		if row.ActorUserID != 1 {
			t.Fatalf("actor filter returned actor %d", row.ActorUserID)
		}
	}
	super.Get("/dev/api/actions?search=superadmin@ims").Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total == 0 {
		t.Fatal("search by e-mail found nothing")
	}
	// A bare date is a UTC day. Building it from local time broke this test
	// between local midnight and UTC midnight (e.g. 00:00-06:00 in UTC+6).
	nowUTC := time.Now().UTC()
	tomorrow := nowUTC.AddDate(0, 0, 1).Format("2006-01-02")
	today := nowUTC.Format("2006-01-02")
	super.Get("/dev/api/actions?from="+tomorrow).Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total != 0 {
		t.Fatalf("from tomorrow = %d rows", page.Meta.Total)
	}
	super.Get("/dev/api/actions?from="+today+"&to="+today).Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total < 3 {
		t.Fatalf("today (UTC) = %d rows, want at least 3", page.Meta.Total)
	}
	// What the dashboard sends: the viewer's calendar day as RFC 3339 bounds.
	// UTC+6 is fixed so the test exercises an offset whatever the machine's zone.
	zone := time.FixedZone("UTC+6", 6*60*60)
	local := time.Now().In(zone)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	dayEnd := dayStart.Add(24*time.Hour - time.Millisecond)
	super.Get("/dev/api/actions?from="+url.QueryEscape(dayStart.Format(time.RFC3339Nano))+
		"&to="+url.QueryEscape(dayEnd.Format(time.RFC3339Nano))).Status(t, http.StatusOK).JSON(t, &page)
	if page.Meta.Total < 3 {
		t.Fatalf("today in UTC+6 = %d rows, want at least 3", page.Meta.Total)
	}
	super.Get("/dev/api/actions?status=maybe").Status(t, http.StatusUnprocessableEntity)

	var facets struct {
		Actions []struct {
			Action string `json:"action"`
		} `json:"actions"`
		Actors []struct {
			UserID int64 `json:"user_id"`
		} `json:"actors"`
	}
	super.Get("/dev/api/actions/facets").Status(t, http.StatusOK).Data(t, &facets)
	if len(facets.Actions) < 2 || len(facets.Actors) == 0 {
		t.Fatalf("facets = %+v", facets)
	}
	var one auditRow
	super.Get(fmt.Sprintf("/dev/api/actions/%d", ok.ID)).Status(t, http.StatusOK).Data(t, &one)
	if one.ID != ok.ID || one.Action != "auth.login" {
		t.Fatalf("get = %+v", one)
	}
}
