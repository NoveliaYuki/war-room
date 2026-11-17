package e2e_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const noSpreadsheetChanges = "No changes found: this file matches what War Room has saved."

// TestSpreadsheetImportSavesOnlyEditedCells imports an unedited export, which
// changes nothing, then a CSV that edits one process and adds another.
func TestSpreadsheetImportSavesOnlyEditedCells(t *testing.T) {
	page := newPage(t)
	title := fmt.Sprintf("E2E Import %d", time.Now().UnixNano())
	jobID := seedWorkbookProcess(t, page, "Import Co", title, "How do you import "+title+"?")

	preview := chooseSpreadsheet(t, page, "board.xlsx", downloadWorkbookFile(t, page))
	expectPreviewText(t, preview, noSpreadsheetChanges)
	if disabled, err := page.Locator("#btn-import-spreadsheet").IsDisabled(); err != nil || !disabled {
		t.Fatalf("import button disabled = %v (err=%v), want disabled when nothing changes", disabled, err)
	}

	csv := fmt.Sprintf("Process ID,Company,Role,Status,Your expected salary\n%s,Import Co,%s,Rejected,95k\n,Import Co,%s (copy),Ongoing,\n", jobID, title, title)
	preview = chooseSpreadsheet(t, page, "board.csv", []byte(csv))
	expectPreviewText(t, preview, "1 process will be updated")
	expectPreviewText(t, preview, "1 new process will be added")
	importSpreadsheet(t, page)

	job := fetchJSON(t, page, "/api/jobs/"+jobID)
	if job["status"] != "rejected" || job["expected_salary"] != "95k" {
		t.Errorf("imported job status=%v expected_salary=%v, want rejected and 95k", job["status"], job["expected_salary"])
	}
	if overview, _ := job["company_overview"].(string); !strings.HasPrefix(overview, "## About Acme") {
		t.Errorf("company overview = %q, want the Markdown kept because its cell was not edited", overview)
	}
	added := fetchJSONList(t, page, "/api/jobs?search="+strings.ReplaceAll(title, " ", "+"))
	if !hasProcess(added, title+" (copy)", "ongoing") {
		t.Errorf("jobs matching %q = %v, want the added process with status ongoing", title, added)
	}
}

// TestSpreadsheetImportWorksInDemo exports the sample board in Spanish,
// imports it unchanged with the interface in English, then saves an edit.
func TestSpreadsheetImportWorksInDemo(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	reloadInLanguage(t, page, "es")
	spanishExport := downloadWorkbookFile(t, page)
	reloadInLanguage(t, page, "en")

	openBackupDialog(t, page)
	preview := chooseSpreadsheet(t, page, "tablero.xlsx", spanishExport)
	expectPreviewText(t, preview, noSpreadsheetChanges)

	preview = chooseSpreadsheet(t, page, "board.csv", []byte("Process ID,Status,Your expected salary\ndemo-1,Rejected,Imported in the demo\n"))
	expectPreviewText(t, preview, "1 process will be updated")
	importSpreadsheet(t, page)

	row := downloadBoardWorkbook(t, page).board.rowWhere(t, "AA", "demo-1")
	expectTexts(t, "imported demo row", row, map[string]string{"L": "Rejected", "I": "Imported in the demo"})
}

// chooseSpreadsheet picks a file in Data & Backups and waits for its preview.
func chooseSpreadsheet(t *testing.T, page playwright.Page, name string, contents []byte) playwright.Locator {
	t.Helper()
	if err := page.Locator("#data-import-file").SetInputFiles([]playwright.InputFile{{Name: name, Buffer: contents}}); err != nil {
		t.Fatalf("choose %s: %v", name, err)
	}
	preview := page.Locator("#data-import-preview")
	result := preview.Locator(".spreadsheet-import-counts, .spreadsheet-import-empty, .spreadsheet-import-error").First()
	if err := result.WaitFor(); err != nil {
		t.Fatalf("wait for the preview of %s: %v", name, err)
	}
	return preview
}

func expectPreviewText(t *testing.T, preview playwright.Locator, want string) {
	t.Helper()
	text, err := preview.TextContent()
	if err != nil || !strings.Contains(text, want) {
		t.Fatalf("spreadsheet import preview = %q (err=%v), want it to contain %q", text, err, want)
	}
}

// importSpreadsheet saves the previewed changes and waits for Data & Backups to close.
func importSpreadsheet(t *testing.T, page playwright.Page) {
	t.Helper()
	if err := page.Locator("#btn-import-spreadsheet").Click(); err != nil {
		t.Fatalf("import spreadsheet: %v", err)
	}
	if err := page.Locator("#data-import-section").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateDetached}); err != nil {
		t.Fatalf("wait for Data & Backups to close after importing: %v", err)
	}
}

// reloadInLanguage saves an interface language and reloads the page in it.
func reloadInLanguage(t *testing.T, page playwright.Page, language string) {
	t.Helper()
	if _, err := page.Evaluate(`language => localStorage.setItem("war-room.language", language)`, language); err != nil {
		t.Fatalf("save language %s: %v", language, err)
	}
	if _, err := page.Reload(); err != nil {
		t.Fatalf("reload in %s: %v", language, err)
	}
	if _, err := page.WaitForFunction("() => document.body.dataset.appReady === 'true'", nil); err != nil {
		t.Fatalf("wait for the app in %s: %v", language, err)
	}
}

func fetchJSON(t *testing.T, page playwright.Page, path string) map[string]any {
	t.Helper()
	value, err := page.Evaluate(`async path => (await fetch(path)).json()`, path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s returned %T, want an object", path, value)
	}
	return object
}

func fetchJSONList(t *testing.T, page playwright.Page, path string) []any {
	t.Helper()
	value, err := page.Evaluate(`async path => (await fetch(path)).json()`, path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	list, ok := value.([]any)
	if !ok {
		t.Fatalf("%s returned %T, want a list", path, value)
	}
	return list
}

func hasProcess(jobs []any, role, status string) bool {
	for _, item := range jobs {
		if job, ok := item.(map[string]any); ok && job["position_title"] == role && job["status"] == status {
			return true
		}
	}
	return false
}
