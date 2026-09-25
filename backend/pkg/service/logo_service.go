package service

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"war-room/backend/pkg/config"
)

// LogoService caches company favicons fetched from the configured endpoint.
type LogoService struct {
	cfg        *config.Config
	httpClient *http.Client
	logoURL    LogoURLBuilder
	mu         sync.Mutex
}

var validLogoHost = regexp.MustCompile(`^(?i)[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

// LogoURLBuilder builds the remote URL used to fetch a company logo.
type LogoURLBuilder func(domain string) string

// NewLogoService creates a logo service with the default favicon endpoint.
func NewLogoService(cfg *config.Config) *LogoService {
	return NewLogoServiceWithClient(cfg, nil, nil)
}

// NewLogoServiceWithClient creates a logo service with injectable HTTP and URL dependencies.
func NewLogoServiceWithClient(cfg *config.Config, client *http.Client, logoURL LogoURLBuilder) *LogoService {
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	if logoURL == nil {
		logoURL = func(domain string) string {
			return fmt.Sprintf("https://www.google.com/s2/favicons?domain=%s&sz=128", url.QueryEscape(domain))
		}
	}
	return &LogoService{
		cfg:        cfg,
		httpClient: client,
		logoURL:    logoURL,
	}
}

// DeduceDomain resolves an explicit company host or infers one from the company name.
func (s *LogoService) DeduceDomain(companyName, explicitDomain string) string {
	if exp := strings.TrimSpace(explicitDomain); exp != "" {
		return normalizeLogoHost(exp)
	}

	norm := strings.ToLower(strings.TrimSpace(companyName))

	suffixes := []string{" technologies", " technology", " inc", " corp", " ltd", " llc", " s.l.", " sl", " gmbh", " sa"}
	cleaned := norm
	for _, suf := range suffixes {
		cleaned = strings.TrimSuffix(cleaned, suf)
	}
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	if len(cleaned) > 2 {
		return cleaned + ".com"
	}
	return ""
}

// normalizeLogoHost validates a hostname before it is used in a filesystem path.
func normalizeLogoHost(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" {
		parsed, err = url.Parse("https://" + value)
		if err != nil {
			return ""
		}
	}
	if !validLogoURL(parsed) {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if !validLogoHostname(host) {
		return ""
	}
	return host
}

func validLogoURL(parsed *url.URL) bool {
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validLogoHostname(host string) bool {
	if host == "" || len(host) > 253 || net.ParseIP(host) != nil || !validLogoHost.MatchString(host) {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

// GetLogo returns a cached logo or fetches and caches one for the company.
func (s *LogoService) GetLogo(companyName, explicitDomain string) ([]byte, string, error) {
	normCompany := strings.ToLower(strings.TrimSpace(companyName))
	if normCompany == "unknown" || normCompany == "" {
		return nil, "", errors.New("unknown company logo not tracked")
	}

	domain := s.DeduceDomain(companyName, explicitDomain)
	if domain == "" {
		return nil, "", errors.New("could not deduce company domain")
	}

	// Check the local cache before making a remote request.
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, mimeType, err := s.readCachedLogo(domain); err == nil {
		return data, mimeType, nil
	}
	if !s.cfg.LogoLookupEnabled {
		return nil, "", errors.New("remote logo lookup is disabled")
	}
	return s.fetchLogo(domain)
}

func (s *LogoService) readCachedLogo(domain string) ([]byte, string, error) {
	formats := []struct{ extension, mimeType string }{
		{".png", "image/png"}, {".svg", "image/svg+xml"}, {".jpg", "image/jpeg"}, {".webp", "image/webp"},
	}
	for _, format := range formats {
		// domain is normalized to a validated DNS hostname before this path is constructed.
		data, err := os.ReadFile(filepath.Join(s.cfg.LogosDir, domain+format.extension)) // #nosec G304
		if err == nil {
			return data, format.mimeType, nil
		}
	}
	return nil, "", os.ErrNotExist
}

func (s *LogoService) fetchLogo(domain string) ([]byte, string, error) {
	resp, err := s.httpClient.Get(s.logoURL(domain))
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, "", fmt.Errorf("remote logo fetch failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	const maxLogoBytes = 5 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLogoBytes+1))
	if err != nil || len(body) == 0 {
		return nil, "", errors.New("empty logo body")
	}
	if len(body) > maxLogoBytes {
		return nil, "", errors.New("logo response exceeds size limit")
	}
	mimeType := http.DetectContentType(body[:min(len(body), 512)])
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, "", errors.New("remote logo response is not an image")
	}
	extension := logoExtensionForMimeType(mimeType)
	if extension == "" {
		return nil, "", errors.New("unsupported logo image type")
	}

	// Cache the bounded response; a cache failure does not invalidate the fetch.
	cachePath := filepath.Join(s.cfg.LogosDir, domain+extension)
	_ = os.WriteFile(cachePath, body, 0600)

	return body, mimeType, nil
}

func logoExtensionForMimeType(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/svg+xml":
		return ".svg"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}
