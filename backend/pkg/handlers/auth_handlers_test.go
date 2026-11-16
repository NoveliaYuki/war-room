package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/service"
)

func TestHandleAuthMeRequiresSession(t *testing.T) {
	handler, _, _ := googleHandler(t, true)
	rec := httptest.NewRecorder()
	handler.handleAuthMe(rec, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("auth/me without session=%d", rec.Code)
	}
}

func TestHandleAuthMeReturnsSignedInProfile(t *testing.T) {
	handler, _, userID := googleHandler(t, true)
	rec := httptest.NewRecorder()
	handler.handleAuthMe(rec, withUserID(httptest.NewRequest(http.MethodGet, "/api/auth/me", nil), userID))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "person@example.com") {
		t.Fatalf("auth/me=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAuthMeRejectsUnknownUserID(t *testing.T) {
	handler, _, _ := googleHandler(t, true)
	rec := httptest.NewRecorder()
	handler.handleAuthMe(rec, withUserID(httptest.NewRequest(http.MethodGet, "/api/auth/me", nil), "no-such-user"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("auth/me for unknown user=%d", rec.Code)
	}
}

func TestHandleSignOutClearsOnlyThisSession(t *testing.T) {
	handler, drive, _ := fakeGoogleHandler(t)
	userID := connectFakeGoogle(t, handler, drive)

	issueRecorder := httptest.NewRecorder()
	if err := handler.auth.IssueSession(issueRecorder, userID); err != nil {
		t.Fatal(err)
	}
	var secondDeviceCookie *http.Cookie
	for _, cookie := range issueRecorder.Result().Cookies() {
		secondDeviceCookie = cookie
	}
	if secondDeviceCookie == nil {
		t.Fatal("second IssueSession did not set a cookie")
	}

	signOutRequest := httptest.NewRequest(http.MethodPost, "/api/auth/signout", nil)
	signOutRequest.AddCookie(secondDeviceCookie)
	signOutRecorder := httptest.NewRecorder()
	handler.handleSignOut(signOutRecorder, signOutRequest)
	if signOutRecorder.Code != http.StatusOK {
		t.Fatalf("sign out=%d", signOutRecorder.Code)
	}

	// The device that signed out is now unauthenticated...
	afterSignOut := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	afterSignOut.AddCookie(secondDeviceCookie)
	if _, ok := handler.auth.Authenticate(httptest.NewRecorder(), afterSignOut); ok {
		t.Fatal("session survived handleSignOut")
	}
	// ...but the original sign-in's own session (and the account) is untouched.
	profileRec := httptest.NewRecorder()
	handler.handleAuthMe(profileRec, withUserID(httptest.NewRequest(http.MethodGet, "/api/auth/me", nil), userID))
	if profileRec.Code != http.StatusOK {
		t.Fatalf("unrelated session was affected by sign-out: %d", profileRec.Code)
	}
}

func TestHandleSignOutWithoutAuthServiceIsUnavailable(t *testing.T) {
	handler := NewHandler(nil, nil, &config.Config{})
	rec := httptest.NewRecorder()
	handler.handleSignOut(rec, httptest.NewRequest(http.MethodPost, "/api/auth/signout", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("sign out without auth service=%d", rec.Code)
	}
}

func TestHandleTestSessionOnlyRegisteredInTestMode(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, TestMode: true, AuthAllowedEmails: []string{"allowed@example.com"}}
	db := googleTestDB(t, dir)
	handler := NewHandler(nil, nil, cfg)
	handler.SetAuthService(service.NewAuthService(db, cfg))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/test/session", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("test session registration=%d body=%s", rec.Code, rec.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		sessionCookie = cookie
	}
	if sessionCookie == nil {
		t.Fatal("test session did not set a cookie")
	}
	meRequest := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meRequest.AddCookie(sessionCookie)
	if _, ok := handler.auth.Authenticate(httptest.NewRecorder(), meRequest); !ok {
		t.Fatal("test session cookie did not authenticate")
	}

	productionCfg := &config.Config{DataDir: dir}
	productionHandler := NewHandler(nil, nil, productionCfg)
	productionMux := http.NewServeMux()
	productionHandler.RegisterRoutes(productionMux)
	notFound := httptest.NewRecorder()
	productionMux.ServeHTTP(notFound, httptest.NewRequest(http.MethodPost, "/api/test/session", nil))
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("test session route must not exist outside test mode, got %d", notFound.Code)
	}
}

func TestHandleTestSessionDefaultsToFirstAllowedEmail(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, TestMode: true, AuthAllowedEmails: []string{"default@example.com"}}
	db := googleTestDB(t, dir)
	handler := NewHandler(nil, nil, cfg)
	handler.SetAuthService(service.NewAuthService(db, cfg))
	rec := httptest.NewRecorder()
	handler.handleTestSession(rec, httptest.NewRequest(http.MethodPost, "/api/test/session", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("test session default email=%d body=%s", rec.Code, rec.Body.String())
	}
	var email string
	if err := db.QueryRow(`SELECT email FROM users`).Scan(&email); err != nil || email != "default@example.com" {
		t.Fatalf("test session user email=%q err=%v", email, err)
	}
}

func TestHandleTestSessionWithoutAuthServiceIsUnavailable(t *testing.T) {
	handler := NewHandler(nil, nil, &config.Config{TestMode: true})
	rec := httptest.NewRecorder()
	handler.handleTestSession(rec, httptest.NewRequest(http.MethodPost, "/api/test/session", strings.NewReader(`{}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("test session without auth service=%d", rec.Code)
	}
}

func TestHandleSignOutReturns500WhenSessionDeleteFails(t *testing.T) {
	handler, drive, _ := fakeGoogleHandler(t)
	userID := connectFakeGoogle(t, handler, drive)
	db := googleHandlerDB(t, handler)

	issueRecorder := httptest.NewRecorder()
	if err := handler.auth.IssueSession(issueRecorder, userID); err != nil {
		t.Fatal(err)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range issueRecorder.Result().Cookies() {
		sessionCookie = cookie
	}
	if sessionCookie == nil {
		t.Fatal("IssueSession did not set a cookie")
	}
	if _, err := db.Exec(`DROP TABLE sessions`); err != nil {
		t.Fatal(err)
	}
	signOutRequest := httptest.NewRequest(http.MethodPost, "/api/auth/signout", nil)
	signOutRequest.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	handler.handleSignOut(rec, signOutRequest)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("sign out with broken session storage status=%d body=%s", rec.Code, rec.Body.String())
	}
}
