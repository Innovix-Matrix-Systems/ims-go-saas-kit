// Package factory builds the cache store selected by CACHE_DRIVER.
package factory

import (
	"fmt"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
	cachedb "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/database"
	cachefile "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/file"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/memory"
	cacheredis "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/redis"
)

// New builds the configured cache store. The returned cleanup function
// releases driver resources (Redis connections, janitor goroutines).
func New(cfg config.Config, queries *dbgen.Queries) (cache.Store, func(), error) {
	switch cfg.Cache.Driver {
	case "memory":
		store := memory.NewWithLimit(cfg.Cache.Prefix, cfg.Cache.MemoryMaxEntries)
		return store, store.Close, nil

	case "file":
		store, err := cachefile.New(cfg.Cache.FileDir, cfg.Cache.Prefix)
		if err != nil {
			return nil, nil, fmt.Errorf("cache: file driver: %w", err)
		}
		return store, func() {}, nil

	case "redis":
		store := cacheredis.New(cacheredis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.CacheDB,
			Prefix:   cfg.Cache.Prefix,
		})
		return store, func() { _ = store.Close() }, nil

	case "database":
		if queries == nil {
			return nil, nil, fmt.Errorf("cache: database driver requires a database connection")
		}
		return cachedb.New(queries, cfg.Cache.Prefix), func() {}, nil

	default:
		return nil, nil, fmt.Errorf("cache: unsupported driver %q", cfg.Cache.Driver)
	}
}
