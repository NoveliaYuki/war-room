package jobimport

import (
	"context"
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strings"
)

var (
	boardSlug    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	numericID    = regexp.MustCompile(`^[0-9]{1,20}$`)
	postingUUID  = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)
	embedGH      = regexp.MustCompile(`greenhouse\.io/(?:embed/job_board/js\?for=|embed/job_app\?for=|embed/job_board\?for=)([A-Za-z0-9._-]+)`)
	embedAshby   = regexp.MustCompile(`jobs\.ashbyhq\.com/([A-Za-z0-9._-]+)/embed`)
	workdayHost  = regexp.MustCompile(`^([a-z0-9-]+)\.wd[0-9]+\.myworkdayjobs\.com$`)
	workdayLang  = regexp.MustCompile(`^[a-z]{2}(-[A-Za-z]{2})?$`)
	workdayPath  = regexp.MustCompile(`^[A-Za-z0-9._~%-]+(/[A-Za-z0-9._~%-]+)*$`)
	greenhouseAt = map[string]bool{
		"boards.greenhouse.io": true, "job-boards.greenhouse.io": true,
		"boards.eu.greenhouse.io": true, "job-boards.eu.greenhouse.io": true,
	}
)

// endpoints holds the fixed board API origins, replaced only in tests.
type endpoints struct {
	greenhouse string
	lever      string
	leverEU    string
	ashby      string
	workday    string
}

var liveEndpoints = endpoints{
	greenhouse: "https://boards-api.greenhouse.io",
	lever:      "https://api.lever.co",
	leverEU:    "https://api.eu.lever.co",
	ashby:      "https://api.ashbyhq.com",
}

// boardPosting identifies one posting on a supported job board.
type boardPosting struct {
	provider string
	slug     string
	id       string
	eu       bool
	host     string
}

// boardFromURL recognizes a posting URL on a hosted job board.
func boardFromURL(target *url.URL) (boardPosting, bool) {
	host := strings.ToLower(target.Hostname())
	parts := strings.Split(strings.Trim(target.EscapedPath(), "/"), "/")
	switch {
	case greenhouseAt[host]:
		return greenhouseFromURL(target, parts)
	case host == "jobs.lever.co", host == "jobs.eu.lever.co":
		return validBoard(boardPosting{provider: "lever", slug: first(parts, 0), id: first(parts, 1), eu: host == "jobs.eu.lever.co"}, postingUUID)
	case host == "jobs.ashbyhq.com":
		return validBoard(boardPosting{provider: "ashby", slug: first(parts, 0), id: first(parts, 1)}, postingUUID)
	case workdayHost.MatchString(host):
		return workdayFromURL(host, parts)
	}
	return boardPosting{}, false
}

func greenhouseFromURL(target *url.URL, parts []string) (boardPosting, bool) {
	query := target.Query()
	if first(parts, 0) == "embed" {
		return validBoard(boardPosting{provider: "greenhouse", slug: query.Get("for"), id: query.Get("token")}, numericID)
	}
	if first(parts, 1) == "jobs" {
		return validBoard(boardPosting{provider: "greenhouse", slug: parts[0], id: first(parts, 2)}, numericID)
	}
	return boardPosting{}, false
}

// workdayFromURL reads links shaped /[locale]/{site}/job/{path} or /details/.
func workdayFromURL(host string, parts []string) (boardPosting, bool) {
	if workdayLang.MatchString(first(parts, 0)) {
		parts = parts[1:]
	}
	kind := first(parts, 1)
	if len(parts) < 3 || (kind != "job" && kind != "details") {
		return boardPosting{}, false
	}
	path := strings.TrimSuffix(strings.Join(parts[2:], "/"), "/apply")
	if strings.Contains(path, "..") {
		return boardPosting{}, false
	}
	return validBoard(boardPosting{provider: "workday", slug: parts[0], id: path, host: host}, workdayPath)
}

func first(parts []string, index int) string {
	if index < len(parts) {
		return parts[index]
	}
	return ""
}

func validBoard(board boardPosting, id *regexp.Regexp) (boardPosting, bool) {
	if !boardSlug.MatchString(board.slug) || !id.MatchString(board.id) {
		return boardPosting{}, false
	}
	return board, true
}

// embeddedBoard finds a job board behind a company careers page, which is how
// most career portals publish postings on their own domain.
func embeddedBoard(page *url.URL, body []byte) (boardPosting, bool) {
	query := page.Query()
	if id := query.Get("gh_jid"); id != "" {
		if match := embedGH.FindSubmatch(body); match != nil {
			return validBoard(boardPosting{provider: "greenhouse", slug: string(match[1]), id: id}, numericID)
		}
	}
	if id := query.Get("ashby_jid"); id != "" {
		if match := embedAshby.FindSubmatch(body); match != nil {
			return validBoard(boardPosting{provider: "ashby", slug: string(match[1]), id: id}, postingUUID)
		}
	}
	return boardPosting{}, false
}

func (r *reader) readBoard(ctx context.Context, board boardPosting) (*Posting, error) {
	switch board.provider {
	case "greenhouse":
		return r.readGreenhouse(ctx, board)
	case "lever":
		return r.readLever(ctx, board)
	case "workday":
		return r.readWorkday(ctx, board)
	}
	return r.readAshby(ctx, board)
}

func (r *reader) getJSON(ctx context.Context, rawURL string, limit int64, target any) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ErrBlockedTarget
	}
	res, err := r.fetcher.get(ctx, parsed, acceptJSON, limit)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(res.body, target); err != nil {
		return newError(CodeUnsupported, "That job board returned an unexpected response.")
	}
	return nil
}

type greenhouseJob struct {
	Title       string `json:"title"`
	CompanyName string `json:"company_name"`
	Content     string `json:"content"`
	AbsoluteURL string `json:"absolute_url"`
	Location    struct {
		Name string `json:"name"`
	} `json:"location"`
	PayRanges []struct {
		MinCents float64 `json:"min_cents"`
		MaxCents float64 `json:"max_cents"`
		Currency string  `json:"currency_type"`
		Title    string  `json:"title"`
	} `json:"pay_input_ranges"`
}

func (r *reader) readGreenhouse(ctx context.Context, board boardPosting) (*Posting, error) {
	var job greenhouseJob
	address := r.api.greenhouse + "/v1/boards/" + url.PathEscape(board.slug) + "/jobs/" + board.id + "?pay_transparency=true"
	if err := r.getJSON(ctx, address, maxPageBytes, &job); err != nil {
		return nil, err
	}
	// Greenhouse entity-escapes the HTML inside its JSON string.
	posting := &Posting{
		PositionTitle: job.Title, CompanyName: job.CompanyName, Source: "greenhouse",
		CompanyOverview: htmlToMarkdown(html.UnescapeString(job.Content)),
		WorkArrangement: workArrangement(job.Location.Name),
	}
	posting.setLocation(job.Location.Name)
	if len(job.PayRanges) > 0 {
		pay := job.PayRanges[0]
		posting.setSalary(pay.MinCents/100, pay.MaxCents/100, pay.Currency, pay.Title)
	}
	return posting, nil
}

type leverJob struct {
	Text          string `json:"text"`
	Description   string `json:"description"`
	Additional    string `json:"additional"`
	ApplyURL      string `json:"applyUrl"`
	WorkplaceType string `json:"workplaceType"`
	Categories    struct {
		Commitment string `json:"commitment"`
		Location   string `json:"location"`
	} `json:"categories"`
	Lists []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	} `json:"lists"`
	SalaryRange *struct {
		Min      float64 `json:"min"`
		Max      float64 `json:"max"`
		Currency string  `json:"currency"`
		Interval string  `json:"interval"`
	} `json:"salaryRange"`
}

func (r *reader) readLever(ctx context.Context, board boardPosting) (*Posting, error) {
	origin := r.api.lever
	if board.eu {
		origin = r.api.leverEU
	}
	var job leverJob
	address := origin + "/v0/postings/" + url.PathEscape(board.slug) + "/" + board.id + "?mode=json"
	if err := r.getJSON(ctx, address, maxPageBytes, &job); err != nil {
		return nil, err
	}
	posting := &Posting{
		PositionTitle: job.Text, CompanyName: titleFromSlug(board.slug), Source: "lever",
		CompanyOverview: htmlToMarkdown(leverHTML(job)), ApplicationURL: job.ApplyURL,
		WorkArrangement: workArrangement(job.WorkplaceType), EmploymentType: employmentType(job.Categories.Commitment),
	}
	posting.setLocation(job.Categories.Location)
	if job.SalaryRange != nil {
		posting.setSalary(job.SalaryRange.Min, job.SalaryRange.Max, job.SalaryRange.Currency, job.SalaryRange.Interval)
	}
	return posting, nil
}

// leverHTML rebuilds the posting's order: Lever splits it into three fields.
func leverHTML(job leverJob) string {
	var page strings.Builder
	page.WriteString(job.Description)
	for _, list := range job.Lists {
		page.WriteString("<h3>" + html.EscapeString(list.Text) + "</h3><ul>" + list.Content + "</ul>")
	}
	page.WriteString(job.Additional)
	return page.String()
}

// titleFromSlug is the only company name Lever exposes.
func titleFromSlug(slug string) string {
	words := strings.FieldsFunc(slug, func(c rune) bool { return c == '-' || c == '_' || c == '.' })
	for index, word := range words {
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

type ashbyBoard struct {
	Jobs []ashbyJob `json:"jobs"`
}

type ashbyJob struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Location        string `json:"location"`
	EmploymentType  string `json:"employmentType"`
	WorkplaceType   string `json:"workplaceType"`
	IsRemote        bool   `json:"isRemote"`
	IsListed        bool   `json:"isListed"`
	ApplyURL        string `json:"applyUrl"`
	DescriptionHTML string `json:"descriptionHtml"`
	Compensation    struct {
		SummaryComponents []struct {
			CompensationType string  `json:"compensationType"`
			Interval         string  `json:"interval"`
			CurrencyCode     string  `json:"currencyCode"`
			MinValue         float64 `json:"minValue"`
			MaxValue         float64 `json:"maxValue"`
		} `json:"summaryComponents"`
	} `json:"compensation"`
}

// readAshby reads the whole board because Ashby has no single-posting endpoint.
func (r *reader) readAshby(ctx context.Context, board boardPosting) (*Posting, error) {
	var listing ashbyBoard
	address := r.api.ashby + "/posting-api/job-board/" + url.PathEscape(board.slug) + "?includeCompensation=true"
	if err := r.getJSON(ctx, address, maxBoardBytes, &listing); err != nil {
		return nil, err
	}
	for _, job := range listing.Jobs {
		if strings.EqualFold(job.ID, board.id) {
			return ashbyPosting(job, board.slug), nil
		}
	}
	return nil, newError(CodeNotFound, "That posting is no longer available.")
}

func ashbyPosting(job ashbyJob, slug string) *Posting {
	arrangement := job.WorkplaceType
	if arrangement == "" && job.IsRemote {
		arrangement = "remote"
	}
	posting := &Posting{
		PositionTitle: job.Title, CompanyName: titleFromSlug(slug), Source: "ashby",
		CompanyOverview: htmlToMarkdown(job.DescriptionHTML), ApplicationURL: job.ApplyURL,
		WorkArrangement: workArrangement(arrangement), EmploymentType: employmentType(job.EmploymentType),
	}
	posting.setLocation(job.Location)
	for _, part := range job.Compensation.SummaryComponents {
		if part.CompensationType == "Salary" {
			posting.setSalary(part.MinValue, part.MaxValue, part.CurrencyCode, part.Interval)
			break
		}
	}
	return posting
}

type workdayJob struct {
	Info struct {
		Title          string `json:"title"`
		JobDescription string `json:"jobDescription"`
		Location       string `json:"location"`
		TimeType       string `json:"timeType"`
		RemoteType     string `json:"remoteType"`
		ExternalURL    string `json:"externalUrl"`
		CanApply       bool   `json:"canApply"`
	} `json:"jobPostingInfo"`
	Organization struct {
		Name string `json:"name"`
	} `json:"hiringOrganization"`
}

// readWorkday uses the JSON behind the career site, which keeps the posting's
// formatting. Each employer has its own host, so board.host is the fixed origin.
func (r *reader) readWorkday(ctx context.Context, board boardPosting) (*Posting, error) {
	origin := r.api.workday
	if origin == "" {
		origin = "https://" + board.host
	}
	tenant := strings.SplitN(board.host, ".", 2)[0]
	var job workdayJob
	address := origin + "/wday/cxs/" + tenant + "/" + url.PathEscape(board.slug) + "/job/" + board.id
	if err := r.getJSON(ctx, address, maxPageBytes, &job); err != nil {
		return nil, err
	}
	posting := &Posting{
		PositionTitle: job.Info.Title, CompanyName: job.Organization.Name, Source: "workday",
		CompanyOverview: htmlToMarkdown(job.Info.JobDescription), ApplicationURL: job.Info.ExternalURL,
		WorkArrangement: workArrangement(job.Info.RemoteType), EmploymentType: employmentType(job.Info.TimeType),
	}
	posting.setLocation(job.Info.Location)
	if !job.Info.CanApply {
		posting.Warnings = append(posting.Warnings, "This posting is no longer accepting applications.")
	}
	return posting, nil
}
