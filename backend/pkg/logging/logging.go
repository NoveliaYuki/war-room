// Package logging configures structured server logs and carries per-request
// diagnostic context.
//
// Logs are the operator's view of the system: they hold the full error
// chain, upstream reasons, and audit events. HTTP clients only ever receive
// a generic message plus the request ID that locates the matching log line.
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

// Redacted replaces the value of any attribute whose key names a secret.
const Redacted = "[REDACTED]"

// sensitiveKeyParts marks attribute keys whose values must never be written,
// even if a caller logs them by mistake.
var sensitiveKeyParts = []string{
	"token", "secret", "password", "passwd", "authorization", "cookie",
	"api_key", "apikey", "encryption_key", "private_key", "credential", "session_id",
}

// sensitiveKeys are exact keys that are secret only as whole names (an OAuth
// "code" or "state"), so substrings such as "status_code" stay visible.
var sensitiveKeys = map[string]bool{"code": true, "state": true, "oauth_code": true, "oauth_state": true}

// ParseLevel converts a LOG_LEVEL value (debug, info, warn, error) to a slog level.
func ParseLevel(value string) (slog.Level, error) {
	var level slog.Level
	if strings.TrimSpace(value) == "" {
		return slog.LevelInfo, nil
	}
	if err := level.UnmarshalText([]byte(strings.TrimSpace(value))); err != nil {
		return slog.LevelInfo, fmt.Errorf("invalid log level %q: use debug, info, warn, or error", value)
	}
	return level, nil
}

// New builds a redacting logger that writes JSON or text records to w.
func New(w io.Writer, level slog.Level, format string) (*slog.Logger, error) {
	options := &slog.HandlerOptions{AddSource: true, Level: level, ReplaceAttr: redact}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		return slog.New(slog.NewJSONHandler(w, options)), nil
	case "text":
		return slog.New(slog.NewTextHandler(w, options)), nil
	default:
		return nil, fmt.Errorf("invalid log format %q: use json or text", format)
	}
}

// Setup installs a logger built by New as the process default. The standard
// library log package is routed through it as well.
func Setup(w io.Writer, level, format string) error {
	parsedLevel, levelErr := ParseLevel(level)
	logger, formatErr := New(w, parsedLevel, format)
	if formatErr != nil {
		logger, _ = New(w, parsedLevel, "json")
	}
	slog.SetDefault(logger)
	log.SetFlags(0)
	return errors.Join(levelErr, formatErr)
}

func redact(_ []string, attr slog.Attr) slog.Attr {
	if IsSensitiveKey(attr.Key) {
		return slog.String(attr.Key, Redacted)
	}
	return attr
}

// IsSensitiveKey reports whether key names a value that must not be logged.
func IsSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	if sensitiveKeys[lower] {
		return true
	}
	for _, part := range sensitiveKeyParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

// Request collects diagnostic fields for one HTTP request. Handlers and
// middleware add to it, and the request logger writes it as one record.
type Request struct {
	// ID identifies the request in logs, the X-Request-ID header, and error bodies.
	ID string

	mu     sync.Mutex
	userID string
	errs   []error
	attrs  []slog.Attr
}

// NewRequest returns a Request with a fresh random ID.
func NewRequest() *Request {
	return &Request{ID: NewID()}
}

// NewID returns a 16-character random hex identifier.
func NewID() string {
	var raw [8]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(raw[:])
}

// SetUserID records the signed-in user that made the request.
func (r *Request) SetUserID(userID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userID = userID
}

// UserID returns the user recorded by SetUserID.
func (r *Request) UserID() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.userID
}

// RecordError keeps err for the request's log record.
func (r *Request) RecordError(err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
}

// Err returns every recorded error joined, or nil.
func (r *Request) Err() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return errors.Join(r.errs...)
}

// AddAttrs attaches extra fields to the request's log record.
func (r *Request) AddAttrs(attrs ...slog.Attr) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attrs = append(r.attrs, attrs...)
}

// Attrs returns a copy of the fields added with AddAttrs.
func (r *Request) Attrs() []slog.Attr {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]slog.Attr(nil), r.attrs...)
}

type requestContextKey struct{}

// WithRequest attaches req to ctx.
func WithRequest(ctx context.Context, req *Request) context.Context {
	return context.WithValue(ctx, requestContextKey{}, req)
}

// RequestFrom returns the Request attached to ctx, or nil.
func RequestFrom(ctx context.Context) *Request {
	req, _ := ctx.Value(requestContextKey{}).(*Request)
	return req
}

// RequestCarrier is implemented by response writers that belong to a logged
// request, so code holding only the writer can still reach its Request.
type RequestCarrier interface {
	LogRequest() *Request
}

// RequestFromWriter returns the Request behind w, or nil.
func RequestFromWriter(w http.ResponseWriter) *Request {
	carrier, ok := w.(RequestCarrier)
	if !ok {
		return nil
	}
	return carrier.LogRequest()
}

// RecordError attaches err to the request behind w. When w is not part of a
// logged request, err is logged immediately so it is never lost.
func RecordError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if req := RequestFromWriter(w); req != nil {
		req.RecordError(err)
		return
	}
	slog.Error("request error outside a logged request", "error", err.Error())
}

// FromContext returns the default logger annotated with the request ID and
// user ID carried by ctx.
func FromContext(ctx context.Context) *slog.Logger {
	logger := slog.Default()
	req := RequestFrom(ctx)
	if req == nil {
		return logger
	}
	logger = logger.With("request_id", req.ID)
	if userID := req.UserID(); userID != "" {
		logger = logger.With("user_id", userID)
	}
	return logger
}

// Audit writes a security or data-lifecycle event. Audit records always log
// at info level or above and carry audit=true so they can be filtered.
func Audit(ctx context.Context, event string, attrs ...any) {
	FromContext(ctx).Info("audit: "+event, append([]any{"audit", true, "event", event}, attrs...)...)
}

// AuditFailure writes a failed or denied audit event at warning level.
func AuditFailure(ctx context.Context, event string, attrs ...any) {
	FromContext(ctx).Warn("audit: "+event, append([]any{"audit", true, "event", event, "outcome", "failure"}, attrs...)...)
}
