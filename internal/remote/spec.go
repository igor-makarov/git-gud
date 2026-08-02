package remote

import (
	"fmt"
	"net/url"
	"strings"
)

// Target identifies an HTTP(S) Git repository and the revision to inspect.
type Target struct {
	URL string
	Ref string
}

// ParseTarget parses URL[@ref]. The @ separator is recognized only in the URL
// path, so userinfo such as https://user@example.com/repo.git remains valid.
func ParseTarget(value string) (Target, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Target{}, fmt.Errorf("repository URL is empty")
	}

	repositoryURL := value
	ref := ""
	schemeEnd := strings.Index(value, "://")
	pathStart := -1
	if schemeEnd >= 0 {
		if slash := strings.IndexByte(value[schemeEnd+3:], '/'); slash >= 0 {
			pathStart = schemeEnd + 3 + slash
		}
	}
	if at := strings.LastIndex(value, "@"); pathStart >= 0 && at > pathStart && at+1 < len(value) {
		repositoryURL, ref = value[:at], value[at+1:]
	}

	parsed, err := url.Parse(repositoryURL)
	if err != nil {
		return Target{}, fmt.Errorf("parse repository URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Target{}, fmt.Errorf("repository URL must use http or https")
	}
	if parsed.Host == "" || parsed.Path == "" {
		return Target{}, fmt.Errorf("repository URL must include a host and path")
	}
	if ref != "" && strings.ContainsAny(ref, "\x00\r\n") {
		return Target{}, fmt.Errorf("invalid ref")
	}

	return Target{URL: parsed.String(), Ref: ref}, nil
}
