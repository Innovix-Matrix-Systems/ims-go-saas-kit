package config

import (
	"errors"
	"time"
)

// Realtime holds the live-update stream settings (GET /api/v1/realtime/stream
// and the Postgres LISTEN/NOTIFY bus behind it; docs/guides/realtime.md).
type Realtime struct {
	// Enabled turns the stream and the publishing on (REALTIME_ENABLED,
	// default true). Off, the endpoint answers 404 and clients keep polling.
	Enabled bool
	// Heartbeat is how often an idle stream sends a comment so proxies keep
	// it open (REALTIME_HEARTBEAT, default 25s).
	Heartbeat time.Duration
	// MaxStream is how long one stream lasts before the server asks the
	// client to reconnect, which re-checks its token (REALTIME_MAX_STREAM,
	// default 30m).
	MaxStream time.Duration
	// MaxStreamsPerUser bounds open streams per user on one server process
	// (REALTIME_MAX_STREAMS_PER_USER, default 5).
	MaxStreamsPerUser int
	// ListenDSN is the connection that LISTENs for events
	// (REALTIME_LISTEN_DSN, default DATABASE_DSN). It must be a real session:
	// LISTEN does not work through PgBouncer in transaction mode.
	ListenDSN string
}

func (c *Config) loadRealtime() error {
	enabled, err := getBool("REALTIME_ENABLED", true)
	if err != nil {
		return err
	}
	heartbeat, err := getDuration("REALTIME_HEARTBEAT", 25*time.Second)
	if err != nil {
		return err
	}
	if heartbeat < time.Second {
		return errors.New("REALTIME_HEARTBEAT must be at least 1s")
	}
	maxStream, err := getDuration("REALTIME_MAX_STREAM", 30*time.Minute)
	if err != nil {
		return err
	}
	if maxStream < heartbeat {
		return errors.New("REALTIME_MAX_STREAM must be longer than REALTIME_HEARTBEAT")
	}
	perUser, err := getInt("REALTIME_MAX_STREAMS_PER_USER", 5)
	if err != nil {
		return err
	}
	if perUser < 1 {
		return errors.New("REALTIME_MAX_STREAMS_PER_USER must be at least 1")
	}
	listenDSN := getString("REALTIME_LISTEN_DSN", "")
	if listenDSN == "" {
		listenDSN = c.Database.Primary.DSN
	}
	c.Realtime = Realtime{
		Enabled: enabled, Heartbeat: heartbeat, MaxStream: maxStream,
		MaxStreamsPerUser: perUser, ListenDSN: listenDSN,
	}
	return nil
}
