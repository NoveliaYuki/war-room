package e2e_test

import (
	"testing"

	"github.com/mxschmitt/playwright-go"
)

const (
	importURLInput   = "#job-import-url"
	importSubmit     = "#job-import-submit"
	importStatus     = "#job-import-status"
	importFormModal  = "#new-process-form"
	importedCardRole = "Senior Platform Engineer"
)

// TestJobImportFillsFormInDemo drives the whole form against the demo adapter,
// because the isolated test stack cannot reach a real job board.
func TestJobImportFillsFormInDemo(t *testing.T) {
	page := newPage(t)
	setPageViewport(t, page, 1280, 800)
	openDemoPage(t, page)
	clickLocator(t, page.Locator("#btn-new-process"), "open the new process form")
	assertEntryStep(t, page)
	assertFormFitsWidth(t, page, []int{1280, 700, 600, 390, 320})
	setPageViewport(t, page, 1280, 800)
	fill(t, page.Locator(importURLInput), "https://careers.example/jobs/1")
	clickLocator(t, page.Locator(importSubmit), "import the posting")
	waitForLocator(t, page.Locator(importStatus+`[data-tone="success"]`), "import success message")
	assertFieldValue(t, page, "position_title", importedCardRole)
	assertFieldValue(t, page, "salary_min", "70000")
	assertFieldValue(t, page, "salary_currency", "EUR")
	waitForLocator(t, page.Locator(".job-import-technology input:checked").First(), "a selected technology")
	assertFormFitsWidth(t, page, []int{1280, 600, 390, 320})
	setPageViewport(t, page, 1280, 800)
	fill(t, page.Locator(importFormModal+` [name="position_title"]`), importedCardRole+" II")
	clickLocator(t, page.Locator(importFormModal+` button[type="submit"]`), "create the process")
	waitForLocator(t, page.Locator(".process-card", playwright.PageLocatorOptions{HasText: importedCardRole}).First(), "imported card")
}

// TestLinkedInLinkGoesToPaste uses the real backend: LinkedIn is refused
// before any request leaves, so this needs no network.
func TestLinkedInLinkGoesToPaste(t *testing.T) {
	page := newPage(t)
	setPageViewport(t, page, 1280, 800)
	waitForApplicationReady(t, page)
	clickLocator(t, page.Locator("#btn-new-process"), "open the new process form")
	const link = "https://www.linkedin.com/jobs/view/123"
	fill(t, page.Locator(importURLInput), link)
	clickLocator(t, page.Locator(importSubmit), "import the posting")
	waitForLocator(t, page.Locator(importStatus+`[data-tone="warning"]`), "paste guidance")
	assertFieldValue(t, page, "job_post_url", link)
}

// assertEntryStep proves nothing shows the form or saves before a choice.
func assertEntryStep(t *testing.T, page playwright.Page) {
	t.Helper()
	waitForLocator(t, page.Locator("#process-entry-manual"), "fill-in-by-hand option")
	visible, err := page.Locator(importFormModal).IsVisible()
	if err != nil {
		t.Fatalf("read form visibility: %v", err)
	}
	if visible {
		t.Fatal("form is visible before choosing how to start")
	}
}

func assertFieldValue(t *testing.T, page playwright.Page, name, want string) {
	t.Helper()
	got, err := page.Locator(importFormModal + ` [name="` + name + `"]`).InputValue()
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}

func assertFormFitsWidth(t *testing.T, page playwright.Page, widths []int) {
	t.Helper()
	for _, width := range widths {
		setPageViewport(t, page, width, 800)
		overflow, err := page.Evaluate(`() => {
			const modal = document.querySelector("#detail-modal");
			const right = modal.getBoundingClientRect().right;
			const row = document.querySelector(".job-import-row");
			const rowFits = !row.offsetWidth || row.getBoundingClientRect().right <= right;
			return modal.scrollWidth > modal.clientWidth || !rowFits;
		}`)
		if err != nil {
			t.Fatalf("measure form at %d: %v", width, err)
		}
		if overflow != false {
			t.Fatalf("new process form overflows at %dpx", width)
		}
	}
}
