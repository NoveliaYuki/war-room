package e2e_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

func TestApplicationShellAndFilters(t *testing.T) {
	page := newPage(t)
	title, err := page.Title()
	if err != nil {
		t.Fatalf("read page title: %v", err)
	}
	if !strings.Contains(strings.ToLower(title), "war room") && !strings.Contains(strings.ToLower(title), "ongoing") {
		t.Fatalf("unexpected page title %q", title)
	}
	for _, selector := range []string{".brand-title", "#search-input", `[data-filter="ongoing"]`, `[data-filter="accepted"]`, `[data-filter="rejected"]`, `[data-filter="all"]`} {
		assertVisible(t, page.Locator(selector))
	}
}

func TestNarrowViewportNavigation(t *testing.T) {
	page := newPage(t)
	if err := page.SetViewportSize(850, 850); err != nil {
		t.Fatalf("set narrow viewport: %v", err)
	}
	assertVisible(t, page.Locator("#btn-menu-toggle"))
	validLayout, err := page.Evaluate(`() => {
		const toggle = document.querySelector("#btn-menu-toggle");
		toggle.click();
		return getComputedStyle(document.querySelector("#header-controls")).display !== "none" &&
			getComputedStyle(document.querySelector("#btn-new-process .new-process-label")).display === "none" &&
			document.documentElement.scrollWidth <= window.innerWidth;
	}`, nil)
	if err != nil || validLayout != true {
		t.Fatalf("narrow viewport navigation or layout is incorrect (valid=%v, err=%v)", validLayout, err)
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
	for key, filter := range map[string]string{"4": "all", "1": "ongoing"} {
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

	schedule := page.Locator(`[data-filter="schedule"]`)
	if err := schedule.Click(); err != nil {
		t.Fatalf("open schedule view: %v", err)
	}
	scheduleHeading := page.Locator(".schedule-container .today-status-title")
	if err := scheduleHeading.WaitFor(); err != nil {
		t.Fatalf("wait for rendered schedule view: %v", err)
	}
	text, err := scheduleHeading.TextContent()
	if err != nil || !strings.HasPrefix(strings.TrimSpace(text), "Today (") {
		t.Fatalf("expected schedule day heading, got %q (err=%v)", text, err)
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
