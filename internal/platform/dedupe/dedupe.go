// Package dedupe protects write endpoints from running the same request
// twice (phase 26.7): a double click, a client retry, a proxy re-send.
//
// Two layers, one middleware:
//
//   - The duplicate guard (default, no client work): the same caller sending
//     the same method, path and body while the first request runs, or within
//     a short window after it succeeded, gets 409 DUPLICATE_REQUEST.
//   - The idempotency key (optional, Stripe's semantics): a client that sends
//     Idempotency-Key gets the first response replayed for that key for a day,
//     so a retry minutes later still runs once.
//
// State lives in the cache. With a shared driver (redis, database) it holds
// across servers; with memory it protects one process. A cache outage lets
// requests through: refusing every write would be worse than a rare
// duplicate.
package dedupe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// HeaderIdempotencyKey is the client's idempotency key header.
const HeaderIdempotencyKey = "Idempotency-Key"

// HeaderReplayed marks a response replayed for an idempotency key.
const HeaderReplayed = "Idempotent-Replayed"

const (
	// maxKeyLength bounds Idempotency-Key.
	maxKeyLength = 255
	// bodySample is how much of a body the fingerprint reads; the rest is
	// streamed to the handler untouched (uploads).
	bodySample   = 1 << 20
	stateRunning = "running"
	stateDone    = "done"
)

// Options configures the middleware.
type Options struct {
	// Window keeps a succeeded request's fingerprint, so a fast repeat is
	// refused too (default 3 s).
	Window time.Duration
	// LockTTL bounds how long a running request holds its fingerprint
	// (a crashed process must not block the request forever).
	LockTTL time.Duration
	// KeyTTL is how long an idempotency key's response is kept (default 24 h).
	KeyTTL time.Duration
	// MaxBody is the largest response stored for an idempotency key; a
	// larger one is not stored (the key then guards only while running).
	MaxBody int
	// Skip lists routes ("POST /api/v1/logout") where identical repeats are
	// normal and harmless; Windows sets a longer window per route (money).
	Skip    []string
	Windows map[string]time.Duration
	// Caller names who sent a request and in which scope (the user id, and
	// the tenant or user staff act as); "" falls back to the client IP.
	Caller func(r *http.Request) string
	Logger *slog.Logger
}

type middleware struct {
	store     cache.Store
	responder *httpx.Responder
	opts      Options
	skip      map[string]bool
}

// Middleware returns the duplicate guard and idempotency-key handling for
// POST, PUT, PATCH and DELETE. Mount it after authentication, inside the
// router group, so the route pattern and the caller are known.
func Middleware(store cache.Store, responder *httpx.Responder, opts Options) func(http.Handler) http.Handler {
	if opts.Window <= 0 {
		opts.Window = 3 * time.Second
	}
	if opts.LockTTL <= 0 {
		opts.LockTTL = time.Minute
	}
	if opts.KeyTTL <= 0 {
		opts.KeyTTL = 24 * time.Hour
	}
	if opts.MaxBody <= 0 {
		opts.MaxBody = 1 << 20
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	m := &middleware{store: store, responder: responder, opts: opts, skip: map[string]bool{}}
	for _, route := range opts.Skip {
		m.skip[route] = true
	}
	return m.handle
}

func (m *middleware) handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			next.ServeHTTP(w, r)
			return
		}
		route := r.Method + " " + chi.RouteContext(r.Context()).RoutePattern()
		if m.skip[route] {
			next.ServeHTTP(w, r)
			return
		}
		bodyHash, err := m.hashBody(r)
		if err != nil {
			m.responder.Error(w, r, apperr.Wrap(apperr.BadRequest, "The request body could not be read.", err))
			return
		}
		caller := m.caller(r)
		if key := strings.TrimSpace(r.Header.Get(HeaderIdempotencyKey)); key != "" {
			m.withKey(w, r, next, key, route, caller, bodyHash)
			return
		}
		window := m.opts.Window
		if custom, ok := m.opts.Windows[route]; ok {
			window = custom
		}
		m.guard(w, r, next, fingerprint("dup", caller, r.Method, r.URL.RequestURI(), bodyHash), window)
	})
}

// guard runs a request once per fingerprint: a second copy while it runs,
// or within the window after it succeeded, gets 409.
func (m *middleware) guard(w http.ResponseWriter, r *http.Request, next http.Handler, key string, window time.Duration) {
	ctx := r.Context()
	created, err := m.store.SetNX(ctx, key, stateRunning, m.opts.LockTTL)
	if err != nil {
		m.opts.Logger.Warn("duplicate guard unavailable; request let through", "error", err)
		next.ServeHTTP(w, r)
		return
	}
	if !created {
		retry := 1
		if ttl, err := m.store.TTL(ctx, key); err == nil && ttl > 0 {
			retry = int(ttl.Seconds()) + 1
		}
		m.responder.Error(w, r, apperr.New(apperr.DuplicateRequest,
			"The same request was just sent and is running or has finished. Wait a moment before sending it again.").WithRetryAfter(retry))
		return
	}
	recorder := &capture{ResponseWriter: w, status: http.StatusOK}
	defer func() {
		// Detached: the request's context may be cancelled already.
		release := context.WithoutCancel(ctx)
		if panicked := recover(); panicked != nil {
			// The request failed (Recover answers 500 further out): free
			// the fingerprint so a retry runs, and keep panicking.
			_ = m.store.Delete(release, key)
			panic(panicked)
		}
		if recorder.status >= 200 && recorder.status < 300 && window > 0 {
			_ = m.store.Set(release, key, stateDone, window)
			return
		}
		_ = m.store.Delete(release, key) // a failure may be retried at once
	}()
	next.ServeHTTP(recorder, r)
}

// stored is an idempotency key's record.
type stored struct {
	State       string              `json:"state"`
	RequestHash string              `json:"request_hash"`
	Status      int                 `json:"status,omitempty"`
	Header      map[string][]string `json:"header,omitempty"`
	Body        []byte              `json:"body,omitempty"`
}

// replayedHeaders are the response headers a replay repeats.
var replayedHeaders = []string{"Content-Type", "Location", "Retry-After"}

// withKey runs a request once per idempotency key and replays its response.
func (m *middleware) withKey(w http.ResponseWriter, r *http.Request, next http.Handler, idempotencyKey, route, caller, bodyHash string) {
	if len(idempotencyKey) > maxKeyLength {
		m.responder.Error(w, r, apperr.Validation(map[string][]string{
			HeaderIdempotencyKey: {"The idempotency key must be at most 255 characters."},
		}))
		return
	}
	ctx := r.Context()
	key := fingerprint("idem", caller, route, r.URL.RequestURI(), idempotencyKey)
	requestHash := fingerprint("req", r.Method, r.URL.RequestURI(), bodyHash)
	created, err := m.store.SetNX(ctx, key, stored{State: stateRunning, RequestHash: requestHash}, m.opts.LockTTL)
	if err != nil {
		m.opts.Logger.Warn("idempotency store unavailable; request let through", "error", err)
		next.ServeHTTP(w, r)
		return
	}
	if !created {
		var previous stored
		if found, err := m.store.Get(ctx, key, &previous); err != nil || !found {
			m.responder.Error(w, r, apperr.New(apperr.IdempotencyInProgress, "A request with this idempotency key is in progress.").WithRetryAfter(1))
			return
		}
		switch {
		case previous.RequestHash != requestHash:
			m.responder.Error(w, r, apperr.New(apperr.IdempotencyKeyReused,
				"This idempotency key was used for a different request. Use a new key for a new request."))
		case previous.State != stateDone:
			m.responder.Error(w, r, apperr.New(apperr.IdempotencyInProgress, "A request with this idempotency key is in progress.").WithRetryAfter(1))
		default:
			for name, values := range previous.Header {
				for _, value := range values {
					w.Header().Add(name, value)
				}
			}
			w.Header().Set(HeaderReplayed, "true")
			w.WriteHeader(previous.Status)
			_, _ = w.Write(previous.Body)
		}
		return
	}

	recorder := &capture{ResponseWriter: w, status: http.StatusOK, keep: m.opts.MaxBody}
	defer func() {
		release := context.WithoutCancel(ctx)
		if panicked := recover(); panicked != nil {
			// A panic is a server error, not the 200 the recorder starts
			// at: never replay it.
			_ = m.store.Delete(release, key)
			panic(panicked)
		}
		// A server error or a response too big to keep: let the client retry.
		if recorder.status >= 500 || recorder.overflow {
			_ = m.store.Delete(release, key)
			return
		}
		header := map[string][]string{}
		for _, name := range replayedHeaders {
			if values := recorder.Header().Values(name); len(values) > 0 {
				header[name] = values
			}
		}
		_ = m.store.Set(release, key, stored{
			State: stateDone, RequestHash: requestHash,
			Status: recorder.status, Header: header, Body: recorder.body.Bytes(),
		}, m.opts.KeyTTL)
	}()
	next.ServeHTTP(recorder, r)
}

// hashBody hashes up to bodySample bytes of the body (and its length) and
// leaves the body readable for the handler.
func (m *middleware) hashBody(r *http.Request) (string, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return "", nil
	}
	sample, err := io.ReadAll(io.LimitReader(r.Body, bodySample))
	if err != nil {
		return "", err
	}
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(sample), r.Body), r.Body}
	sum := sha256.Sum256(sample)
	return hex.EncodeToString(sum[:]) + ":" + strconv.FormatInt(r.ContentLength, 10), nil
}

func (m *middleware) caller(r *http.Request) string {
	if m.opts.Caller != nil {
		if who := m.opts.Caller(r); who != "" {
			return who
		}
	}
	return "ip:" + httpx.ClientIP(r.Context())
}

func fingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "dedupe:" + parts[0] + ":" + hex.EncodeToString(sum[:])
}

// capture records the status and, for idempotency keys, the body (up to
// keep bytes).
type capture struct {
	http.ResponseWriter
	status   int
	wrote    bool
	keep     int
	body     bytes.Buffer
	overflow bool
}

func (c *capture) WriteHeader(code int) {
	if !c.wrote {
		c.wrote, c.status = true, code
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *capture) Write(b []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	if c.keep > 0 && !c.overflow {
		if c.body.Len()+len(b) > c.keep {
			c.overflow = true
			c.body.Reset()
		} else {
			c.body.Write(b)
		}
	}
	return c.ResponseWriter.Write(b)
}

// Flush forwards flushes when supported (inner wrappers reach the real
// writer through this one).
func (c *capture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (c *capture) Unwrap() http.ResponseWriter { return c.ResponseWriter }
