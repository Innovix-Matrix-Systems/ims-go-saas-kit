package access

import (
	"context"
	"testing"
)

type fakeTenantLookup struct {
	calls [][]int64
}

func (f *fakeTenantLookup) RefsByIDs(_ context.Context, ids []int64) (map[int64]TenantRef, error) {
	f.calls = append(f.calls, ids)
	out := make(map[int64]TenantRef, len(ids))
	for _, id := range ids {
		if id == 1 {
			out[id] = TenantRef{ID: 1, Name: "Acme", Code: "ACME"}
		}
	}
	return out, nil
}

func TestTenantSlimsBatchesAndDeduplicates(t *testing.T) {
	fake := &fakeTenantLookup{}
	handler := &RoleHandler{tenants: fake}
	tenant := int64(1)

	got := handler.tenantSlims(context.Background(), []Role{
		{ID: 1, TenantID: &tenant},
		{ID: 2, TenantID: &tenant},
		{ID: 3}, // system role: no tenant
	})

	if len(fake.calls) != 1 {
		t.Fatalf("expected one batched lookup, got %d", len(fake.calls))
	}
	if len(fake.calls[0]) != 1 || fake.calls[0][0] != 1 {
		t.Fatalf("lookup ids = %v, want [1]", fake.calls[0])
	}
	if got[1] == nil || got[1].Code != "ACME" {
		t.Errorf("tenant = %+v, want code ACME", got[1])
	}
}

func TestNewRoleResponseEmbedsTenantForTenantRoles(t *testing.T) {
	tenant := int64(1)
	resp := NewRoleResponse(
		Role{ID: 2, TenantID: &tenant, Name: "Admin"},
		&TenantSlim{ID: 1, Name: "Acme", Code: "ACME"},
	)
	if resp.Tenant == nil || resp.Tenant.Code != "ACME" || resp.TenantID == nil {
		t.Errorf("role response = %+v", resp)
	}

	system := NewRoleResponse(Role{ID: 1, Name: "Super-Admin"}, nil)
	if system.Tenant != nil {
		t.Errorf("system role tenant = %+v, want nil", system.Tenant)
	}
}
