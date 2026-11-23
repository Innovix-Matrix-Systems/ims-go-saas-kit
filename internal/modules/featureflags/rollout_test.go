package featureflags

import "testing"

func TestRolloutBucketsAreStableAndSpread(t *testing.T) {
	flag := Flag{Key: "feature.example"}
	percent := int16(25)
	st := state{Value: true, Rollout: &percent}

	on := 0
	for userID := int64(1); userID <= 10000; userID++ {
		first := st.enabledFor(flag, 7, userID)
		if first != st.enabledFor(flag, 99, userID) {
			t.Fatalf("user %d changed bucket with the tenant on a per-user rollout", userID)
		}
		if first {
			on++
		}
	}
	if on < 2300 || on > 2700 {
		t.Fatalf("25%% rollout enabled %d of 10000", on)
	}

	// No subject: a partial rollout cannot be decided, so it stays off.
	if st.enabledFor(flag, 0, 0) {
		t.Fatal("partial rollout enabled without a subject")
	}

	// Per-tenant rollouts put all users of a tenant on the same side.
	byTenant := Flag{Key: "feature.example", RolloutBy: RolloutByTenant}
	for tenantID := int64(1); tenantID <= 50; tenantID++ {
		want := st.enabledFor(byTenant, tenantID, 1)
		for userID := int64(2); userID <= 20; userID++ {
			if st.enabledFor(byTenant, tenantID, userID) != want {
				t.Fatalf("tenant %d split across users", tenantID)
			}
		}
	}
}

func TestNewStateRules(t *testing.T) {
	hundred, zero, bad := 100, 0, 101
	if st, err := newState(true, &hundred); err != nil || st.Rollout != nil {
		t.Fatalf("100%% = %+v, %v; want plain on", st, err)
	}
	if st, err := newState(true, &zero); err != nil || st.Rollout == nil || *st.Rollout != 0 {
		t.Fatalf("0%% = %+v, %v; want staged rollout", st, err)
	}
	if _, err := newState(true, &bad); err == nil {
		t.Fatal("101% accepted")
	}
	if _, err := newState(false, &zero); err == nil {
		t.Fatal("rollout on a disabled flag accepted")
	}
}
