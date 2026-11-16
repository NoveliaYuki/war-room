package jobimport

import (
	"net/url"
	"strings"
)

// pasteOnlyHosts forbid or block automated reads, so no request is ever sent.
var pasteOnlyHosts = []string{"linkedin.com", "indeed.com", "glassdoor.com"}

var trackingParams = map[string]bool{
	"gclid": true, "fbclid": true, "msclkid": true, "ref": true, "referrer": true,
	"source": true, "src": true, "trk": true, "lever-origin": true, "lever-source": true,
}

// Canonicalize normalizes a posting URL so the same posting compares equal.
func Canonicalize(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return nil, ErrBlockedTarget
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	query := parsed.Query()
	for key := range query {
		if trackingParams[strings.ToLower(key)] || strings.HasPrefix(strings.ToLower(key), "utm_") {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed, nil
}

func isPasteOnly(host string) bool {
	host = strings.ToLower(host)
	for _, blocked := range pasteOnlyHosts {
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}
	return false
}

// domainFrom returns the host of a company website, ignoring social profiles
// that would give the card the wrong logo.
func domainFrom(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	if !strings.Contains(host, ".") || isPasteOnly(host) {
		return ""
	}
	return host
}
