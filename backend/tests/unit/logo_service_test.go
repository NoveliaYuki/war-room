package backend_test

import (
	"os"
	"path/filepath"
	"testing"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/service"
)

func TestDeduceDomain(t *testing.T) {
	cfg := &config.Config{LogosDir: t.TempDir()}
	svc := service.NewLogoService(cfg)
	got := svc.DeduceDomain("Example Company", "")
	if got == "" {
		t.Errorf("Expected a domain, got empty")
	}
}

func TestDeduceDomainRejectsUnsafeExplicitDomains(t *testing.T) {
	cfg := &config.Config{LogosDir: t.TempDir()}
	svc := service.NewLogoService(cfg)
	for _, domain := range []string{"../../outside", "https://test@example.invalid", "javascript:alert(1)", "bad host.test", "example.com?x=1"} {
		if got := svc.DeduceDomain("Acme", domain); got != "" {
			t.Errorf("DeduceDomain(%q) = %q, want empty", domain, got)
		}
	}

	outside := filepath.Join(filepath.Dir(cfg.LogosDir), "outside.png")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.GetLogo("Acme", "../outside"); err == nil {
		t.Fatal("GetLogo accepted a path traversal domain")
	}
}
