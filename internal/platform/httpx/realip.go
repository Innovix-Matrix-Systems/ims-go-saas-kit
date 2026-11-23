package httpx

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const (
	trustedProxyKey contextKey = "trusted_proxy"
	clientIPKey     contextKey = "client_ip"
	userAgentKey    contextKey = "user_agent"
)

// UserAgent returns the caller's User-Agent header ("" outside a request).
func UserAgent(ctx context.Context) string {
	agent, _ := ctx.Value(userAgentKey).(string)
	return agent
}

// ClientIP returns the caller's IP address resolved by RealIP ("" outside an
// HTTP request).
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey).(string)
	return ip
}

// trustedProxy reports whether RealIP determined the immediate peer is a
// trusted proxy (and therefore forwarded headers may be honoured).
func trustedProxy(ctx context.Context) bool {
	trusted, _ := ctx.Value(trustedProxyKey).(bool)
	return trusted
}

// FromTrustedProxy reports whether the request came through a trusted proxy
// (see RealIP), so headers that proxy forwards may be honoured.
func FromTrustedProxy(r *http.Request) bool { return trustedProxy(r.Context()) }

// RealIP resolves the client IP from proxy headers (X-Forwarded-For,
// X-Real-IP) and rewrites r.RemoteAddr so downstream consumers — rate limiting,
// login throttling and access logs — see the real client.
//
// Forwarded headers are honoured only when the immediate peer is a trusted
// proxy; otherwise they are ignored (the peer address wins). This replaces
// chi's middleware.RealIP.
func RealIP(trusted []string) func(http.Handler) http.Handler {
	prefixes := parseTrustedProxies(trusted)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(prefixes) > 0 && isTrusted(peerAddr(r.RemoteAddr), prefixes) {
				r = r.Clone(context.WithValue(r.Context(), trustedProxyKey, true))
				if client := resolveClientIP(r, prefixes); client.IsValid() {
					r.RemoteAddr = net.JoinHostPort(client.String(), "0")
				}
			}
			// Services (e.g. the activity log) read the client IP from the
			// context because they never see the *http.Request.
			ctx := r.Context()
			if client := peerAddr(r.RemoteAddr); client.IsValid() {
				ctx = context.WithValue(ctx, clientIPKey, client.String())
			}
			if agent := r.UserAgent(); agent != "" {
				if len(agent) > 512 {
					agent = agent[:512]
				}
				ctx = context.WithValue(ctx, userAgentKey, agent)
			}
			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}

func resolveClientIP(r *http.Request, prefixes []netip.Prefix) netip.Addr {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		// Walk right-to-left, skipping trusted hops; the first untrusted entry
		// is the real client. Fall back to the leftmost entry when every hop is
		// trusted.
		for i := len(parts) - 1; i >= 0; i-- {
			addr, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if err != nil {
				continue
			}
			if !isTrusted(addr, prefixes) {
				return addr
			}
		}
		for _, part := range parts {
			if addr, err := netip.ParseAddr(strings.TrimSpace(part)); err == nil {
				return addr
			}
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		if addr, err := netip.ParseAddr(realIP); err == nil {
			return addr
		}
	}
	return netip.Addr{}
}

func parseTrustedProxies(trusted []string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(trusted))
	for _, raw := range trusted {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.Contains(raw, "/") {
			if prefix, err := netip.ParsePrefix(raw); err == nil {
				prefixes = append(prefixes, prefix.Masked())
				continue
			}
			continue
		}
		if addr, err := netip.ParseAddr(raw); err == nil {
			prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return prefixes
}

func isTrusted(addr netip.Addr, prefixes []netip.Prefix) bool {
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func peerAddr(remoteAddr string) netip.Addr {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}
