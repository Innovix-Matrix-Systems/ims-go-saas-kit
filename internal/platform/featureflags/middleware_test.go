package featureflags

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

type fakeResolver struct {
	enabled bool
	err     error
}

func (f fakeResolver) IsEnabled(context.Context, tenancy.Scope, int64, string) (bool, error) {
	return f.enabled, f.err
}

func TestRequire(t *testing.T) {
	responder := httpx.NewResponder("https://api.test", "local", false)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	authedRequest := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		ctx := authn.WithIdentity(r.Context(), &authn.Identity{UserID: 1})
		ctx = tenancy.WithScope(ctx, tenancy.Scope{TenantID: 1, BranchID: 1})
		return r.WithContext(ctx)
	}

	t.Run("enabled passes through", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Require(fakeResolver{enabled: true}, "feature.audit", responder)(next).ServeHTTP(rec, authedRequest())
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
	})

	t.Run("disabled returns 403 with the flag key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Require(fakeResolver{enabled: false}, "feature.audit", responder)(next).ServeHTTP(rec, authedRequest())
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		var problem struct {
			Meta map[string]any `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
			t.Fatalf("decode problem: %v", err)
		}
		if problem.Meta["feature_flag"] != "feature.audit" {
			t.Fatalf("meta = %+v, want feature_flag=feature.audit", problem.Meta)
		}
	})

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		Require(fakeResolver{enabled: true}, "feature.audit", responder)(next).ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}
