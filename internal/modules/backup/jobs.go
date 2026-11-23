package backup

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/queue"
)

const KindRun = "backup.run"

type RunPayload struct {
	RunID int64 `json:"run_id"`
}

func RegisterJob(registry *queue.Registry, service *Service) error {
	return registry.Register(queue.Definition{
		Kind:        KindRun,
		Description: "Creates a compressed PostgreSQL backup archive and copies it to the configured destinations.",
		Queue:       queue.QueueHeavy,
		MaxRetries:  1,
		Timeout:     service.cfg.Timeout,
		SystemJob:   true,
		Validate:    validateRunPayload,
		Handle: func(ctx context.Context, payload []byte) error {
			var job RunPayload
			if err := json.Unmarshal(payload, &job); err != nil {
				return err
			}
			return service.Execute(ctx, job.RunID)
		},
	})
}

func validateRunPayload(payload []byte) error {
	var job RunPayload
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("invalid %s payload: %w", KindRun, err)
	}
	if job.RunID < 1 {
		return fmt.Errorf("%s run_id must be positive", KindRun)
	}
	return nil
}
