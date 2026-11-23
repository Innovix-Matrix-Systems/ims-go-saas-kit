package httpx

import "testing"

func TestDescribeUserAgent(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36":                   "Chrome 131 on Windows",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0":     "Edge 131 on Windows",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari": "Safari 18 on iOS",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14.7; rv:133.0) Gecko/20100101 Firefox/133.0":                                               "Firefox 133 on macOS",
		"curl/8.9.1":    "curl 8.9.1",
		"okhttp/4.12.0": "okhttp/4.12.0",
		"":              "",
	}
	for agent, want := range cases {
		if got := DescribeUserAgent(agent); got != want {
			t.Errorf("DescribeUserAgent(%q) = %q, want %q", agent, got, want)
		}
	}
}
