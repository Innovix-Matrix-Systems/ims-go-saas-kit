package dev

import (
	"encoding/json"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
)

// Action is one audited developer dashboard mutation.
type Action struct {
	ID           int64           `json:"id"`
	ActorUserID  int64           `json:"actor_user_id"`
	Action       string          `json:"action"`
	Target       *string         `json:"target"`
	Params       json.RawMessage `json:"params"`
	Status       string          `json:"status"`
	ErrorMessage *string         `json:"error_message"`
	DurationMS   int32           `json:"duration_ms"`
	IP           *string         `json:"ip"`
	UserAgent    *string         `json:"user_agent"`
	ActorEmail   *string         `json:"actor_email"`
	ActorName    *string         `json:"actor_name"`
	CreatedAt    time.Time       `json:"created_at"`
}

func mapAction(row dbgen.DevAction) Action {
	params := json.RawMessage(row.Params)
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	return Action{
		ID:           row.ID,
		ActorUserID:  row.ActorUserID,
		Action:       row.Action,
		Target:       row.Target,
		Params:       params,
		Status:       row.Status,
		ErrorMessage: row.ErrorMessage,
		DurationMS:   row.DurationMs,
		IP:           row.Ip,
		UserAgent:    row.UserAgent,
		CreatedAt:    row.CreatedAt.Time,
	}
}
