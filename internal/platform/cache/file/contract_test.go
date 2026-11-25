package file

import (
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache/cachetest"
)

func TestContract(t *testing.T) {
	cachetest.RunContract(t, func(t *testing.T) cache.Store {
		store, err := New(t.TempDir(), "contract-")
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		return store
	})
}
