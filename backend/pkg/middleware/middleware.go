// Package middleware provides HTTP security, CORS, and request logging middleware.
package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"war-room/backend/pkg/logging"
)

// Middleware represents an HTTP middleware function.
type Middleware func(http.Handler) http.Handler

// Chain combines multiple middlewares onto a final http.Handler.
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// Logger is an alias for RequestLogger.
var Logger = RequestLogger

// SecurityHeaders attaches production security headers to all responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// CORS attaches allowed-origin headers and rejects disallowed origins before handlers run.
func CORS(allowedOrigins string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := isOriginAllowed(allowedOrigins, origin)
			setCORSOriginHeaders(w, allowedOrigins, origin, allowed)
			if origin != "" && !allowed {
				logging.RecordError(w, fmt.Errorf("cors: origin %q is not allowed", origin))
				http.Error(w, "origin is not allowed", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodOptions {
				handleCORSPreflight(w, origin, allowed)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isOriginAllowed(allowedOrigins, origin string) bool {
	if allowedOrigins == "*" {
		return true
	}
	for _, candidate := range strings.Split(allowedOrigins, ",") {
		if origin != "" && strings.TrimSpace(candidate) == origin {
			return true
		}
	}
	return false
}

func setCORSOriginHeaders(w http.ResponseWriter, allowedOrigins, origin string, allowed bool) {
	if origin != "" {
		w.Header().Add("Vary", "Origin")
		if allowed {
			if allowedOrigins == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
		}
		return
	}
	if allowedOrigins == "*" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
}

func handleCORSPreflight(w http.ResponseWriter, origin string, allowed bool) {
	if origin != "" && !allowed {
		http.Error(w, "origin is not allowed", http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, X-Requested-With")
	w.WriteHeader(http.StatusNoContent)
}

// RequestIDHeader carries the request ID that locates a request's log record.
const RequestIDHeader = "X-Request-ID"

// maxLoggedHeaderLength bounds client-controlled header values in log records.
const maxLoggedHeaderLength = 256

// quietPaths are polled continuously; their successful requests log at debug.
var quietPaths = map[string]bool{"/api/health": true, "/api/jobs/counts": true}

// RequestLogger assigns each request an ID, exposes it in the X-Request-ID
// response header, and writes one structured record when the request ends.
// The record carries every error handlers recorded, so a failed request is
// fully explained by a single log line. The query string is never logged:
// OAuth callbacks carry codes there and lookups carry user data.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		req := logging.NewRequest()
		w.Header().Set(RequestIDHeader, req.ID)
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK, request: req}
		next.ServeHTTP(rw, r.WithContext(logging.WithRequest(r.Context(), req)))
		logRequest(r, rw, time.Since(start))
	})
}

func logRequest(r *http.Request, rw *responseWriter, duration time.Duration) {
	attrs := []slog.Attr{
		slog.String("request_id", rw.request.ID),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", rw.status),
		slog.Float64("duration_ms", float64(duration.Microseconds())/1000),
		slog.Int64("bytes_in", r.ContentLength),
		slog.Int64("bytes_out", rw.written),
		slog.String("remote_addr", r.RemoteAddr),
	}
	attrs = appendHeaderAttr(attrs, "forwarded_for", r.Header.Get("X-Forwarded-For"))
	attrs = appendHeaderAttr(attrs, "user_agent", r.Header.Get("User-Agent"))
	if userID := rw.request.UserID(); userID != "" {
		attrs = append(attrs, slog.String("user_id", userID))
	}
	if err := rw.request.Err(); err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	attrs = append(attrs, rw.request.Attrs()...)
	slog.Default().LogAttrs(context.Background(), requestLogLevel(r.URL.Path, rw.status), "http request", attrs...)
}

func appendHeaderAttr(attrs []slog.Attr, key, value string) []slog.Attr {
	if value == "" {
		return attrs
	}
	if len(value) > maxLoggedHeaderLength {
		value = value[:maxLoggedHeaderLength]
	}
	return append(attrs, slog.String(key, value))
}

func requestLogLevel(path string, status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status == http.StatusUnauthorized && path == "/api/auth/me":
		// The frontend probes this endpoint to decide whether to show sign-in.
		return slog.LevelInfo
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	case quietPaths[path]:
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

// Recoverer converts a handler panic into a logged error with its stack and
// a generic 500 response, so one bad request cannot leak internals or kill
// the connection without a trace.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			logging.FromContext(r.Context()).Error("handler panic",
				"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()), "path", r.URL.Path)
			logging.RecordError(w, fmt.Errorf("panic: %v", recovered))
			writeJSONError(w, http.StatusInternalServerError, "Internal server error")
		}()
		next.ServeHTTP(w, r)
	})
}

// writeJSONError writes a generic error body that includes the request ID.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	body := map[string]string{"error": message}
	if req := logging.RequestFromWriter(w); req != nil {
		body["request_id"] = req.ID
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type responseWriter struct {
	http.ResponseWriter
	status      int
	written     int64
	wroteHeader bool
	request     *logging.Request
}

func (rw *responseWriter) WriteHeader(status int) {
	if !rw.wroteHeader {
		rw.status, rw.wroteHeader = status, true
	}
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(body []byte) (int, error) {
	rw.wroteHeader = true
	n, err := rw.ResponseWriter.Write(body)
	rw.written += int64(n)
	return n, err
}

// LogRequest implements logging.RequestCarrier.
func (rw *responseWriter) LogRequest() *logging.Request { return rw.request }

// Unwrap lets http.ResponseController reach the underlying writer.
func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }
