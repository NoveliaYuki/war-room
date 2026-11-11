package e2e_test

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

//go:embed testdata/language_check.js
var languageCheckScript string

//go:embed testdata/language_data.js
var languageDataScript string

// languageWidths covers a phone and a desktop layout.
var languageWidths = []int{390, 1280}

type languageMode struct {
	name     string
	path     string
	signedIn bool
}

type layoutFinding struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Text string `json:"text"`
}

// renderedState is what one language showed for one screen state.
type renderedState struct {
	texts  map[string]bool
	layout []layoutFinding
}

// languageData holds the supported languages, their dictionaries, and the
// record text (names, links, and user input) that stays as written.
type languageData struct {
	Languages    []string                     `json:"languages"`
	Dictionaries map[string]map[string]string `json:"dictionaries"`
	Terms        []string                     `json:"terms"`
}

// languageScreen opens one screen and reports each state it shows.
type languageScreen struct {
	name string
	open func(t *testing.T, run *languageRun, capture func(state string))
}

// languageRun is one page rendered in one language, width, and mode.
type languageRun struct {
	page     playwright.Page
	language string
	width    int
	mode     languageMode
	jobID    string
	data     *languageData
}

// TestEveryLanguageRendersOnlyItsOwnText opens every main screen in every
// supported language, signed in and in the demo, at phone and desktop widths.
// It fails when a translated screen shows interface text in another language,
// a browser validation message in another language, or text that overflows,
// is clipped, or overlaps a control where the English screen does not.
func TestEveryLanguageRendersOnlyItsOwnText(t *testing.T) {
	modes := []languageMode{{name: "signed in", path: "/", signedIn: true}, {name: "demo", path: "/?demo"}}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) { checkLanguageMode(t, mode) })
	}
}

func checkLanguageMode(t *testing.T, mode languageMode) {
	jobID := "demo-2"
	if mode.signedIn {
		jobID = seedLanguageFixture(t)
	}
	data := loadLanguageData(t, mode)
	if len(data.Languages) < 2 || data.Languages[0] != "en" {
		t.Fatalf("supported languages=%v, want English first and at least one translation", data.Languages)
	}
	var problems []string
	for _, width := range languageWidths {
		for _, screen := range languageScreens() {
			for state, byLanguage := range renderInEveryLanguage(t, mode, width, jobID, data, screen) {
				where := fmt.Sprintf("%s, %dpx, %s", mode.name, width, state)
				problems = append(problems, foreignText(data, byLanguage, where)...)
				problems = append(problems, newLayoutProblems(data, byLanguage, where)...)
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("%d language problems:\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

// renderInEveryLanguage renders screen in each supported language and groups the results by state.
func renderInEveryLanguage(t *testing.T, mode languageMode, width int, jobID string, data *languageData, screen languageScreen) map[string]map[string]renderedState {
	t.Helper()
	states := map[string]map[string]renderedState{}
	for _, language := range data.Languages {
		run := &languageRun{language: language, width: width, mode: mode, jobID: jobID, data: data}
		for state, rendered := range renderLanguageScreen(t, run, screen) {
			if states[state] == nil {
				states[state] = map[string]renderedState{}
			}
			states[state][language] = rendered
		}
	}
	return states
}

// renderLanguageScreen opens a fresh page in run.language and records each state of screen.
func renderLanguageScreen(t *testing.T, run *languageRun, screen languageScreen) map[string]renderedState {
	t.Helper()
	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Locale:   playwright.String("en-US"),
		Viewport: &playwright.Size{Width: run.width, Height: 900},
	})
	if err != nil {
		t.Fatalf("create browser context: %v", err)
	}
	defer func() { _ = context.Close() }()
	if err := context.AddInitScript(playwright.Script{Content: playwright.String(fmt.Sprintf(`localStorage.setItem("war-room.language", %q)`, run.language))}); err != nil {
		t.Fatalf("set interface language: %v", err)
	}
	if run.mode.signedIn {
		addSessionCookie(t, context)
	}
	page, err := context.NewPage()
	if err != nil {
		t.Fatalf("create browser page: %v", err)
	}
	run.page = page
	openLanguagePage(t, page, run.mode.path)
	rendered := map[string]renderedState{}
	screen.open(t, run, func(state string) {
		settleLanguagePage(t, page)
		rendered[screen.name+" "+state] = captureLanguageState(t, page)
	})
	if len(rendered) == 0 {
		t.Fatalf("%s (%s, %s, %dpx) showed no state to check", screen.name, run.language, run.mode.name, run.width)
	}
	return rendered
}

func addSessionCookie(t *testing.T, context playwright.BrowserContext) {
	t.Helper()
	cookie := seedTestSession(t)
	cookieURL := baseURL
	if err := context.AddCookies([]playwright.OptionalCookie{{Name: cookie.Name, Value: cookie.Value, URL: &cookieURL}}); err != nil {
		t.Fatalf("seed session cookie: %v", err)
	}
}

func openLanguagePage(t *testing.T, page playwright.Page, path string) {
	t.Helper()
	if _, err := page.Goto(baseURL + path); err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if _, err := page.WaitForFunction("() => document.body.dataset.appReady === 'true'", nil); err != nil {
		t.Fatalf("wait for %s: %v", path, err)
	}
}

func settleLanguagePage(t *testing.T, page playwright.Page) {
	t.Helper()
	// Option grids and menus finish their layout a frame after a click or a short transition.
	if _, err := page.Evaluate(`() => new Promise(resolve => setTimeout(() => requestAnimationFrame(() => requestAnimationFrame(resolve)), 250))`, nil); err != nil {
		t.Fatalf("wait for layout: %v", err)
	}
}

func captureLanguageState(t *testing.T, page playwright.Page) renderedState {
	t.Helper()
	texts, err := page.Evaluate(languageCheckScript, "text")
	if err != nil {
		t.Fatalf("collect page text: %v", err)
	}
	layout, err := page.Evaluate(languageCheckScript, "layout")
	if err != nil {
		t.Fatalf("inspect page layout: %v", err)
	}
	state := renderedState{texts: map[string]bool{}}
	for _, text := range texts.([]any) {
		state.texts[text.(string)] = true
	}
	decodeJSONValue(t, layout, &state.layout)
	return state
}

// languageScreens lists every main screen; each must open in every language.
func languageScreens() []languageScreen {
	return []languageScreen{
		{"board", func(t *testing.T, run *languageRun, capture func(string)) {
			capture("")
			if run.mode.signedIn {
				assertServerMessageLanguage(t, run)
			}
		}},
		{"filters", func(t *testing.T, run *languageRun, capture func(string)) {
			requireClick(t, run, "#process-filter-trigger")
			capture("")
		}},
		{"sort menu", func(t *testing.T, run *languageRun, capture func(string)) {
			requireClick(t, run, "#process-sort-trigger")
			capture("")
		}},
		{"account menu", func(t *testing.T, run *languageRun, capture func(string)) {
			openAccountMenu(t, run)
			capture("")
		}},
		{"data and backups", func(t *testing.T, run *languageRun, capture func(string)) {
			openAccountMenu(t, run)
			requireClick(t, run, "#btn-data-management-menu")
			capture("")
		}},
		{"schedule", func(t *testing.T, run *languageRun, capture func(string)) {
			requireClick(t, run, "#tab-schedule")
			capture("")
		}},
		{"new process", func(t *testing.T, run *languageRun, capture func(string)) {
			requireClick(t, run, "#btn-new-process")
			clickIfVisible(t, run, "#process-entry-manual")
			capture("")
			assertValidationLanguage(t, run)
		}},
		{"job details", func(t *testing.T, run *languageRun, capture func(string)) {
			openFixtureDetails(t, run, 0)
			capture("overview")
			openFixtureDetails(t, run, 1)
			tabs := run.page.Locator(".stage-tab-item")
			count, err := tabs.Count()
			if err != nil || count == 0 {
				t.Fatalf("job details show no stage tabs (%s, %dpx, err=%v)", run.language, run.width, err)
			}
			for index := 0; index < count; index++ {
				clickLocator(t, tabs.Nth(index), "open stage tab")
				capture(fmt.Sprintf("stage %d", index+1))
			}
		}},
		{"edit details", func(t *testing.T, run *languageRun, capture func(string)) {
			openFixtureDetails(t, run, 0)
			requireClick(t, run, ".btn-edit-details")
			capture("")
		}},
		{"edit schedule", func(t *testing.T, run *languageRun, capture func(string)) {
			openFixtureDetails(t, run, 1)
			requireClick(t, run, ".btn-edit-meeting-schedule")
			capture("")
		}},
		{"add interviewer", func(t *testing.T, run *languageRun, capture func(string)) {
			openFixtureDetails(t, run, 1)
			requireClick(t, run, ".btn-add-stage-interviewer")
			capture("")
		}},
		{"technology library", func(t *testing.T, run *languageRun, capture func(string)) {
			requireClick(t, run, "#process-filter-trigger")
			requireClick(t, run, "#btn-technology-library")
			capture("")
		}},
		{"search periods", func(t *testing.T, run *languageRun, capture func(string)) {
			requireClick(t, run, "#process-filter-trigger")
			requireClick(t, run, "#search-period-trigger")
			capture("menu")
			requireClick(t, run, "[data-manage-search-periods]")
			capture("manager")
		}},
		{"CV library", func(t *testing.T, run *languageRun, capture func(string)) {
			if _, err := run.page.Evaluate(`() => window.dispatchEvent(new CustomEvent("war-room:open-cv-library", { detail: {} }))`, nil); err != nil {
				t.Fatalf("open CV library: %v", err)
			}
			waitForLocator(t, run.page.Locator("#cv-library-upload-file"), "CV library")
			capture("")
		}},
		{"delete confirmation", func(t *testing.T, run *languageRun, capture func(string)) {
			requireClick(t, run, fmt.Sprintf(`.process-card[data-id=%q] .btn-card-delete`, run.jobID))
			capture("")
		}},
	}
}

func requireClick(t *testing.T, run *languageRun, selector string) {
	t.Helper()
	locator := run.page.Locator(selector).First()
	if err := locator.ScrollIntoViewIfNeeded(); err != nil {
		t.Fatalf("%s is missing (%s, %s, %dpx): %v", selector, run.language, run.mode.name, run.width, err)
	}
	clickLocator(t, locator, selector)
	settleLanguagePage(t, run.page)
}

// clickIfVisible clicks a control that only some layouts show.
func clickIfVisible(t *testing.T, run *languageRun, selector string) bool {
	t.Helper()
	locator := run.page.Locator(selector).First()
	if visible, err := locator.IsVisible(); err != nil || !visible {
		return false
	}
	clickLocator(t, locator, selector)
	settleLanguagePage(t, run.page)
	return true
}

func openAccountMenu(t *testing.T, run *languageRun) {
	t.Helper()
	toggled := clickIfVisible(t, run, "#btn-menu-toggle")
	if !clickIfVisible(t, run, "#btn-account-menu") && !toggled {
		t.Fatalf("account menu is missing (%s, %s, %dpx)", run.language, run.mode.name, run.width)
	}
}

// openFixtureDetails opens the fixture job; pane 0 is the overview and pane 1 the stages on phones.
func openFixtureDetails(t *testing.T, run *languageRun, pane int) {
	t.Helper()
	if visible, _ := run.page.Locator(".stage-tab-item").First().IsVisible(); !visible {
		if open, _ := run.page.Locator("#detail-modal .modal-header").IsVisible(); !open {
			requireClick(t, run, fmt.Sprintf(`.process-card[data-id=%q] .btn-card-open-details`, run.jobID))
			waitForLocator(t, run.page.Locator("#detail-modal .modal-header"), "job details")
		}
	}
	panes := run.page.Locator(".mobile-detail-view-button")
	if visible, _ := panes.First().IsVisible(); visible {
		clickLocator(t, panes.Nth(pane), "switch job details pane")
		settleLanguagePage(t, run.page)
	}
}

// assertValidationLanguage submits the empty new-process form and checks the browser's message.
func assertValidationLanguage(t *testing.T, run *languageRun) {
	t.Helper()
	form := run.page.Locator("form").Filter(playwright.LocatorFilterOptions{Has: run.page.Locator("[name=position_title]")}).First()
	clickLocator(t, form.Locator("button[type=submit]").First(), "submit empty new process")
	message, err := form.Locator("[name=position_title]").Evaluate("(field) => field.validationMessage", nil)
	if err != nil {
		t.Fatalf("read validation message: %v", err)
	}
	if want := run.data.Dictionaries[run.language]["validation.required"]; message != want {
		t.Errorf("required-field message in %s (%s, %dpx)=%q, want %q", run.language, run.mode.name, run.width, message, want)
	}
}

// assertServerMessageLanguage triggers a real API error and checks the message shown for it.
func assertServerMessageLanguage(t *testing.T, run *languageRun) {
	t.Helper()
	message, err := run.page.Evaluate(`async () => {
		const { api } = await import("/public/js/api.js");
		try { await api.createTechnology({ name: "LinguaCheck", aliases: [] }); return "no error"; } catch (error) { return error.message; }
	}`, nil)
	if err != nil {
		t.Fatalf("trigger a server error: %v", err)
	}
	if want := run.data.Dictionaries[run.language]["server.technologyExists"]; message != want {
		t.Errorf("server error in %s (%dpx)=%q, want %q", run.language, run.width, message, want)
	}
}

// seedLanguageFixture adds a process with the server's default stages, a question, and a meeting.
func seedLanguageFixture(t *testing.T) string {
	t.Helper()
	page := newPage(t)
	jobID, err := page.Evaluate(`async (today) => {
		const send = async (url, method, body) => {
			const response = await fetch(url, { method, headers: { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) });
			if (!response.ok) throw new Error(method + " " + url + " " + response.status + " " + await response.text());
			return response.json();
		};
		const job = await send("/api/jobs", "POST", { company_name: "Lingua Labs", position_title: "Localization Engineer", keyword_note: "Kubernetes • Go", work_arrangement: "hybrid", employment_type: "permanent", salary_type: "limited", salary_min: 60000, salary_max: 80000, salary_currency: "EUR", create_default_stages: true });
		await send("/api/jobs/" + job.id, "PUT", { status: "ongoing" });
		const stage = (await send("/api/jobs/" + job.id, "GET")).stages[0];
		await send("/api/questions", "POST", { stage_id: stage.id, question: "How do you design for failure?" });
		await send("/api/stages/" + stage.id + "/schedule", "PUT", { meeting_date: today, meeting_time: "10:00–10:45", meeting_type: "video", meeting_url: "https://meet.example.com/lingua" });
		await send("/api/stages/" + stage.id + "/set-current", "PUT");
		// A rerun against the same backend finds the technology already in the catalog.
		await send("/api/technologies", "POST", { name: "LinguaCheck", aliases: [] }).catch(() => {});
		return job.id;
	}`, time.Now().Format("2006-01-02"))
	if err != nil {
		t.Fatalf("seed language fixture: %v", err)
	}
	return jobID.(string)
}

func loadLanguageData(t *testing.T, mode languageMode) *languageData {
	t.Helper()
	page := newPage(t)
	openLanguagePage(t, page, mode.path)
	value, err := page.Evaluate(languageDataScript, map[string]any{"signedIn": mode.signedIn, "defaultStageTexts": defaultStageTexts(t)})
	if err != nil {
		t.Fatalf("load language data: %v", err)
	}
	var data languageData
	decodeJSONValue(t, value, &data)
	sort.Slice(data.Terms, func(i, j int) bool { return len(data.Terms[i]) > len(data.Terms[j]) })
	return &data
}

// foreignText reports interface text that a translation shows exactly as another language does.
func foreignText(data *languageData, byLanguage map[string]renderedState, where string) []string {
	var problems []string
	for _, language := range data.Languages[1:] {
		for text := range byLanguage[language].texts {
			for _, other := range data.Languages {
				if other == language || !byLanguage[other].texts[text] || sharedText(data, text, language, other) {
					continue
				}
				problems = append(problems, fmt.Sprintf("%s shows %s text (%s): %q", language, other, where, text))
				break
			}
		}
	}
	return problems
}

var letters = regexp.MustCompile(`\p{L}{2,}`)

// webAddress matches email addresses, links, and host names, which read the same in every language.
var webAddress = regexp.MustCompile(`[\w.+-]+@[\w-]+(?:\.[\w-]+)+|https?://\S*|[\w-]+(?:\.[\w-]+)+\S*`)

// sharedText reports whether text may legitimately read the same in both languages:
// it is only names, links, or user input, or its message is identical in both dictionaries.
func sharedText(data *languageData, text, language, other string) bool {
	value := text[strings.Index(text, "\t")+1:]
	stripped := webAddress.ReplaceAllString(value, " ")
	for _, term := range data.Terms {
		stripped = strings.ReplaceAll(stripped, term, " ")
	}
	if !letters.MatchString(stripped) {
		return true
	}
	for key, message := range data.Dictionaries[language] {
		if data.Dictionaries[other][key] == message && messagePattern(message).MatchString(value) {
			return true
		}
	}
	return false
}

var messagePatterns = map[string]*regexp.Regexp{}

// messagePattern matches a dictionary message with any values in its {placeholders}.
func messagePattern(message string) *regexp.Regexp {
	if pattern, ok := messagePatterns[message]; ok {
		return pattern
	}
	parts := regexp.MustCompile(`\{\w+\}`).Split(message, -1)
	for index, part := range parts {
		parts[index] = regexp.QuoteMeta(part)
	}
	pattern := regexp.MustCompile("^" + strings.Join(parts, ".+?") + "$")
	messagePatterns[message] = pattern
	return pattern
}

// newLayoutProblems reports overflow, clipping, or overlap that a translation shows and English does not.
func newLayoutProblems(data *languageData, byLanguage map[string]renderedState, where string) []string {
	english := map[string]bool{}
	for _, finding := range byLanguage["en"].layout {
		english[finding.Kind+" "+finding.Path] = true
	}
	var problems []string
	for _, language := range data.Languages[1:] {
		for _, finding := range byLanguage[language].layout {
			if !english[finding.Kind+" "+finding.Path] {
				problems = append(problems, fmt.Sprintf("%s: %s at %s (%s): %q", language, finding.Kind, finding.Path, where, finding.Text))
			}
		}
	}
	return problems
}
