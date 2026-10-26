package middleware

import (
	"context"
	"net/http"
	"strings"
)

type contextKey int

const userIDContextKey contextKey = iota

// SessionAuthenticator validates a request's session cookie and reports the
// signed-in user. Implemented by service.AuthService.
type SessionAuthenticator interface {
	Authenticate(w http.ResponseWriter, r *http.Request) (userID string, ok bool)
}

// publicAPIPaths lists /api/* routes that must work without a session: the
// health probe, and the Google OAuth entry points that establish a session
// in the first place. handleTestSession is added to this list only when the
// caller explicitly includes it (config.TestMode), never unconditionally.
var publicAPIPaths = map[string]bool{
	"/api/health":          true,
	"/api/google/connect":  true,
	"/api/google/callback": true,
}

// RequireSession rejects unauthenticated requests to protected /api/* routes
// with 401 and attaches the signed-in user's ID to the request context for
// authenticated ones. Non-API paths (static assets, the SPA shell) and the
// paths in extraPublicPaths (in addition to the fixed public set) pass
// through unconditionally; the frontend decides what to render for a signed
// out visitor by calling /api/auth/me, not by the backend gating page loads.
func RequireSession(auth SessionAuthenticator, extraPublicPaths ...string) Middleware {
	public := publicAPIPaths
	if len(extraPublicPaths) > 0 {
		public = make(map[string]bool, len(publicAPIPaths)+len(extraPublicPaths))
		for path := range publicAPIPaths {
			public[path] = true
		}
		for _, path := range extraPublicPaths {
			public[path] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") || public[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			if isUnsafeMethod(r.Method) && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"cross-site requests are not allowed"}`))
				return
			}
			userID, ok := auth.Authenticate(w, r)
			if !ok {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"sign in required"}`))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDContextKey, userID)))
		})
	}
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

// UserIDFromContext returns the signed-in user's ID attached by RequireSession.
func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey).(string)
	return userID, ok
}

// ContextWithUserID attaches userID the same way RequireSession does. It
// exists for tests that call a handler function directly (bypassing the
// middleware chain) and need to simulate an authenticated request.
func ContextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}
