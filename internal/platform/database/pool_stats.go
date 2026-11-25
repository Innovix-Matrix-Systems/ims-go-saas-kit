package database

import "time"

// PoolBusyShare is the share of a pool's connections in use at which the
// health check warns: the pool is close to making callers wait.
const PoolBusyShare = 0.8

// PoolStats is one connection pool's state, read from memory (no query).
// The developer dashboard shows it; the public health report never does.
type PoolStats struct {
	Name string `json:"name"`
	// Active is connections in use right now; Idle are open and free;
	// Constructing are being opened; Total is all three.
	Active       int32 `json:"active"`
	Idle         int32 `json:"idle"`
	Constructing int32 `json:"constructing"`
	Total        int32 `json:"total"`
	Max          int32 `json:"max"`
	// Acquires counts connections handed out since start; Waited is how many
	// of them found the pool empty and had to wait, and Canceled how many
	// gave up (context canceled) while waiting.
	Acquires int64 `json:"acquires"`
	Waited   int64 `json:"waited"`
	Canceled int64 `json:"canceled"`
	// AvgAcquireMS is the mean time to get a connection, waits included.
	AvgAcquireMS float64 `json:"avg_acquire_ms"`
	// Busy is true at PoolBusyShare of Max or more.
	Busy bool `json:"busy"`
}

// PoolStats returns every pool's state in configuration order.
func (r *Registry) PoolStats() []PoolStats {
	out := make([]PoolStats, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.conns[name].PoolStats())
	}
	return out
}

// PoolStats returns this connection's pool state.
func (c *Conn) PoolStats() PoolStats {
	stat := c.Pool.Stat()
	stats := PoolStats{
		Name:         c.Name,
		Active:       stat.AcquiredConns(),
		Idle:         stat.IdleConns(),
		Constructing: stat.ConstructingConns(),
		Total:        stat.TotalConns(),
		Max:          stat.MaxConns(),
		Acquires:     stat.AcquireCount(),
		Waited:       stat.EmptyAcquireCount(),
		Canceled:     stat.CanceledAcquireCount(),
	}
	if stats.Acquires > 0 {
		stats.AvgAcquireMS = float64(stat.AcquireDuration().Nanoseconds()) / float64(stats.Acquires) / float64(time.Millisecond)
	}
	stats.Busy = poolBusy(stats.Active, stats.Max)
	return stats
}

func poolBusy(active, max int32) bool {
	return max > 0 && float64(active) >= PoolBusyShare*float64(max)
}
