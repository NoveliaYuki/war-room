package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeAuthenticator struct {
	userID string
	ok     bool
}

func (f fakeAuthenticator) Authenticate(http.ResponseWriter, *http.Request) (string, bool) {
	return f.userID, f.ok
}

func TestRequireSessionRejectsUnauthenticatedAPIRequests(t *testing.T) {
	handler := RequireSession(fakeAuthenticator{ok: false})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not run for an unauthenticated request")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestRequireSessionAllowsPublicAPIPathsWithoutAuth(t *testing.T) {
	for _, path := range []string{"/api/health", "/api/google/connect", "/api/google/callback"} {
		called := false
		handler := RequireSession(fakeAuthenticator{ok: false})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if !called {
			t.Fatalf("public path %q did not reach the handler", path)
		}
	}
}

func TestRequireSessionAllowsNonAPIPathsWithoutAuth(t *testing.T) {
	called := false
	handler := RequireSession(fakeAuthenticator{ok: false})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/index.html", nil))
	if !called {
		t.Fatal("static asset path did not reach the handler")
	}
}

func TestRequireSessionAttachesUserIDOnSuccess(t *testing.T) {
	var sawUserID string
	var sawOK bool
	handler := RequireSession(fakeAuthenticator{userID: "user-1", ok: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawUserID, sawOK = UserIDFromContext(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if !sawOK || sawUserID != "user-1" {
		t.Fatalf("UserIDFromContext() = (%q, %t), want (\"user-1\", true)", sawUserID, sawOK)
	}
}

func TestRequireSessionHonorsExtraPublicPaths(t *testing.T) {
	called := false
	handler := RequireSession(fakeAuthenticator{ok: false}, "/api/test/session")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/test/session", nil))
	if !called {
		t.Fatal("extra public path did not reach the handler")
	}
}

func TestUserIDFromContextReportsMissingValue(t *testing.T) {
	if _, ok := UserIDFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Fatal("UserIDFromContext reported ok for a context with no user ID")
	}
}

func TestRequireSessionRejectsCrossSiteMutations(t *testing.T) {
	handler := RequireSession(fakeAuthenticator{userID: "u1", ok: true})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	cases := []struct {
		method, site string
		want         int
	}{
		{http.MethodPost, "cross-site", http.StatusForbidden},
		{http.MethodDelete, "cross-site", http.StatusForbidden},
		{http.MethodPost, "same-origin", http.StatusNoContent},
		{http.MethodPost, "", http.StatusNoContent},
		{http.MethodGet, "cross-site", http.StatusNoContent},
	}
	for _, test := range cases {
		request := httptest.NewRequest(test.method, "/api/jobs", nil)
		if test.site != "" {
			request.Header.Set("Sec-Fetch-Site", test.site)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != test.want {
			t.Fatalf("%s with Sec-Fetch-Site=%q: status=%d, want %d", test.method, test.site, recorder.Code, test.want)
		}
	}
}
