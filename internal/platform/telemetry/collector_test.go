package telemetry

import (
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

type recordingSink struct {
	mu       sync.Mutex
	requests int
	entries  []RequestEntry
	logs     int
	flushed  chan struct{}
}

func (s *recordingSink) InsertRequests(entries []RequestEntry) error {
	s.mu.Lock()
	s.requests += len(entries)
	s.entries = append(s.entries, entries...)
	s.mu.Unlock()
	s.signal()
	return nil
}

func (s *recordingSink) InsertQueries([]QueryEntry) error          { return nil }
func (s *recordingSink) InsertCacheEvents([]CacheEventEntry) error { return nil }
func (s *recordingSink) InsertQueueEvents([]QueueEventEntry) error { return nil }

func (s *recordingSink) InsertLogs(entries []LogEntry) error {
	s.mu.Lock()
	s.logs += len(entries)
	s.mu.Unlock()
	s.signal()
	return nil
}

func (s *recordingSink) InsertExceptions([]ExceptionEntry) error { return nil }

func (s *recordingSink) signal() {
	select {
	case s.flushed <- struct{}{}:
	default:
	}
}

func TestCollectorFlushesBatchesOnClose(t *testing.T) {
	sink := &recordingSink{flushed: make(chan struct{}, 4)}
	collector := NewCollector(CaptureConfig{
		Enabled:    true,
		Capture:    config.DevCapture{HTTP: true, Logs: true},
		SampleRate: 1,
	}, NewRedactor(nil), sink, slog.New(slog.NewTextHandler(io.Discard, nil)))

	collector.Request(RequestEntry{Method: "GET", Path: "/x"})
	collector.Log(LogEntry{Level: "WARN", Message: "hi"})
	collector.Close()

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.requests != 1 || sink.logs != 1 {
		t.Fatalf("sink requests=%d logs=%d, want 1/1", sink.requests, sink.logs)
	}
}

func TestCollectorDisabledNoOps(t *testing.T) {
	sink := &recordingSink{flushed: make(chan struct{}, 4)}
	collector := NewCollector(CaptureConfig{Enabled: false}, NewRedactor(nil), sink, nil)
	collector.Request(RequestEntry{Method: "GET"})
	collector.Close()
	if sink.requests != 0 {
		t.Fatalf("disabled collector wrote %d requests", sink.requests)
	}
}

func TestCaptureContext(t *testing.T) {
	ctx := t.Context()
	if CaptureFromContext(ctx) {
		t.Fatal("context captured by default")
	}
	if !CaptureFromContext(WithCapture(ctx, true)) {
		t.Fatal("WithCapture(true) not honored")
	}
}

func TestSampleRateBounds(t *testing.T) {
	collector := NewCollector(CaptureConfig{Enabled: true, SampleRate: 1}, NewRedactor(nil), &recordingSink{flushed: make(chan struct{}, 1)}, nil)
	for i := 0; i < 10; i++ {
		if !collector.Sample() {
			t.Fatal("sample rate 1 must sample everything")
		}
	}
	collector.Close()

	off := NewCollector(CaptureConfig{Enabled: true, SampleRate: 0}, NewRedactor(nil), &recordingSink{flushed: make(chan struct{}, 1)}, nil)
	for i := 0; i < 10; i++ {
		if off.Sample() {
			t.Fatal("sample rate 0 must sample nothing")
		}
	}
	off.Close()
}
