package webhooks

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// errBlockedAddress is a delivery to an address webhooks may not reach.
var errBlockedAddress = errors.New("webhooks: the endpoint resolves to a private, loopback or link-local address")

// blockedPrefixes are the networks a webhook must not reach from the server
// (SSRF): loopback, private, carrier-grade NAT, link-local (cloud metadata at
// 169.254.169.254), unique-local IPv6, unspecified addresses and local-use
// NAT64 (where the embedded IPv4 can sit anywhere).
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}

// IPv6 ranges that carry an IPv4 address: a NAT64 gateway (64:ff9b::/96,
// the last four bytes) or a 6to4 relay (2002::/16, bytes two to five) would
// connect to it, so it is checked like the address itself.
var (
	nat64     = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour = netip.MustParsePrefix("2002::/16")
)

func blockedAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if addr.IsMulticast() {
		return true
	}
	if embedded, ok := embeddedIPv4(addr); ok && blockedAddress(embedded) {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// embeddedIPv4 returns the IPv4 address a NAT64 or 6to4 address carries.
func embeddedIPv4(addr netip.Addr) (netip.Addr, bool) {
	if !addr.Is6() {
		return netip.Addr{}, false
	}
	b := addr.As16()
	switch {
	case nat64.Contains(addr):
		return netip.AddrFrom4([4]byte(b[12:16])), true
	case sixToFour.Contains(addr):
		return netip.AddrFrom4([4]byte(b[2:6])), true
	}
	return netip.Addr{}, false
}

// newClient returns the delivery HTTP client: a timeout, no redirects (a
// redirect could point anywhere), and unless allowPrivate a dialer that
// checks the address it actually connects to, so DNS that changes between
// the check and the connection (rebinding) cannot reach an internal host.
func newClient(timeout time.Duration, allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	if !allowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			addr, err := netip.ParseAddr(host)
			if err != nil || blockedAddress(addr) {
				return errBlockedAddress
			}
			return nil
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil // a proxy would connect for us, past the check
	return &http.Client{
		Timeout:   timeout,
		Transport: tracing.Transport(transport),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// urlProblem checks an endpoint URL when it is saved and returns what is
// wrong with it ("" when nothing): an absolute http(s) URL, https unless
// private addresses are allowed, and not a literal blocked address.
// Hostnames are checked again at every connection.
func urlProblem(raw string, allowPrivate bool) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "The URL must be an absolute http or https URL."
	}
	if allowPrivate {
		return ""
	}
	if parsed.Scheme != "https" {
		return "The URL must use https."
	}
	host := strings.ToLower(parsed.Hostname())
	if addr, err := netip.ParseAddr(host); (err == nil && blockedAddress(addr)) || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "The URL may not point at a private, loopback or link-local address."
	}
	return ""
}
