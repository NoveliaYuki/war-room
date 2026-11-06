package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"war-room/backend/pkg/service"
)

type fakeAPITokenStore struct {
	lifetime time.Duration
	revoked  string
	err      error
	tokens   []service.APIToken
}

func (f *fakeAPITokenStore) CreateAPIToken(_, _ string, lifetime time.Duration) (string, service.APIToken, error) {
	f.lifetime = lifetime
	return "wr_secret", service.APIToken{ID: "token-1", ExpiresAt: time.Unix(0, 0)}, f.err
}

func (f *fakeAPITokenStore) ListAPITokens(string) ([]service.APIToken, error) { return f.tokens, f.err }

func (f *fakeAPITokenStore) RevokeAPIToken(_, id string) error {
	f.revoked = id
	return f.err
}

func runCommand(store apiTokenStore, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := runAPITokenCommand(store, args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestAPITokenCreateWritesOnlyTheTokenToStdout(t *testing.T) {
	store := &fakeAPITokenStore{}
	code, stdout, stderr := runCommand(store, "create", "--email", "a@example.com", "--days", "7")
	if code != 0 || stdout != "wr_secret\n" || strings.Contains(stderr, "wr_secret") {
		t.Fatalf("code=%d stdout=%q stderr leaks token=%t", code, stdout, strings.Contains(stderr, "wr_secret"))
	}
	if store.lifetime != 7*24*time.Hour {
		t.Fatalf("lifetime = %v, want 7 days", store.lifetime)
	}
}

func TestAPITokenListAndRevoke(t *testing.T) {
	used := time.Unix(10, 0)
	store := &fakeAPITokenStore{tokens: []service.APIToken{{ID: "a", Name: "laptop"}, {ID: "b", LastUsedAt: &used}}}
	code, stdout, _ := runCommand(store, "list", "--email", "a@example.com")
	if code != 0 || !strings.Contains(stdout, "last_used=never") || !strings.Contains(stdout, "last_used=1970-01-01T00:00:10Z") {
		t.Fatalf("list code=%d output=%q", code, stdout)
	}
	if code, _, _ := runCommand(store, "revoke", "--email", "a@example.com", "--id", "a"); code != 0 || store.revoked != "a" {
		t.Fatalf("revoke code=%d revoked=%q", code, store.revoked)
	}
}

func TestAPITokenCommandRejectsBadUsageAndReportsErrors(t *testing.T) {
	for _, args := range [][]string{{}, {"create"}, {"rotate", "--email", "a@example.com"}, {"list", "--unknown"}} {
		if code, _, _ := runCommand(&fakeAPITokenStore{}, args...); code != 2 {
			t.Fatalf("args %v exit code = %d, want 2", args, code)
		}
	}
	failing := &fakeAPITokenStore{err: errors.New("boom")}
	for _, args := range [][]string{
		{"create", "--email", "a@example.com"},
		{"list", "--email", "a@example.com"},
		{"revoke", "--email", "a@example.com", "--id", "a"},
		{"revoke", "--email", "a@example.com"},
	} {
		if code, stdout, _ := runCommand(failing, args...); code != 1 || stdout != "" {
			t.Fatalf("args %v code=%d stdout=%q, want 1 with no stdout", args, code, stdout)
		}
	}
}
