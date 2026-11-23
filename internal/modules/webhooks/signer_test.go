package webhooks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestSignMatchesStandardWebhooks(t *testing.T) {
	// The example from the Standard Webhooks specification.
	got, err := sign("whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw", "msg_p5jXN8AQM9LWM0D4loKWxJek",
		time.Unix(1614265330, 0), []byte(`{"test": 2432232314}`))
	if err != nil || got != "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=" {
		t.Fatalf("sign() = %q, %v", got, err)
	}
}

func TestSignaturesDuringRotation(t *testing.T) {
	current, _ := newSecret()
	previous, _ := newSecret()
	header, err := signatures("evt_1", time.Unix(1, 0), []byte("{}"), current, "", previous)
	if err != nil {
		t.Fatalf("signatures() error = %v", err)
	}
	parts := strings.Split(header, " ")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "v1,") || parts[0] == parts[1] {
		t.Fatalf("header = %q, want two v1 signatures", header)
	}
	if !strings.HasPrefix(current, secretPrefix) {
		t.Fatalf("secret = %q, want the whsec_ prefix", current)
	}
}

func TestURLProblems(t *testing.T) {
	cases := []struct {
		url          string
		allowPrivate bool
		ok           bool
	}{
		{"https://hooks.acme.test/ims", false, true},
		{"http://hooks.acme.test/ims", false, false},
		{"https://127.0.0.1/hook", false, false},
		{"https://10.1.2.3/hook", false, false},
		{"https://169.254.169.254/latest/meta-data", false, false},
		{"https://[::1]/hook", false, false},
		{"https://localhost/hook", false, false},
		{"http://localhost:9000/hook", true, true},
		{"ftp://hooks.acme.test", true, false},
		{"/relative", true, false},
	}
	for _, tc := range cases {
		if got := urlProblem(tc.url, tc.allowPrivate) == ""; got != tc.ok {
			t.Errorf("urlProblem(%q, allowPrivate=%v) ok = %v, want %v", tc.url, tc.allowPrivate, got, tc.ok)
		}
	}
}

func TestBlockedAddresses(t *testing.T) {
	for address, blocked := range map[string]bool{
		"127.0.0.1": true, "10.0.0.5": true, "172.20.1.1": true, "192.168.1.10": true,
		"169.254.169.254": true, "100.64.0.1": true, "::1": true, "fd00::1": true, "fe80::1": true,
		"::ffff:127.0.0.1": true, "224.0.0.1": true,
		"8.8.8.8": false, "203.0.113.7": false, "2606:4700::1111": false,
		// IPv6 that a NAT64 gateway or 6to4 relay turns into IPv4.
		"64:ff9b::a9fe:a9fe": true, "64:ff9b::a00:5": true, "64:ff9b::808:808": false,
		"64:ff9b:1::a00:5": true, "2002:7f00:1::1": true, "2002:c0a8:101::": true, "2002:808:808::1": false,
	} {
		if got := blockedAddress(netip.MustParseAddr(address)); got != blocked {
			t.Errorf("blockedAddress(%s) = %v, want %v", address, got, blocked)
		}
	}
}

func TestClientRefusesInternalAddressesAtConnectTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
	if _, err := newClient(time.Second, false).Do(req); !errors.Is(err, errBlockedAddress) {
		t.Fatalf("Do(loopback) error = %v, want the blocked-address error", err)
	}
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
	res, err := newClient(time.Second, true).Do(req)
	if err != nil {
		t.Fatalf("Do(loopback, allowed) error = %v", err)
	}
	_ = res.Body.Close()
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/", http.StatusFound)
	}))
	defer server.Close()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
	res, err := newClient(time.Second, true).Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want the 302 itself", res.StatusCode)
	}
}
