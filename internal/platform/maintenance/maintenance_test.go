package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
)

type fakeStore struct {
	mu    sync.Mutex
	state State
	gets  int
	err   error
}

func (f *fakeStore) Get(context.Context) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	return f.state, f.err
}

func (f *fakeStore) Down(_ context.Context, message string, retry int, hash string, by int64) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	f.state = State{Down: true, Since: &now, Message: message, RetryAfter: retry, HasSecret: hash != "", secretHash: hash}
	if by > 0 {
		f.state.ByUserID = &by
	}
	return f.state, nil
}

func (f *fakeStore) Up(context.Context, int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	was := f.state.Down
	f.state = State{}
	return was, nil
}

type fakeAudit struct{ events []Event }

func (f *fakeAudit) Maintenance(_ context.Context, event Event) { f.events = append(f.events, event) }

func newService(store *fakeStore) *Service {
	return NewService(store, NewChecker(store, time.Hour, nil))
}

func TestDownValidatesAndDefaults(t *testing.T) {
	tests := []struct {
		name      string
		in        DownInput
		wantField string
	}{
		{name: "defaults", in: DownInput{}},
		{name: "retry too large", in: DownInput{RetryAfter: MaxRetryAfter + 1}, wantField: "retry_after"},
		{name: "negative retry", in: DownInput{RetryAfter: -1}, wantField: "retry_after"},
		{name: "short secret", in: DownInput{Secret: "short"}, wantField: "secret"},
		{name: "secret and generate", in: DownInput{Secret: "long-enough", GenerateSecret: true}, wantField: "secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, _, err := newService(&fakeStore{}).Down(context.Background(), tt.in)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Down() error = %v", err)
				}
				if state.Message != DefaultMessage || state.RetryAfter != DefaultRetryAfter {
					t.Fatalf("state = %+v, want defaults", state)
				}
				return
			}
			var appErr *apperr.Error
			if !errors.As(err, &appErr) || appErr.Fields[tt.wantField] == nil {
				t.Fatalf("Down() error = %v, want a validation error on %q", err, tt.wantField)
			}
		})
	}
}

func TestDownUpUpdatesCheckerAndRecords(t *testing.T) {
	store := &fakeStore{}
	audit := &fakeAudit{}
	service := newService(store).WithAudit(audit)
	ctx := context.Background()

	if service.Checker().IsDown(ctx) {
		t.Fatal("IsDown() before down = true")
	}
	_, secret, err := service.Down(ctx, DownInput{Message: "Upgrading", RetryAfter: 120, GenerateSecret: true})
	if err != nil {
		t.Fatalf("Down() error = %v", err)
	}
	if len(secret) != 32 {
		t.Fatalf("generated secret = %q, want 32 hex characters", secret)
	}
	// The checker's interval is an hour: only the direct update can make it see the change.
	state := service.Checker().Current(ctx)
	if !state.Down || state.Message != "Upgrading" || !state.Allows(secret) || state.Allows("wrong-secret") {
		t.Fatalf("checker state after down = %+v", state)
	}

	changed, err := service.Up(ctx, 0)
	if err != nil || !changed {
		t.Fatalf("Up() = %v, %v; want true, nil", changed, err)
	}
	if service.Checker().IsDown(ctx) {
		t.Fatal("IsDown() after up = true")
	}
	if changed, _ := service.Up(ctx, 0); changed {
		t.Fatal("second Up() = true, want false")
	}
	if len(audit.events) != 2 || !audit.events[0].Down || audit.events[1].Down {
		t.Fatalf("audited events = %+v, want [down up] (a no-op up records nothing)", audit.events)
	}
	if down := audit.events[0]; down.Message != "Upgrading" || down.RetryAfter != 120 || !down.WithBypass {
		t.Errorf("down event = %+v", down)
	}
	if up := audit.events[1]; up.DownSince == nil {
		t.Errorf("up event = %+v, want the down period's start", up)
	}
}

func TestCheckerRefreshesAfterInterval(t *testing.T) {
	store := &fakeStore{}
	checker := NewChecker(store, 5*time.Second, nil)
	now := time.Now()
	checker.now = func() time.Time { return now }
	ctx := context.Background()

	if checker.IsDown(ctx) {
		t.Fatal("IsDown() = true on an up store")
	}
	// Another process takes the app down.
	_, _ = store.Down(ctx, "x", 60, "", 0)
	if checker.IsDown(ctx) {
		t.Fatal("IsDown() = true within the interval, want the cached up state")
	}
	now = now.Add(6 * time.Second)
	if !checker.IsDown(ctx) {
		t.Fatal("IsDown() = false after the interval, want the refreshed down state")
	}
	if store.gets != 2 {
		t.Fatalf("store reads = %d, want 2", store.gets)
	}

	// A failing read keeps the last known state.
	store.err = errors.New("db gone")
	now = now.Add(6 * time.Second)
	if !checker.IsDown(ctx) {
		t.Fatal("IsDown() = false after a failed read, want the last known down state")
	}
}

func TestGuard(t *testing.T) {
	responder := httpx.NewResponder("http://localhost", "local", false)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	store := &fakeStore{}
	service := newService(store)
	handler := Guard(service.Checker(), responder, OpenPaths...)(ok)

	serve := func(path, bypass string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if bypass != "" {
			req.Header.Set(BypassHeader, bypass)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve("/api/v1/branch", ""); rec.Code != http.StatusOK {
		t.Fatalf("up: status = %d, want 200", rec.Code)
	}

	const secret = "let-me-in-please"
	if _, _, err := service.Down(context.Background(), DownInput{Message: "Upgrading", RetryAfter: 120, Secret: secret}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path, bypass string
		want         int
	}{
		{"/api/v1/branch", "", http.StatusServiceUnavailable},
		{"/mcp", "", http.StatusServiceUnavailable},
		{"/oauth/authorize", "", http.StatusServiceUnavailable},
		{"/api/v1/branch", "wrong-secret", http.StatusServiceUnavailable},
		{"/api/v1/branch", secret, http.StatusOK},
		{"/up", "", http.StatusOK},
		{"/api/health", "", http.StatusOK},
		{"/metrics", "", http.StatusOK},
		{"/dev", "", http.StatusOK},
		{"/dev/api/maintenance", "", http.StatusOK},
		{"/docs", "", http.StatusOK},
		{"/docs/openapi.yaml", "", http.StatusOK},
		{"/developer", "", http.StatusServiceUnavailable}, // "/dev/*" is a path prefix, not a string prefix
	}
	for _, tt := range tests {
		rec := serve(tt.path, tt.bypass)
		if rec.Code != tt.want {
			t.Errorf("down %s (bypass %q): status = %d, want %d", tt.path, tt.bypass, rec.Code, tt.want)
		}
	}

	rec := serve("/api/v1/branch", "")
	if got := rec.Header().Get("Retry-After"); got != "120" {
		t.Errorf("Retry-After = %q, want 120", got)
	}
	var problem struct {
		Status     int    `json:"status"`
		ErrorCode  string `json:"error_code"`
		Detail     string `json:"detail"`
		RetryAfter int    `json:"retry_after"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v (%s)", err, rec.Body.String())
	}
	if problem.Status != 503 || problem.ErrorCode != "MAINTENANCE_MODE" || problem.Detail != "Upgrading" || problem.RetryAfter != 120 {
		t.Errorf("problem = %+v", problem)
	}
}
