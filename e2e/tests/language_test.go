package e2e_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// TestLanguageFollowsBrowserUntilChosen checks browser detection, an explicit
// choice that survives a reload, and a switch that needs no reload.
func TestLanguageFollowsBrowserUntilChosen(t *testing.T) {
	page := newLocalizedPage(t, "es-ES")
	openDemoPage(t, page)
	expectLocatorText(t, page.Locator("#tab-board"), "Tablero")
	expectDocumentLanguage(t, page, "es")
	if saved := savedLanguage(t, page); saved != "" {
		t.Fatalf("detected browser language should not be saved, got %q", saved)
	}

	clickLocator(t, page.Locator("#btn-account-menu"), "open account menu")
	catalan := page.Locator(`#account-menu-panel [data-language-option="ca"]`)
	clickLocator(t, catalan, "choose Catalan")
	expectLocatorText(t, page.Locator("#tab-board"), "Tauler")
	expectDocumentLanguage(t, page, "ca")
	if checked, err := catalan.GetAttribute("aria-checked"); err != nil || checked != "true" {
		t.Fatalf("Catalan option aria-checked=%q, want true (err=%v)", checked, err)
	}
	assertVisible(t, page.Locator("#account-menu-panel"))
	expectCardStatusesIn(t, page, []string{"En espera", "En curs", "Acceptat", "Rebutjat"})

	if _, err := page.Reload(); err != nil {
		t.Fatalf("reload application: %v", err)
	}
	if _, err := page.WaitForFunction("() => document.body.dataset.appReady === 'true'", nil); err != nil {
		t.Fatalf("wait for reloaded application: %v", err)
	}
	expectLocatorText(t, page.Locator("#tab-board"), "Tauler")
	if saved := savedLanguage(t, page); saved != "ca" {
		t.Fatalf("saved language=%q, want ca", saved)
	}

	clickLocator(t, page.Locator("#btn-account-menu"), "reopen account menu")
	clickLocator(t, page.Locator(`#account-menu-panel [data-language-option="en"]`), "choose English")
	expectLocatorText(t, page.Locator("#tab-board"), "Board")
	expectDocumentLanguage(t, page, "en")
}

// TestUnsupportedBrowserLanguageFallsBackToEnglish checks the English default.
func TestUnsupportedBrowserLanguageFallsBackToEnglish(t *testing.T) {
	page := newLocalizedPage(t, "de-DE")
	openDemoPage(t, page)
	expectLocatorText(t, page.Locator("#tab-board"), "Board")
	expectDocumentLanguage(t, page, "en")
}

// TestSignInScreenLanguageSelector checks the selector shown before sign-in.
func TestSignInScreenLanguageSelector(t *testing.T) {
	page, err := newBrowserContext(t, "ca-ES").NewPage()
	if err != nil {
		t.Fatalf("create browser page: %v", err)
	}
	if _, err := page.Goto(baseURL); err != nil {
		t.Fatalf("open sign-in screen: %v", err)
	}
	message := page.Locator("#sign-in-message")
	waitForLocator(t, message, "sign-in message")
	expectLocatorText(t, message, "Inicia la sessió amb el compte de Google autoritzat per continuar.")
	clickLocator(t, page.Locator(`#sign-in-screen [data-language-option="es"]`), "choose Spanish")
	expectLocatorText(t, message, "Inicia sesión con la cuenta de Google autorizada para continuar.")
	expectLocatorText(t, page.Locator("#btn-google-signin span"), "Iniciar sesión con Google")
	if pressed, err := page.Locator(`#sign-in-screen [data-language-option="es"]`).GetAttribute("aria-pressed"); err != nil || pressed != "true" {
		t.Fatalf("Spanish option aria-pressed=%q, want true (err=%v)", pressed, err)
	}
}

func expectLocatorText(t *testing.T, locator playwright.Locator, want string) {
	t.Helper()
	if err := playwright.NewPlaywrightAssertions().Locator(locator).ToHaveText(want); err != nil {
		t.Fatalf("expected text %q: %v", want, err)
	}
}

func expectDocumentLanguage(t *testing.T, page playwright.Page, want string) {
	t.Helper()
	lang, err := page.Evaluate("() => document.documentElement.lang")
	if err != nil || lang != want {
		t.Fatalf("document language=%v, want %q (err=%v)", lang, want, err)
	}
}

func savedLanguage(t *testing.T, page playwright.Page) string {
	t.Helper()
	value, err := page.Evaluate(`() => window.localStorage.getItem("war-room.language") || ""`)
	if err != nil {
		t.Fatalf("read saved language: %v", err)
	}
	return value.(string)
}

func expectCardStatusesIn(t *testing.T, page playwright.Page, allowed []string) {
	t.Helper()
	pills := page.Locator(".process-card .status-pill")
	waitForLocator(t, pills.First(), "translated status pill")
	texts, err := pills.AllTextContents()
	if err != nil || len(texts) == 0 {
		t.Fatalf("read status pills: %v (count=%d)", err, len(texts))
	}
	for _, text := range texts {
		if !slices.Contains(allowed, strings.TrimSpace(text)) {
			t.Fatalf("status pill %q is not translated (allowed %v)", text, allowed)
		}
	}
}
