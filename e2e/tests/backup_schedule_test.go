package e2e_test

import (
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// TestSavingABackupScheduleInPlainWords saves a weekly schedule from Data &
// backups as a signed-in user and checks it is described in words, never
// cron syntax, and stored with the browser's time zone. The test backend has
// no Google credentials and its scheduler is off, so nothing reaches Drive.
func TestSavingABackupScheduleInPlainWords(t *testing.T) {
	page := newPage(t)
	t.Cleanup(func() { resetBackupSchedule(t, page) })
	clickLocator(t, page.Locator("#btn-account-menu"), "open account menu")
	clickLocator(t, page.Locator("#btn-data-management-menu"), "open Data & backups")
	form := page.Locator("#backup-schedule-form")
	waitForLocator(t, page.Locator(".backup-schedule[data-ready=true] fieldset.backup-schedule-fields:not([disabled])"), "enabled backup schedule form")

	assertText(t, page.Locator("#backup-schedule-summary"), "Automatic backups are off.")
	if visible, _ := page.Locator("#backup-schedule-weekday").IsVisible(); visible {
		t.Fatal("the weekday is only asked for weekly backups")
	}
	chooseWeeklySchedule(t, page, form)
	assertText(t, page.Locator("#backup-schedule-summary"), "Backs up every Wednesday at 6:45") // Chromium may put a narrow space before PM.
	clickLocator(t, page.Locator("#btn-save-backup-schedule"), "save the schedule")
	waitForLocator(t, page.Locator("#backup-schedule-status:has-text('Schedule saved.')"), "saved confirmation")
	waitForLocator(t, page.Locator("#backup-schedule-next"), "next backup time")

	assertText(t, page.Locator("#backup-schedule-next"), "Next backup: Wednesday")
	assertNoCronSyntax(t, page)
	stored, err := page.Evaluate(`async () => {
		const schedule = await (await fetch("/api/google/backup-schedule", { cache: "no-store" })).json();
		return [schedule.frequency, String(schedule.weekday), schedule.time, schedule.time_zone === Intl.DateTimeFormat().resolvedOptions().timeZone ? "browser zone" : schedule.time_zone];
	}`, nil)
	if err != nil {
		t.Fatalf("read the saved schedule: %v", err)
	}
	if got := strings.Join(toStrings(stored), " "); got != "weekly 3 18:45 browser zone" {
		t.Fatalf("stored schedule = %q", got)
	}
}

// chooseWeeklySchedule picks Every week on Wednesday at 18:45.
func chooseWeeklySchedule(t *testing.T, page playwright.Page, form playwright.Locator) {
	t.Helper()
	clickLocator(t, form.Locator("label:has(input[value=weekly])"), "choose Every week")
	if _, err := page.Locator("#backup-schedule-weekday").SelectOption(playwright.SelectOptionValues{Values: &[]string{"3"}}); err != nil {
		t.Fatalf("choose Wednesday: %v", err)
	}
	if err := page.Locator("#backup-schedule-time").Fill("18:45"); err != nil {
		t.Fatalf("enter the time: %v", err)
	}
}

func assertNoCronSyntax(t *testing.T, page playwright.Page) {
	t.Helper()
	modalText, err := page.Locator("#detail-modal").InnerText()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(modalText, "* *") || strings.Contains(strings.ToLower(modalText), "cron") {
		t.Fatalf("the schedule must never be shown as cron syntax: %q", modalText)
	}
}

func assertText(t *testing.T, locator playwright.Locator, want string) {
	t.Helper()
	if err := locator.Filter(playwright.LocatorFilterOptions{HasText: want}).WaitFor(); err != nil {
		got, _ := locator.TextContent()
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func toStrings(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, _ := item.(string)
		out = append(out, text)
	}
	return out
}

// resetBackupSchedule turns the shared test account's schedule off again.
func resetBackupSchedule(t *testing.T, page playwright.Page) {
	t.Helper()
	if _, err := page.Evaluate(`() => fetch("/api/google/backup-schedule", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ frequency: "off", weekday: 1, time: "09:00", time_zone: "UTC" }) })`, nil); err != nil {
		t.Errorf("reset backup schedule: %v", err)
	}
}
