package tenancy

import (
	"context"
	"testing"
)

func TestScopeEffectiveIDs(t *testing.T) {
	identity := Scope{TenantID: 5, BranchID: 6}

	if identity.EffectiveTenantID() != 5 || identity.EffectiveBranchID() != 6 {
		t.Fatalf("identity scope = %d, %d", identity.EffectiveTenantID(), identity.EffectiveBranchID())
	}

	global := Scope{TenantID: 5, BranchID: 6, Acting: ActingGlobal}
	if global.EffectiveTenantID() != 0 || global.EffectiveBranchID() != 0 {
		t.Fatalf("global acting scope = %d, %d", global.EffectiveTenantID(), global.EffectiveBranchID())
	}

	actingTenant := Scope{TenantID: 5, BranchID: 6, Acting: ActingTenant, ActingTenantID: 9}
	if actingTenant.EffectiveTenantID() != 9 || actingTenant.EffectiveBranchID() != 0 {
		t.Fatalf("acting tenant scope = %d, %d", actingTenant.EffectiveTenantID(), actingTenant.EffectiveBranchID())
	}

	actingBranch := Scope{TenantID: 5, BranchID: 6, Acting: ActingBranch, ActingTenantID: 9, ActingBranchID: 10}
	if actingBranch.EffectiveTenantID() != 9 || actingBranch.EffectiveBranchID() != 10 {
		t.Fatalf("acting branch scope = %d, %d", actingBranch.EffectiveTenantID(), actingBranch.EffectiveBranchID())
	}
	if !actingBranch.IsImpersonating() {
		t.Fatal("acting branch scope should be impersonating")
	}
}

func TestScopeContextRoundTrip(t *testing.T) {
	scope := Scope{TenantID: 7, BranchID: 8, Internal: true}

	ctx := WithScope(context.Background(), scope)
	got, ok := FromContext(ctx)
	if !ok || got != scope {
		t.Fatalf("FromContext() = %+v, %v", got, ok)
	}

	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("FromContext() on bare context = present, want absent")
	}
}

func TestScopeAllTenants(t *testing.T) {
	cases := []struct {
		name  string
		scope Scope
		want  bool
	}{
		{"internal", Scope{TenantID: 5, Internal: true}, true},
		{"global acting read", Scope{TenantID: 5, Acting: ActingGlobal}, true},
		{"identity", Scope{TenantID: 5}, false},
		{"acting tenant", Scope{TenantID: 5, Acting: ActingTenant, ActingTenantID: 9}, false},
		{"acting branch", Scope{TenantID: 5, Acting: ActingBranch, ActingTenantID: 9, ActingBranchID: 10}, false},
	}
	for _, tc := range cases {
		if got := tc.scope.AllTenants(); got != tc.want {
			t.Errorf("%s: AllTenants() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
