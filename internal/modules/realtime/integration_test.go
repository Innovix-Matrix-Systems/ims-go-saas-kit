package realtime_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/announcements"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/notifications"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/realtime"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/pubsub"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// live starts the app's listener and an HTTP server, as `ims serve` does.
func live(t *testing.T, app *testkit.App) *httptest.Server {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		app.Deps.RealtimeListener.Run(ctx)
		close(done)
	}()
	server := httptest.NewServer(app.Router)
	t.Cleanup(func() {
		cancel()
		<-done // the listener closes the hub, which ends the open streams
		server.Close()
	})
	return server
}

// stream opens a live-update stream and returns the event names it receives.
func stream(t *testing.T, server *httptest.Server, token string) <-chan string {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+realtime.StreamPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "text/event-stream")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	events := make(chan string, 16)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			if name, ok := strings.CutPrefix(scanner.Text(), "event: "); ok {
				events <- name
			}
		}
	}()
	expect(t, events, "ready")
	return events
}

func expect(t *testing.T, events <-chan string, want string) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatalf("event = %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no %q event within 5s", want)
	}
}

// waitListening publishes probes until the listener has subscribed, so the
// test does not race the LISTEN.
func waitListening(t *testing.T, app *testkit.App, userID int64, events <-chan string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := app.Deps.Platform.Realtime.Publish(context.Background(), pubsub.ToUsers("probe", userID)); err != nil {
			t.Fatalf("publish probe: %v", err)
		}
		select {
		case got := <-events:
			if got != "probe" {
				t.Fatalf("event = %q while waiting for the listener", got)
			}
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("the listener never delivered a probe")
}

// The stream reaches the people a change is for and nobody else: a tenant's
// notification nudges its members, not another tenant's; an announcement
// nudges its audience when it goes live and when the scheduler sees it start.
func TestStreamDeliversOnlyToTheAudience(t *testing.T) {
	app := testkit.New(t)
	server := live(t, app)
	otherTenant := app.Tenant(t, "Other Org")
	member := app.User(t, testkit.Role("Admin"))
	outsider := app.User(t, testkit.Role("Admin"), testkit.InTenant(otherTenant))

	memberEvents := stream(t, server, member.Token)
	outsiderEvents := stream(t, server, outsider.Token)
	waitListening(t, app, member.ID, memberEvents)
	waitListening(t, app, outsider.ID, outsiderEvents)
	ctx := context.Background()

	// A tenant-wide notification: the fan-out (run here as the worker
	// would) tells the addressed members once their counts are in.
	service := app.Deps.Platform.Notifications
	notification, _, err := service.Create(ctx, tenancy.Scope{TenantID: testkit.DefaultTenant}, 0, notifications.CreateInput{
		EventType: notifications.EventRenewalReminder,
		Priority:  notifications.PriorityInfo,
		Title:     "Live update",
		Targets:   []notifications.TargetInput{{Type: notifications.TargetTenant}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := service.ProcessFanOut(ctx, notification.ID); err != nil {
		t.Fatalf("ProcessFanOut() error = %v", err)
	}
	expect(t, memberEvents, notifications.RealtimeKind)

	// Reading it tells the reader's other tabs.
	if err := service.MarkRead(ctx, tenancy.Scope{TenantID: testkit.DefaultTenant}, member.ID, notification.ID); err != nil {
		t.Fatalf("MarkRead() error = %v", err)
	}
	expect(t, memberEvents, notifications.RealtimeKind)

	// An announcement live now for the member's organization only.
	staff := app.SuperAdmin(t)
	started := time.Now().Add(-time.Minute)
	if _, err := app.Deps.Platform.Announcements.Create(ctx, staff.ID, announcements.Input{
		Title: "Maintenance tonight", Kind: announcements.KindInfo, TenantIDs: []int64{testkit.DefaultTenant},
		StartsAt: &started, Dismissible: true, Status: announcements.StatusPublished,
	}); err != nil {
		t.Fatalf("Announcements.Create() error = %v", err)
	}
	expect(t, memberEvents, announcements.RealtimeKind)

	// A scheduled one is told when the scheduler sees its window open.
	later := time.Now().Add(time.Hour)
	if _, err := app.Deps.Platform.Announcements.Create(ctx, staff.ID, announcements.Input{
		Title: "New feature", Kind: announcements.KindInfo, StartsAt: &later, Dismissible: true, Status: announcements.StatusPublished,
	}); err != nil {
		t.Fatalf("Announcements.Create(scheduled) error = %v", err)
	}
	changed, err := app.Deps.Platform.Announcements.PublishTransitions(ctx, later.Add(-time.Minute), later.Add(time.Minute))
	if err != nil || changed != 1 {
		t.Fatalf("PublishTransitions() = %d, %v; want 1", changed, err)
	}
	expect(t, memberEvents, announcements.RealtimeKind)
	expect(t, outsiderEvents, announcements.RealtimeKind) // every organization

	// The outsider heard nothing else: the next event it gets is this probe.
	waitListening(t, app, outsider.ID, outsiderEvents)
}

func TestStreamNeedsSignInAndCanBeTurnedOff(t *testing.T) {
	app := testkit.New(t)
	app.Guest().Get(realtime.StreamPath).Status(t, http.StatusUnauthorized)

	t.Setenv("REALTIME_ENABLED", "false")
	off := testkit.New(t)
	member := off.User(t, testkit.Role("Admin"))
	off.As(member).Get(realtime.StreamPath).Status(t, http.StatusNotFound)
}

func TestStreamLimitsStreamsPerUser(t *testing.T) {
	t.Setenv("REALTIME_MAX_STREAMS_PER_USER", "1")
	app := testkit.New(t)
	server := live(t, app)
	member := app.User(t, testkit.Role("Admin"))
	stream(t, server, member.Token)

	request, _ := http.NewRequest(http.MethodGet, server.URL+realtime.StreamPath, nil)
	request.Header.Set("Authorization", "Bearer "+member.Token)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second stream status = %d, want 429", response.StatusCode)
	}
}
