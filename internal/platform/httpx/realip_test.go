package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRealIPTrustedPeerHonoursForwarded(t *testing.T) {
	mw := RealIP([]string{"10.0.0.0/8"})
	var got string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.5")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != "203.0.113.7:0" {
		t.Errorf("RemoteAddr = %q, want 203.0.113.7:0", got)
	}
}

func TestRealIPUntrustedPeerIgnoresForwarded(t *testing.T) {
	mw := RealIP([]string{"10.0.0.0/8"})
	var got string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.9:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != "198.51.100.9:5555" {
		t.Errorf("RemoteAddr = %q, want the peer unchanged", got)
	}
}

func TestRealIPSkipsTrustedHopsRightToLeft(t *testing.T) {
	mw := RealIP([]string{"10.0.0.0/8", "192.168.0.0/16"})
	var got string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	// Client, then two trusted proxies: the rightmost untrusted entry wins.
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 192.168.1.1, 10.0.0.5")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != "203.0.113.7:0" {
		t.Errorf("RemoteAddr = %q, want 203.0.113.7:0", got)
	}
}

func TestRealIPUsesXRealIPFallback(t *testing.T) {
	mw := RealIP([]string{"10.0.0.5"})
	var got string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Real-IP", "203.0.113.42")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != "203.0.113.42:0" {
		t.Errorf("RemoteAddr = %q, want 203.0.113.42:0", got)
	}
}

func TestRealIPNoTrustedProxiesIsNoop(t *testing.T) {
	mw := RealIP(nil)
	var got string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != "10.0.0.5:1234" {
		t.Errorf("RemoteAddr = %q, want unchanged when no proxies configured", got)
	}
}
