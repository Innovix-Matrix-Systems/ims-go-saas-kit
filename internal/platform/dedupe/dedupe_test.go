package dedupe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
)

// harness is a router with the middleware in front of a counting handler.
type harness struct {
	router http.Handler
	runs   atomic.Int64
	status int
	block  chan struct{} // when set, the handler waits on it
	panics atomic.Bool   // when set, the handler panics before answering
}

func newHarness(t *testing.T, store cache.Store, opts Options) *harness {
	t.Helper()
	h := &harness{status: http.StatusCreated}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		opts.Caller = func(r *http.Request) string { return r.Header.Get("X-User") }
		r.Use(Middleware(store, httpx.NewResponder("http://test", "local", false), opts))
		handler := func(w http.ResponseWriter, r *http.Request) {
			n := h.runs.Add(1)
			if h.panics.Load() {
				panic("handler failed")
			}
			body, _ := io.ReadAll(r.Body)
			if h.block != nil {
				<-h.block
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(h.status)
			_ = json.NewEncoder(w).Encode(map[string]any{"run": n, "body": string(body)})
		}
		r.Post("/orders", handler)
		r.Post("/orders/{id}/read", handler)
		r.Get("/orders", handler)
	})
	// Recover sits outside the middleware, as in the application.
	h.router = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer func() {
			if recover() != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		r.ServeHTTP(w, req)
	})
	return h
}

func (h *harness) send(user, path, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("X-User", user)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func newStore(t *testing.T) cache.Store {
	t.Helper()
	store := memory.New("dedupe-test-")
	t.Cleanup(store.Close)
	return store
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var problem struct {
		ErrorCode string `json:"error_code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &problem)
	return problem.ErrorCode
}

func TestConcurrentDuplicatesRunOnce(t *testing.T) {
	h := newHarness(t, newStore(t), Options{})
	h.block = make(chan struct{})
	var wg sync.WaitGroup
	var first *httptest.ResponseRecorder
	wg.Add(1)
	go func() {
		defer wg.Done()
		first = h.send("u1", "/orders", `{"item":1}`)
	}()
	for h.runs.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	second := h.send("u1", "/orders", `{"item":1}`)
	close(h.block)
	wg.Wait()

	if first.Code != http.StatusCreated || second.Code != http.StatusConflict || errorCode(t, second) != "DUPLICATE_REQUEST" {
		t.Fatalf("first=%d second=%d %s", first.Code, second.Code, second.Body)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("the 409 has no Retry-After")
	}
	if h.runs.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", h.runs.Load())
	}
}

func TestWindowAfterSuccess(t *testing.T) {
	h := newHarness(t, newStore(t), Options{Window: 80 * time.Millisecond})
	if rec := h.send("u1", "/orders", `{"item":1}`); rec.Code != http.StatusCreated {
		t.Fatalf("first = %d", rec.Code)
	}
	if rec := h.send("u1", "/orders", `{"item":1}`); rec.Code != http.StatusConflict {
		t.Fatalf("repeat inside the window = %d, want 409", rec.Code)
	}
	// Other callers, bodies and paths are other requests.
	for _, rec := range []*httptest.ResponseRecorder{
		h.send("u2", "/orders", `{"item":1}`),
		h.send("u1", "/orders", `{"item":2}`),
		h.send("u1", "/orders?draft=1", `{"item":1}`),
	} {
		if rec.Code != http.StatusCreated {
			t.Fatalf("a different request was refused: %d %s", rec.Code, rec.Body)
		}
	}
	time.Sleep(120 * time.Millisecond)
	if rec := h.send("u1", "/orders", `{"item":1}`); rec.Code != http.StatusCreated {
		t.Fatalf("repeat after the window = %d, want 201", rec.Code)
	}
}

func TestFailuresCanBeRetriedAtOnce(t *testing.T) {
	h := newHarness(t, newStore(t), Options{})
	h.status = http.StatusUnprocessableEntity
	h.send("u1", "/orders", `{}`)
	if rec := h.send("u1", "/orders", `{}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("retry after a 422 = %d, want it to run again", rec.Code)
	}
	if h.runs.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2", h.runs.Load())
	}
}

func TestSkippedRoutesAndReads(t *testing.T) {
	h := newHarness(t, newStore(t), Options{Skip: []string{"POST /orders/{id}/read"}})
	h.send("u1", "/orders/7/read", ``)
	if rec := h.send("u1", "/orders/7/read", ``); rec.Code != http.StatusCreated {
		t.Fatalf("a skipped route was guarded: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	for range 2 {
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("GET was guarded: %d", rec.Code)
		}
	}
}

func TestIdempotencyKeyReplaysTheFirstAnswer(t *testing.T) {
	h := newHarness(t, newStore(t), Options{Window: time.Millisecond})
	first := h.send("u1", "/orders", `{"item":1}`, HeaderIdempotencyKey, "k-1")
	time.Sleep(5 * time.Millisecond)
	replay := h.send("u1", "/orders", `{"item":1}`, HeaderIdempotencyKey, "k-1")
	if replay.Code != http.StatusCreated || replay.Body.String() != first.Body.String() || replay.Header().Get(HeaderReplayed) != "true" {
		t.Fatalf("replay = %d %q (replayed=%q), want the first answer %q", replay.Code, replay.Body, replay.Header().Get(HeaderReplayed), first.Body)
	}
	if h.runs.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", h.runs.Load())
	}
	if rec := h.send("u1", "/orders", `{"item":2}`, HeaderIdempotencyKey, "k-1"); errorCode(t, rec) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("a reused key = %d %s", rec.Code, rec.Body)
	}
	// Keys are per caller.
	if rec := h.send("u2", "/orders", `{"item":1}`, HeaderIdempotencyKey, "k-1"); rec.Header().Get(HeaderReplayed) != "" {
		t.Fatal("another caller got the first caller's answer")
	}
}

func TestIdempotencyKeyDoesNotKeepServerErrors(t *testing.T) {
	h := newHarness(t, newStore(t), Options{})
	h.status = http.StatusServiceUnavailable
	h.send("u1", "/orders", `{}`, HeaderIdempotencyKey, "k-2")
	h.status = http.StatusCreated
	if rec := h.send("u1", "/orders", `{}`, HeaderIdempotencyKey, "k-2"); rec.Code != http.StatusCreated || rec.Header().Get(HeaderReplayed) != "" {
		t.Fatalf("retry after a 503 = %d (replayed=%q), want a real run", rec.Code, rec.Header().Get(HeaderReplayed))
	}
}

func TestLargeBodiesReachTheHandlerWhole(t *testing.T) {
	h := newHarness(t, newStore(t), Options{})
	body := strings.Repeat("x", bodySample+4096)
	rec := h.send("u1", "/orders", body)
	var answer struct {
		Body string `json:"body"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &answer)
	if len(answer.Body) != len(body) {
		t.Fatalf("the handler read %d bytes, want %d", len(answer.Body), len(body))
	}
}

// brokenStore fails every call, like a cache outage.
type brokenStore struct{ cache.Store }

func (brokenStore) SetNX(context.Context, string, any, time.Duration) (bool, error) {
	return false, errors.New("cache down")
}

func TestCacheOutageFailsOpen(t *testing.T) {
	h := newHarness(t, brokenStore{}, Options{})
	for range 2 {
		if rec := h.send("u1", "/orders", `{}`); rec.Code != http.StatusCreated {
			t.Fatalf("a write was refused during a cache outage: %d", rec.Code)
		}
	}
}

// A panicking handler is a failed request: neither the duplicate guard nor
// an idempotency key may keep it as a success.
func TestPanicsAreNotKeptAsSuccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header []string
	}{
		{name: "duplicate guard"},
		{name: "idempotency key", header: []string{HeaderIdempotencyKey, "k-panic"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, newStore(t), Options{})
			h.panics.Store(true)
			if rec := h.send("u1", "/orders", `{"n":1}`, tc.header...); rec.Code != http.StatusInternalServerError {
				t.Fatalf("panicking request = %d, want 500", rec.Code)
			}
			h.panics.Store(false)
			retry := h.send("u1", "/orders", `{"n":1}`, tc.header...)
			if retry.Code != http.StatusCreated || retry.Header().Get(HeaderReplayed) != "" {
				t.Fatalf("retry = %d (replayed %q), want a fresh 201", retry.Code, retry.Header().Get(HeaderReplayed))
			}
			if got := h.runs.Load(); got != 2 {
				t.Fatalf("handler ran %d times, want 2", got)
			}
		})
	}
}

// A flush from inside the handler reaches the real writer through the
// guard's recorder.
func TestGuardForwardsFlush(t *testing.T) {
	mw := Middleware(newStore(t), httpx.NewResponder("http://test", "local", false), Options{})
	r := chi.NewRouter()
	r.With(mw).Post("/stream", func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("the guard's response writer is not an http.Flusher")
		}
		_, _ = w.Write([]byte("chunk"))
		flusher.Flush()
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/stream", strings.NewReader("{}")))
	if !rec.Flushed {
		t.Error("the flush did not reach the underlying writer")
	}
}
