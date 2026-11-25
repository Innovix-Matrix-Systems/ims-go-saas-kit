package telemetry

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
)

// CaptureConfig mirrors the DEV_DASHBOARD_* configuration.
type CaptureConfig struct {
	Enabled      bool
	Capture      config.DevCapture
	SampleRate   float64
	MaxBodyBytes int64
}

const (
	channelBuffer = 2048
	batchSize     = 100
	batchLinger   = 750 * time.Millisecond
)

// Collector buffers telemetry off the request path and flushes batches to the
// sink. A nil collector is safe to use everywhere (all methods no-op).
type Collector struct {
	capture  CaptureConfig
	redactor *Redactor
	sink     Sink
	logger   *slog.Logger

	requests    chan RequestEntry
	queries     chan QueryEntry
	cacheEvents chan CacheEventEntry
	queueEvents chan QueueEventEntry
	logs        chan LogEntry
	exceptions  chan ExceptionEntry

	dropped atomic.Int64
	closed  atomic.Bool
	wg      sync.WaitGroup
}

// NewCollector starts the background workers. Call Close on shutdown.
func NewCollector(capture CaptureConfig, redactor *Redactor, sink Sink, logger *slog.Logger) *Collector {
	collector := &Collector{
		capture:     capture,
		redactor:    redactor,
		sink:        sink,
		logger:      logger,
		requests:    make(chan RequestEntry, channelBuffer),
		queries:     make(chan QueryEntry, channelBuffer),
		cacheEvents: make(chan CacheEventEntry, channelBuffer),
		queueEvents: make(chan QueueEventEntry, channelBuffer),
		logs:        make(chan LogEntry, channelBuffer),
		exceptions:  make(chan ExceptionEntry, channelBuffer),
	}
	if !capture.Enabled || sink == nil {
		return collector
	}
	collector.wg.Add(6)
	go runWorker(collector, collector.requests, collector.flushRequests)
	go runWorker(collector, collector.queries, collector.flushQueries)
	go runWorker(collector, collector.cacheEvents, collector.flushCacheEvents)
	go runWorker(collector, collector.queueEvents, collector.flushQueueEvents)
	go runWorker(collector, collector.logs, collector.flushLogs)
	go runWorker(collector, collector.exceptions, collector.flushExceptions)
	return collector
}

// Close drains the buffered batches and stops the workers.
func (c *Collector) Close() {
	if c == nil {
		return
	}
	c.closed.Store(true)
	c.wg.Wait()
}

// Enabled reports whether capture is on at all.
func (c *Collector) Enabled() bool { return c != nil && c.capture.Enabled && c.sink != nil }

// Captures reports the per-category toggle state.
func (c *Collector) Captures() config.DevCapture {
	if c == nil {
		return config.DevCapture{}
	}
	return c.capture.Capture
}

// SampleRate is the share of requests captured (0 when capture is off).
func (c *Collector) SampleRate() float64 {
	if !c.Enabled() {
		return 0
	}
	return min(max(c.capture.SampleRate, 0), 1)
}

// Sample decides whether one request is captured at the configured rate.
func (c *Collector) Sample() bool {
	if c == nil || !c.capture.Enabled {
		return false
	}
	if c.capture.SampleRate >= 1 {
		return true
	}
	if c.capture.SampleRate <= 0 {
		return false
	}
	return rand.Float64() < c.capture.SampleRate
}

// Drops counts entries discarded under backpressure since startup.
func (c *Collector) Drops() int64 {
	if c == nil {
		return 0
	}
	return c.dropped.Load()
}

func (c *Collector) Request(entry RequestEntry) {
	if c == nil || !c.capture.Enabled || !c.capture.Capture.HTTP {
		return
	}
	push(c, c.requests, entry)
}

func (c *Collector) Query(entry QueryEntry) {
	if c == nil || !c.capture.Enabled || !c.capture.Capture.SQL {
		return
	}
	push(c, c.queries, entry)
}

func (c *Collector) CacheEvent(entry CacheEventEntry) {
	if c == nil || !c.capture.Enabled || !c.capture.Capture.Cache {
		return
	}
	push(c, c.cacheEvents, entry)
}

func (c *Collector) QueueEvent(entry QueueEventEntry) {
	if c == nil || !c.capture.Enabled || !c.capture.Capture.Queue {
		return
	}
	push(c, c.queueEvents, entry)
}

func (c *Collector) Log(entry LogEntry) {
	if c == nil || !c.capture.Enabled || !c.capture.Capture.Logs {
		return
	}
	push(c, c.logs, entry)
}

func (c *Collector) Exception(entry ExceptionEntry) {
	if c == nil || !c.capture.Enabled || !c.capture.Capture.HTTP {
		return
	}
	push(c, c.exceptions, entry)
}

type captureKey struct{}

// WithCapture marks a request context as captured (or explicitly not, when
// sampling skips it). The SQL/cache/log hooks read this bit.
func WithCapture(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, captureKey{}, enabled)
}

// CaptureFromContext reports whether the current request is captured.
func CaptureFromContext(ctx context.Context) bool {
	enabled, _ := ctx.Value(captureKey{}).(bool)
	return enabled
}

// JobEnqueued implements queue.JobObserver: it records dispatches with the
// River job id so the dashboard can deep-link to the job.
func (c *Collector) JobEnqueued(ctx context.Context, kind, queue string, jobID int64, duration time.Duration, err error) {
	if c == nil || !c.capture.Enabled || !c.capture.Capture.Queue {
		return
	}
	event := "dispatched"
	errorText := ""
	if err != nil {
		event = "dispatch_failed"
		errorText = err.Error()
	}
	var id *int64
	if jobID > 0 {
		value := jobID
		id = &value
	}
	c.QueueEvent(QueueEventEntry{
		RequestID:  httpx.RequestIDFromContext(ctx),
		Event:      event,
		Queue:      queue,
		Kind:       kind,
		JobID:      id,
		DurationMS: float64(duration.Microseconds()) / 1000.0,
		Error:      errorText,
		CreatedAt:  time.Now(),
	})
}

// JobFinished implements queue.JobObserver for worker-side capture.
func (c *Collector) JobFinished(ctx context.Context, kind, queue string, jobID int64, duration time.Duration, err error) {
	if c == nil || !c.capture.Enabled || !c.capture.Capture.Queue {
		return
	}
	event := "executed"
	errorText := ""
	if err != nil {
		event = "failed"
		errorText = err.Error()
	}
	id := jobID
	c.QueueEvent(QueueEventEntry{
		Event:      event,
		Queue:      queue,
		Kind:       kind,
		JobID:      &id,
		DurationMS: float64(duration.Microseconds()) / 1000.0,
		Error:      errorText,
		CreatedAt:  time.Now(),
	})
}

func push[T any](c *Collector, channel chan T, entry T) {
	select {
	case channel <- entry:
	default:
		c.dropped.Add(1)
	}
}

func runWorker[T any](c *Collector, channel chan T, flush func([]T)) {
	defer c.wg.Done()
	batch := make([]T, 0, batchSize)
	ticker := time.NewTicker(batchLinger)
	defer ticker.Stop()
	for {
		select {
		case entry := <-channel:
			batch = append(batch, entry)
			if len(batch) >= batchSize {
				flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				flush(batch)
				batch = batch[:0]
			}
			if c.closed.Load() && len(channel) == 0 {
				return
			}
		}
	}
}

func (c *Collector) flushRequests(batch []RequestEntry) {
	if err := c.sink.InsertRequests(batch); err != nil {
		c.dropped.Add(int64(len(batch)))
		c.logError("requests", err)
	}
}

func (c *Collector) flushQueries(batch []QueryEntry) {
	if err := c.sink.InsertQueries(batch); err != nil {
		c.dropped.Add(int64(len(batch)))
		c.logError("queries", err)
	}
}

func (c *Collector) flushCacheEvents(batch []CacheEventEntry) {
	if err := c.sink.InsertCacheEvents(batch); err != nil {
		c.dropped.Add(int64(len(batch)))
		c.logError("cache_events", err)
	}
}

func (c *Collector) flushQueueEvents(batch []QueueEventEntry) {
	if err := c.sink.InsertQueueEvents(batch); err != nil {
		c.dropped.Add(int64(len(batch)))
		c.logError("queue_events", err)
	}
}

func (c *Collector) flushLogs(batch []LogEntry) {
	if err := c.sink.InsertLogs(batch); err != nil {
		c.dropped.Add(int64(len(batch)))
		c.logError("logs", err)
	}
}

func (c *Collector) flushExceptions(batch []ExceptionEntry) {
	if err := c.sink.InsertExceptions(batch); err != nil {
		c.dropped.Add(int64(len(batch)))
		c.logError("exceptions", err)
	}
}

func (c *Collector) logError(kind string, err error) {
	if c.logger == nil {
		return
	}
	c.logger.Warn("telemetry: flush failed", "kind", kind, "error", err)
}
