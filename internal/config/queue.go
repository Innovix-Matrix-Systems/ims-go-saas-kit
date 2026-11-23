package config

import (
	"fmt"
	"time"
)

// Queue and scheduler settings (QUEUE_*, SCHEDULE_*).

// Channel holds per-queue retry/timeout settings.
type Channel struct {
	Tries   int
	Timeout time.Duration
}

// Queue holds the queue engine settings.
type Queue struct {
	Driver            string
	WorkerConcurrency int
	Critical          Channel
	Default           Channel
	Heavy             Channel
}

// Schedule holds scheduler settings.
type Schedule struct {
	Timezone string
	// HistoryDays is how long schedule_runs rows are kept (0 = forever).
	HistoryDays int
}

func (c *Config) loadQueue() error {
	concurrency, err := getInt("QUEUE_WORKER_CONCURRENCY", 10)
	if err != nil {
		return err
	}
	critical, err := loadChannel("QUEUE_CRITICAL_", 3, 60)
	if err != nil {
		return err
	}
	defaultChannel, err := loadChannel("QUEUE_DEFAULT_", 5, 120)
	if err != nil {
		return err
	}
	heavy, err := loadChannel("QUEUE_HEAVY_", 1, 1800)
	if err != nil {
		return err
	}
	c.Queue = Queue{
		Driver:            getString("QUEUE_DRIVER", "river"),
		WorkerConcurrency: concurrency,
		Critical:          critical,
		Default:           defaultChannel,
		Heavy:             heavy,
	}
	return nil
}

func loadChannel(prefix string, defaultTries, defaultTimeoutSeconds int) (Channel, error) {
	tries, err := getInt(prefix+"TRIES", defaultTries)
	if err != nil {
		return Channel{}, err
	}
	timeoutSeconds, err := getInt(prefix+"TIMEOUT", defaultTimeoutSeconds)
	if err != nil {
		return Channel{}, err
	}
	return Channel{Tries: tries, Timeout: time.Duration(timeoutSeconds) * time.Second}, nil
}

func (c *Config) loadSchedule() error {
	days, err := getInt("SCHEDULE_HISTORY_DAYS", 30)
	if err != nil {
		return err
	}
	if days < 0 {
		return fmt.Errorf("SCHEDULE_HISTORY_DAYS must be zero or more, got %d", days)
	}
	c.Schedule = Schedule{Timezone: getString("SCHEDULE_TIMEZONE", c.App.Timezone), HistoryDays: days}
	return nil
}
