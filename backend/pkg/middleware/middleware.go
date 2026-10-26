// Package middleware provides HTTP security, CORS, and request logging middleware.
package middleware

import (
	"log"
	"net/http"
	"strings"
	"time"
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

// RequestLogger logs incoming HTTP requests with timing.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)
		if r.URL.Path != "/api/jobs/counts" {
			log.Printf("%d %v", rw.status, duration)
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}
