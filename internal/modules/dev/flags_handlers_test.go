package dev

import (
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/featureflags"
)

func TestParseFlagTarget(t *testing.T) {
	tests := []struct {
		name    string
		rawType string
		rawID   string
		want    featureflags.Target
		wantErr bool
	}{
		{name: "empty is global", want: featureflags.Target{Type: featureflags.TargetGlobal}},
		{name: "explicit global", rawType: "global", want: featureflags.Target{Type: featureflags.TargetGlobal}},
		{name: "tenant", rawType: "tenant", rawID: "5", want: featureflags.Target{Type: featureflags.TargetTenant, ID: 5}},
		{name: "user", rawType: "USER", rawID: "7", want: featureflags.Target{Type: featureflags.TargetUser, ID: 7}},
		{name: "unsupported type", rawType: "branch", rawID: "1", wantErr: true},
		{name: "global with id", rawType: "global", rawID: "1", wantErr: true},
		{name: "tenant without id", rawType: "tenant", wantErr: true},
		{name: "negative id", rawType: "user", rawID: "-1", wantErr: true},
		{name: "non numeric id", rawType: "user", rawID: "abc", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseFlagTarget(test.rawType, test.rawID)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseFlagTarget(%q, %q) = %+v, want error", test.rawType, test.rawID, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFlagTarget(%q, %q) error = %v", test.rawType, test.rawID, err)
			}
			if got != test.want {
				t.Fatalf("parseFlagTarget(%q, %q) = %+v, want %+v", test.rawType, test.rawID, got, test.want)
			}
		})
	}
}

func TestToDevFlagsMarksTargetOverrides(t *testing.T) {
	items := []featureflags.EffectiveFlag{
		{
			Flag:   featureflags.Flag{Key: "feature.audit", Default: false, Description: "audit", Group: "features"},
			Value:  true,
			Source: "tenant",
		},
		{
			Flag:   featureflags.Flag{Key: "feature.data_processing", Default: true, Description: "processing", Group: "features"},
			Value:  true,
			Source: "default",
		},
	}

	tenant := toDevFlags(items, featureflags.Target{Type: featureflags.TargetTenant, ID: 3})
	if !tenant[0].Overridden || tenant[1].Overridden {
		t.Fatalf("tenant overrides = %+v", tenant)
	}

	global := toDevFlags(items, featureflags.Target{Type: featureflags.TargetGlobal})
	if global[0].Overridden || global[1].Overridden {
		t.Fatalf("global overrides = %+v", global)
	}

	globalItems := []featureflags.EffectiveFlag{
		{Flag: featureflags.Flag{Key: "feature.audit"}, Value: true, Source: "global"},
	}
	if !toDevFlags(globalItems, featureflags.Target{Type: featureflags.TargetGlobal})[0].Overridden {
		t.Fatalf("global source must mark the global target as overridden")
	}

	// An explicitly stored value is still an override when it equals the code
	// default: the row exists and must stay clearable.
	pinned := []featureflags.EffectiveFlag{
		{
			Flag:   featureflags.Flag{Key: "feature.data_processing", Default: true},
			Value:  true,
			Source: "user",
		},
	}
	if !toDevFlags(pinned, featureflags.Target{Type: featureflags.TargetUser, ID: 7})[0].Overridden {
		t.Fatalf("stored user value equal to the default must still be marked overridden")
	}
}
