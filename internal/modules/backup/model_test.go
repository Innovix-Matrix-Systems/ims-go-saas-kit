package backup

import (
	"encoding/json"
	"testing"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
)

// A run that failed before copying anywhere must still serialize its
// destinations as [] — the dashboard reads .length on it.
func TestRunDestinationsNeverNull(t *testing.T) {
	for _, stored := range []string{"null", "[]", ""} {
		run, err := mapRun(dbgen.BackupRun{Destinations: []byte(stored)})
		if err != nil {
			t.Fatalf("mapRun(%q) error = %v", stored, err)
		}
		out, _ := json.Marshal(run.Destinations)
		if string(out) != "[]" {
			t.Errorf("mapRun(%q) destinations = %s, want []", stored, out)
		}
	}

	payload, err := encodeDestinations(nil)
	if err != nil || string(payload) != "[]" {
		t.Errorf("encodeDestinations(nil) = %s, %v; want []", payload, err)
	}
}
