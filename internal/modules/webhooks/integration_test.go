package webhooks_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/webhooks"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// receiver records the webhooks it gets and answers with status.
type receiver struct {
	*httptest.Server
	mu       sync.Mutex
	requests []received
	status   int
}

type received struct {
	header http.Header
	body   []byte
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{status: http.StatusOK}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.requests = append(r.requests, received{header: req.Header.Clone(), body: body})
		status := r.status
		r.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) all() []received {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]received(nil), r.requests...)
}

// verify checks a request the way a Standard Webhooks library does.
func verify(t *testing.T, secret string, req received) {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(req.header.Get("webhook-id") + "." + req.header.Get("webhook-timestamp") + "." + string(req.body)))
	want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	for _, signature := range strings.Fields(req.header.Get("webhook-signature")) {
		if hmac.Equal([]byte(signature), []byte(want)) {
			return
		}
	}
	t.Fatalf("no valid signature in %q", req.header.Get("webhook-signature"))
}

// grantAPIAccess gives the tenant's plan api.access (webhooks need it).
func grantAPIAccess(t *testing.T, app *testkit.App, tenantID int64) {
	t.Helper()
	if _, err := app.Pool.Exec(context.Background(),
		`UPDATE subscriptions SET overrides = '{"api.access": true}' WHERE tenant_id = $1`, tenantID); err != nil {
		t.Fatalf("grant api.access: %v", err)
	}
}

type endpointAnswer struct {
	ID       int64  `json:"id"`
	Secret   string `json:"secret"`
	IsActive bool   `json:"is_active"`
}

func createEndpoint(t *testing.T, app *testkit.App, admin *testkit.User, url string, events ...string) endpointAnswer {
	t.Helper()
	var endpoint endpointAnswer
	app.As(admin).Post("/api/v1/webhook", map[string]any{"url": url, "events": events, "description": "CRM"}).
		Status(t, http.StatusCreated).Data(t, &endpoint)
	return endpoint
}

// runDeliveries runs every queued webhook.deliver job once.
func runDeliveries(t *testing.T, app *testkit.App) {
	t.Helper()
	definition, ok := app.Deps.Platform.QueueRegistry.Definition(webhooks.KindDeliver)
	if !ok {
		t.Fatal("webhook.deliver is not registered")
	}
	rows, err := app.Pool.Query(context.Background(), `SELECT id, args->'payload' FROM river_job
		WHERE args->>'kind' = $1 AND state IN ('available', 'scheduled') ORDER BY id`, webhooks.KindDeliver)
	if err != nil {
		t.Fatalf("queued jobs: %v", err)
	}
	type job struct {
		id      int64
		payload []byte
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.payload); err != nil {
			t.Fatalf("scan job: %v", err)
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	for _, j := range jobs {
		if _, err := app.Pool.Exec(context.Background(), `UPDATE river_job SET state = 'completed', finalized_at = now() WHERE id = $1`, j.id); err != nil {
			t.Fatalf("finish job: %v", err)
		}
		if err := definition.Handle(context.Background(), j.payload); err != nil {
			t.Fatalf("webhook.deliver: %v", err)
		}
	}
}

func TestWebhookEndToEnd(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	hook := newReceiver(t)

	app.As(admin).Post("/api/v1/webhook", map[string]any{"url": hook.URL, "events": []string{"user.created"}}).
		Status(t, http.StatusPaymentRequired) // no api.access on the plan yet
	grantAPIAccess(t, app, testkit.DefaultTenant)

	app.As(admin).Post("/api/v1/webhook", map[string]any{"url": hook.URL, "events": []string{"branch.created"}}).
		Status(t, http.StatusUnprocessableEntity) // not a webhook event
	endpoint := createEndpoint(t, app, admin, hook.URL, "user.created")
	if !strings.HasPrefix(endpoint.Secret, "whsec_") {
		t.Fatalf("secret = %q", endpoint.Secret)
	}
	var shown map[string]any
	app.As(admin).Get("/api/v1/webhook/"+strconv.FormatInt(endpoint.ID, 10)).Status(t, http.StatusOK).Data(t, &shown)
	if _, leaked := shown["secret"]; leaked {
		t.Fatal("the secret is shown after creation")
	}

	// Creating a user delivers one signed user.created event; creating a
	// branch (not subscribed, not deliverable) delivers nothing.
	created := app.User(t, testkit.Email("new.hire@acme.test"))
	app.As(admin).Post("/api/v1/branch", `{"name":"Comilla"}`).Status(t, http.StatusCreated)
	runDeliveries(t, app)
	got := hook.all()
	if len(got) != 1 {
		t.Fatalf("webhooks received = %d, want 1", len(got))
	}
	verify(t, endpoint.Secret, got[0])
	var payload struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		TenantID int64  `json:"tenant_id"`
		Data     struct {
			SubjectID int64          `json:"subject_id"`
			Changes   map[string]any `json:"changes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got[0].body, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.Type != "user.created" || payload.TenantID != testkit.DefaultTenant || payload.Data.SubjectID != created.ID ||
		payload.Data.Changes["email"] != "new.hire@acme.test" || payload.ID != got[0].header.Get("webhook-id") {
		t.Fatalf("payload = %+v", payload)
	}

	// The delivery log shows it; a redelivery sends the same event id.
	var log []struct {
		ID             int64  `json:"id"`
		EventID        string `json:"event_id"`
		Status         string `json:"status"`
		ResponseStatus *int   `json:"response_status"`
	}
	path := "/api/v1/webhook/" + strconv.FormatInt(endpoint.ID, 10)
	app.As(admin).Get(path+"/deliveries").Status(t, http.StatusOK).Data(t, &log)
	if len(log) != 1 || log[0].Status != webhooks.StatusSucceeded || log[0].ResponseStatus == nil || *log[0].ResponseStatus != 200 {
		t.Fatalf("delivery log = %+v", log)
	}
	app.As(admin).Post(fmt.Sprintf("/api/v1/webhook/deliveries/%d/redeliver", log[0].ID), nil).Status(t, http.StatusOK)
	if again := hook.all(); len(again) != 2 || again[1].header.Get("webhook-id") != log[0].EventID {
		t.Fatalf("redelivery = %d requests", len(again))
	}

	// A test event; then rotation: two signatures for a day, both valid.
	app.As(admin).Post(path+"/test", nil).Status(t, http.StatusOK)
	var rotated endpointAnswer
	app.As(admin).Post(path+"/rotate-secret", nil).Status(t, http.StatusOK).Data(t, &rotated)
	app.As(admin).Post(path+"/test", `{}`).Status(t, http.StatusOK)
	last := hook.all()[len(hook.all())-1]
	if len(strings.Fields(last.header.Get("webhook-signature"))) != 2 {
		t.Fatalf("signatures after a rotation = %q, want two", last.header.Get("webhook-signature"))
	}
	verify(t, rotated.Secret, last)
	verify(t, endpoint.Secret, last)
}

func TestFailingEndpointIsRetriedThenDisabled(t *testing.T) {
	t.Setenv("WEBHOOK_DISABLE_AFTER", "1")
	app := testkit.New(t)
	admin := app.User(t)
	grantAPIAccess(t, app, testkit.DefaultTenant)
	hook := newReceiver(t)
	hook.status = http.StatusInternalServerError
	endpoint := createEndpoint(t, app, admin, hook.URL, "*")

	app.User(t)
	for range 8 { // eight attempts, then abandoned
		runDeliveries(t, app)
	}
	if n := len(hook.all()); n != 8 {
		t.Fatalf("attempts = %d, want 8", n)
	}
	var shown endpointAnswer
	app.As(admin).Get("/api/v1/webhook/"+strconv.FormatInt(endpoint.ID, 10)).Status(t, http.StatusOK).Data(t, &shown)
	if shown.IsActive {
		t.Fatal("the endpoint is still active after an abandoned delivery")
	}
	var status string
	if err := app.Pool.QueryRow(context.Background(), `SELECT status FROM webhook_deliveries WHERE endpoint_id = $1`, endpoint.ID).Scan(&status); err != nil || status != webhooks.StatusAbandoned {
		t.Fatalf("delivery status = %q, %v", status, err)
	}
	if n := countRows(t, app, `SELECT count(*) FROM notifications WHERE event_type = 'webhook.disabled'`); n != 1 {
		t.Fatalf("disabled notices = %d, want 1", n)
	}

	// Turning it back on resets the failure count.
	var back struct {
		IsActive            bool `json:"is_active"`
		ConsecutiveFailures int  `json:"consecutive_failures"`
	}
	app.As(admin).Patch("/api/v1/webhook/"+strconv.FormatInt(endpoint.ID, 10), map[string]any{"is_active": true}).
		Status(t, http.StatusOK).Data(t, &back)
	if !back.IsActive || back.ConsecutiveFailures != 0 {
		t.Fatalf("re-enabled endpoint = %+v", back)
	}
}

func TestWebhooksStayInTheirTenant(t *testing.T) {
	app := testkit.New(t)
	adminA := app.User(t)
	tenantB := app.Tenant(t, "Tenant B")
	adminB := app.User(t, testkit.InTenant(tenantB))
	grantAPIAccess(t, app, testkit.DefaultTenant)
	hook := newReceiver(t)
	endpoint := createEndpoint(t, app, adminA, hook.URL, "user.created")

	app.As(adminB).Get("/api/v1/webhook/"+strconv.FormatInt(endpoint.ID, 10)).Status(t, http.StatusNotFound)
	app.As(adminB).Delete("/api/v1/webhook/"+strconv.FormatInt(endpoint.ID, 10)).Status(t, http.StatusNotFound)
	var list []endpointAnswer
	app.As(adminB).Get("/api/v1/webhook").Status(t, http.StatusOK).Data(t, &list)
	if len(list) != 0 {
		t.Fatalf("tenant B sees %d endpoints", len(list))
	}
	app.User(t, testkit.InTenant(tenantB)) // a user.created in tenant B
	runDeliveries(t, app)
	if n := len(hook.all()); n != 0 {
		t.Fatalf("tenant A's endpoint got %d of tenant B's events", n)
	}
}

func countRows(t *testing.T, app *testkit.App, query string, args ...any) int {
	t.Helper()
	var n int
	if err := app.Pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
