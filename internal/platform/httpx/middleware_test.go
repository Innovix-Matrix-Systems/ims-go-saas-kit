package httpx

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDPropagatesHeader(t *testing.T) {
	var seen string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if seen != "abc-123" {
		t.Errorf("context request id = %q, want abc-123", seen)
	}
	if got := rec.Header().Get("X-Request-ID"); got != "abc-123" {
		t.Errorf("header request id = %q, want abc-123", got)
	}
}

func TestRequestIDGeneratesWhenMissing(t *testing.T) {
	var seen string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Error("generated request id is empty")
	}
	if rec.Header().Get("X-Request-ID") != seen {
		t.Error("header and context request id differ")
	}
}

func TestRecoverWritesProblem(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	responder := NewResponder("https://api.test", "local", false)

	handler := AccessLog(logger)(Recover(logger, responder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("content type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "INTERNAL_SERVER_ERROR") {
		t.Errorf("body = %s, want internal error code", rec.Body.String())
	}
}

func TestPaginationShape(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://api.test/api/v1/users?search=a", nil)
	meta, links := NewPagination(req, 2, 10, 42)

	if meta.CurrentPage != 2 || meta.LastPage != 5 || meta.PerPage != 10 || meta.Total != 42 {
		t.Errorf("meta = %+v", meta)
	}
	if meta.From == nil || *meta.From != 11 {
		t.Errorf("from = %v, want 11", meta.From)
	}
	if meta.To == nil || *meta.To != 20 {
		t.Errorf("to = %v, want 20", meta.To)
	}
	if links.First == nil || !strings.Contains(*links.First, "page=1") {
		t.Errorf("links.first = %v", links.First)
	}
	if links.Prev == nil || !strings.Contains(*links.Prev, "page=1") {
		t.Errorf("links.prev = %v", links.Prev)
	}
	if links.Next == nil || !strings.Contains(*links.Next, "page=3") {
		t.Errorf("links.next = %v", links.Next)
	}
	if len(meta.Links) != 7 {
		t.Errorf("meta.links length = %d, want 7 (prev + 5 pages + next)", len(meta.Links))
	}
}
