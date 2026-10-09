package agent

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// LoopbackBaseURL is the rule for UNREAL_REVIEW_OPENROUTER_API and
// UNREAL_REVIEW_GITHUB_API. An empty value means the caller keeps the public
// API. A set value is used only when it is an http(s) URL on 127.0.0.1, ::1,
// or localhost. Anything else is an error, so a bearer token is not sent to
// an arbitrary host. Secret reexec leaves both variables in the environment.
func LoopbackBaseURL(envName, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", envName, err)
	}
	schemeOK := strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
	host := strings.ToLower(u.Hostname())
	if !schemeOK || host == "" {
		return "", fmt.Errorf("%s must be an http or https URL on 127.0.0.1, ::1, or localhost", envName)
	}
	if loopbackHost(host) {
		return raw, nil
	}
	return "", fmt.Errorf("%s host %q is not loopback; use 127.0.0.1, ::1, or localhost", envName, u.Hostname())
}

func loopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

// LoopbackHTTPClient follows redirects only while the next URL stays on
// 127.0.0.1, ::1, or localhost. A loopback stand-in that redirects off-host
// must not receive the bearer on that next request.
func LoopbackHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: refuseOffLoopbackRedirect}
}

func refuseOffLoopbackRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if req.URL == nil || !loopbackHost(req.URL.Hostname()) {
		host := ""
		if req.URL != nil {
			host = req.URL.Host
		}
		return fmt.Errorf("refusing redirect to %s: not loopback", host)
	}
	return nil
}
