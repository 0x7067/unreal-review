package agent

import (
	"fmt"
	"net/url"
	"strings"
)

// LoopbackBaseURL is the rule for UNREAL_REVIEW_OPENROUTER_API and
// UNREAL_REVIEW_GITHUB_API. An empty value means the caller keeps the public
// API. A set value is used only when it is an http(s) URL on 127.0.0.1, ::1,
// or localhost. Anything else is an error, so a bearer token is not sent to
// an arbitrary host. Secret reexec leaves both variables in the environment;
// this check is what keeps that from being an open redirect.
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
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return raw, nil
	default:
		return "", fmt.Errorf("%s host %q is not loopback; use 127.0.0.1, ::1, or localhost", envName, u.Hostname())
	}
}
