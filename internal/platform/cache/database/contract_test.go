package database

import (
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/cachetest"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database/dbtest"
)

func TestContract(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool, "cache")

	cachetest.RunContract(t, func(t *testing.T) cache.Store {
		return New(queries, "contract-")
	})
}
