// Package metrics is a small, dependency-free Prometheus text-format exporter
// for the operational surfaces. It deliberately keeps
// cardinality low: HTTP metrics are labelled by method, matched route pattern
// and status only (never tenant or user). Disabled unless METRICS_ENABLED.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// durationBuckets are the histogram upper bounds in seconds.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type requestKey struct {
	method string
	route  string
	status int
}

type routeKey struct {
	method string
	route  string
}

// Registry accumulates counters and request-duration histograms. It is safe
// for concurrent use.
type Registry struct {
	mu        sync.Mutex
	counters  map[string]map[string]uint64 // name → serialized labels → value
	requests  map[requestKey]uint64
	durations map[routeKey]*histogram
	inFlight  int64
	startedAt time.Time
}

type histogram struct {
	buckets []uint64 // len == len(durationBuckets); cumulative
	sum     float64
	count   uint64
}

// New creates an empty metrics registry.
func New() *Registry {
	return &Registry{
		counters:  map[string]map[string]uint64{},
		requests:  map[requestKey]uint64{},
		durations: map[routeKey]*histogram{},
		startedAt: time.Now(),
	}
}

// IncCounter increments a named counter with the given labels. Use for
// non-HTTP instrumentation (queue jobs, scheduler runs).
func (m *Registry) IncCounter(name string, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byLabels, ok := m.counters[name]
	if !ok {
		byLabels = map[string]uint64{}
		m.counters[name] = byLabels
	}
	byLabels[serializeLabels(labels)]++
}

// Middleware records HTTP request counts and durations after the handler runs.
func (m *Registry) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		m.mu.Lock()
		m.inFlight++
		m.mu.Unlock()

		defer func() {
			route := "unmatched"
			if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil && routeCtx.RoutePattern() != "" {
				route = routeCtx.RoutePattern()
			}
			m.observe(r.Method, route, rec.status, time.Since(start).Seconds())
		}()

		next.ServeHTTP(rec, r)
	})
}

func (m *Registry) observe(method, route string, status int, seconds float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inFlight--
	m.requests[requestKey{method: method, route: route, status: status}]++

	key := routeKey{method: method, route: route}
	h, ok := m.durations[key]
	if !ok {
		h = &histogram{buckets: make([]uint64, len(durationBuckets))}
		m.durations[key] = h
	}
	h.count++
	h.sum += seconds
	for i, bound := range durationBuckets {
		if seconds <= bound {
			h.buckets[i]++
		}
	}
}

// Handler serves the Prometheus text exposition format.
func (m *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(m.render()))
	})
}

func (m *Registry) render() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder
	b.WriteString("# HELP ims_http_requests_total Total HTTP requests by method, route and status.\n")
	b.WriteString("# TYPE ims_http_requests_total counter\n")
	for _, k := range sortedRequestKeys(m.requests) {
		fmt.Fprintf(&b, "ims_http_requests_total{method=%q,route=%q,status=\"%d\"} %d\n",
			k.method, k.route, k.status, m.requests[k])
	}

	b.WriteString("# HELP ims_http_request_duration_seconds HTTP request latency.\n")
	b.WriteString("# TYPE ims_http_request_duration_seconds histogram\n")
	for _, k := range sortedRouteKeys(m.durations) {
		h := m.durations[k]
		for i, bound := range durationBuckets {
			fmt.Fprintf(&b, "ims_http_request_duration_seconds_bucket{method=%q,route=%q,le=%q} %d\n",
				k.method, k.route, formatFloat(bound), h.buckets[i])
		}
		fmt.Fprintf(&b, "ims_http_request_duration_seconds_bucket{method=%q,route=%q,le=\"+Inf\"} %d\n",
			k.method, k.route, h.count)
		fmt.Fprintf(&b, "ims_http_request_duration_seconds_sum{method=%q,route=%q} %g\n", k.method, k.route, h.sum)
		fmt.Fprintf(&b, "ims_http_request_duration_seconds_count{method=%q,route=%q} %d\n", k.method, k.route, h.count)
	}

	b.WriteString("# HELP ims_http_requests_in_flight Current in-flight HTTP requests.\n")
	b.WriteString("# TYPE ims_http_requests_in_flight gauge\n")
	fmt.Fprintf(&b, "ims_http_requests_in_flight %d\n", m.inFlight)

	names := make([]string, 0, len(m.counters))
	for name := range m.counters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, "# TYPE %s counter\n", name)
		byLabels := m.counters[name]
		labels := make([]string, 0, len(byLabels))
		for l := range byLabels {
			labels = append(labels, l)
		}
		sort.Strings(labels)
		for _, l := range labels {
			fmt.Fprintf(&b, "%s%s %d\n", name, l, byLabels[l])
		}
	}
	return b.String()
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wrote {
		return
	}
	r.wrote = true
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// WroteHeader reports whether a status has already been sent.
func (r *statusRecorder) WroteHeader() bool { return r.wrote }

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func serializeLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, labels[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func sortedRequestKeys(m map[requestKey]uint64) []requestKey {
	keys := make([]requestKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].status < keys[j].status
	})
	return keys
}

func sortedRouteKeys(m map[routeKey]*histogram) []routeKey {
	keys := make([]routeKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].method < keys[j].method
	})
	return keys
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
