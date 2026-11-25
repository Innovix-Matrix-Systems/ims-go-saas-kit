package dev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
)

// PlaygroundJobKind is the synthetic queue job created from the local-only
// Playground screen. It exists so developers can exercise the queue admin,
// retries, worker log/telemetry and job detail views without touching real
// business jobs.
const PlaygroundJobKind = "dev.test"

// PlaygroundJobPayload is the dev.test payload.
type PlaygroundJobPayload struct {
	SleepMS int    `json:"sleep_ms"`
	Fail    bool   `json:"fail"`
	Tag     string `json:"tag"`
}

// RegisterPlaygroundJob adds the dev.test definition to the queue registry.
func RegisterPlaygroundJob(registry *queue.Registry) error {
	return registry.Register(queue.Definition{
		Kind:        PlaygroundJobKind,
		Description: "Synthetic queue job created from the developer Playground.",
		Queue:       queue.QueueDefault,
		MaxRetries:  0,
		Timeout:     30 * time.Second,
		SystemJob:   true,
		Validate:    validatePlaygroundJob,
		Handle:      handlePlaygroundJob,
	})
}

func validatePlaygroundJob(payload []byte) error {
	var job PlaygroundJobPayload
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("invalid %s payload: %w", PlaygroundJobKind, err)
	}
	if job.SleepMS < 0 || job.SleepMS > 10000 {
		return fmt.Errorf("%s sleep_ms must be between 0 and 10000", PlaygroundJobKind)
	}
	return nil
}

func handlePlaygroundJob(ctx context.Context, payload []byte) error {
	var job PlaygroundJobPayload
	if err := json.Unmarshal(payload, &job); err != nil {
		return err
	}
	if job.SleepMS > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(job.SleepMS) * time.Millisecond):
		}
	}
	if job.Fail {
		return errors.New("playground job failed on purpose")
	}
	return nil
}
