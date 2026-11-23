package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// fakeAuth verifies every token as one identity.
type fakeAuth struct{ identity authn.Identity }

func (f fakeAuth) Issue(context.Context, int64, string) (authn.Token, error) {
	return authn.Token{}, nil
}
func (f fakeAuth) Verify(context.Context, string) (*authn.Identity, error) {
	identity := f.identity
	return &identity, nil
}
func (f fakeAuth) Revoke(context.Context, int64, string) error      { return nil }
func (f fakeAuth) RevokeAll(context.Context, int64) error           { return nil }
func (f fakeAuth) RevokeOthers(context.Context, int64, int64) error { return nil }

func (f fakeAuth) IssueSetup(context.Context, int64, string) (authn.Token, error) {
	return authn.Token{}, nil
}

func captureOne(t *testing.T, inner http.Handler) RequestEntry {
	t.Helper()
	sink := &recordingSink{flushed: make(chan struct{}, 4)}
	collector := NewCollector(CaptureConfig{
		Enabled: true, Capture: config.DevCapture{HTTP: true}, SampleRate: 1,
	}, NewRedactor(nil), sink, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/branch", nil)
	req.Header.Set("Authorization", "Bearer any")
	collector.HTTPMiddleware(inner).ServeHTTP(httptest.NewRecorder(), req)
	collector.Close()

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.entries) != 1 {
		t.Fatalf("captured %d requests, want 1", len(sink.entries))
	}
	return sink.entries[0]
}

func value(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// The identity and scope are added by layers inside the telemetry
// middleware. Context values never travel back up, so telemetry must read
// them from the shared httpx.RequestInfo holder (regression: user_id and
// tenant_id were always empty).
func TestCapturedRequestKnowsTheUserAndTenant(t *testing.T) {
	responder := httpx.NewResponder("http://localhost", "testing", false)
	scope := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpx.NoteScope(r.Context(), 7, 12) // what tenancy.Resolve does
			next.ServeHTTP(w, r)
		})
	}
	handler := authn.Authenticate(fakeAuth{authn.Identity{UserID: 42, IsActive: true}}, responder)(
		scope(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })))

	entry := captureOne(t, handler)
	if value(entry.UserID) != 42 || value(entry.TenantID) != 7 || value(entry.BranchID) != 12 || entry.ImpersonatorID != nil {
		t.Fatalf("entry user=%d tenant=%d branch=%d impersonator=%v; want 42/7/12/nil",
			value(entry.UserID), value(entry.TenantID), value(entry.BranchID), entry.ImpersonatorID)
	}
}

// Acting as a user: the entry is the target, on behalf of the caller.
func TestCapturedRequestKnowsTheImpersonator(t *testing.T) {
	responder := httpx.NewResponder("http://localhost", "testing", false)
	actAs := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpx.NoteUser(r.Context(), 9, 42) // what tenancy does for X-Acting-User
			httpx.NoteScope(r.Context(), 3, 5)
			next.ServeHTTP(w, r)
		})
	}
	handler := authn.Authenticate(fakeAuth{authn.Identity{UserID: 42, IsActive: true}}, responder)(
		actAs(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })))

	entry := captureOne(t, handler)
	if value(entry.UserID) != 9 || value(entry.ImpersonatorID) != 42 || value(entry.TenantID) != 3 {
		t.Fatalf("entry user=%d impersonator=%d tenant=%d; want 9/42/3",
			value(entry.UserID), value(entry.ImpersonatorID), value(entry.TenantID))
	}
}

// An unauthenticated request still records nothing about a user.
func TestCapturedGuestRequestHasNoUser(t *testing.T) {
	entry := captureOne(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	if entry.UserID != nil || entry.TenantID != nil || entry.ImpersonatorID != nil {
		t.Fatalf("guest entry = %+v; want no user, tenant or impersonator", entry)
	}
}
