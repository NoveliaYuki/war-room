package e2e_test

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

var boardCSVFilename = regexp.MustCompile(`^war-room-board-\d{4}-\d{2}-\d{2}\.csv$`)

// TestBoardCSVExportDownloadsEveryProcess downloads the board from Data &
// Backups while signed in and parses it as a strict RFC 4180 file.
func TestBoardCSVExportDownloadsEveryProcess(t *testing.T) {
	page := newPage(t)
	title := fmt.Sprintf(`E2E CSV, "quoted" %d`, time.Now().UnixNano())
	company := `=HYPERLINK("https://example.com","x")`
	createCSVProcess(t, page, company, title)

	openBackupDialog(t, page)
	download, err := page.ExpectDownload(func() error {
		return page.Locator("#btn-export-csv").Click()
	})
	if err != nil {
		t.Fatalf("download board CSV: %v", err)
	}
	if name := download.SuggestedFilename(); !boardCSVFilename.MatchString(name) {
		t.Fatalf("CSV filename = %q, want war-room-board-YYYY-MM-DD.csv", name)
	}
	contents := readDownload(t, download)
	if !bytes.HasPrefix(contents, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("CSV does not start with a UTF-8 byte order mark: % x", contents[:min(len(contents), 3)])
	}
	if !bytes.Contains(contents, []byte("\r\n")) {
		t.Fatal("CSV rows are not separated by CRLF")
	}
	records, err := csv.NewReader(bytes.NewReader(contents[3:])).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	row := csvRowWithRole(t, records, title)
	if want := "'" + company; row["Company"] != want {
		t.Fatalf("Company = %q, want neutralized %q", row["Company"], want)
	}
	if row["Status"] != "Waiting" {
		t.Fatalf("Status = %q, want Waiting", row["Status"])
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(row["Date added"]) {
		t.Fatalf("Date added = %q, want an ISO date", row["Date added"])
	}
}

func createCSVProcess(t *testing.T, page playwright.Page, company, title string) {
	t.Helper()
	clickLocator(t, page.Locator("#btn-new-process"), "open create form")
	clickLocator(t, page.Locator("#process-entry-manual"), "choose to fill in by hand")
	fill(t, page.Locator(`input[name="company_name"]`), company)
	fill(t, page.Locator(`input[name="position_title"]`), title)
	clickLocator(t, page.Locator(`button[type="submit"]`), "create process")
	if _, err := page.WaitForFunction(`() => !document.querySelector("#modal-backdrop")?.classList.contains("active")`, nil); err != nil {
		t.Fatalf("wait for process creation modal to close: %v", err)
	}
}

func readDownload(t *testing.T, download playwright.Download) []byte {
	t.Helper()
	path, err := download.Path()
	if err != nil {
		t.Fatalf("locate downloaded CSV: %v", err)
	}
	contents, err := os.ReadFile(path) // #nosec G304 -- reads the file Playwright just downloaded into its temporary directory.
	if err != nil {
		t.Fatalf("read downloaded CSV: %v", err)
	}
	return contents
}

// csvRowWithRole returns the row whose Role column is role, keyed by header.
func csvRowWithRole(t *testing.T, records [][]string, role string) map[string]string {
	t.Helper()
	if len(records) < 2 {
		t.Fatalf("CSV has %d rows, want a header and at least one process", len(records))
	}
	header := records[0]
	for _, record := range records[1:] {
		row := make(map[string]string, len(header))
		for index, name := range header {
			row[name] = record[index]
		}
		if row["Role"] == role {
			return row
		}
	}
	t.Fatalf("CSV has no row for role %q", role)
	return nil
}
