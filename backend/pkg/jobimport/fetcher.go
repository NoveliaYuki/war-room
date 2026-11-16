package jobimport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	fetchTimeout   = 10 * time.Second
	maxRedirects   = 3
	maxPageBytes   = 3 << 20
	maxBoardBytes  = 20 << 20
	fetchUserAgent = "WarRoomJobImport/1.0"
	// Each request names one format: Workday answers a page URL with JSON when
	// the Accept header allows it, which hides the page's structured data.
	acceptPage = "text/html,application/xhtml+xml"
	acceptJSON = "application/json"
)

// blockedPrefixes lists non-public ranges that netip's classifiers miss.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

// isPublicAddr reports whether addr is safe for a user-supplied fetch.
func isPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// fetcher performs bounded GET requests to user-supplied public URLs.
type fetcher struct {
	client    *http.Client
	allowPort string
}

// newFetcher builds the outbound client. The address check runs on every dial
// after DNS resolution, so rebinding and redirects cannot reach internal hosts.
func newFetcher(allowAddr func(netip.Addr) bool, allowPort string) *fetcher {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			return checkDialAddress(address, allowAddr)
		},
	}
	f := &fetcher{allowPort: allowPort}
	f.client = &http.Client{
		Timeout: fetchTimeout,
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         dialer.DialContext,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 5 * time.Second,
			MaxIdleConns:        16,
			IdleConnTimeout:     30 * time.Second,
		},
		CheckRedirect: f.checkRedirect,
	}
	return f
}

func checkDialAddress(address string, allowAddr func(netip.Addr) bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ErrBlockedTarget
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !allowAddr(addr) {
		return ErrBlockedTarget
	}
	return nil
}

func (f *fetcher) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errTooManyRedirects
	}
	return f.validateTarget(req.URL)
}

// validateTarget rejects URLs that are not plain public HTTPS addresses.
func (f *fetcher) validateTarget(target *url.URL) error {
	host := target.Hostname()
	if target.Scheme != "https" || target.User != nil || host == "" {
		return ErrBlockedTarget
	}
	if port := target.Port(); port != "" && port != f.allowPort {
		return ErrBlockedTarget
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return ErrBlockedTarget
	}
	if f.allowPort == "443" && !strings.Contains(host, ".") {
		return ErrBlockedTarget
	}
	return nil
}

// response is one size-bounded upstream body.
type response struct {
	body        []byte
	contentType string
	finalURL    *url.URL
}

// get fetches target, reading at most limit bytes of decompressed content.
func (f *fetcher) get(ctx context.Context, target *url.URL, accept string, limit int64) (*response, error) {
	if err := f.validateTarget(target); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, ErrBlockedTarget
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	req.Header.Set("Accept", accept)
	res, err := f.client.Do(req) // #nosec G704 -- validateTarget and the dial check enforce public HTTPS targets.
	if err != nil {
		return nil, classifyTransportError(err)
	}
	defer func() { _ = res.Body.Close() }()
	if err := statusError(res.StatusCode); err != nil {
		return nil, err
	}
	contentType, err := acceptedContentType(res.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, classifyTransportError(err)
	}
	if int64(len(body)) > limit {
		return nil, newError(CodeUnsupported, "That page is too large to import.")
	}
	return &response{body: body, contentType: contentType, finalURL: res.Request.URL}, nil
}

func acceptedContentType(header string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return "", newError(CodeUnsupported, "That link does not point to a job page.")
	}
	switch mediaType {
	case "text/html", "application/xhtml+xml", "application/json":
		return mediaType, nil
	}
	return "", newError(CodeUnsupported, "That link does not point to a job page.")
}

func statusError(status int) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusNotFound || status == http.StatusGone:
		return newError(CodeNotFound, "That posting is no longer available.")
	case status == http.StatusUnauthorized, status == http.StatusForbidden,
		status == http.StatusTooManyRequests, status == http.StatusServiceUnavailable:
		return newError(CodeBlocked, "That site does not allow automatic imports.")
	}
	return newError(CodeUnsupported, fmt.Sprintf("That site answered with status %d.", status))
}

func classifyTransportError(err error) error {
	switch {
	case errors.Is(err, ErrBlockedTarget):
		return ErrBlockedTarget
	case errors.Is(err, errTooManyRedirects):
		return newError(CodeUnsupported, "That link redirects too many times.")
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return newError(CodeTimeout, "That site took too long to respond.")
	}
	return newError(CodeBlocked, "That site could not be reached.")
}

func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}
