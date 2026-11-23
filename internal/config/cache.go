package config

import "path/filepath"

// Cache and Redis settings (CACHE_*, REDIS_*).

// Cache holds the cache driver settings.
type Cache struct {
	Driver  string
	Prefix  string
	FileDir string
	// MemoryMaxEntries bounds the memory driver (CACHE_MEMORY_MAX_ENTRIES).
	MemoryMaxEntries int
}

// Redis holds the Redis client settings (cache driver, queue-adjacent use).
type Redis struct {
	Addr     string
	Password string
	DB       int
	CacheDB  int
}

func (c *Config) loadCache() error {
	// The memory driver is per-process: fine for local development, wrong for
	// any deployment that runs more than one process (API + worker +
	// scheduler share permission/tenancy caches and rate-limit counters).
	// Outside local the default is the shared database driver.
	defaultDriver := "database"
	if c.IsLocal() {
		defaultDriver = "memory"
	}
	maxEntries, err := getInt("CACHE_MEMORY_MAX_ENTRIES", 100000)
	if err != nil {
		return err
	}
	c.Cache = Cache{
		Driver:           getString("CACHE_DRIVER", defaultDriver),
		Prefix:           getString("CACHE_PREFIX", "ims-"),
		FileDir:          getString("CACHE_FILE_DIR", ""),
		MemoryMaxEntries: maxEntries,
	}
	if c.Cache.FileDir == "" {
		c.Cache.FileDir = filepath.Join(c.App.CacheDir, "data")
	}
	return nil
}

func (c *Config) loadRedis() error {
	db, err := getInt("REDIS_DB", 0)
	if err != nil {
		return err
	}
	cacheDB, err := getInt("REDIS_CACHE_DB", 1)
	if err != nil {
		return err
	}
	c.Redis = Redis{
		Addr:     getString("REDIS_ADDR", "127.0.0.1:6379"),
		Password: getString("REDIS_PASSWORD", ""),
		DB:       db,
		CacheDB:  cacheDB,
	}
	return nil
}
