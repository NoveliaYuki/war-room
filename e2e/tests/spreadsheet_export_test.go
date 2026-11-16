package e2e_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

var boardWorkbookFilename = regexp.MustCompile(`^war-room-board-\d{4}-\d{2}-\d{2}\.xlsx$`)

type xlsxCell struct {
	Ref     string  `xml:"r,attr"`
	Type    string  `xml:"t,attr"`
	Value   string  `xml:"v"`
	Inline  string  `xml:"is>t"`
	Formula *string `xml:"f"`
}

type xlsxWorksheet struct {
	Pane struct {
		YSplit string `xml:"ySplit,attr"`
		State  string `xml:"state,attr"`
	} `xml:"sheetViews>sheetView>pane"`
	Cols []struct {
		Min    int    `xml:"min,attr"`
		Hidden string `xml:"hidden,attr"`
	} `xml:"cols>col"`
	Rows []struct {
		Cells []xlsxCell `xml:"c"`
	} `xml:"sheetData>row"`
	AutoFilter struct {
		Ref string `xml:"ref,attr"`
	} `xml:"autoFilter"`
	Merges []struct {
		Ref string `xml:"ref,attr"`
	} `xml:"mergeCells>mergeCell"`
}

// xlsxRow maps column letters to cells.
type xlsxRow map[string]xlsxCell

func (row xlsxRow) text(column string) string {
	cell := row[column]
	if cell.Type == "inlineStr" {
		return cell.Inline
	}
	return cell.Value
}

func (sheet xlsxWorksheet) row(index int) xlsxRow {
	row := xlsxRow{}
	for _, cell := range sheet.Rows[index].Cells {
		row[strings.TrimRight(cell.Ref, "0123456789")] = cell
	}
	return row
}

// rowWhere returns the first data row whose column holds want.
func (sheet xlsxWorksheet) rowWhere(t *testing.T, column, want string) xlsxRow {
	t.Helper()
	for index := range sheet.Rows {
		if row := sheet.row(index); row.text(column) == want {
			return row
		}
	}
	t.Fatalf("no row has %q in column %s", want, column)
	return nil
}

type exportedWorkbook struct {
	sheetNames []string
	board      xlsxWorksheet
	questions  xlsxWorksheet
}

// TestBoardWorkbookExportDownloadsEveryProcess downloads the board from Data &
// Backups while signed in and reads it as an Office Open XML workbook.
func TestBoardWorkbookExportDownloadsEveryProcess(t *testing.T) {
	page := newPage(t)
	title := fmt.Sprintf("E2E Workbook %d", time.Now().UnixNano())
	company := `=HYPERLINK("https://example.com","x")`
	question := "How do you test " + title + "?"
	jobID := seedWorkbookProcess(t, page, company, title, question)

	workbook := downloadBoardWorkbook(t, page)
	if strings.Join(workbook.sheetNames, "|") != "Board|Interview questions" {
		t.Fatalf("sheets = %q, want Board and Interview questions", workbook.sheetNames)
	}
	assertBoardSheet(t, workbook.board, company, title)
	assertQuestionsSheet(t, workbook.questions, jobID, title, question)
}

// TestBoardWorkbookExportWorksInDemo downloads the sample board in the browser demo.
func TestBoardWorkbookExportWorksInDemo(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	workbook := downloadBoardWorkbook(t, page)
	if len(workbook.board.Rows) < 3 || len(workbook.questions.Rows) < 2 {
		t.Fatalf("demo workbook has %d board rows and %d question rows, want sample processes and questions", len(workbook.board.Rows), len(workbook.questions.Rows))
	}
	if header := workbook.board.row(1).text("A"); header != "1.1 Company" {
		t.Fatalf("demo board A2 = %q, want 1.1 Company", header)
	}
}

func seedWorkbookProcess(t *testing.T, page playwright.Page, company, title, question string) string {
	t.Helper()
	jobID, err := page.Evaluate(`async ({ company, title, question }) => {
		const send = async (url, method, body) => {
			const response = await fetch(url, { method, headers: { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) });
			if (!response.ok) throw new Error(method + " " + url + " " + response.status + " " + await response.text());
			return response.json();
		};
		const job = await send("/api/jobs", "POST", {
			company_name: company, position_title: title, salary_type: "limited", salary_min: 60000, salary_max: 80000, salary_currency: "EUR",
			company_overview: "## About Acme\nWe build **fast** services.\n\n- Go\n- [Docs](https://example.com/docs)", create_default_stages: true,
		});
		const stage = (await send("/api/jobs/" + job.id, "GET")).stages[0];
		await send("/api/questions", "POST", { stage_id: stage.id, question, answer_notes: "Unit tests\nand e2e" });
		return job.id;
	}`, map[string]any{"company": company, "title": title, "question": question})
	if err != nil {
		t.Fatalf("seed workbook process: %v", err)
	}
	return jobID.(string)
}

func downloadBoardWorkbook(t *testing.T, page playwright.Page) exportedWorkbook {
	t.Helper()
	openBackupDialog(t, page)
	download, err := page.ExpectDownload(func() error {
		return page.Locator("#btn-export-spreadsheet").Click()
	})
	if err != nil {
		t.Fatalf("download board workbook: %v", err)
	}
	if name := download.SuggestedFilename(); !boardWorkbookFilename.MatchString(name) {
		t.Fatalf("workbook filename = %q, want war-room-board-YYYY-MM-DD.xlsx", name)
	}
	contents := readDownload(t, download)
	archive, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatalf("open workbook as ZIP: %v", err)
	}
	var names struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
		} `xml:"sheets>sheet"`
	}
	decodeWorkbookPart(t, archive, "xl/workbook.xml", &names)
	workbook := exportedWorkbook{}
	for _, sheet := range names.Sheets {
		workbook.sheetNames = append(workbook.sheetNames, sheet.Name)
	}
	decodeWorkbookPart(t, archive, "xl/worksheets/sheet1.xml", &workbook.board)
	decodeWorkbookPart(t, archive, "xl/worksheets/sheet2.xml", &workbook.questions)
	return workbook
}

func decodeWorkbookPart(t *testing.T, archive *zip.Reader, name string, target any) {
	t.Helper()
	file, err := archive.Open(name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer func() { _ = file.Close() }()
	contents, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := xml.Unmarshal(contents, target); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
}

// expectTexts fails for every column whose text differs from want.
func expectTexts(t *testing.T, label string, row xlsxRow, want map[string]string) {
	t.Helper()
	for column, text := range want {
		if got := row.text(column); got != text {
			t.Errorf("%s column %s = %q, want %q", label, column, got, text)
		}
	}
}

// expectNumber fails unless the cell is a numeric cell whose value matches pattern.
func expectNumber(t *testing.T, label string, cell xlsxCell, pattern string) {
	t.Helper()
	if cell.Type != "" || cell.Formula != nil || !regexp.MustCompile(pattern).MatchString(cell.Value) {
		t.Errorf("%s cell = %+v, want a number matching %s", label, cell, pattern)
	}
}

func assertBoardLayout(t *testing.T, sheet xlsxWorksheet) {
	t.Helper()
	expectTexts(t, "group header", sheet.row(0), map[string]string{"A": "1 Job specification", "L": "2 Process", "R": "3 Application", "W": "4 Recruiter"})
	expectTexts(t, "column header", sheet.row(1), map[string]string{"A": "1.1 Company", "C": "1.3 Description", "F": "1.6 Published salary – minimum", "I": "1.9 Your expected salary", "Z": "4.4 First contact"})
	merges := make([]string, 0, len(sheet.Merges))
	for _, merge := range sheet.Merges {
		merges = append(merges, merge.Ref)
	}
	if got := strings.Join(merges, " "); got != "A1:K1 L1:Q1 R1:V1 W1:Z1" {
		t.Errorf("merged group headers = %q", got)
	}
	if sheet.Pane.YSplit != "2" || sheet.Pane.State != "frozen" || !strings.HasPrefix(sheet.AutoFilter.Ref, "A2:Z") {
		t.Errorf("frozen rows %q (%q), autofilter %q", sheet.Pane.YSplit, sheet.Pane.State, sheet.AutoFilter.Ref)
	}
}

func assertBoardSheet(t *testing.T, sheet xlsxWorksheet, company, title string) {
	t.Helper()
	assertBoardLayout(t, sheet)
	row := sheet.rowWhere(t, "B", title)
	if cell := row["A"]; cell.Type != "inlineStr" || cell.Formula != nil {
		t.Errorf("company cell = %+v, want an inline string", cell)
	}
	expectTexts(t, "board row", row, map[string]string{
		"A": company,
		"C": "About Acme\nWe build fast services.\n\n• Go\n• Docs (https://example.com/docs)",
		"L": "Waiting",
		"V": "No",
	})
	expectNumber(t, "minimum salary", row["F"], `^60000$`)
	expectNumber(t, "date added", row["O"], `^\d{5}$`)
}

func assertQuestionsSheet(t *testing.T, sheet xlsxWorksheet, jobID, title, question string) {
	t.Helper()
	expectTexts(t, "question header", sheet.row(0), map[string]string{"A": "Company", "C": "Stage", "D": "Question", "G": "Process ID", "I": "Question ID"})
	hidden := []string{}
	for _, column := range sheet.Cols {
		if column.Hidden == "1" {
			hidden = append(hidden, fmt.Sprint(column.Min))
		}
	}
	if got := strings.Join(hidden, ","); got != "7,8,9" {
		t.Errorf("hidden columns = %q, want the three ID columns 7,8,9", got)
	}
	row := sheet.rowWhere(t, "D", question)
	expectTexts(t, "question row", row, map[string]string{"B": title, "C": "Recruiter Screen", "E": "Unit tests\nand e2e", "F": "No", "G": jobID})
	if row.text("H") == "" || row.text("I") == "" {
		t.Errorf("stage and question IDs = %q, %q, want both set", row.text("H"), row.text("I"))
	}
}

func readDownload(t *testing.T, download playwright.Download) []byte {
	t.Helper()
	path, err := download.Path()
	if err != nil {
		t.Fatalf("locate download: %v", err)
	}
	contents, err := os.ReadFile(path) // #nosec G304 -- reads the file Playwright just downloaded into its temporary directory.
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	return contents
}
