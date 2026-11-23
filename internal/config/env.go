package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Reading values from the environment.

var allowedEnvs = map[string]bool{
	EnvLocal:       true,
	EnvDevelopment: true,
	EnvDev:         true,
	EnvStaging:     true,
	EnvProduction:  true,
}

// normalizeEnv trims, lowercases and validates an APP_ENV value.
func normalizeEnv(raw string) (string, error) {
	env := strings.ToLower(strings.TrimSpace(raw))
	if !allowedEnvs[env] {
		return "", fmt.Errorf("APP_ENV %q is not supported (allowed: local, development, dev, staging, production)", raw)
	}
	return env, nil
}

func resolveEnvFile(explicit string) (string, error) {
	path := explicit
	required := path != ""
	if path == "" {
		path = os.Getenv("APP_ENV_FILE")
		required = path != ""
	}
	if path == "" {
		path = ".env"
	}
	if _, err := os.Stat(path); err != nil {
		if required {
			return "", fmt.Errorf("env file %q: %w", path, err)
		}
		return "", nil
	}
	return path, nil
}

func getString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getBool(key string, def bool) (bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("%s: invalid boolean %q", key, raw)
	}
	return v, nil
}

func getInt(key string, def int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, raw)
	}
	return v, nil
}

func getInt64(key string, def int64) (int64, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, raw)
	}
	return v, nil
}

func getDuration(key string, def time.Duration) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	raw = strings.TrimSpace(raw)
	if d, err := time.ParseDuration(raw); err == nil {
		return d, nil
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		return time.Duration(secs) * time.Second, nil
	}
	return 0, fmt.Errorf("%s: invalid duration %q", key, raw)
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func containsFold(list []string, needle string) bool {
	for _, item := range list {
		if strings.EqualFold(item, needle) {
			return true
		}
	}
	return false
}
