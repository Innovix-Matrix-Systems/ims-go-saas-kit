package app

import (
	"io"
	"log/slog"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

// TestPlatformWiresExchange guards against the identity import/export service
// being constructed but never attached to the platform, which makes every user
// import/export/template endpoint return 503.
func TestPlatformWiresExchange(t *testing.T) {
	t.Setenv("APP_ENV_FILE", "")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	deps, cleanup, err := NewDependencies(cfg, logger)
	if err != nil {
		t.Fatalf("NewDependencies() error = %v", err)
	}
	defer cleanup()

	if deps.Platform == nil {
		t.Fatal("deps.Platform is nil")
	}
	if deps.Platform.Exchange == nil {
		t.Fatal("platform.Exchange is not wired; user import/export endpoints would return 503")
	}
	if deps.Platform.Files == nil {
		t.Fatal("platform.Files is not wired; avatar/file endpoints would return 503")
	}
}
