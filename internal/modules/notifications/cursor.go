package notifications

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// Cursor is the encoded feed position. PointsToNext marks cursors produced for
// forward paging (older rows); the prev cursor pages toward newer rows.
type Cursor struct {
	CreatedAt    string `json:"created_at"`
	ID           int64  `json:"id"`
	PointsToNext bool   `json:"points_to_next"`
}

// EncodeCursor serializes a cursor as base64url JSON.
func EncodeCursor(createdAt time.Time, id int64, pointsToNext bool) string {
	payload, err := json.Marshal(Cursor{
		CreatedAt:    createdAt.UTC().Format(time.RFC3339Nano),
		ID:           id,
		PointsToNext: pointsToNext,
	})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

// DecodeCursor parses a cursor; invalid or empty input returns nil so callers
// serve the first page.
func DecodeCursor(raw string) *Cursor {
	if raw == "" {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil
	}
	var cursor Cursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return nil
	}
	if cursor.ID < 1 {
		return nil
	}
	if _, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt); err != nil {
		return nil
	}
	return &cursor
}

func (c *Cursor) timestamp() time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, c.CreatedAt)
	return parsed
}
