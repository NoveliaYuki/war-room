package e2e_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const (
	openInMapsLabel = "Open in Maps"
	modalMapsLink   = "#detail-modal .stage-schedule-metadata > .btn-join-meeting-inline"
	minTouchTarget  = 44
	phoneMaxWidth   = 760
)

// TestOnsiteMeetingOpensMaps schedules through the UI, so it proves the link
// survives the real API as well as the browser-only demo store.
func TestOnsiteMeetingOpensMaps(t *testing.T) {
	for _, mode := range []struct {
		name    string
		openJob func(*testing.T, playwright.Page)
	}{
		{"app", openNewProcessCard},
		{"demo", openDemoProcessCard},
	} {
		t.Run(mode.name, func(t *testing.T) {
			page := newPage(t)
			mapsURL := fmt.Sprintf("https://maps.example/%s-office-%d", mode.name, time.Now().UnixNano())
			routeMapsLink(t, page)
			setPageViewport(t, page, 1280, 800)
			mode.openJob(t, page)
			assertMissingMapsLinkPrompt(t, page)
			scheduleOnsiteMeeting(t, page, mapsURL, 3)
			assertMapsLinkFits(t, page, modalMapsLink, []int{1280, 1081})
			setPageViewport(t, page, 390, 844)
			clickLocator(t, page.Locator(`#detail-modal [data-mobile-pane="stages"]`), "open the mobile stages pane")
			assertMapsLinkFits(t, page, modalMapsLink, []int{1080, 600, 390, 360, 320})
			setPageViewport(t, page, 1280, 800)
			closeDetailAndOpenSchedule(t, page)
			assertMapsLinkFits(t, page, fmt.Sprintf(`.meeting-row-right a[href=%q]`, mapsURL), scheduleLayoutWidths)

			restoreDesktopSchedule(t, page)
			clickLocator(t, page.Locator(fmt.Sprintf(`.meeting-row-card:has(a[href=%q]) .btn-open-meeting`, mapsURL)), "open the scheduled job")
			scheduleOnsiteMeeting(t, page, mapsURL, 0)
			closeDetailAndOpenSchedule(t, page)
			todayLink := fmt.Sprintf(`.today-meeting-actions a[href=%q]`, mapsURL)
			assertMapsLinkFits(t, page, todayLink, scheduleLayoutWidths)
			assertMapsLinkOpensNewTab(t, page, todayLink, mapsURL)
		})
	}
}

var scheduleLayoutWidths = []int{1280, 1100, 600, 390, 361, 360, 320}

func routeMapsLink(t *testing.T, page playwright.Page) {
	t.Helper()
	err := page.Context().Route("https://maps.example/**", func(route playwright.Route) {
		if err := route.Fulfill(playwright.RouteFulfillOptions{Status: playwright.Int(200), Body: "maps"}); err != nil {
			t.Errorf("fulfill maps route: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("route maps link: %v", err)
	}
}

func openNewProcessCard(t *testing.T, page playwright.Page) {
	t.Helper()
	card := createStatusCycleProcess(t, page, fmt.Sprintf("Onsite Maps Engineer %d", time.Now().UnixNano()))
	clickLocator(t, card.Locator(".btn-card-open-details"), "open job details")
	clickLocator(t, page.Locator("#btn-init-first-stage"), "add the first interview stage")
	fill(t, page.Locator(`.stage-create-form [name="custom_title"]`), "On-site interview")
	if _, err := page.Locator(`.stage-create-form [name="stage_type"]`).SelectOption(playwright.SelectOptionValues{Values: &[]string{"HR"}}); err != nil {
		t.Fatalf("choose stage type: %v", err)
	}
	clickLocator(t, page.Locator(".stage-create-save"), "save the first interview stage")
	waitForLocator(t, page.Locator("#detail-modal .btn-edit-meeting-schedule"), "stage schedule action")
}

func openDemoProcessCard(t *testing.T, page playwright.Page) {
	t.Helper()
	openDemoPage(t, page)
	clickLocator(t, page.Locator(".process-card").First().Locator(".btn-card-open-details"), "open demo job details")
}

func scheduleOnsiteMeeting(t *testing.T, page playwright.Page, mapsURL string, daysFromToday int) {
	t.Helper()
	clickLocator(t, page.Locator("#detail-modal .btn-edit-meeting-schedule"), "edit interview schedule")
	form := page.Locator("#schedule-stage-form")
	waitForLocator(t, form, "schedule form")
	fill(t, form.Locator(`[name="meeting_date"]`), time.Now().AddDate(0, 0, daysFromToday).Format(time.DateOnly))
	fill(t, form.Locator(`[name="meeting_time"]`), "10:00")
	if _, err := form.Locator(`[name="meeting_type"]`).SelectOption(playwright.SelectOptionValues{Values: &[]string{"onsite"}}); err != nil {
		t.Fatalf("select on-site format: %v", err)
	}
	assertVisible(t, form.Locator(`label:has-text("Google Maps Link")`))
	fill(t, form.Locator(`[name="meeting_url"]`), mapsURL)
	clickLocator(t, form.Locator(`button[type="submit"]`), "save on-site meeting")
	if mapsURL == "" {
		waitForLocator(t, page.Locator("#detail-modal .btn-add-maps-link"), "maps link prompt in stage banner")
		return
	}
	waitForLocator(t, page.Locator(fmt.Sprintf(`#detail-modal .btn-join-meeting-inline:has-text(%q)`, openInMapsLabel)), "maps button in stage banner")
}

// assertMissingMapsLinkPrompt covers meetings saved as on-site before their link
// field existed: they must lead to the field rather than show no action.
func assertMissingMapsLinkPrompt(t *testing.T, page playwright.Page) {
	t.Helper()
	scheduleOnsiteMeeting(t, page, "", 2)
	clickLocator(t, page.Locator("#detail-modal .btn-add-maps-link"), "add the missing maps link")
	field := page.Locator(`#schedule-stage-form [name="meeting_url"]`)
	waitForLocator(t, field, "maps link field")
	assertVisible(t, page.Locator(`#schedule-stage-form label:has-text("Google Maps Link")`))
	focused, err := field.Evaluate("input => document.activeElement === input", nil)
	if err != nil || focused != true {
		t.Fatalf("maps link field should receive focus (focused=%v, err=%v)", focused, err)
	}
	clickLocator(t, page.Locator(".btn-cancel-schedule"), "close the schedule form")
	waitForLocator(t, page.Locator("#detail-modal .btn-edit-meeting-schedule"), "stage schedule action")
}

func closeDetailAndOpenSchedule(t *testing.T, page playwright.Page) {
	t.Helper()
	clickLocator(t, page.Locator("#detail-modal .modal-close-btn").First(), "close job details")
	selectFilter(t, page, "schedule")
	waitForLocator(t, page.Locator(".schedule-container .today-status-title"), "daily schedule")
}

// restoreDesktopSchedule dismisses the account panel that phone layouts keep
// open, which otherwise stays over the page after widening the viewport.
func restoreDesktopSchedule(t *testing.T, page playwright.Page) {
	t.Helper()
	setPageViewport(t, page, 1280, 800)
	clickLocator(t, page.Locator(".schedule-container .today-status-title"), "dismiss the account panel")
}

type mapsLinkLayout struct {
	Label    string  `json:"label"`
	Height   float64 `json:"height"`
	Left     float64 `json:"left"`
	Right    float64 `json:"right"`
	ContentL float64 `json:"contentLeft"`
	ContentR float64 `json:"contentRight"`
	Viewport float64 `json:"viewport"`
	Document float64 `json:"document"`
}

// assertMapsLinkFits fails when the label, wider than Join Call, is clipped or
// widens the page at any layout breakpoint.
func assertMapsLinkFits(t *testing.T, page playwright.Page, selector string, widths []int) {
	t.Helper()
	for _, width := range widths {
		setPageViewport(t, page, width, 844)
		layout := readMapsLinkLayout(t, page, selector)
		fits := layout.ContentL >= layout.Left-1 && layout.ContentR <= layout.Right+1 && layout.Right <= layout.Viewport+1
		if !fits || layout.Document > layout.Viewport {
			t.Errorf("maps link does not fit at %dpx: %+v", width, layout)
		}
		if width <= phoneMaxWidth && layout.Height < minTouchTarget {
			t.Errorf("maps link touch target at %dpx = %.1fpx, want at least %dpx", width, layout.Height, minTouchTarget)
		}
		if layout.Label != openInMapsLabel {
			t.Errorf("maps link label at %dpx = %q, want %q", width, layout.Label, openInMapsLabel)
		}
	}
}

func readMapsLinkLayout(t *testing.T, page playwright.Page, selector string) mapsLinkLayout {
	t.Helper()
	encoded, err := page.Locator(selector).First().Evaluate(`link => {
		document.getAnimations().forEach(animation => animation.finish());
		const box = link.getBoundingClientRect();
		const range = document.createRange();
		range.selectNodeContents(link);
		const content = range.getBoundingClientRect();
		return JSON.stringify({
			label: link.textContent.trim(), height: box.height, left: box.left, right: box.right,
			contentLeft: content.left, contentRight: content.right,
			viewport: window.innerWidth, document: document.documentElement.scrollWidth,
		});
	}`, nil)
	if err != nil {
		t.Fatalf("read maps link layout for %s: %v", selector, err)
	}
	var layout mapsLinkLayout
	if err := json.Unmarshal([]byte(encoded.(string)), &layout); err != nil {
		t.Fatalf("decode maps link layout: %v", err)
	}
	return layout
}

func assertMapsLinkOpensNewTab(t *testing.T, page playwright.Page, selector, mapsURL string) {
	t.Helper()
	restoreDesktopSchedule(t, page)
	opened, err := page.Context().ExpectPage(func() error {
		return page.Locator(selector).First().Click()
	})
	if err != nil {
		t.Fatalf("open maps link: %v", err)
	}
	if err := opened.WaitForURL(mapsURL); err != nil {
		t.Fatalf("maps tab url=%q, want %q: %v", opened.URL(), mapsURL, err)
	}
}
