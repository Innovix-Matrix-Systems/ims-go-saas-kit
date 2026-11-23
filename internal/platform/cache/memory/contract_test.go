package memory

import (
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/cachetest"
)

func TestContract(t *testing.T) {
	cachetest.RunContract(t, func(t *testing.T) cache.Store {
		store := New("contract-")
		t.Cleanup(store.Close)
		return store
	})
}
