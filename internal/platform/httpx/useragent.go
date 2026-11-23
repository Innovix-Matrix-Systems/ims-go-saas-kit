package httpx

import (
	"regexp"
	"strings"
)

var userAgentBrowsers = []struct {
	pattern *regexp.Regexp
	name    string
}{
	{regexp.MustCompile(`Edg/(\d+)`), "Edge"},
	{regexp.MustCompile(`OPR/(\d+)`), "Opera"},
	{regexp.MustCompile(`Firefox/(\d+)`), "Firefox"},
	{regexp.MustCompile(`Chrome/(\d+)`), "Chrome"},
	{regexp.MustCompile(`Version/(\d+).*Safari`), "Safari"},
	{regexp.MustCompile(`curl/([\d.]+)`), "curl"},
}

var userAgentSystems = []struct {
	pattern *regexp.Regexp
	name    string
}{
	{regexp.MustCompile(`Windows`), "Windows"},
	{regexp.MustCompile(`iPhone|iPad`), "iOS"},
	{regexp.MustCompile(`Mac OS X`), "macOS"},
	{regexp.MustCompile(`Android`), "Android"},
	{regexp.MustCompile(`Linux`), "Linux"},
}

// DescribeUserAgent turns a user agent into "Chrome 131 on Windows" (the
// same reading as the dashboard's audit log). Unknown agents keep their
// first word ("okhttp/4.12.0").
func DescribeUserAgent(agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return ""
	}
	browser := strings.Fields(agent)[0]
	for _, candidate := range userAgentBrowsers {
		if match := candidate.pattern.FindStringSubmatch(agent); match != nil {
			browser = candidate.name + " " + match[1]
			break
		}
	}
	for _, system := range userAgentSystems {
		if system.pattern.MatchString(agent) {
			return browser + " on " + system.name
		}
	}
	return browser
}
