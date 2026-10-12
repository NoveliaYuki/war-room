package e2e_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const defaultBaseURL = "http://127.0.0.1:4040"

var browser playwright.Browser
var baseURL = defaultBaseURL

// TestMain starts the application and shared headless browser for E2E tests.
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve repository root:", err)
		return 1
	}
	var server *exec.Cmd
	if configuredURL := os.Getenv("WARROOM_E2E_BASE_URL"); configuredURL != "" {
		baseURL = configuredURL
	} else {
		dataDir, err := os.MkdirTemp("", "war-room-playwright-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "create test data directory:", err)
			return 1
		}
		defer func() {
			if err := os.RemoveAll(dataDir); err != nil {
				fmt.Fprintln(os.Stderr, "remove test data directory:", err)
			}
		}()
		server = startServer(root, dataDir)
		defer stopServer(server)
	}

	pw, err := playwright.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "start Playwright driver:", err)
		return 1
	}
	defer func() {
		if err := pw.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, "stop Playwright driver:", err)
		}
	}()

	browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args:     []string{"--no-sandbox"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "launch Chromium:", err)
		return 1
	}
	defer func() {
		if err := browser.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "close browser:", err)
		}
	}()

	return m.Run()
}

// startServer launches the Go application with isolated test data and static assets.
func startServer(root, dataDir string) *exec.Cmd {
	backendDir := filepath.Join(root, "backend")
	staticDir := filepath.Join(root, "frontend")
	cmd := exec.Command("go", "run", "./cmd/server")
	cmd.Dir = backendDir
	cmd.Env = append(os.Environ(),
		"PORT=4040",
		"HOST=127.0.0.1",
		"WARROOM_DATA_DIR="+dataDir,
		"STATIC_DIR="+staticDir,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "start application:", err)
		os.Exit(1)
	}

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		// The target is an explicit E2E configuration value, so the readiness probe
		// intentionally follows WARROOM_E2E_BASE_URL when supplied.
		response, err := http.Get(baseURL) // #nosec G107 -- intentionally targets the configured E2E application
		if err == nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				fmt.Fprintln(os.Stderr, "close readiness response:", closeErr)
			}
			if response.StatusCode == http.StatusOK {
				return cmd
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	stopServer(cmd)
	fmt.Fprintln(os.Stderr, "application did not become ready before timeout")
	os.Exit(1)
	return nil
}

// stopServer terminates the application process group and waits for it to exit.
func stopServer(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		fmt.Fprintln(os.Stderr, "terminate application:", err)
	}
	done := make(chan struct{})
	go func() {
		if err := cmd.Wait(); err != nil {
			fmt.Fprintln(os.Stderr, "wait for application:", err)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			fmt.Fprintln(os.Stderr, "force-stop application:", err)
		}
		<-done
	}
}

// newPage creates an isolated browser page and closes it at test completion.
func newPage(t *testing.T) playwright.Page {
	t.Helper()
	page, err := browser.NewPage()
	if err != nil {
		t.Fatalf("create browser page: %v", err)
	}
	t.Cleanup(func() {
		if err := page.Close(); err != nil {
			t.Errorf("close browser page: %v", err)
		}
	})
	if _, err := page.Goto(baseURL); err != nil {
		t.Fatalf("open application: %v", err)
	}
	if _, err := page.WaitForFunction("() => document.body.dataset.appReady === 'true'", nil); err != nil {
		t.Fatalf("wait for application initialization: %v", err)
	}
	return page
}

func setPageViewport(t *testing.T, page playwright.Page, width, height int) {
	t.Helper()
	if err := page.SetViewportSize(width, height); err != nil {
		t.Fatalf("set %dx%d viewport: %v", width, height, err)
	}
	if _, err := page.Evaluate(`() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`, nil); err != nil {
		t.Fatalf("wait for %dx%d viewport layout: %v", width, height, err)
	}
}

func selectFilter(t *testing.T, page playwright.Page, filter string) {
	t.Helper()
	if filter == "schedule" {
		if err := page.Locator("#tab-schedule").Click(); err != nil {
			t.Fatalf("open daily schedule: %v", err)
		}
		return
	}
	menu := page.Locator("#filter-menu")
	visible, err := menu.IsVisible()
	if err != nil {
		t.Fatalf("check process view menu visibility: %v", err)
	}
	if !visible {
		if err := page.Locator("#filter-menu-trigger").Click(); err != nil {
			t.Fatalf("open process view menu: %v", err)
		}
	}
	if err := page.Locator(`[data-filter="` + filter + `"]`).Click(); err != nil {
		t.Fatalf("select %s filter: %v", filter, err)
	}
}

func TestApplicationShellAndFilters(t *testing.T) {
	page := newPage(t)
	title, err := page.Title()
	if err != nil {
		t.Fatalf("read page title: %v", err)
	}
	if !strings.Contains(strings.ToLower(title), "war room") && !strings.Contains(strings.ToLower(title), "ongoing") {
		t.Fatalf("unexpected page title %q", title)
	}
	for _, selector := range []string{".brand-title", "#search-input", "#filter-current-action", "#filter-menu-trigger", "#tab-schedule"} {
		assertVisible(t, page.Locator(selector))
	}
	if err := page.Locator("#filter-menu-trigger").Click(); err != nil {
		t.Fatalf("open process view menu: %v", err)
	}
	for _, filter := range []string{"ongoing", "accepted", "rejected", "all"} {
		assertVisible(t, page.Locator(`[data-filter="`+filter+`"]`))
	}
	assertVisible(t, page.Locator("#tab-schedule"))
	if err := page.Keyboard().Press("Escape"); err != nil {
		t.Fatalf("close process view menu: %v", err)
	}
	assertVisible(t, page.Locator("#btn-new-process"))
	mobileActionVisible, err := page.Locator("#btn-new-process-mobile").IsVisible()
	if err != nil || mobileActionVisible {
		t.Fatalf("expected compact new-process action to be hidden on desktop (visible=%t, err=%v)", mobileActionVisible, err)
	}
}

type layoutBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Right  float64 `json:"right"`
}

type toolbarLayout struct {
	ViewportWidth        float64   `json:"viewportWidth"`
	DocumentWidth        float64   `json:"documentWidth"`
	CardColumns          int       `json:"cardColumns"`
	Search               layoutBox `json:"search"`
	SearchFontSize       float64   `json:"searchFontSize"`
	SearchFilterWidth    layoutBox `json:"searchFilterWidth"`
	FirstCard            layoutBox `json:"firstCard"`
	Filter               layoutBox `json:"filter"`
	ViewControls         layoutBox `json:"viewControls"`
	Sort                 layoutBox `json:"sort"`
	Toolbar              layoutBox `json:"toolbar"`
	Header               layoutBox `json:"header"`
	Theme                layoutBox `json:"theme"`
	FilterHeight         float64   `json:"filterHeight"`
	SortHeight           float64   `json:"sortHeight"`
	PeriodInToolbar      bool      `json:"periodInToolbar"`
	PeriodInFilters      bool      `json:"periodInFilters"`
	PeriodInViewControls bool      `json:"periodInViewControls"`
}

func TestToolbarResponsiveLayoutAndSalaryFilterStyle(t *testing.T) {
	page := newPage(t)
	for _, viewport := range []struct{ width, height int }{{2048, 1200}, {1440, 900}, {1100, 900}, {1000, 900}, {900, 900}, {801, 900}, {760, 900}, {728, 900}, {727, 900}, {722, 900}, {721, 900}, {390, 844}} {
		setPageViewport(t, page, viewport.width, viewport.height)
		layout := readToolbarLayout(t, page, viewport.width)
		assertToolbarHasNoOverflow(t, viewport.width, layout)
		assertSearchWidthUsesAvailableSpace(t, viewport.width, layout)
		if layout.SearchFontSize != 13 {
			t.Errorf("normal view at %dpx should preserve search text size at 13px, got %.1fpx", viewport.width, layout.SearchFontSize)
		}
		if !layout.PeriodInFilters || layout.PeriodInViewControls {
			t.Errorf("%dpx standard view should place the period control inside Filters", viewport.width)
		}
	}
	openDemoPage(t, page)
	for _, viewport := range []struct{ width, height int }{{2048, 1200}, {1440, 900}, {1360, 900}, {1200, 900}, {1116, 900}, {1115, 900}, {1100, 900}, {1000, 900}, {900, 900}, {850, 900}, {801, 900}, {800, 900}, {780, 900}, {760, 900}, {728, 900}, {727, 900}, {722, 900}, {721, 900}, {650, 900}, {500, 900}, {420, 900}, {390, 844}, {360, 800}, {320, 720}} {
		setPageViewport(t, page, viewport.width, viewport.height)
		layout := readToolbarLayout(t, page, viewport.width)
		assertToolbarHasNoOverflow(t, viewport.width, layout)
		assertToolbarAtViewport(t, viewport.width, layout)
		if layout.SearchFontSize != 13 {
			t.Errorf("%dpx viewport should preserve the search text size at 13px, got %.1fpx", viewport.width, layout.SearchFontSize)
		}
	}
	assertToolbarWithActiveFilter(t, page)
	assertExpectedSalaryInputStyle(t, page)
	if count, err := page.Locator(".process-sort-control > span").Count(); err != nil {
		t.Fatalf("count redundant sort labels: %v", err)
	} else if count != 0 {
		t.Error("sort control should communicate its purpose through the selected sort value")
	}
}

func TestJobDetailActionsRemainReadable(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	if err := page.Locator(".process-card").First().Locator(".btn-card-open-details").Click(); err != nil {
		t.Fatalf("open demo job details: %v", err)
	}
	if err := page.Locator("#detail-modal .job-cv-row").WaitFor(); err != nil {
		t.Fatalf("wait for CV controls: %v", err)
	}
	if _, err := page.Evaluate("() => document.getAnimations().forEach(animation => animation.finish())", nil); err != nil {
		t.Fatalf("finish detail modal animation: %v", err)
	}
	assertJobDetailActionsReadable(t, readJobDetailActionMetrics(t, page))
}

func TestNewProcessOverviewTabsAreEasyToTap(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	setPageViewport(t, page, 390, 844)
	if err := page.Locator("#btn-new-process-mobile").Click(); err != nil {
		t.Fatalf("open the new-process form: %v", err)
	}
	sourceTab := page.Locator("#create-overview-tab-source")
	previewTab := page.Locator("#create-overview-tab-preview")
	if err := sourceTab.WaitFor(); err != nil {
		t.Fatalf("wait for overview editor tabs: %v", err)
	}
	for _, tab := range []playwright.Locator{sourceTab, previewTab} {
		bounds, err := tab.BoundingBox()
		if err != nil || bounds == nil {
			t.Fatalf("read overview tab touch target: bounds=%v, err=%v", bounds, err)
		}
		if bounds.Height < 44 {
			t.Errorf("overview editor tabs should provide at least 44px touch targets, got %.1fpx", bounds.Height)
		}
	}
	if err := previewTab.Click(); err != nil {
		t.Fatalf("open the overview preview on mobile: %v", err)
	}
	selected, err := previewTab.GetAttribute("aria-selected")
	if err != nil || selected != "true" {
		t.Fatalf("preview tab should become selected after tapping it (selected=%q, err=%v)", selected, err)
	}
}

type jobDetailActionMetric struct {
	FontSize float64 `json:"fontSize"`
	Height   float64 `json:"height"`
}

func readJobDetailActionMetrics(t *testing.T, page playwright.Page) []jobDetailActionMetric {
	t.Helper()
	metrics, err := page.Evaluate(`() => {
	  const controls = [
	    ...document.querySelectorAll("#detail-modal .job-cv-row button, #detail-modal .markdown-field-edit"),
	  ].filter(button => button.getClientRects().length > 0);
	  return controls.map(button => ({
	    fontSize: Number.parseFloat(getComputedStyle(button).fontSize),
	    height: button.getBoundingClientRect().height,
	  }));
	}`, nil)
	if err != nil || metrics == nil {
		t.Fatalf("read job-detail action metrics: %v", err)
	}
	encoded, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("encode job-detail action metrics: %v", err)
	}
	var controls []jobDetailActionMetric
	if err := json.Unmarshal(encoded, &controls); err != nil {
		t.Fatalf("decode job-detail action metrics: %v", err)
	}
	return controls
}

func assertJobDetailActionsReadable(t *testing.T, controls []jobDetailActionMetric) {
	t.Helper()
	if len(controls) == 0 {
		t.Fatal("expected visible job-detail actions")
	}
	for _, control := range controls {
		if control.FontSize < 12 || control.Height < 34 {
			t.Errorf("job-detail CV and edit actions should have readable labels and usable click targets (font %.1fpx, height %.1fpx)", control.FontSize, control.Height)
		}
	}
}

func TestQuestionEditorInputsAreMobileFriendly(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	setPageViewport(t, page, 390, 844)
	openMobileStagePane(t, page)
	if err := page.Locator("#active-add-question-form .add-question-input").WaitFor(); err != nil {
		t.Fatalf("wait for the mobile question input: %v", err)
	}
	if _, err := page.Evaluate("() => document.getAnimations().forEach(animation => animation.finish())", nil); err != nil {
		t.Fatalf("finish mobile detail entrance animation: %v", err)
	}
	state, err := page.Evaluate(`() => {
	  const input = document.querySelector("#active-add-question-form .add-question-input");
	  const notes = document.querySelector("#stage-panel .question-answer-box");
	  return {
	    inputHeight: input.getBoundingClientRect().height,
	    inputFontSize: Number.parseFloat(getComputedStyle(input).fontSize),
	    notesFontSize: Number.parseFloat(getComputedStyle(notes).fontSize),
	  };
	}`, nil)
	if err != nil {
		t.Fatalf("read mobile question editor styles: %v", err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("encode mobile question editor styles: %v", err)
	}
	var metrics struct {
		InputHeight   float64 `json:"inputHeight"`
		InputFontSize float64 `json:"inputFontSize"`
		NotesFontSize float64 `json:"notesFontSize"`
	}
	if err := json.Unmarshal(encoded, &metrics); err != nil {
		t.Fatalf("decode mobile question editor styles: %v", err)
	}
	if metrics.InputHeight < 44 || metrics.InputFontSize < 16 || metrics.NotesFontSize < 16 {
		t.Errorf("mobile question editor should provide 44px touch input and 16px text, got input %.0fpx/%.0fpx and notes %.0fpx", metrics.InputHeight, metrics.InputFontSize, metrics.NotesFontSize)
	}

}

func TestInterviewerFormInputsAreMobileFriendly(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	setPageViewport(t, page, 390, 844)
	openMobileInterviewerForm(t, page)
	assertMobileInterviewerForm(t, readMobileInterviewerForm(t, page))
}

type mobileInterviewerFieldMetric struct {
	Height   float64 `json:"height"`
	FontSize float64 `json:"fontSize"`
}

type mobileInterviewerFormMetrics struct {
	Fields  []mobileInterviewerFieldMetric `json:"fields"`
	Actions []float64                      `json:"actions"`
}

func openMobileStagePane(t *testing.T, page playwright.Page) {
	t.Helper()
	if err := page.Locator(".process-card").First().Locator(".btn-card-open-details").Click(); err != nil {
		t.Fatalf("open demo process details: %v", err)
	}
	if err := page.Locator(`#detail-modal [data-mobile-pane="stages"]`).Click(); err != nil {
		t.Fatalf("open the mobile interview stages pane: %v", err)
	}
}

func openMobileInterviewerForm(t *testing.T, page playwright.Page) {
	t.Helper()
	openMobileStagePane(t, page)
	if err := page.Locator(".btn-add-stage-interviewer").Click(); err != nil {
		t.Fatalf("open the mobile interviewer form: %v", err)
	}
	if err := page.Locator(".stage-interviewer-form").WaitFor(); err != nil {
		t.Fatalf("wait for the mobile interviewer form: %v", err)
	}
}

func readMobileInterviewerForm(t *testing.T, page playwright.Page) mobileInterviewerFormMetrics {
	t.Helper()
	state, err := page.Evaluate(`() => {
	  const form = document.querySelector(".stage-interviewer-form");
	  const fields = [...form.querySelectorAll("input, textarea")];
	  const actions = [...form.querySelectorAll(".stage-interviewer-form-actions button")];
	  return {
	    fields: fields.map(field => ({ height: field.getBoundingClientRect().height, fontSize: Number.parseFloat(getComputedStyle(field).fontSize) })),
	    actions: actions.map(button => button.getBoundingClientRect().height),
	  };
	}`, nil)
	if err != nil {
		t.Fatalf("read mobile interviewer form styles: %v", err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("encode mobile interviewer form styles: %v", err)
	}
	var metrics mobileInterviewerFormMetrics
	if err := json.Unmarshal(encoded, &metrics); err != nil {
		t.Fatalf("decode mobile interviewer form styles: %v", err)
	}
	return metrics
}

func assertMobileInterviewerForm(t *testing.T, metrics mobileInterviewerFormMetrics) {
	t.Helper()
	for _, field := range metrics.Fields {
		if field.Height < 44 || field.FontSize < 16 {
			t.Errorf("mobile interviewer fields should provide 44px touch targets and 16px text, got %.0fpx/%.0fpx", field.Height, field.FontSize)
		}
	}
	for _, height := range metrics.Actions {
		if height < 44 {
			t.Errorf("mobile interviewer actions should be at least 44px high, got %.0fpx", height)
		}
	}
}

func TestPeriodSelectionIsCountedAndClearedWithFilters(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	if err := page.Locator("#process-filter-trigger").Click(); err != nil {
		t.Fatalf("open Filters: %v", err)
	}
	if err := page.Locator("#search-period-trigger").Click(); err != nil {
		t.Fatalf("open search period selector: %v", err)
	}
	if err := page.Locator(`[data-search-period="unassigned"]`).Click(); err != nil {
		t.Fatalf("select unassigned period: %v", err)
	}
	if _, err := page.Evaluate("() => new Promise(resolve => setTimeout(resolve, 25))", nil); err != nil {
		t.Fatalf("wait for filter focus handling after period selection: %v", err)
	}
	assertVisible(t, page.Locator("#process-filter-panel"))
	trigger := page.Locator("#search-period-trigger")
	assertVisible(t, trigger)
	if value, err := trigger.GetAttribute("aria-label"); err != nil || !strings.Contains(value, "No period") {
		t.Errorf("period selector should identify the selected scope, got %q (err=%v)", value, err)
	}
	assertPeriodFilterCounted(t, page)
	clearAllProcessFilters(t, page)
	assertPeriodFilterCleared(t, page)
}

func assertPeriodFilterCounted(t *testing.T, page playwright.Page) {
	t.Helper()
	assertVisible(t, page.Locator("#process-filter-count"))
	assertVisible(t, page.Locator("#process-filter-clear"))
	count, err := page.Locator("#process-filter-count").TextContent()
	if err != nil || strings.TrimSpace(count) != "1" {
		t.Errorf("selecting one period should show one active filter, got %q (err=%v)", count, err)
	}
	chips, err := page.Locator("#process-filter-chips [data-remove-filter]").Count()
	if err != nil || chips != 0 {
		t.Errorf("period scope should not be rendered as a criteria chip, got %d (err=%v)", chips, err)
	}
}

func clearAllProcessFilters(t *testing.T, page playwright.Page) {
	t.Helper()
	if err := page.Locator("#process-filter-clear").Click(); err != nil {
		t.Fatalf("clear selected period: %v", err)
	}
}

func assertPeriodFilterCleared(t *testing.T, page playwright.Page) {
	t.Helper()
	cleared, err := page.Evaluate(`() => document.querySelector("#search-period-filter").value === "all"`, nil)
	if err != nil || cleared != true {
		t.Errorf("clearing filters should restore all periods (cleared=%v, err=%v)", cleared, err)
	}
	for _, selector := range []string{"#process-filter-count", "#process-filter-clear"} {
		visible, err := page.Locator(selector).IsVisible()
		if err != nil {
			t.Fatalf("check %s after clearing filters: %v", selector, err)
		}
		if visible {
			t.Errorf("%s should be hidden after clearing the selected period", selector)
		}
	}
}

func TestFilterPopoverStaysWithinNarrowViewports(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	trigger := page.Locator("#process-filter-trigger")
	for _, width := range []int{1000, 900, 801, 760, 722, 721, 390, 320} {
		setPageViewport(t, page, width, 844)
		bounds := readFilterPopoverBounds(t, trigger, page.Locator("#process-filter-panel"), width)
		if bounds.Left < 0 || bounds.Right > bounds.ViewportWidth {
			t.Errorf("Filters popover should remain within the %dpx viewport, got left %.1fpx and right %.1fpx", width, bounds.Left, bounds.Right)
		}
		if width <= 420 && bounds.ScrollWidth > bounds.ClientWidth+1 {
			t.Errorf("Filters popover should not scroll horizontally at %dpx (scroll width %.1fpx, client width %.1fpx)", width, bounds.ScrollWidth, bounds.ClientWidth)
		}
	}
}

type popoverBounds struct {
	Left          float64 `json:"left"`
	Right         float64 `json:"right"`
	ViewportWidth float64 `json:"viewportWidth"`
	ScrollWidth   float64 `json:"scrollWidth"`
	ClientWidth   float64 `json:"clientWidth"`
}

func readFilterPopoverBounds(t *testing.T, trigger, panel playwright.Locator, width int) popoverBounds {
	t.Helper()
	expanded, err := trigger.GetAttribute("aria-expanded")
	if err != nil {
		t.Fatalf("read Filters state at %dpx: %v", width, err)
	}
	if expanded == "true" {
		if err := trigger.Click(); err != nil {
			t.Fatalf("close Filters before checking %dpx: %v", width, err)
		}
	}
	if err := trigger.Click(); err != nil {
		t.Fatalf("open Filters at %dpx: %v", width, err)
	}
	state, err := panel.Evaluate(`element => {
	  const rect = element.getBoundingClientRect();
	  return {
	    left: rect.left,
	    right: rect.right,
	    viewportWidth: window.innerWidth,
	    scrollWidth: element.scrollWidth,
	    clientWidth: element.clientWidth,
	  };
	}`, nil)
	if err != nil {
		t.Fatalf("read Filters bounds at %dpx: %v", width, err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("encode Filters bounds at %dpx: %v", width, err)
	}
	var bounds popoverBounds
	if err := json.Unmarshal(encoded, &bounds); err != nil {
		t.Fatalf("decode Filters bounds at %dpx: %v", width, err)
	}
	return bounds
}

func TestSearchAndFiltersRemainAvailableInAllView(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	selectFilter(t, page, "all")

	for _, selector := range []string{"#process-toolbar", "#search-input", "#process-filter-trigger"} {
		assertVisible(t, page.Locator(selector))
	}
	if visible, err := page.Locator(".process-sort-control").IsVisible(); err != nil {
		t.Fatalf("check sort control in all view: %v", err)
	} else if visible {
		t.Error("status-specific sort control should be hidden in all view")
	}

	selectFilter(t, page, "schedule")
	if visible, err := page.Locator("#process-toolbar").IsVisible(); err != nil {
		t.Fatalf("check toolbar in daily schedule: %v", err)
	} else if visible {
		t.Error("search toolbar should be hidden in daily schedule")
	}
}

func TestMutedTextMeetsAAContrastInBothThemes(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	for _, theme := range []string{"dark", "light"} {
		if _, err := page.Evaluate(`theme => { document.documentElement.dataset.theme = theme; }`, theme); err != nil {
			t.Fatalf("set %s theme: %v", theme, err)
		}
		state, err := page.Evaluate(`() => {
		  const styles = getComputedStyle(document.documentElement);
		  const names = ["--bg-app", "--bg-surface", "--bg-surface-elevated", "--bg-card", "--bg-input"];
		  return {
		    muted: styles.getPropertyValue("--text-muted").trim(),
		    backgrounds: Object.fromEntries(names.map(name => [name, styles.getPropertyValue(name).trim()])),
		  };
		}`, nil)
		if err != nil {
			t.Fatalf("read %s theme colors: %v", theme, err)
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			t.Fatalf("encode %s theme colors: %v", theme, err)
		}
		var colors struct {
			Muted       string            `json:"muted"`
			Backgrounds map[string]string `json:"backgrounds"`
		}
		if err := json.Unmarshal(encoded, &colors); err != nil {
			t.Fatalf("decode %s theme colors: %v", theme, err)
		}
		for backgroundName, background := range colors.Backgrounds {
			ratio, err := contrastRatio(colors.Muted, background)
			if err != nil {
				t.Fatalf("calculate %s theme contrast for %s: %v", theme, backgroundName, err)
			}
			if ratio < 4.5 {
				t.Errorf("%s muted text contrast against %s is %.2f:1; want at least 4.5:1", theme, backgroundName, ratio)
			}
		}
	}
}

func contrastRatio(firstColor, secondColor string) (float64, error) {
	firstLuminance, err := relativeLuminance(firstColor)
	if err != nil {
		return 0, err
	}
	secondLuminance, err := relativeLuminance(secondColor)
	if err != nil {
		return 0, err
	}
	if firstLuminance < secondLuminance {
		firstLuminance, secondLuminance = secondLuminance, firstLuminance
	}
	return (firstLuminance + 0.05) / (secondLuminance + 0.05), nil
}

func relativeLuminance(color string) (float64, error) {
	value := strings.TrimPrefix(color, "#")
	if len(value) != 6 {
		return 0, fmt.Errorf("expected six-digit hex color, got %q", color)
	}
	parsed, err := strconv.ParseUint(value, 16, 24)
	if err != nil {
		return 0, fmt.Errorf("parse hex color %q: %w", color, err)
	}
	red := linearizeChannel(float64((parsed>>16)&0xff) / 255)
	green := linearizeChannel(float64((parsed>>8)&0xff) / 255)
	blue := linearizeChannel(float64(parsed&0xff) / 255)
	return 0.2126*red + 0.7152*green + 0.0722*blue, nil
}

func linearizeChannel(channel float64) float64 {
	if channel <= 0.04045 {
		return channel / 12.92
	}
	return math.Pow((channel+0.055)/1.055, 2.4)
}

func TestSearchPeriodMenuClosesWhenKeyboardFocusLeaves(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	trigger := page.Locator("#search-period-trigger")
	if err := page.Locator("#process-filter-trigger").Click(); err != nil {
		t.Fatalf("open Filters: %v", err)
	}
	if err := trigger.Focus(); err != nil {
		t.Fatalf("focus search period selector: %v", err)
	}
	if err := page.Keyboard().Press("ArrowDown"); err != nil {
		t.Fatalf("open search period options: %v", err)
	}
	assertVisible(t, page.Locator("#search-period-menu"))
	if err := page.Locator("#process-filter-expected-salary").Focus(); err != nil {
		t.Fatalf("move focus to another filter: %v", err)
	}
	if _, err := page.WaitForFunction("() => document.querySelector('#search-period-menu').hidden", nil); err != nil {
		t.Fatalf("wait for search period menu to close: %v", err)
	}
	menuVisible, err := page.Locator("#search-period-menu").IsVisible()
	if err != nil {
		t.Fatalf("check search period menu visibility: %v", err)
	}
	if menuVisible {
		t.Fatal("search period menu should close after keyboard focus leaves the selector")
	}
	activeID, err := page.Evaluate("() => document.activeElement.id", nil)
	if err != nil {
		t.Fatalf("read focused control after leaving search period selector: %v", err)
	}
	if activeID != "process-filter-expected-salary" {
		t.Fatalf("keyboard focus should continue to another filter after leaving the selector, got %q", activeID)
	}
}

func assertToolbarWithActiveFilter(t *testing.T, page playwright.Page) {
	t.Helper()
	setPageViewport(t, page, 390, 844)
	if err := page.Locator("#process-filter-trigger").Click(); err != nil {
		t.Fatalf("open process filters: %v", err)
	}
	if err := page.Locator(`input[name="process-filter-arrangement"][value="hybrid"]`).Click(); err != nil {
		t.Fatalf("select hybrid filter: %v", err)
	}
	layout := readToolbarLayout(t, page, 390)
	assertCompactControlRows(t, 390, layout)
}

func readToolbarLayout(t *testing.T, page playwright.Page, width int) toolbarLayout {
	t.Helper()
	if _, err := page.Evaluate("() => document.getAnimations().forEach((animation) => animation.finish())", nil); err != nil {
		t.Fatalf("finish toolbar entrance animations at %dpx: %v", width, err)
	}
	state, err := page.Evaluate(`() => {
	  const box = selector => {
	    const rect = document.querySelector(selector).getBoundingClientRect();
	    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height, right: rect.right };
	  };
	  return {
	    viewportWidth: window.innerWidth,
	    documentWidth: document.documentElement.scrollWidth,
	    cardColumns: getComputedStyle(document.querySelector(".cards-grid")).gridTemplateColumns.split(/\s+/).filter(Boolean).length,
	    search: box(".process-search"),
	    searchFontSize: Number.parseFloat(getComputedStyle(document.querySelector("#search-input")).fontSize),
		searchFilterWidth: (() => {
		  const group = document.querySelector(".toolbar-search-filter").getBoundingClientRect();
		  return { x: group.x, y: group.y, width: group.width, height: group.height, right: group.right };
		})(),
	    firstCard: (() => {
	      const card = document.querySelector(".process-card");
	      if (!card) return { x: 0, y: 0, width: 0, height: 0, right: 0 };
	      const rect = card.getBoundingClientRect();
	      return { x: rect.x, y: rect.y, width: rect.width, height: rect.height, right: rect.right };
	    })(),
	    filter: box(".process-filter-control"),
	    viewControls: box(".toolbar-view-controls"),
	    sort: box(".process-sort-control"),
	    toolbar: box(".process-toolbar"),
	    header: box(".header-inner"),
	    theme: box("#btn-theme-toggle"),
	    filterHeight: box("#process-filter-trigger").height,
		sortHeight: box("#process-sort-trigger").height,
	    periodInToolbar: document.querySelector("#search-period-trigger").closest("#process-toolbar") !== null,
	    periodInFilters: document.querySelector("#search-period-trigger").closest("#process-filter-panel") !== null,
		periodInViewControls: document.querySelector("#search-period-trigger").closest(".toolbar-view-controls") !== null,
	  };
	}`, nil)
	if err != nil {
		t.Fatalf("read toolbar geometry at %dpx: %v", width, err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("encode toolbar geometry at %dpx: %v", width, err)
	}
	var layout toolbarLayout
	if err := json.Unmarshal(encoded, &layout); err != nil {
		t.Fatalf("decode toolbar geometry at %dpx: %v", width, err)
	}
	return layout
}

func assertToolbarHasNoOverflow(t *testing.T, width int, layout toolbarLayout) {
	t.Helper()
	if layout.DocumentWidth > layout.ViewportWidth {
		t.Errorf("%dpx toolbar causes horizontal overflow: document %.0fpx, viewport %.0fpx", width, layout.DocumentWidth, layout.ViewportWidth)
	}
}

func assertToolbarAtViewport(t *testing.T, width int, layout toolbarLayout) {
	t.Helper()
	if !layout.PeriodInFilters || layout.PeriodInViewControls {
		t.Errorf("%dpx period selector should live inside Filters", width)
	}
	if layout.CardColumns > 1 {
		assertToolbarRow(t, width, layout.Search, layout.Filter, layout.Sort)
		assertWideToolbar(t, width, layout)
	} else if width > 420 {
		assertCompactToolbar(t, width, layout)
	} else {
		assertNarrowToolbar(t, width, layout)
	}
	assertHeaderActionAlignment(t, width, layout)
}

func assertWideToolbar(t *testing.T, width int, layout toolbarLayout) {
	t.Helper()
	assertToolbarRow(t, width, layout.Search, layout.Filter, layout.Sort)
	if abs(layout.SearchFilterWidth.Width-421) > 2 {
		t.Errorf("%dpx search and filters should keep their preferred 421px width (got %.1fpx)", width, layout.SearchFilterWidth.Width)
	}
	expectedSearchWidth := 342.0
	if abs(layout.Search.Width-expectedSearchWidth) > 2 {
		t.Errorf("%dpx toolbar search should use the available width before Filters (got %.0fpx, expected %.0fpx)", width, layout.Search.Width, expectedSearchWidth)
	}
	if abs(layout.Sort.Right-layout.Toolbar.Right) > 2 {
		t.Errorf("%dpx toolbar should align sort to the right edge (sort %.1fpx, toolbar %.1fpx)", width, layout.Sort.Right, layout.Toolbar.Right)
	}
}

func assertCompactToolbar(t *testing.T, width int, layout toolbarLayout) {
	t.Helper()
	assertCompactControlRows(t, width, layout)
	assertToolbarRow(t, width, layout.Filter, layout.Sort)
	assertSearchWidthUsesAvailableSpace(t, width, layout)
	if layout.FilterHeight < 40 || layout.SortHeight < 40 {
		t.Errorf("%dpx viewport has undersized touch controls (filter %.0fpx, sort %.0fpx)", width, layout.FilterHeight, layout.SortHeight)
	}
}

func assertSearchWidthUsesAvailableSpace(t *testing.T, width int, layout toolbarLayout) {
	t.Helper()
	expected := min(342.0, layout.Toolbar.Width)
	if abs(layout.Search.Width-expected) > 2 {
		t.Errorf("%dpx viewport should keep search at its preferred 342px width until space is constrained (search %.1fpx, expected %.1fpx)", width, layout.Search.Width, expected)
	}
}

func assertCompactControlRows(t *testing.T, width int, layout toolbarLayout) {
	t.Helper()
	if width <= 420 {
		return
	}
	assertToolbarRow(t, width, layout.Filter, layout.ViewControls)
}

func assertNarrowToolbar(t *testing.T, width int, layout toolbarLayout) {
	t.Helper()
	if abs((layout.Sort.Y+layout.Sort.Height/2)-(layout.Filter.Y+layout.Filter.Height/2)) > 1 {
		t.Errorf("%dpx toolbar should keep sort aligned with Filters", width)
	}
	assertSearchWidthUsesAvailableSpace(t, width, layout)
	if abs(layout.Sort.Right-layout.Toolbar.Right) > 2 {
		t.Errorf("%dpx sort control should remain aligned to the toolbar's right edge (sort %.1fpx, toolbar %.1fpx)", width, layout.Sort.Right, layout.Toolbar.Right)
	}
}

func assertHeaderActionAlignment(t *testing.T, width int, layout toolbarLayout) {
	t.Helper()
	if width > 1000 && abs(layout.Header.Right-layout.Theme.Right) > 2 {
		t.Errorf("desktop actions should align to the header's right inset (header %.1fpx, theme %.1fpx)", layout.Header.Right, layout.Theme.Right)
	}
}

func assertToolbarRow(t *testing.T, width int, boxes ...layoutBox) {
	t.Helper()
	for _, box := range boxes[1:] {
		if abs((box.Y+box.Height/2)-(boxes[0].Y+boxes[0].Height/2)) > 1 {
			t.Errorf("%dpx viewport should keep controls on one row", width)
			return
		}
	}
}

func assertExpectedSalaryInputStyle(t *testing.T, page playwright.Page) {
	t.Helper()
	setPageViewport(t, page, 1440, 900)
	input := page.Locator("#process-filter-expected-salary")
	visible, err := input.IsVisible()
	if err != nil {
		t.Fatalf("check expected salary input visibility: %v", err)
	}
	if !visible {
		if err := page.Locator("#process-filter-trigger").Click(); err != nil {
			t.Fatalf("open process filters: %v", err)
		}
	}
	if err := input.Fill("€85k"); err != nil {
		t.Fatalf("enter expected salary filter: %v", err)
	}
	style, err := input.Evaluate(`element => ({
	  type: element.type,
	  height: getComputedStyle(element).height,
	  radius: getComputedStyle(element).borderRadius,
	  fontSize: getComputedStyle(element).fontSize,
	})`, nil)
	if err != nil {
		t.Fatalf("read expected salary input style: %v", err)
	}
	styleValues := style.(map[string]interface{})
	if styleValues["type"] != "text" || styleValues["radius"] == "0px" || styleValues["height"] != "38px" || styleValues["fontSize"] != "12px" {
		t.Errorf("expected salary filter should use the styled app input, got %#v", styleValues)
	}
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func TestDemoSearchFiltersAndTodayMeeting(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	assertCardCount(t, page, 6)

	for _, filter := range []struct {
		name  string
		count int
	}{{"all", 12}, {"accepted", 1}, {"rejected", 2}, {"waiting", 3}, {"ongoing", 6}} {
		selectFilter(t, page, filter.name)
		assertCardCount(t, page, filter.count)
	}

	search := page.Locator("#search-input")
	if err := search.Fill("openai"); err != nil {
		t.Fatalf("search by lowercase company name: %v", err)
	}
	waitForCardCount(t, page, 1)
	cardText, err := page.Locator(".process-card").First().TextContent()
	if err != nil || !strings.Contains(cardText, "OpenAI") {
		t.Fatalf("case-insensitive search returned wrong card %q (err=%v)", cardText, err)
	}
	if err := search.Fill("no-company-or-role-matches-this"); err != nil {
		t.Fatalf("search for missing company: %v", err)
	}
	waitForCardCount(t, page, 0)
	assertVisible(t, page.Locator(".empty-state"))
	if err := search.Fill(""); err != nil {
		t.Fatalf("clear search: %v", err)
	}
	assertCardCount(t, page, 6)

	assertDemoMeetingToday(t, page)
	assertTodayMeetingsStack(t, page)
}

func TestDemoNvidiaLogoLoads(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	logo := page.Locator(`.process-card[data-id="demo-3"] img.company-logo-img`)
	if _, err := page.WaitForFunction(`() => {
		const logo = document.querySelector('.process-card[data-id="demo-3"] img.company-logo-img');
		return logo?.complete && logo.naturalWidth > 0;
	}`, nil); err != nil {
		t.Fatalf("wait for NVIDIA logo to load: %v", err)
	}
	source, err := logo.GetAttribute("src")
	if err != nil || !strings.HasSuffix(source, "/demo/assets/logos/nvidia-eye.png") {
		t.Fatalf("NVIDIA demo card logo source=%q, want bundled NVIDIA eye mark (err=%v)", source, err)
	}
}

func TestKeyboardAccessibilityForNavigation(t *testing.T) {
	page := newPage(t)
	assertSkipLinkIsFirstFocus(t, page)
	assertSearchAccessibleName(t, page)
	assertFilterPressedStates(t, page, "ongoing")
	selectFilter(t, page, "accepted")
	assertFilterPressedStates(t, page, "accepted")
}

func TestModalContainsAndRestoresKeyboardFocus(t *testing.T) {
	page := newPage(t)
	openAndAssertDialogNameAndFocus(t, page)
	assertDialogFocusWraps(t, page)
	closeDialogAndAssertFocusRestored(t, page)
}

func openAndAssertDialogNameAndFocus(t *testing.T, page playwright.Page) {
	t.Helper()
	trigger := page.Locator("#btn-new-process")
	if err := trigger.Click(); err != nil {
		t.Fatalf("open process dialog: %v", err)
	}
	state, err := page.Evaluate(`() => {
		const dialog = document.querySelector("#detail-modal");
		const name = document.querySelector('input[name="position_title"]');
		const labelId = dialog.getAttribute("aria-labelledby");
		return document.activeElement === name && dialog.getAttribute("aria-modal") === "true" &&
			document.querySelector(".app-container").inert && labelId &&
			document.getElementById(labelId)?.textContent.includes("Add New Selection Process");
	}`, nil)
	if err != nil || state != true {
		t.Fatalf("opening a dialog should name it and focus its first field (state=%v, err=%v)", state, err)
	}
}

func assertDialogFocusWraps(t *testing.T, page playwright.Page) {
	t.Helper()
	if _, err := page.Evaluate(`() => {
		const dialog = document.querySelector("#detail-modal");
		const controls = [...dialog.querySelectorAll('a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])')];
		controls.at(-1).focus();
	}`, nil); err != nil {
		t.Fatalf("focus final dialog control: %v", err)
	}
	if err := page.Keyboard().Press("Tab"); err != nil {
		t.Fatalf("tab from final dialog control: %v", err)
	}
	wrapped, err := page.Evaluate(`() => document.activeElement === document.querySelector("#detail-modal .modal-close-btn")`, nil)
	if err != nil || wrapped != true {
		t.Fatalf("tabbing should wrap to the first dialog control (wrapped=%v, err=%v)", wrapped, err)
	}
}

func closeDialogAndAssertFocusRestored(t *testing.T, page playwright.Page) {
	t.Helper()
	if err := page.Keyboard().Press("Escape"); err != nil {
		t.Fatalf("close process dialog: %v", err)
	}
	if _, err := page.WaitForFunction(`() => !document.querySelector("#modal-backdrop").classList.contains("active") && document.querySelector("#detail-modal").innerHTML === ""`, nil); err != nil {
		t.Fatalf("wait for dialog close: %v", err)
	}
	restored, err := page.Evaluate(`() => document.activeElement === document.querySelector("#btn-new-process") && !document.querySelector(".app-container").inert`, nil)
	if err != nil || restored != true {
		t.Fatalf("closing should return focus to its trigger (restored=%v, err=%v)", restored, err)
	}
}

func assertSkipLinkIsFirstFocus(t *testing.T, page playwright.Page) {
	t.Helper()
	if err := page.Keyboard().Press("Tab"); err != nil {
		t.Fatalf("focus first keyboard control: %v", err)
	}
	skipLinkFocused, err := page.Evaluate(`() => {
		const link = document.querySelector(".skip-link");
		return document.activeElement === link && link.getAttribute("href") === "#main-content" &&
			getComputedStyle(link).transform === "matrix(1, 0, 0, 1, 0, 0)";
	}`, nil)
	if err != nil || skipLinkFocused != true {
		t.Fatalf("first keyboard control should reveal the skip link (focused=%v, err=%v)", skipLinkFocused, err)
	}
}

func assertSearchAccessibleName(t *testing.T, page playwright.Page) {
	t.Helper()
	searchName, err := page.Locator("#search-input").GetAttribute("aria-label")
	if err != nil || searchName != "Search selection processes" {
		t.Fatalf("search field accessible name=%q, err=%v", searchName, err)
	}
}

func assertFilterPressedStates(t *testing.T, page playwright.Page, activeFilter string) {
	t.Helper()
	for _, filter := range []string{"ongoing", "accepted", "rejected", "all"} {
		pressed, err := page.Locator(fmt.Sprintf(`[data-filter="%s"]`, filter)).GetAttribute("aria-checked")
		if err != nil || pressed != fmt.Sprint(filter == activeFilter) {
			t.Fatalf("initial %s filter pressed state=%q, err=%v", filter, pressed, err)
		}
	}
}

func assertDemoMeetingToday(t *testing.T, page playwright.Page) {
	t.Helper()
	selectFilter(t, page, "schedule")
	assertVisible(t, page.Locator(".schedule-container .today-status-title"))
	if count, err := page.Locator(".today-meeting-card").Count(); err != nil || count < 1 {
		t.Fatalf("demo should always include a meeting today (count=%d, err=%v)", count, err)
	}
}

func assertTodayMeetingsStack(t *testing.T, page playwright.Page) {
	t.Helper()
	for _, width := range []int{1440, 402, 320} {
		if err := page.SetViewportSize(width, 950); err != nil {
			t.Fatalf("set %dpx viewport for Today meetings: %v", width, err)
		}
		stacked, err := page.Evaluate(`() => {
			const grid = document.querySelector(".today-meetings-grid");
			const first = grid?.querySelector(".today-meeting-card");
			if (!grid || !first) return false;
			const clone = first.cloneNode(true);
			clone.dataset.e2eLayoutProbe = "true";
			grid.appendChild(clone);
			const firstRect = first.getBoundingClientRect();
			const secondRect = clone.getBoundingClientRect();
			clone.remove();
			return secondRect.top >= firstRect.bottom && Math.abs(secondRect.left - firstRect.left) < 1;
		}`, nil)
		if err != nil || stacked != true {
			t.Fatalf("Today meeting cards are not stacked at %dpx (stacked=%v, err=%v)", width, stacked, err)
		}
	}
}

func TestPhoneViewportsDoNotClipCardsOrControls(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	for _, width := range []int{402, 390, 320} {
		if err := page.SetViewportSize(width, 850); err != nil {
			t.Fatalf("set %dpx viewport: %v", width, err)
		}
		valid, err := page.Evaluate(`() => {
			const card = document.querySelector(".process-card");
			const menu = document.querySelector("#btn-menu-toggle");
			const dataManagement = document.querySelector("#btn-data-management");
			const cardRect = card?.getBoundingClientRect();
			const overflowingControl = card && [...card.querySelectorAll("button, a, input, select")].some(control => {
				const rect = control.getBoundingClientRect();
				return rect.width > 0 && (rect.left < cardRect.left - 1 || rect.right > cardRect.right + 1 || rect.right > window.innerWidth + 1);
			});
			return document.documentElement.scrollWidth <= window.innerWidth &&
				card && card.getBoundingClientRect().right <= window.innerWidth &&
				!overflowingControl && card.scrollWidth <= card.clientWidth + 1 &&
				dataManagement && dataManagement.getBoundingClientRect().height >= 44 &&
				getComputedStyle(menu).display !== "none";
		}`, nil)
		if err != nil || valid != true {
			metrics, _ := page.Evaluate(`() => {
				const card = document.querySelector(".process-card");
				const rect = card?.getBoundingClientRect();
				const overflowing = card && [...card.querySelectorAll("button, a, input, select")].filter(control => {
					const box = control.getBoundingClientRect();
					return box.width > 0 && (box.left < rect.left - 1 || box.right > rect.right + 1 || box.right > window.innerWidth + 1);
				}).map(control => ({label: control.getAttribute("aria-label") || control.textContent.trim(), box: control.getBoundingClientRect().toJSON()}));
				return JSON.stringify({viewport: window.innerWidth, document: document.documentElement.scrollWidth, card: rect?.toJSON(), cardWidth: card?.clientWidth, cardScrollWidth: card?.scrollWidth, overflowing, menu: getComputedStyle(document.querySelector("#btn-menu-toggle")).display});
			}`, nil)
			t.Fatalf("phone layout clips a card or hides navigation at %dpx (valid=%v, metrics=%v, err=%v)", width, valid, metrics, err)
		}
	}
	if err := page.SetViewportSize(402, 850); err != nil {
		t.Fatalf("restore phone viewport for search check: %v", err)
	}
	if err := page.Locator("#btn-menu-toggle").Click(); err != nil {
		t.Fatalf("open phone navigation menu: %v", err)
	}
	if err := page.Locator(".search-focus").Click(); err != nil {
		t.Fatalf("focus search in phone navigation menu: %v", err)
	}
	validSearch, err := page.Evaluate(`() => {
		const input = document.querySelector("#search-input");
		return document.activeElement === input &&
			getComputedStyle(input).fontSize === "13px" &&
			document.querySelector("#btn-menu-toggle").getAttribute("aria-expanded") === "true" &&
			document.documentElement.scrollWidth <= window.innerWidth;
	}`, nil)
	if err != nil || validSearch != true {
		metrics, _ := page.Evaluate(`() => JSON.stringify({active: document.activeElement?.id, font: getComputedStyle(document.querySelector("#search-input")).fontSize, expanded: document.querySelector("#btn-menu-toggle").getAttribute("aria-expanded"), viewport: window.innerWidth, document: document.documentElement.scrollWidth})`, nil)
		t.Fatalf("phone search should stay focused with consistent text and without clipping (valid=%v, metrics=%v, err=%v)", validSearch, metrics, err)
	}
}

func TestMobileCardSurfaceAllowsPageScrolling(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	setPageViewport(t, page, 390, 844)
	if err := page.Locator("#process-sort-trigger").Click(); err != nil {
		t.Fatalf("open sort options: %v", err)
	}
	if err := page.Locator(`[data-sort-mode="manual"]`).Click(); err != nil {
		t.Fatalf("enable manual card order: %v", err)
	}
	state, err := page.Evaluate(`() => {
		const card = document.querySelector(".process-card");
		const dragHandle = card?.querySelector(".btn-card-drag");
		return {
			cardTouchAction: card && getComputedStyle(card).touchAction,
			dragHandleTouchAction: dragHandle && getComputedStyle(dragHandle).touchAction,
			pageCanScroll: document.documentElement.scrollHeight > window.innerHeight,
		};
	}`, nil)
	if err != nil {
		t.Fatalf("read phone card touch behavior: %v", err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("encode phone card touch behavior: %v", err)
	}
	var touchState struct {
		CardTouchAction       string `json:"cardTouchAction"`
		DragHandleTouchAction string `json:"dragHandleTouchAction"`
		PageCanScroll         bool   `json:"pageCanScroll"`
	}
	if err := json.Unmarshal(encoded, &touchState); err != nil {
		t.Fatalf("decode phone card touch behavior: %v", err)
	}
	if touchState.CardTouchAction != "pan-y" || touchState.DragHandleTouchAction != "none" || !touchState.PageCanScroll {
		t.Errorf("phone card surface should allow page scrolling while the drag handle retains drag control: %+v", touchState)
	}
}

func openDemoPage(t *testing.T, page playwright.Page) {
	t.Helper()
	if _, err := page.Goto(baseURL + "/?demo"); err != nil {
		t.Fatalf("open demo application: %v", err)
	}
	if _, err := page.WaitForFunction("() => document.body.dataset.appReady === 'true'", nil); err != nil {
		t.Fatalf("wait for demo initialization: %v", err)
	}
}

func assertCardCount(t *testing.T, page playwright.Page, count int) {
	t.Helper()
	waitForCardCount(t, page, count)
	actual, err := page.Locator(".process-card").Count()
	if err != nil || actual != count {
		t.Fatalf("card count=%d, want %d (err=%v)", actual, count, err)
	}
}

func waitForCardCount(t *testing.T, page playwright.Page, count int) {
	t.Helper()
	expression := fmt.Sprintf("() => document.querySelectorAll('.process-card').length === %d", count)
	if _, err := page.WaitForFunction(expression, nil); err != nil {
		t.Fatalf("wait for %d cards: %v", count, err)
	}
}

func TestNarrowViewportNavigation(t *testing.T) {
	page := newPage(t)
	if err := page.SetViewportSize(760, 850); err != nil {
		t.Fatalf("set narrow viewport: %v", err)
	}
	assertVisible(t, page.Locator("#btn-menu-toggle"))
	validLayout, err := page.Evaluate(`() => {
		const toggle = document.querySelector("#btn-menu-toggle");
		toggle.click();
		return getComputedStyle(document.querySelector("#header-controls")).display !== "none" &&
			getComputedStyle(document.querySelector("#btn-new-process-mobile")).display !== "none" &&
			getComputedStyle(document.querySelector("#btn-new-process-mobile .new-process-label")).display === "none" &&
			document.documentElement.scrollWidth <= window.innerWidth;
	}`, nil)
	if err != nil || validLayout != true {
		t.Fatalf("narrow viewport navigation or layout is incorrect (valid=%v, err=%v)", validLayout, err)
	}
}

func TestDemoTechnologyStacksFitNarrowCards(t *testing.T) {
	page := newPage(t)
	if err := page.SetViewportSize(390, 844); err != nil {
		t.Fatalf("set phone viewport: %v", err)
	}
	openDemoPage(t, page)
	valid, err := page.Evaluate(`() => {
		const stacks = [...document.querySelectorAll('.card-tech-stack')];
		return stacks.length > 0 && document.documentElement.scrollWidth <= window.innerWidth && stacks.every(stack => {
			const bounds = stack.getBoundingClientRect();
			return bounds.left >= 0 && bounds.right <= window.innerWidth;
		});
	}`, nil)
	if err != nil || valid != true {
		t.Fatalf("technology stack cards exceed the phone viewport (valid=%v, err=%v)", valid, err)
	}
}

func TestMobileCardHighlightsDoNotReserveDesktopHeight(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	setPageViewport(t, page, 390, 844)
	minimumHeight, err := page.Locator(".process-card .keyword-note").First().Evaluate("element => getComputedStyle(element).minHeight", nil)
	if err != nil {
		t.Fatalf("read phone role highlight minimum height: %v", err)
	}
	if minimumHeight != "0px" {
		t.Errorf("phone role highlights should use their natural height, got %v", minimumHeight)
	}
}

func TestDemoStageTabsExposeHorizontalOverflow(t *testing.T) {
	page := newPage(t)
	openDemoPage(t, page)
	setPageViewport(t, page, 1440, 900)
	if err := page.Locator("#cards-grid .process-card").First().Click(); err != nil {
		t.Fatalf("open demo process details: %v", err)
	}
	if err := page.Locator("#detail-modal .stages-tab-bar").WaitFor(); err != nil {
		t.Fatalf("wait for interview stage tabs: %v", err)
	}
	if _, err := page.WaitForFunction(`() => {
		const tabs = document.querySelector("#detail-modal .stages-tab-bar");
		const next = document.querySelector("#detail-modal .stage-tabs-scroll-button.is-next");
		return tabs && tabs.scrollWidth > tabs.clientWidth && next && !next.hidden;
	}`, nil); err != nil {
		t.Fatalf("overflowing interview stages should expose a scroll control: %v", err)
	}
	if err := page.Locator("#detail-modal .stage-tabs-scroll-button.is-next").Click(); err != nil {
		t.Fatalf("scroll to additional interview stages: %v", err)
	}
	if _, err := page.WaitForFunction(`() => {
		const tabs = document.querySelector("#detail-modal .stages-tab-bar");
		const previous = document.querySelector("#detail-modal .stage-tabs-scroll-button.is-prev");
		return tabs && tabs.scrollLeft > 0 && previous && !previous.hidden;
	}`, nil); err != nil {
		t.Fatalf("scrolling stages should expose the control to return left: %v", err)
	}
	setPageViewport(t, page, 390, 844)
	if err := page.Locator(`[data-mobile-pane="stages"]`).Click(); err != nil {
		t.Fatalf("open phone interview stages: %v", err)
	}
	valid, err := page.Evaluate(`() => {
		const row = document.querySelector("#detail-modal .stage-hero-actions")?.getBoundingClientRect();
		const current = document.querySelector("#detail-modal #btn-toggle-current-stage")?.getBoundingClientRect();
		const moveUp = document.querySelector("#detail-modal #btn-move-stage-up")?.getBoundingClientRect();
		const remove = document.querySelector("#detail-modal #btn-delete-stage")?.getBoundingClientRect();
		const relationship = document.querySelector("#detail-modal .stage-recruiter-source-select");
		return Boolean(row && current && moveUp && remove)
			&& current.right < moveUp.left
			&& remove.right <= row.right + 1
			&& relationship?.getBoundingClientRect().height >= 44
			&& Number.parseFloat(getComputedStyle(relationship).fontSize) >= 16;
	}`, nil)
	if err != nil || valid != true {
		t.Errorf("on phone widths, keep stage controls grouped and the interviewer role select easy to tap (valid=%v, err=%v)", valid, err)
	}
	assertPhoneOverviewTapTargets(t, page)
}

func assertPhoneOverviewTapTargets(t *testing.T, page playwright.Page) {
	t.Helper()
	if err := page.Locator(`[data-mobile-pane="overview"]`).Click(); err != nil {
		t.Fatalf("open phone job overview: %v", err)
	}
	valid, err := page.Evaluate(`() => {
		const search = document.querySelector("#detail-modal #technology-assignment-search");
		const editButtons = [...document.querySelectorAll("#detail-modal .split-pane-left .markdown-field-edit")];
		return search?.getBoundingClientRect().height >= 44
			&& Number.parseFloat(getComputedStyle(search).fontSize) >= 16
			&& editButtons.length > 0
			&& editButtons.every(button => button.getBoundingClientRect().width >= 44);
	}`, nil)
	if err != nil || valid != true {
		t.Errorf("on phone widths, make technology search readable and markdown edit buttons easy to tap (valid=%v, err=%v)", valid, err)
	}
}

func TestIntermediateViewportUsesOpenSearchMenu(t *testing.T) {
	page := newPage(t)
	if err := page.SetViewportSize(780, 850); err != nil {
		t.Fatalf("set intermediate viewport: %v", err)
	}
	menu := page.Locator("#btn-menu-toggle")
	if err := menu.Click(); err != nil {
		t.Fatalf("open intermediate navigation menu: %v", err)
	}
	searchWrapper := page.Locator(".search-focus")
	if err := searchWrapper.Click(); err != nil {
		t.Fatalf("focus search in intermediate navigation menu: %v", err)
	}
	search := page.Locator("#search-input")
	if err := search.Fill("platform"); err != nil {
		t.Fatalf("type in search while intermediate navigation menu is open: %v", err)
	}
	validMenu, err := page.Evaluate(`() => {
		const menu = document.querySelector("#btn-menu-toggle");
		return getComputedStyle(menu).display !== "none" &&
			menu.getAttribute("aria-expanded") === "true" &&
			document.querySelector("#header-controls").classList.contains("is-open") &&
			document.activeElement === document.querySelector("#search-input") &&
			document.querySelector("#search-input").value === "platform" &&
			document.documentElement.scrollWidth <= window.innerWidth;
	}`, nil)
	if err != nil || validMenu != true {
		t.Fatalf("intermediate search menu should stay open and fit the viewport (valid=%v, err=%v)", validMenu, err)
	}
}

func TestCompactDesktopKeepsDailyScheduleTabVisible(t *testing.T) {
	page := newPage(t)
	for _, width := range []int{1001, 1024, 1080, 1136, 1150, 1151, 1280, 1360, 1361} {
		if err := page.SetViewportSize(width, 850); err != nil {
			t.Fatalf("set %dpx desktop viewport: %v", width, err)
		}
		visibleTab, err := page.Evaluate(`() => {
			const tabs = document.querySelector(".filter-tabs");
			const views = document.querySelector(".view-controls");
			const trigger = document.querySelector("#filter-menu-trigger");
			const create = document.querySelector("#btn-new-process");
			const theme = document.querySelector("#btn-theme-toggle");
			const bounds = trigger.getBoundingClientRect();
			return [
				getComputedStyle(document.querySelector("#btn-menu-toggle")).display === "none",
				tabs.scrollWidth <= tabs.clientWidth, bounds.right <= window.innerWidth,
				bounds.left >= tabs.getBoundingClientRect().left,
				views.getBoundingClientRect().right <= create.getBoundingClientRect().left,
				create.getBoundingClientRect().right <= theme.getBoundingClientRect().left,
				theme.getBoundingClientRect().right <= window.innerWidth,
				document.querySelector("#btn-data-management") !== null,
				document.querySelector(".filter-current-group").innerText.includes(document.querySelector("#filter-current-label").innerText),
			].map(Boolean).join(",");
		}`, nil)
		if err != nil || visibleTab != "true,true,true,true,true,true,true,true,true" {
			t.Fatalf("compact desktop controls overlap or clip the process view selector at %dpx (visible=%v, err=%v)", width, visibleTab, err)
		}
		if err := page.Locator("#filter-menu-trigger").Click(); err != nil {
			t.Fatalf("open process view menu at %dpx: %v", width, err)
		}
		assertVisible(t, page.Locator("#tab-schedule"))
		if err := page.Keyboard().Press("Escape"); err != nil {
			t.Fatalf("close process view menu at %dpx: %v", width, err)
		}
	}
}

func TestSecurityHeadersBlockExternalImages(t *testing.T) {
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(baseURL) // #nosec G107 -- targets the configured local E2E application.
	if err != nil {
		t.Fatalf("read application security headers: %v", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close application response: %v", err)
		}
	}()
	policy := response.Header.Get("Content-Security-Policy")
	if !strings.Contains(policy, "img-src 'self' data:") || strings.Contains(policy, "https:") {
		t.Fatalf("image policy allows unneeded external requests: %q", policy)
	}
}

func TestKeyboardSearchAndFilterShortcuts(t *testing.T) {
	page := newPage(t)
	search := page.Locator("#search-input")
	if err := page.Keyboard().Press("/"); err != nil {
		t.Fatalf("press search shortcut: %v", err)
	}
	if err := playwright.NewPlaywrightAssertions().Locator(search).ToBeFocused(); err != nil {
		t.Fatalf("search shortcut did not focus the field: %v", err)
	}
	if err := search.Blur(); err != nil {
		t.Fatalf("blur search field: %v", err)
	}
	for key, filter := range map[string]string{"5": "all", "1": "waiting", "2": "ongoing"} {
		if err := page.Keyboard().Press(key); err != nil {
			t.Fatalf("press filter shortcut %s: %v", key, err)
		}
		className, err := page.Locator(fmt.Sprintf(`.filter-tab[data-filter="%s"]`, filter)).GetAttribute("class")
		if err != nil || !strings.Contains(className, "active") {
			t.Fatalf("shortcut %s did not activate %s filter (class=%q, err=%v)", key, filter, className, err)
		}
	}
}

func TestCreateJobAndScheduleView(t *testing.T) {
	page := newPage(t)
	uniqueTitle := fmt.Sprintf("E2E Security Engineer %d", time.Now().UnixNano())
	if err := page.Locator("#btn-new-process").Click(); err != nil {
		t.Fatalf("open create form: %v", err)
	}
	assertVisible(t, page.Locator("#detail-modal"))
	if err := page.Locator("details.process-form-disclosure").First().Locator("summary").Click(); err != nil {
		t.Fatalf("open optional job details: %v", err)
	}
	fill(t, page.Locator(`input[name="company_name"]`), "E2E Test Company")
	fill(t, page.Locator(`input[name="position_title"]`), uniqueTitle)
	fill(t, page.Locator(`input[name="salary_min"]`), "85000")
	fill(t, page.Locator(`input[name="salary_max"]`), "130000")
	if err := page.Locator(`button[type="submit"]`).Click(); err != nil {
		t.Fatalf("submit create form: %v", err)
	}
	card := page.Locator(fmt.Sprintf(`.process-card:has-text("%s")`, uniqueTitle))
	if err := card.WaitFor(); err != nil {
		t.Fatalf("wait for created card: %v", err)
	}
	cardText, err := card.TextContent()
	if err != nil {
		t.Fatalf("read created card: %v", err)
	}
	if !strings.Contains(cardText, "E2E Test Company") {
		t.Fatalf("created card is missing company name: %q", cardText)
	}
	scheduleMeetingWithNotes(t, page, card)
	assertScheduleViewShowsSavedNotes(t, page)
}

func assertScheduleViewShowsSavedNotes(t *testing.T, page playwright.Page) {
	t.Helper()
	selectFilter(t, page, "schedule")
	scheduleHeading := page.Locator(".schedule-container .today-status-title")
	if err := scheduleHeading.WaitFor(); err != nil {
		t.Fatalf("wait for rendered schedule view: %v", err)
	}
	text, err := scheduleHeading.TextContent()
	if err != nil || !strings.HasPrefix(strings.TrimSpace(text), "Today (") {
		t.Fatalf("expected schedule day heading, got %q (err=%v)", text, err)
	}
	note := page.Locator(".schedule-container .meeting-note-content").First()
	if err := note.WaitFor(); err != nil {
		t.Fatalf("saved meeting notes are not visible in schedule view: %v", err)
	}
	if value, err := note.TextContent(); err != nil || strings.TrimSpace(value) != "Note: E2E preparation notes" {
		t.Fatalf("saved meeting notes = %q, err=%v", value, err)
	}
}

func scheduleMeetingWithNotes(t *testing.T, page playwright.Page, card playwright.Locator) {
	t.Helper()
	jobID, err := card.GetAttribute("data-id")
	if err != nil || jobID == "" {
		t.Fatalf("read created process ID before scheduling: id=%q err=%v", jobID, err)
	}
	createTestInterviewStage(t, page, jobID)
	if err := card.Locator(".btn-card-open-details").Click(); err != nil {
		t.Fatalf("open created process details: %v", err)
	}
	if err := page.Locator("#detail-modal .btn-edit-meeting-schedule").Click(); err != nil {
		t.Fatalf("open schedule form: %v", err)
	}
	dateValue, err := page.Evaluate(`() => {
		const date = new Date();
		const pad = value => String(value).padStart(2, "0");
		return date.getFullYear() + "-" + pad(date.getMonth() + 1) + "-" + pad(date.getDate());
	}`, nil)
	if err != nil {
		t.Fatalf("read local date for schedule test: %v", err)
	}
	fill(t, page.Locator(`#schedule-stage-form input[name="meeting_date"]`), dateValue.(string))
	fill(t, page.Locator(`#schedule-stage-form input[name="meeting_time"]`), "11:00 AM - 11:30 AM CEST")
	fill(t, page.Locator(`#schedule-stage-form textarea[name="notes"]`), "E2E preparation notes")
	if err := page.Locator(`#schedule-stage-form button[type="submit"]`).Click(); err != nil {
		t.Fatalf("save schedule form: %v", err)
	}
	if err := page.Locator("#detail-modal .btn-edit-meeting-schedule").WaitFor(); err != nil {
		t.Fatalf("wait for saved schedule details: %v", err)
	}
	if err := page.Locator("#detail-modal .modal-close-btn").Click(); err != nil {
		t.Fatalf("close process details: %v", err)
	}
}

func TestJobStatusCyclesThroughEveryValue(t *testing.T) {
	page := newPage(t)
	uniqueTitle := fmt.Sprintf("E2E Status Cycle %d", time.Now().UnixNano())
	card := createStatusCycleProcess(t, page, uniqueTitle)
	if err := card.Locator(".btn-card-open-details").Click(); err != nil {
		t.Fatalf("open created process details: %v", err)
	}
	status := page.Locator("#detail-modal .editable-job-status")
	if err := status.WaitFor(); err != nil {
		t.Fatalf("wait for editable status: %v", err)
	}
	cycleCreatedStatusToOngoing(t, page, card, status)
	cycleExistingStatuses(t, page, card, status)
	if err := page.Locator("#detail-modal .modal-close-btn").Click(); err != nil {
		t.Fatalf("close process details: %v", err)
	}
	if _, err := page.Reload(); err != nil {
		t.Fatalf("reload after status changes: %v", err)
	}
	if _, err := page.WaitForFunction("() => document.body.dataset.appReady === 'true'", nil); err != nil {
		t.Fatalf("wait for application after reload: %v", err)
	}
	assertPersistedJobStatus(t, uniqueTitle, "waiting")
}

func assertPersistedJobStatus(t *testing.T, title, expectedStatus string) {
	t.Helper()
	response, err := http.Get(baseURL + "/api/jobs?status=all") // #nosec G107 -- targets the configured local E2E application.
	if err != nil {
		t.Fatalf("read persisted jobs: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var jobs []struct {
		PositionTitle string `json:"position_title"`
		Status        string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode persisted jobs: %v", err)
	}
	for _, job := range jobs {
		if job.PositionTitle == title {
			if job.Status != expectedStatus {
				t.Fatalf("persisted status=%q, want %s", job.Status, expectedStatus)
			}
			return
		}
	}
	t.Fatalf("created process %q was not persisted", title)
}

func cycleCreatedStatusToOngoing(t *testing.T, page playwright.Page, card, status playwright.Locator) {
	t.Helper()
	cycleStatusAndVerifyFilter(t, page, card, status, "Waiting", "Ongoing", "ongoing")
}

func cycleExistingStatuses(t *testing.T, page playwright.Page, card, status playwright.Locator) {
	t.Helper()
	cycleStatusAndVerifyFilter(t, page, card, status, "Ongoing", "Rejected", "rejected")
	cycleStatusAndVerifyFilter(t, page, card, status, "Rejected", "Approved", "accepted")
	cycleStatusAndVerifyFilter(t, page, card, status, "Approved", "Waiting", "waiting")
}

func TestCardHoldDragPersistsOrderAndReleasesDragState(t *testing.T) {
	page := newPage(t)
	titleA := fmt.Sprintf("E2E Drag Alpha %d", time.Now().UnixNano())
	titleB := fmt.Sprintf("E2E Drag Beta %d", time.Now().UnixNano())
	cardA := createStatusCycleProcess(t, page, titleA)
	selectSortMode(t, page, "manual")
	cardB := createStatusCycleProcess(t, page, titleB)
	initialOrder := cardOrder(t, page, titleA, titleB)
	source, target := cardA, cardB
	if !initialOrder {
		source, target = cardB, cardA
	}
	dragCardAfter(t, page, source, target)
	expectedOrder := !initialOrder
	if _, err := page.WaitForFunction(cardOrderExpression(titleA, titleB, expectedOrder), nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(3000)}); err != nil {
		t.Fatalf("drag did not reorder cards: %v", err)
	}
	assertCardDragReleased(t, page)
	waitForPersistedCardOrder(t, page, titleA, titleB, expectedOrder)
	if _, err := page.Reload(); err != nil {
		t.Fatalf("reload after card reorder: %v", err)
	}
	waitForApplicationReady(t, page)
	waitForCardOrder(t, page, titleA, titleB, expectedOrder, "card order did not survive reload")
	dragCardAfter(t, page, target, source)
	waitForCardOrder(t, page, titleA, titleB, initialOrder, "reverse drag did not restore the original order")
	assertCardDragReleased(t, page)
	waitForPersistedCardOrder(t, page, titleA, titleB, initialOrder)
	if _, err := page.Reload(); err != nil {
		t.Fatalf("reload after restoring card order: %v", err)
	}
	waitForApplicationReady(t, page)
	waitForCardOrder(t, page, titleA, titleB, initialOrder, "restored card order did not survive reload")
}

func selectSortMode(t *testing.T, page playwright.Page, mode string) {
	t.Helper()
	if err := page.Locator("#process-sort-trigger").Click(); err != nil {
		t.Fatalf("open process sort menu: %v", err)
	}
	if err := page.Locator(`[data-sort-mode="` + mode + `"]`).Click(); err != nil {
		t.Fatalf("select %s process sort: %v", mode, err)
	}
}

func TestInterviewQuestionsCanBeAddedReorderedAndDeleted(t *testing.T) {
	page := newPage(t)
	title := fmt.Sprintf("E2E Question Process %d", time.Now().UnixNano())
	card, jobID := openQuestionProcess(t, page, title)
	questions := page.Locator("#detail-modal .questions-workspace .questions-list")
	questionA := fmt.Sprintf("E2E Question Alpha %d", time.Now().UnixNano())
	questionB := fmt.Sprintf("E2E Question Beta %d", time.Now().UnixNano())
	addInterviewQuestion(t, page, questionA)
	addInterviewQuestion(t, page, questionB)
	itemA := questions.Locator(fmt.Sprintf(`.question-item:has-text("%s")`, questionA))
	itemB := questions.Locator(fmt.Sprintf(`.question-item:has-text("%s")`, questionB))
	initialOrder := questionIsBefore(t, page, questionA, questionB)
	source, target := itemA, itemB
	if !initialOrder {
		source, target = itemB, itemA
	}
	dragQuestionAfter(t, page, source, target)
	expectedOrder := !initialOrder
	waitForQuestionOrder(t, page, questionA, questionB, expectedOrder, "question drag did not reorder the list")
	assertQuestionDragReleased(t, page)
	waitForPersistedQuestionOrder(t, page, jobID, questionA, questionB, expectedOrder)
	reopenQuestionProcess(t, page, card)
	waitForQuestionOrder(t, page, questionA, questionB, expectedOrder, "question order did not survive reload")
	deleteQuestionAndVerify(t, page, itemA, itemB, questionA)
}

func TestQuestionDragOverlayStaysAlignedWithItsCard(t *testing.T) {
	page := newPage(t)
	title := fmt.Sprintf("E2E Question Drag Alignment %d", time.Now().UnixNano())
	openQuestionProcess(t, page, title)
	setPageViewport(t, page, 390, 844)
	question := fmt.Sprintf("E2E Question Alignment %d", time.Now().UnixNano())
	addInterviewQuestion(t, page, question)
	item := page.Locator(fmt.Sprintf(`.questions-workspace .question-item:has-text("%s")`, question))
	itemBox, err := item.BoundingBox()
	if err != nil || itemBox == nil {
		t.Fatalf("read question bounds: box=%v err=%v", itemBox, err)
	}
	handle := item.Locator(".question-drag-handle")
	handleBox, err := handle.BoundingBox()
	if err != nil || handleBox == nil {
		t.Fatalf("read question handle bounds: box=%v err=%v", handleBox, err)
	}
	mouse := page.Mouse()
	if err := mouse.Move(handleBox.X+handleBox.Width/2, handleBox.Y+handleBox.Height/2); err != nil {
		t.Fatalf("move to question handle: %v", err)
	}
	if err := mouse.Down(); err != nil {
		t.Fatalf("press question handle: %v", err)
	}
	defer func() { _ = mouse.Up() }()
	if err := mouse.Move(handleBox.X+handleBox.Width/2, handleBox.Y+handleBox.Height/2+40); err != nil {
		t.Fatalf("move dragged question: %v", err)
	}
	aligned, err := page.Evaluate(`(expectedLeft) => {
		const item = document.querySelector(".question-item.is-dragging");
		return Boolean(item) && Math.abs(item.getBoundingClientRect().left - expectedLeft) < 1;
	}`, itemBox.X)
	if err != nil || aligned != true {
		t.Fatalf("dragged question shifted horizontally from its card (aligned=%v err=%v)", aligned, err)
	}
}

func TestInvalidBackupImportPreservesExistingProcesses(t *testing.T) {
	page := newPage(t)
	title := fmt.Sprintf("E2E Backup Preservation %d", time.Now().UnixNano())
	card := createStatusCycleProcess(t, page, title)
	if _, err := page.WaitForFunction(`() => !document.querySelector("#modal-backdrop")?.classList.contains("active")`, nil); err != nil {
		t.Fatalf("wait for process creation modal to close: %v", err)
	}
	importButton := openBackupDialog(t, page)
	assertBackupImportDisabled(t, importButton, "before closing and reopening the dialog")
	importButton = reopenBackupDuringDetailClose(t, page, card)
	assertBackupImportDisabled(t, importButton, "without a file and confirmation")
	invalidBackup := createInvalidBackupFixture(t)
	if err := page.Locator("#backup-import-file").SetInputFiles(invalidBackup); err != nil {
		t.Fatalf("select invalid backup: %v", err)
	}
	assertBackupImportDisabled(t, importButton, "until replacement is confirmed")
	if err := page.Locator("#backup-import-confirm").Check(); err != nil {
		t.Fatalf("confirm replacement: %v", err)
	}
	assertImportEnabled(t, importButton)
	if err := importButton.Click(); err != nil {
		t.Fatalf("submit invalid backup: %v", err)
	}
	status := page.Locator("#backup-import-status")
	if _, err := page.WaitForFunction(`() => {
		const status = document.querySelector("#backup-import-status")?.textContent.trim();
		return status && status !== "Validating and importing backup…";
	}`, nil); err != nil {
		t.Fatalf("wait for invalid backup error: %v", err)
	}
	message, err := status.TextContent()
	if err != nil || strings.TrimSpace(message) == "" {
		t.Fatalf("expected an invalid backup error message, got %q (err=%v)", message, err)
	}
	if visible, err := card.IsVisible(); err != nil || !visible {
		t.Fatalf("invalid import removed existing process (visible=%t, err=%v)", visible, err)
	}
	assertImportEnabled(t, importButton)
}

func reopenBackupDuringDetailClose(t *testing.T, page playwright.Page, card playwright.Locator) playwright.Locator {
	t.Helper()
	if err := page.Locator("#btn-close-data-modal").Click(); err != nil {
		t.Fatalf("close initial backup dialog: %v", err)
	}
	if err := card.Locator(".btn-card-open-details").Click(); err != nil {
		t.Fatalf("open process details: %v", err)
	}
	closeButton := page.Locator("#detail-modal .modal-close-btn")
	if err := closeButton.WaitFor(); err != nil {
		t.Fatalf("wait for detail close button: %v", err)
	}
	if _, err := page.WaitForFunction(`() => !document.querySelector("#detail-modal")?.classList.contains("modal-animating")`, nil); err != nil {
		t.Fatalf("wait for detail opening animation: %v", err)
	}
	if err := closeButton.Click(); err != nil {
		t.Fatalf("close process details: %v", err)
	}
	if _, err := page.WaitForFunction(`() => !document.querySelector("#modal-backdrop")?.classList.contains("active")`, nil); err != nil {
		t.Fatalf("wait for details backdrop to close: %v", err)
	}
	return openBackupDialog(t, page)
}

func openBackupDialog(t *testing.T, page playwright.Page) playwright.Locator {
	t.Helper()
	if err := page.Locator("#btn-data-management").Click(); err != nil {
		t.Fatalf("open data and backups: %v", err)
	}
	importButton := page.Locator("#btn-import-backup")
	if err := importButton.WaitFor(); err != nil {
		t.Fatalf("wait for import button: %v", err)
	}
	return importButton
}

func createInvalidBackupFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invalid-backup.zip")
	if err := os.WriteFile(path, []byte("not a ZIP archive"), 0o600); err != nil {
		t.Fatalf("write invalid backup fixture: %v", err)
	}
	return path
}

func assertBackupImportDisabled(t *testing.T, button playwright.Locator, reason string) {
	t.Helper()
	if enabled, err := button.IsEnabled(); err != nil || enabled {
		t.Fatalf("import must stay disabled %s (enabled=%t, err=%v)", reason, enabled, err)
	}
}

func assertImportEnabled(t *testing.T, button playwright.Locator) {
	t.Helper()
	if enabled, err := button.IsEnabled(); err != nil || !enabled {
		t.Fatalf("import should be enabled (enabled=%t, err=%v)", enabled, err)
	}
}

func openQuestionProcess(t *testing.T, page playwright.Page, title string) (playwright.Locator, string) {
	t.Helper()
	card := createStatusCycleProcess(t, page, title)
	jobID, err := card.GetAttribute("data-id")
	if err != nil || jobID == "" {
		t.Fatalf("read created process ID: id=%q err=%v", jobID, err)
	}
	createTestInterviewStage(t, page, jobID)
	if err := card.Locator(".btn-card-open-details").Click(); err != nil {
		t.Fatalf("open process details: %v", err)
	}
	if err := page.Locator("#detail-modal .questions-workspace .questions-list").WaitFor(); err != nil {
		t.Fatalf("wait for active stage questions: %v", err)
	}
	return card, jobID
}

func createTestInterviewStage(t *testing.T, page playwright.Page, jobID string) {
	t.Helper()
	createdStage, err := page.Evaluate(`async (id) => {
		const response = await fetch("/api/stages", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ job_id: id, stage_type: "Technical", custom_title: "Technical interview" }),
		});
		return response.ok;
	}`, jobID)
	if err != nil || createdStage != true {
		t.Fatalf("create a test interview stage: success=%v err=%v", createdStage, err)
	}
}

func reopenQuestionProcess(t *testing.T, page playwright.Page, card playwright.Locator) {
	t.Helper()
	if err := page.Locator("#detail-modal .modal-close-btn").Click(); err != nil {
		t.Fatalf("close process details: %v", err)
	}
	if _, err := page.Reload(); err != nil {
		t.Fatalf("reload after question changes: %v", err)
	}
	waitForApplicationReady(t, page)
	selectFilter(t, page, "ongoing")
	if err := card.WaitFor(); err != nil {
		t.Fatalf("wait for process after reload: %v", err)
	}
	if err := card.Locator(".btn-card-open-details").Click(); err != nil {
		t.Fatalf("reopen process details: %v", err)
	}
	if err := page.Locator("#detail-modal .questions-workspace .questions-list").WaitFor(); err != nil {
		t.Fatalf("wait for questions after reload: %v", err)
	}
}

func deleteQuestionAndVerify(t *testing.T, page playwright.Page, itemA, itemB playwright.Locator, questionA string) {
	t.Helper()
	if err := itemA.Locator(".q-del").Click(); err != nil {
		t.Fatalf("delete the reordered question: %v", err)
	}
	expression := fmt.Sprintf(`() => !Array.from(document.querySelectorAll(".questions-workspace .question-item")).some((item) => item.textContent.includes(%q))`, questionA)
	if _, err := page.WaitForFunction(expression, nil); err != nil {
		t.Fatalf("deleted question remained in the list: %v", err)
	}
	if err := itemB.WaitFor(); err != nil {
		t.Fatalf("remaining question disappeared after deletion: %v", err)
	}
}

func addInterviewQuestion(t *testing.T, page playwright.Page, question string) {
	t.Helper()
	input := page.Locator("#active-add-question-form .add-question-input")
	if err := input.Fill(question); err != nil {
		t.Fatalf("fill question: %v", err)
	}
	if err := page.Locator("#active-add-question-form button[type=submit]").Click(); err != nil {
		t.Fatalf("add question: %v", err)
	}
	if err := page.Locator(fmt.Sprintf(`.questions-workspace .question-item:has-text("%s")`, question)).WaitFor(); err != nil {
		t.Fatalf("wait for added question: %v", err)
	}
}

func questionIsBefore(t *testing.T, page playwright.Page, questionA, questionB string) bool {
	t.Helper()
	result, err := page.Evaluate(questionOrderExpression(questionA, questionB, true), nil)
	if err != nil {
		t.Fatalf("read question order: %v", err)
	}
	ordered, ok := result.(bool)
	if !ok {
		t.Fatalf("question order check returned %T, want bool", result)
	}
	return ordered
}

func dragQuestionAfter(t *testing.T, page playwright.Page, source, target playwright.Locator) {
	t.Helper()
	sourceHandle := source.Locator(".question-drag-handle")
	sourceBox, err := sourceHandle.BoundingBox()
	if err != nil || sourceBox == nil {
		t.Fatalf("read question handle bounds: box=%v err=%v", sourceBox, err)
	}
	targetBox, err := target.BoundingBox()
	if err != nil || targetBox == nil {
		t.Fatalf("read target question bounds: box=%v err=%v", targetBox, err)
	}
	mouse := page.Mouse()
	if err := mouse.Move(sourceBox.X+sourceBox.Width/2, sourceBox.Y+sourceBox.Height/2); err != nil {
		t.Fatalf("move pointer to question handle: %v", err)
	}
	if err := mouse.Down(); err != nil {
		t.Fatalf("press question handle: %v", err)
	}
	steps := 10
	if err := mouse.Move(targetBox.X+targetBox.Width/2, targetBox.Y+targetBox.Height*0.8, playwright.MouseMoveOptions{Steps: &steps}); err != nil {
		t.Fatalf("move question after target: %v", err)
	}
	if err := mouse.Up(); err != nil {
		t.Fatalf("release question handle: %v", err)
	}
}

func questionOrderExpression(questionA, questionB string, aBeforeB bool) string {
	return fmt.Sprintf(`() => {
		const items = Array.from(document.querySelectorAll(".questions-workspace .question-item"));
		const a = items.find((item) => item.textContent.includes(%q));
		const b = items.find((item) => item.textContent.includes(%q));
		return Boolean(a && b) && (items.indexOf(a) < items.indexOf(b)) === %t;
	}`, questionA, questionB, aBeforeB)
}

func waitForQuestionOrder(t *testing.T, page playwright.Page, questionA, questionB string, aBeforeB bool, message string) {
	t.Helper()
	if _, err := page.WaitForFunction(questionOrderExpression(questionA, questionB, aBeforeB), nil); err != nil {
		t.Fatalf("%s: %v", message, err)
	}
}

func assertQuestionDragReleased(t *testing.T, page playwright.Page) {
	t.Helper()
	state, err := page.Evaluate(`() => !document.querySelector(".questions-workspace .question-item.is-dragging") && !document.querySelector(".question-drop-placeholder")`, nil)
	if err != nil || state != true {
		t.Fatalf("question drag state was not released (state=%v err=%v)", state, err)
	}
}

func waitForPersistedQuestionOrder(t *testing.T, page playwright.Page, jobID, questionA, questionB string, aBeforeB bool) {
	t.Helper()
	expression := fmt.Sprintf(`() => fetch("/api/jobs/%s").then((response) => response.json()).then((job) => {
		const questions = (job.stages || []).flatMap((stage) => stage.questions || []);
		const a = questions.findIndex((question) => question.question === %q);
		const b = questions.findIndex((question) => question.question === %q);
		return a >= 0 && b >= 0 && (a < b) === %t;
	})`, jobID, questionA, questionB, aBeforeB)
	if _, err := page.WaitForFunction(expression, nil); err != nil {
		t.Fatalf("reordered question order was not persisted: %v", err)
	}
}

func dragCardAfter(t *testing.T, page playwright.Page, source, target playwright.Locator) {
	t.Helper()
	scrollCardIntoView(t, source)
	waitForCardEntranceAnimations(t, page, source, target)
	dragHandle := source.Locator(".btn-card-drag")
	if err := dragHandle.Hover(); err != nil {
		t.Fatalf("hover source card drag handle: %v", err)
	}
	mouse := page.Mouse()
	if err := mouse.Down(); err != nil {
		t.Fatalf("hold source card: %v", err)
	}
	if _, err := page.WaitForFunction(`() => Boolean(document.querySelector(".process-card.is-dragging"))`, nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(3000)}); err != nil {
		t.Fatalf("wait for hold-to-drag activation: %v", err)
	}
	targetBox, err := target.BoundingBox()
	if err != nil || targetBox == nil {
		t.Fatalf("read target card bounds after hold activation: box=%v err=%v", targetBox, err)
	}
	steps := 10
	if err := mouse.Move(targetBox.X+targetBox.Width/2, targetBox.Y+targetBox.Height*0.75, playwright.MouseMoveOptions{Steps: &steps}); err != nil {
		t.Fatalf("move source card after target: %v", err)
	}
	if err := mouse.Up(); err != nil {
		t.Fatalf("release dragged card: %v", err)
	}
}

func waitForCardEntranceAnimations(t *testing.T, page playwright.Page, source, target playwright.Locator) {
	t.Helper()
	sourceID, err := source.GetAttribute("data-id")
	if err != nil || sourceID == "" {
		t.Fatalf("read source card ID: id=%q err=%v", sourceID, err)
	}
	targetID, err := target.GetAttribute("data-id")
	if err != nil || targetID == "" {
		t.Fatalf("read target card ID: id=%q err=%v", targetID, err)
	}
	animationsFinished := fmt.Sprintf(`() => [%q, %q].every((id) => {
		const card = document.querySelector('.process-card[data-id="' + id + '"]');
		return card && card.getAnimations().every((animation) => animation.playState === "finished");
	})`, sourceID, targetID)
	if _, err := page.WaitForFunction(animationsFinished, nil); err != nil {
		t.Fatalf("wait for card entrance animations: %v", err)
	}
}

func scrollCardIntoView(t *testing.T, card playwright.Locator) {
	t.Helper()
	if err := card.ScrollIntoViewIfNeeded(); err != nil {
		t.Fatalf("scroll source card into view: %v", err)
	}
}

func waitForCardOrder(t *testing.T, page playwright.Page, titleA, titleB string, aBeforeB bool, message string) {
	t.Helper()
	if _, err := page.WaitForFunction(cardOrderExpression(titleA, titleB, aBeforeB), nil); err != nil {
		t.Fatalf("%s: %v", message, err)
	}
}

func assertCardDragReleased(t *testing.T, page playwright.Page) {
	t.Helper()
	state, err := page.Evaluate(`() => !document.body.classList.contains("is-reordering-cards") && !document.querySelector(".process-card.is-dragging")`, nil)
	if err != nil || state != true {
		t.Fatalf("drag state was not released (state=%v err=%v)", state, err)
	}
}

func waitForPersistedCardOrder(t *testing.T, page playwright.Page, titleA, titleB string, aBeforeB bool) {
	t.Helper()
	if _, err := page.WaitForFunction(cardOrderPersistenceExpression(titleA, titleB, aBeforeB), nil); err != nil {
		t.Fatalf("reordered card order was not persisted: %v", err)
	}
}

func waitForApplicationReady(t *testing.T, page playwright.Page) {
	t.Helper()
	if _, err := page.WaitForFunction("() => document.body.dataset.appReady === 'true'", nil); err != nil {
		t.Fatalf("wait for application after reload: %v", err)
	}
}

func cardOrder(t *testing.T, page playwright.Page, titleA, titleB string) bool {
	t.Helper()
	result, err := page.Evaluate(cardOrderExpression(titleA, titleB, true), nil)
	if err != nil {
		t.Fatalf("read current card order: %v", err)
	}
	ordered, ok := result.(bool)
	if !ok {
		t.Fatalf("card order check returned %T, want bool", result)
	}
	return ordered
}

func cardOrderExpression(titleA, titleB string, aBeforeB bool) string {
	return fmt.Sprintf(`() => {
		const cards = Array.from(document.querySelectorAll(".process-card"));
		const a = cards.find((card) => card.textContent.includes(%q));
		const b = cards.find((card) => card.textContent.includes(%q));
		return Boolean(a && b) && (cards.indexOf(a) < cards.indexOf(b)) === %t;
	}`, titleA, titleB, aBeforeB)
}

func cardOrderPersistenceExpression(titleA, titleB string, aBeforeB bool) string {
	return fmt.Sprintf(`() => fetch("/api/jobs?status=ongoing").then((response) => response.json()).then((jobs) => {
		const a = jobs.findIndex((job) => job.position_title === %q);
		const b = jobs.findIndex((job) => job.position_title === %q);
		return a >= 0 && b >= 0 && (a < b) === %t;
	})`, titleA, titleB, aBeforeB)
}

func cycleStatusAndVerifyFilter(t *testing.T, page playwright.Page, card, status playwright.Locator, current, next, filter string) {
	t.Helper()
	cycleJobStatus(t, page, status, current, next)
	if err := page.Locator("#detail-modal .modal-close-btn").Click(); err != nil {
		t.Fatalf("close details after changing status to %s: %v", next, err)
	}
	selectFilter(t, page, filter)
	if err := card.WaitFor(); err != nil {
		t.Fatalf("process did not appear in %s filter: %v", filter, err)
	}
	if err := card.Locator(".btn-card-open-details").Click(); err != nil {
		t.Fatalf("reopen process in %s filter: %v", filter, err)
	}
	if err := status.WaitFor(); err != nil {
		t.Fatalf("wait for process status in %s filter: %v", filter, err)
	}
}

func createStatusCycleProcess(t *testing.T, page playwright.Page, uniqueTitle string) playwright.Locator {
	t.Helper()
	if err := page.Locator("#btn-new-process").Click(); err != nil {
		t.Fatalf("open create form: %v", err)
	}
	fill(t, page.Locator(`input[name="company_name"]`), "E2E Status Company")
	fill(t, page.Locator(`input[name="position_title"]`), uniqueTitle)
	if err := page.Locator(`button[type="submit"]`).Click(); err != nil {
		t.Fatalf("create process for status cycle: %v", err)
	}
	card := page.Locator(fmt.Sprintf(`.process-card:has-text("%s")`, uniqueTitle))
	if err := card.WaitFor(); err != nil {
		t.Fatalf("wait for created process: %v", err)
	}
	return card
}

func cycleJobStatus(t *testing.T, page playwright.Page, status playwright.Locator, current, next string) {
	t.Helper()
	text, err := status.TextContent()
	if err != nil || strings.TrimSpace(text) != current {
		t.Fatalf("status=%q before click, want %q (err=%v)", text, current, err)
	}
	if err := status.Click(); err != nil {
		t.Fatalf("cycle status from %s: %v", current, err)
	}
	expression := fmt.Sprintf(`() => document.querySelector("#detail-modal .editable-job-status")?.textContent.trim() === %q`, next)
	if _, err := page.WaitForFunction(expression, nil); err != nil {
		t.Fatalf("status did not advance from %s to %s: %v", current, next, err)
	}
}

func assertVisible(t *testing.T, locator playwright.Locator) {
	t.Helper()
	visible, err := locator.IsVisible()
	if err != nil || !visible {
		t.Fatalf("expected element to be visible (visible=%t, err=%v)", visible, err)
	}
}

func fill(t *testing.T, locator playwright.Locator, value string) {
	t.Helper()
	if err := locator.Fill(value); err != nil {
		t.Fatalf("fill form field: %v", err)
	}
}
