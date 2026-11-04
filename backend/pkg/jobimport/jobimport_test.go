package jobimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/html"

	"war-room/backend/pkg/models"
)

const testUUID = "34413f8d-26bf-4bbc-8ade-eb309a0e2245"

// newTestImporter serves mux over TLS and lets the fetcher reach it, because
// the production address check rightly refuses loopback.
func newTestImporter(t *testing.T, mux http.Handler) (*Importer, string) {
	t.Helper()
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	parsed, _ := url.Parse(server.URL)
	f := newFetcher(func(netip.Addr) bool { return true }, parsed.Port())
	f.client.Transport.(*http.Transport).TLSClientConfig.RootCAs = server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	api := endpoints{greenhouse: server.URL, lever: server.URL, leverEU: server.URL, ashby: server.URL, workday: server.URL}
	return newImporter(&reader{fetcher: f, api: api}), server.URL
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func errorCode(t *testing.T, err error) string {
	t.Helper()
	var importError *Error
	if !errors.As(err, &importError) {
		t.Fatalf("error = %v, want *Error", err)
	}
	return importError.Code
}

func TestIsPublicAddrRefusesInternalRanges(t *testing.T) {
	for _, value := range []string{
		"127.0.0.1", "10.0.0.1", "172.17.0.2", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0",
		"::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "::ffff:169.254.169.254", "64:ff9b::7f00:1", "2002:7f00:1::", "224.0.0.1",
	} {
		if isPublicAddr(netip.MustParseAddr(value)) {
			t.Errorf("%s was treated as public", value)
		}
	}
	for _, value := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !isPublicAddr(netip.MustParseAddr(value)) {
			t.Errorf("%s was refused", value)
		}
	}
}

func TestPreviewRefusesUnsafeTargetsWithoutFetching(t *testing.T) {
	importer := New()
	for _, raw := range []string{
		"http://example.com/job", "ftp://example.com/job", "https://user:pw@example.com/job",
		"https://example.com:8443/job", "https://localhost/job", "https://metadata.internal/job",
		"https://printer.local/job", "not a url", "",
	} {
		if _, err := importer.Preview(context.Background(), "u", raw); errorCode(t, err) != CodeInvalidURL {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

func TestDefaultFetcherRefusesLoopbackAtDialTime(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	f := newFetcher(isPublicAddr, parsed.Port())
	if _, err := f.get(context.Background(), parsed, acceptPage, maxPageBytes); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("error = %v", err)
	}
}

func TestRedirectsAreRevalidatedAndCapped(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/to-http", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/", http.StatusFound)
	})
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	})
	importer, base := newTestImporter(t, mux)
	if _, err := importer.Preview(context.Background(), "u", base+"/to-http"); errorCode(t, err) != CodeInvalidURL {
		t.Fatalf("http redirect: %v", err)
	}
	if _, err := importer.Preview(context.Background(), "u", base+"/loop"); errorCode(t, err) != CodeUnsupported {
		t.Fatalf("redirect loop: %v", err)
	}
}

func TestPasteOnlySitesAreNeverFetched(t *testing.T) {
	for _, raw := range []string{"https://www.linkedin.com/jobs/view/123", "https://es.linkedin.com/jobs/view/1", "https://www.indeed.com/viewjob?jk=1"} {
		if _, err := New().Preview(context.Background(), "u", raw); errorCode(t, err) != CodePasteRequired {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestUpstreamFailuresMapToCodes(t *testing.T) {
	mux := http.NewServeMux()
	for path, status := range map[string]int{"/gone": 404, "/closed": 410, "/forbidden": 403, "/limited": 429, "/broken": 500} {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
	}
	mux.HandleFunc("/pdf", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF"))
	})
	mux.HandleFunc("/huge", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(make([]byte, maxPageBytes+1))
	})
	mux.HandleFunc("/plain", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Nothing structured</body></html>"))
	})
	importer, base := newTestImporter(t, mux)
	for path, code := range map[string]string{
		"/gone": CodeNotFound, "/closed": CodeNotFound, "/forbidden": CodeBlocked, "/limited": CodeBlocked,
		"/broken": CodeUnsupported, "/pdf": CodeUnsupported, "/huge": CodeUnsupported, "/plain": CodeUnsupported,
	} {
		if _, err := importer.Preview(context.Background(), "u", base+path); errorCode(t, err) != code {
			t.Errorf("%s: %v, want %s", path, err, code)
		}
	}
}

func TestClassifyTransportError(t *testing.T) {
	cases := map[string]error{
		CodeTimeout: context.DeadlineExceeded,
		CodeBlocked: errors.New("connection refused"),
	}
	for code, input := range cases {
		if got := errorCode(t, classifyTransportError(input)); got != code {
			t.Errorf("%v = %s", input, got)
		}
	}
}

func TestBoardFromURL(t *testing.T) {
	cases := map[string]string{
		"https://boards.greenhouse.io/acme/jobs/123":                             "greenhouse",
		"https://job-boards.eu.greenhouse.io/acme/jobs/123":                      "greenhouse",
		"https://boards.greenhouse.io/embed/job_app?for=acme&token=123":          "greenhouse",
		"https://jobs.lever.co/acme/" + testUUID:                                 "lever",
		"https://jobs.eu.lever.co/acme/" + testUUID + "/apply":                   "lever",
		"https://jobs.ashbyhq.com/acme/" + testUUID:                              "ashby",
		"https://jobs.ashbyhq.com/acme/" + testUUID + "/application":             "ashby",
		"https://boards.greenhouse.io/acme":                                      "",
		"https://boards.greenhouse.io/acme/jobs/not-a-number":                    "",
		"https://jobs.lever.co/acme/short":                                       "",
		"https://jobs.ashbyhq.com/" + url.PathEscape("../evil") + "/" + testUUID: "",
		"https://company.example/careers/123":                                    "",
	}
	for raw, provider := range cases {
		parsed, _ := url.Parse(raw)
		board, ok := boardFromURL(parsed)
		if ok != (provider != "") || board.provider != provider {
			t.Errorf("%s: %+v %v", raw, board, ok)
		}
	}
}

func TestReadGreenhouseUnescapesContentAndConvertsCents(t *testing.T) {
	importer, _ := newTestImporter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/boards/acme/jobs/42" || r.URL.Query().Get("pay_transparency") != "true" {
			t.Errorf("unexpected request %s", r.URL)
		}
		writeJSON(w, map[string]any{
			"title": "Staff Engineer", "company_name": "Acme", "location": map[string]string{"name": "Remote, Spain"},
			"content":          "&lt;h2&gt;&lt;strong&gt;Role &lt;/strong&gt;&lt;/h2&gt;&lt;ul&gt;&lt;li&gt;Python&lt;/li&gt;&lt;/ul&gt;",
			"pay_input_ranges": []map[string]any{{"min_cents": 16500000, "max_cents": 19000000, "currency_type": "USD", "title": "Annual Base Salary Range:"}},
		})
	}))
	posting, err := importer.reader.readBoard(context.Background(), boardPosting{provider: "greenhouse", slug: "acme", id: "42"})
	if err != nil {
		t.Fatal(err)
	}
	if posting.CompanyOverview != "## Role\n\n- Python" {
		t.Fatalf("overview = %q", posting.CompanyOverview)
	}
	if *posting.SalaryMin != 165000 || *posting.SalaryMax != 190000 || posting.SalaryCurrency != "USD" {
		t.Fatalf("salary = %v %v %s", *posting.SalaryMin, *posting.SalaryMax, posting.SalaryCurrency)
	}
	if posting.WorkArrangement != "remote" || posting.KeywordNote != "Remote, Spain" {
		t.Fatalf("details = %+v", posting)
	}
}

func TestReadLeverUsesRegionAndBuildsOverview(t *testing.T) {
	importer, _ := newTestImporter(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"text": "Backend Engineer", "description": "<p>Intro</p>", "additional": "<p>Outro</p>", "applyUrl": "https://jobs.lever.co/acme/x/apply",
			"workplaceType": "hybrid", "categories": map[string]string{"commitment": "Permanent", "location": "Madrid"},
			"lists":       []map[string]string{{"text": "Requirements", "content": "<li>Go</li>"}},
			"salaryRange": map[string]any{"min": 50000, "max": 60000, "currency": "EUR", "interval": "per-year-salary"},
		})
	}))
	posting, err := importer.reader.readBoard(context.Background(), boardPosting{provider: "lever", slug: "acme-labs", id: testUUID, eu: true})
	if err != nil {
		t.Fatal(err)
	}
	if posting.CompanyOverview != "Intro\n\n### Requirements\n\n- Go\n\nOutro" {
		t.Fatalf("overview = %q", posting.CompanyOverview)
	}
	if posting.CompanyName != "Acme Labs" || posting.EmploymentType != "permanent" || posting.WorkArrangement != "hybrid" || *posting.SalaryMin != 50000 {
		t.Fatalf("posting = %+v", posting)
	}
}

func TestReadAshbyFindsPostingOnBoard(t *testing.T) {
	importer, _ := newTestImporter(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"jobs": []map[string]any{{
			"id": testUUID, "title": "Data Engineer", "location": "Lisbon", "employmentType": "FullTime", "isRemote": true,
			"descriptionHtml": "<p>Build</p>", "applyUrl": "https://jobs.ashbyhq.com/acme/x/application",
			"compensation": map[string]any{"summaryComponents": []map[string]any{
				{"compensationType": "EquityPercentage", "interval": "NONE"},
				{"compensationType": "Salary", "interval": "1 YEAR", "currencyCode": "GBP", "minValue": 70000, "maxValue": 80000},
			}},
		}}})
	}))
	posting, err := importer.reader.readBoard(context.Background(), boardPosting{provider: "ashby", slug: "acme", id: strings.ToUpper(testUUID)})
	if err != nil {
		t.Fatal(err)
	}
	if posting.WorkArrangement != "remote" || posting.SalaryCurrency != "GBP" || *posting.SalaryMax != 80000 || posting.EmploymentType != "permanent" {
		t.Fatalf("posting = %+v", posting)
	}
	_, err = importer.reader.readBoard(context.Background(), boardPosting{provider: "ashby", slug: "acme", id: "00000000-0000-0000-0000-000000000000"})
	if errorCode(t, err) != CodeNotFound {
		t.Fatalf("missing posting: %v", err)
	}
}

func TestReadWorkdayUsesTheSiteJSON(t *testing.T) {
	importer, _ := newTestImporter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wday/cxs/acme/Careers/job/City/Role_JR1" || r.Header.Get("Accept") != acceptJSON {
			t.Errorf("unexpected request %s accept=%q", r.URL.Path, r.Header.Get("Accept"))
		}
		writeJSON(w, map[string]any{
			"hiringOrganization": map[string]string{"name": "Acme SA"},
			"jobPostingInfo": map[string]any{
				"title": "Agentic Lead", "jobDescription": "<p><b>SOBRE TI</b></p><ul><li>Python</li></ul>", "location": "Barcelona",
				"timeType": "Full time", "remoteType": "Hybrid", "externalUrl": "https://acme.wd3.myworkdayjobs.com/Careers/job/City/Role_JR1", "canApply": false,
			},
		})
	}))
	posting, err := importer.reader.readBoard(context.Background(), boardPosting{provider: "workday", slug: "Careers", id: "City/Role_JR1", host: "acme.wd3.myworkdayjobs.com"})
	if err != nil {
		t.Fatal(err)
	}
	if posting.CompanyName != "Acme SA" || posting.WorkArrangement != "hybrid" || posting.EmploymentType != "permanent" || posting.KeywordNote != "Barcelona" {
		t.Fatalf("posting = %+v", posting)
	}
	if posting.CompanyOverview != "**SOBRE TI**\n\n- Python" || len(posting.Warnings) != 1 {
		t.Fatalf("posting = %+v", posting)
	}
}

// TestPageRequestAsksForHTMLOnly protects a real failure: Workday answers page
// URLs with JSON if the request accepts it, which hides the structured data.
func TestPageRequestAsksForHTMLOnly(t *testing.T) {
	importer, base := newTestImporter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "json") {
			writeJSON(w, map[string]string{"unexpected": "json"})
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(jsonLDPage(map[string]any{"@type": "JobPosting", "title": "Role", "description": "<p>Text</p>"}))
	}))
	if _, err := importer.Preview(context.Background(), "u", base+"/job"); err != nil {
		t.Fatal(err)
	}
}

func TestCareerPageEmbeddingGreenhouseUsesTheBoard(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/careers/role", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<script src="https://boards.greenhouse.io/embed/job_board/js?for=acme"></script>`))
	})
	mux.HandleFunc("/v1/boards/acme/jobs/77", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"title": "SRE", "company_name": "Acme", "content": "&lt;p&gt;Run it&lt;/p&gt;"})
	})
	importer, base := newTestImporter(t, mux)
	posting, err := importer.Preview(context.Background(), "u", base+"/careers/role?gh_jid=77&utm_source=x")
	if err != nil {
		t.Fatal(err)
	}
	if posting.Source != "greenhouse" || strings.Contains(posting.JobPostURL, "utm_source") {
		t.Fatalf("posting = %+v", posting)
	}
}

func TestEmbeddedAshby(t *testing.T) {
	page, _ := url.Parse("https://company.example/jobs?ashby_jid=" + testUUID)
	board, ok := embeddedBoard(page, []byte(`<script src="https://jobs.ashbyhq.com/acme/embed"></script>`))
	if !ok || board.provider != "ashby" || board.slug != "acme" {
		t.Fatalf("board = %+v %v", board, ok)
	}
}

func jsonLDPage(job map[string]any) []byte {
	encoded, _ := json.Marshal(job)
	return []byte(`<html><head><script type="application/ld+json">` + string(encoded) + `</script></head></html>`)
}

func TestPostingFromJSONLD(t *testing.T) {
	posting := postingFromJSONLD(jsonLDPage(map[string]any{"@graph": []any{map[string]any{"@type": "Organization"}, map[string]any{
		"@type": []any{"Thing", "JobPosting"}, "title": "Engineer &amp; Lead", "description": "&lt;p&gt;<b>Lead</b>&lt;/p&gt;",
		"hiringOrganization": map[string]any{"name": "Acme", "sameAs": "https://www.linkedin.com/company/acme", "url": "https://www.acme.example/about"},
		"jobLocationType":    "TELECOMMUTE", "employmentType": []any{"FULL_TIME"}, "validThrough": "2020-01-01",
		"jobLocation": []any{map[string]any{"address": map[string]any{"addressLocality": "Berlin", "addressCountry": "DE"}}},
		"baseSalary":  map[string]any{"currency": "EUR", "value": map[string]any{"minValue": "60,000", "maxValue": 80000.0, "unitText": "YEAR"}},
	}}}))
	if posting == nil {
		t.Fatal("no posting found")
	}
	if posting.PositionTitle != "Engineer & Lead" || posting.CompanyDomain != "acme.example" || posting.KeywordNote != "Berlin, DE" {
		t.Fatalf("posting = %+v", posting)
	}
	if posting.WorkArrangement != "remote" || posting.EmploymentType != "permanent" || *posting.SalaryMin != 60000 {
		t.Fatalf("posting = %+v", posting)
	}
	if len(posting.Warnings) != 1 {
		t.Fatalf("warnings = %v", posting.Warnings)
	}
}

func TestPostingFromJSONLDVariants(t *testing.T) {
	single := postingFromJSONLD(jsonLDPage(map[string]any{
		"@type": "JobPosting", "title": "Role", "description": "<p>Text</p>", "hiringOrganization": "Acme",
		"baseSalary": map[string]any{"currency": "USD", "value": map[string]any{"value": 100000, "unitText": "YEAR"}},
	}))
	if single.CompanyName != "Acme" || *single.SalaryMin != 100000 || *single.SalaryMax != 100000 {
		t.Fatalf("posting = %+v", single)
	}
	for _, page := range [][]byte{
		[]byte(`<script type="application/ld+json">{broken</script>`),
		jsonLDPage(map[string]any{"@type": "Article"}),
	} {
		if postingFromJSONLD(page) != nil {
			t.Fatalf("unexpected posting from %s", page)
		}
	}
}

func TestSalaryNeverSavedInTheWrongUnit(t *testing.T) {
	cases := []struct {
		currency, interval string
		kept               bool
	}{
		{"EUR", "YEAR", true}, {"usd", "per-year-salary", true}, {"GBP", "", true},
		{"EUR", "HOUR", false}, {"USD", "per-month-salary", false}, {"CHF", "YEAR", false}, {"", "YEAR", false},
	}
	for _, item := range cases {
		posting := &Posting{}
		posting.setSalary(50000, 60000, item.currency, item.interval)
		if (posting.SalaryMin != nil) != item.kept {
			t.Errorf("%+v: salary = %v", item, posting.SalaryMin)
		}
		if !item.kept && len(posting.Warnings) != 1 {
			t.Errorf("%+v: warnings = %v", item, posting.Warnings)
		}
	}
	swapped := &Posting{}
	swapped.setSalary(90000, 70000, "EUR", "YEAR")
	if *swapped.SalaryMin != 70000 {
		t.Fatalf("min = %d", *swapped.SalaryMin)
	}
	empty := &Posting{}
	empty.setSalary(0, 0, "EUR", "YEAR")
	if empty.SalaryMin != nil || len(empty.Warnings) != 0 {
		t.Fatalf("empty = %+v", empty)
	}
}

func TestHTMLToMarkdown(t *testing.T) {
	cases := map[string]string{
		`<h2><strong>Role</strong></h2><p>Be <b>great </b>at <em>this</em></p>`:        "## Role\n\nBe **great** at *this*",
		`<ul><li>One<ul><li>Nested</li></ul></li><li><p>Two</p></li></ul>`:             "- One\n- Nested\n- Two",
		`<ol><li>First</li><li>Second</li></ol>`:                                       "1. First\n2. Second",
		`<blockquote><p>Quote</p></blockquote><hr>`:                                    "> Quote",
		`<script>alert(1)</script><img src=x onerror=alert(1)><p>Safe</p>`:             "Safe",
		`<a href="https://a.example/x">Site</a> <a href="javascript:alert(1)">Bad</a>`: "[Site](https://a.example/x) Bad",
		`<div>Loose<br>text<div>Inner</div></div>`:                                     "Loose text\n\nInner",
		`<table><tr><td>Cell</td></tr></table>`:                                        "Cell",
		``:                                                                             "",
	}
	for input, want := range cases {
		if got := htmlToMarkdown(input); got != want {
			t.Errorf("%q = %q, want %q", input, got, want)
		}
	}
}

func TestHTMLToMarkdownBoundsHostileInput(t *testing.T) {
	deep := strings.Repeat("<span>", 200000) + "x"
	start := time.Now()
	htmlToMarkdown(deep)
	if time.Since(start) > 5*time.Second {
		t.Fatal("deeply nested HTML took too long")
	}
	long := htmlToMarkdown("<p>" + strings.Repeat("word ", maxOverviewRunes) + "</p>")
	if len([]rune(long)) > maxOverviewRunes {
		t.Fatalf("overview has %d runes", len([]rune(long)))
	}
}

func TestCanonicalizeMatchesTheSamePosting(t *testing.T) {
	first, _ := Canonicalize("HTTPS://Jobs.Example.com/role/?utm_source=x&gclid=1&lever-source=y#apply")
	second, _ := Canonicalize("https://jobs.example.com/role")
	if first.String() != second.String() {
		t.Fatalf("%s != %s", first, second)
	}
	kept, _ := Canonicalize("https://acme.example/careers?gh_jid=12")
	if kept.Query().Get("gh_jid") != "12" {
		t.Fatalf("lost posting id: %s", kept)
	}
}

func servePosting(hits *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(jsonLDPage(map[string]any{"@type": "JobPosting", "title": "Role", "description": "<p>Text</p>"}))
	})
}

func TestCacheServesRepeatsAndExpires(t *testing.T) {
	var hits atomic.Int32
	importer, base := newTestImporter(t, servePosting(&hits))
	now := time.Now()
	importer.now = func() time.Time { return now }
	for _, raw := range []string{base + "/job", base + "/job?utm_campaign=x"} {
		if _, err := importer.Preview(context.Background(), "u", raw); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
	now = now.Add(cacheTTL + time.Second)
	if _, err := importer.Preview(context.Background(), "u", base+"/job"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits after expiry = %d", hits.Load())
	}
}

func TestCachedCopiesAreIndependent(t *testing.T) {
	var hits atomic.Int32
	importer, base := newTestImporter(t, servePosting(&hits))
	first, _ := importer.Preview(context.Background(), "u", base+"/job")
	first.Warnings = append(first.Warnings, "changed")
	second, _ := importer.Preview(context.Background(), "u", base+"/job")
	if len(second.Warnings) != 0 {
		t.Fatalf("warnings = %v", second.Warnings)
	}
}

func TestCacheStaysBounded(t *testing.T) {
	importer := newImporter(&reader{})
	now := time.Now()
	importer.now = func() time.Time { return now }
	for index := 0; index < maxCacheEntries*2; index++ {
		importer.store(fmt.Sprint(index), Posting{})
	}
	if len(importer.cache) > maxCacheEntries {
		t.Fatalf("cache has %d entries", len(importer.cache))
	}
}

func TestRateLimitIsPerUserAndResets(t *testing.T) {
	var hits atomic.Int32
	importer, base := newTestImporter(t, servePosting(&hits))
	now := time.Now()
	importer.now = func() time.Time { return now }
	for index := 0; index < rateLimit; index++ {
		if _, err := importer.Preview(context.Background(), "a", fmt.Sprintf("%s/%d", base, index)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := importer.Preview(context.Background(), "a", base+"/next"); errorCode(t, err) != CodeRateLimited {
		t.Fatalf("over limit: %v", err)
	}
	if _, err := importer.Preview(context.Background(), "a", base+"/0"); err != nil {
		t.Fatalf("cached posting was limited: %v", err)
	}
	if _, err := importer.Preview(context.Background(), "b", base+"/next"); err != nil {
		t.Fatalf("other user was limited: %v", err)
	}
	now = now.Add(rateWindow + time.Second)
	if _, err := importer.Preview(context.Background(), "a", base+"/after"); err != nil {
		t.Fatalf("after reset: %v", err)
	}
}

func TestConcurrentImportsShareOneRead(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	importer, base := newTestImporter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		servePosting(&hits).ServeHTTP(w, r)
	}))
	var group sync.WaitGroup
	for index := 0; index < 10; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := importer.Preview(context.Background(), fmt.Sprint(index), base+"/job"); err != nil {
				t.Error(err)
			}
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	group.Wait()
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestCancelledCallerDoesNotCacheOrBreakOthers(t *testing.T) {
	release := make(chan struct{})
	var hits atomic.Int32
	importer, base := newTestImporter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		servePosting(&hits).ServeHTTP(w, r)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := importer.Preview(ctx, "u", base+"/job"); errorCode(t, err) != CodeTimeout {
		t.Fatalf("cancelled: %v", err)
	}
	close(release)
	if _, err := importer.Preview(context.Background(), "u", base+"/job"); err != nil {
		t.Fatal(err)
	}
}

func TestBusySlotsFailClearly(t *testing.T) {
	importer := newImporter(&reader{})
	parsed, _ := url.Parse("https://example.com/job")
	for index := 0; index < maxConcurrent; index++ {
		importer.slots <- struct{}{}
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	go func() {
		_, err := importer.acquireAndRead(ctx, parsed)
		done <- err
	}()
	if err := <-done; errorCode(t, err) != CodeBusy {
		t.Fatalf("busy: %v", err)
	}
}

func TestParserPanicBecomesFailure(t *testing.T) {
	importer := newImporter(&reader{})
	parsed, _ := url.Parse("https://example.com/job")
	if _, err := importer.readWithSlot(parsed); errorCode(t, err) != CodeUnsupported {
		t.Fatalf("panic: %v", err)
	}
}

func names(items []Suggestion) string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		marker := ""
		if item.Recommended {
			marker = "*"
		}
		values = append(values, item.Name+marker)
	}
	return strings.Join(values, ",")
}

func TestSuggestTechnologies(t *testing.T) {
	catalog := []models.Technology{{ID: "t1", Name: "PostgreSQL", Aliases: []string{"Postgres"}}, {ID: "t2", Name: "Internal Tool"}}
	overview := strings.Join([]string{
		"## About us", "We use Kubernetes everywhere.",
		"## Requirements", "- Strong Python and Postgres", "- Golang services", "- AWS or GCP", "- Python",
		"## Nice to have", "- Terraform",
		"## Perks", "- Docker stickers",
	}, "\n")
	got := SuggestTechnologies(overview, catalog)
	want := "Go*,PostgreSQL*,Python*,Amazon Web Services,Google Cloud Platform,Terraform"
	if names(got) != want {
		t.Fatalf("got %s, want %s", names(got), want)
	}
	for _, item := range got {
		if item.Name == "PostgreSQL" && item.ID != "t1" {
			t.Fatalf("catalog id lost: %+v", item)
		}
		if item.Name == "Python" && item.ID != "" {
			t.Fatalf("starter has id: %+v", item)
		}
	}
}

func TestSuggestTechnologiesAvoidsOrdinaryWords(t *testing.T) {
	for _, line := range []string{
		"Own our go-to-market motion", "We go fast", "Go above and beyond", "Build trust with swift feedback",
		"at the helm of a spark of ideas", "C-level stakeholders",
		"R&D teams", "Golden rule",
	} {
		got := names(SuggestTechnologies(line, nil))
		if got != "" {
			t.Errorf("%q suggested %s", line, got)
		}
	}
	for line, want := range map[string]string{
		"Languages: Go, Rust": "Go*,Rust*", "- Go, Python": "Go*,Python*", "Experience with C++ and C#": "C#*,C++*",
		"We build in Go.": "Go*", "Node.js and Next.js": "Next.js*,Node.js*",
	} {
		if got := names(SuggestTechnologies(line, nil)); got != want {
			t.Errorf("%q = %s, want %s", line, got, want)
		}
	}
}

func TestSuggestTechnologiesReadsSpanishPostings(t *testing.T) {
	overview := strings.Join([]string{
		"**NUESTRO EQUIPO**", "**SOBRE TI**", "- Python sólido, Docker", "- Cloud (AWS, GCP o Azure)",
		"Construirlos con frameworks como LangGraph o FastAPI.",
		"Valoraremos experiencia en:", "- OpenSearch", "**TUS BENEFICIOS:**", "- Kubernetes gratis",
	}, "\n")
	want := "Docker*,Python*,Amazon Web Services,FastAPI,Google Cloud Platform,LangGraph,Microsoft Azure,OpenSearch"
	if got := names(SuggestTechnologies(overview, nil)); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestStarterEntriesYieldToCatalogAliases(t *testing.T) {
	got := SuggestTechnologies("Kubernetes", []models.Technology{{ID: "k", Name: "K8s Platform", Aliases: []string{"Kubernetes"}}})
	if len(got) != 1 || got[0].ID != "k" {
		t.Fatalf("got %+v", got)
	}
}

func TestWorkdayLinks(t *testing.T) {
	cases := map[string]string{
		"https://mango.wd3.myworkdayjobs.com/es/Mango_Work/job/Palau-Spain/ROLE_JR1?source=LinkedIn": "Palau-Spain/ROLE_JR1",
		"https://acme.wd1.myworkdayjobs.com/Site/job/City/Role_JR1/apply":                            "City/Role_JR1",
		"https://acme.wd5.myworkdayjobs.com/en-US/Site/details/Role_JR1":                             "Role_JR1",
		"https://acme.wd3.myworkdayjobs.com/Site":                                                    "",
		"https://acme.wd3.myworkdayjobs.com/Site/about/City":                                         "",
		"https://acme.wd3.myworkdayjobs.com/Site/job/a/../secret":                                    "",
		"https://evil.example/acme.wd3.myworkdayjobs.com/Site/job/x":                                 "",
	}
	for raw, want := range cases {
		parsed, _ := url.Parse(raw)
		board, ok := boardFromURL(parsed)
		if ok != (want != "") || board.id != want {
			t.Errorf("%s: %+v %v", raw, board, ok)
		}
	}
}

func TestBoardReadsReportUpstreamFailures(t *testing.T) {
	boards := []boardPosting{
		{provider: "greenhouse", slug: "acme", id: "1"},
		{provider: "lever", slug: "acme", id: testUUID},
		{provider: "ashby", slug: "acme", id: testUUID},
		{provider: "workday", slug: "Site", id: "City/Role", host: "acme.wd3.myworkdayjobs.com"},
	}
	missing, _ := newTestImporter(t, http.NotFoundHandler())
	broken, _ := newTestImporter(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{broken"))
	}))
	for _, board := range boards {
		if _, err := missing.reader.readBoard(context.Background(), board); errorCode(t, err) != CodeNotFound {
			t.Errorf("%s missing: %v", board.provider, err)
		}
		if _, err := broken.reader.readBoard(context.Background(), board); errorCode(t, err) != CodeUnsupported {
			t.Errorf("%s broken: %v", board.provider, err)
		}
	}
}

func TestReadFollowsBoardAndEmbeddedPages(t *testing.T) {
	importer, base := newTestImporter(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"title": "Role", "company_name": "Acme", "content": "&lt;p&gt;Text&lt;/p&gt;"})
	}))
	direct, _ := url.Parse("https://boards.greenhouse.io/acme/jobs/1")
	posting, err := importer.reader.read(context.Background(), direct)
	if err != nil || posting.Source != "greenhouse" {
		t.Fatalf("posting = %+v err = %v", posting, err)
	}
	if err := importer.reader.getJSON(context.Background(), "://bad", maxPageBytes, &posting); errorCode(t, err) != CodeInvalidURL {
		t.Fatalf("bad address: %v", err)
	}
	parsed, _ := url.Parse(base + "/page")
	if _, err := importer.reader.read(context.Background(), parsed); errorCode(t, err) != CodeUnsupported {
		t.Fatalf("json page: %v", err)
	}
}

func TestSmallHelpers(t *testing.T) {
	if ErrBlockedTarget.Error() == "" {
		t.Fatal("empty message")
	}
	if err := checkDialAddress("no-port", isPublicAddr); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("dial address: %v", err)
	}
	if _, err := acceptedContentType(";;"); errorCode(t, err) != CodeUnsupported {
		t.Fatalf("content type: %v", err)
	}
}

func TestValueHelpers(t *testing.T) {
	if domainFrom("https://bad host/") != "" || domainFrom("https://www.acme.example") != "acme.example" {
		t.Fatal("domainFrom")
	}
	if workArrangement("Flexible") != "" || workArrangement("On-site") != "on_site" {
		t.Fatal("workArrangement")
	}
}

func TestEmptyValues(t *testing.T) {
	if positiveAmount(0) != nil || attribute(&html.Node{}, "href") != "" {
		t.Fatal("empty values")
	}
	if containsTerm("anything", "") || findJobPosting(map[string]any{}, 5) != nil || findJobPosting("text", 0) != nil {
		t.Fatal("empty matches")
	}
	if got := htmlToMarkdown("<!-- note --><ul>text<li>One</li></ul>"); got != "- One" {
		t.Fatalf("list = %q", got)
	}
}

func TestCacheDropsExpiredEntriesFirst(t *testing.T) {
	importer := newImporter(&reader{})
	now := time.Now()
	importer.now = func() time.Time { return now }
	for index := 0; index < maxCacheEntries; index++ {
		importer.store(fmt.Sprint(index), Posting{})
	}
	now = now.Add(cacheTTL + time.Second)
	importer.store("fresh", Posting{})
	if len(importer.cache) != 1 {
		t.Fatalf("cache has %d entries", len(importer.cache))
	}
}
