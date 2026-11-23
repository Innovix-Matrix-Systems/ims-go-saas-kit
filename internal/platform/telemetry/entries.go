package telemetry

import (
	"encoding/json"
	"time"
)

// RequestEntry is one captured HTTP request/response pair.
type RequestEntry struct {
	RequestID     string
	Method        string
	Path          string
	Route         string
	Status        int
	DurationMS    float64
	RequestBytes  int64
	ResponseBytes int64
	UserID        *int64 // the identity the request ran as
	// ImpersonatorID is who was really calling when acting as a user
	// (X-Acting-User); UserID is then the target.
	ImpersonatorID *int64
	TenantID       *int64
	BranchID       *int64
	IP             string
	UserAgent      string
	Headers        map[string][]string
	RequestBody    json.RawMessage
	ResponseBody   json.RawMessage
	CreatedAt      time.Time
}

// QueryEntry is one captured SQL statement.
type QueryEntry struct {
	RequestID  string
	SQL        string
	Args       json.RawMessage
	DurationMS float64
	RowCount   int64
	Error      string
	CreatedAt  time.Time
}

// CacheEventEntry is one captured cache operation.
type CacheEventEntry struct {
	RequestID  string
	Operation  string
	Key        string
	Hit        *bool
	DurationMS float64
	CreatedAt  time.Time
}

// QueueEventEntry is one queue dispatch or worker outcome.
type QueueEventEntry struct {
	RequestID  string
	Event      string
	Queue      string
	Kind       string
	JobID      *int64
	DurationMS float64
	Error      string
	CreatedAt  time.Time
}

// LogEntry is one captured structured log record.
type LogEntry struct {
	RequestID  string
	Level      string
	Message    string
	Attributes json.RawMessage
	CreatedAt  time.Time
}

// ExceptionEntry is one captured panic/exception.
type ExceptionEntry struct {
	RequestID string
	Class     string
	Message   string
	Stack     string
	CreatedAt time.Time
}

// Sink persists telemetry batches. Implementations must be safe for concurrent
// use by the collector workers.
type Sink interface {
	InsertRequests(entries []RequestEntry) error
	InsertQueries(entries []QueryEntry) error
	InsertCacheEvents(entries []CacheEventEntry) error
	InsertQueueEvents(entries []QueueEventEntry) error
	InsertLogs(entries []LogEntry) error
	InsertExceptions(entries []ExceptionEntry) error
}
