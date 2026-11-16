package jobimport

import (
	"strings"
)

// Posting is the normalized draft of a job posting. It never contains
// per-user data, so one cached copy can serve every user.
type Posting struct {
	PositionTitle   string   `json:"position_title"`
	CompanyName     string   `json:"company_name"`
	CompanyOverview string   `json:"company_overview"`
	CompanyDomain   string   `json:"company_domain"`
	JobPostURL      string   `json:"job_post_url"`
	ApplicationURL  string   `json:"application_url"`
	KeywordNote     string   `json:"keyword_note"`
	WorkArrangement string   `json:"work_arrangement"`
	EmploymentType  string   `json:"employment_type"`
	SalaryMin       *int64   `json:"salary_min"`
	SalaryMax       *int64   `json:"salary_max"`
	SalaryCurrency  string   `json:"salary_currency"`
	Source          string   `json:"source"`
	Warnings        []string `json:"warnings"`
}

const maxKeywordRunes = 100

// setLocation keeps the location in the card's keyword note when it fits.
func (p *Posting) setLocation(location string) {
	location = collapseSpace(location)
	if location != "" && len([]rune(location)) <= maxKeywordRunes {
		p.KeywordNote = location
	}
}

// setSalary stores an annual range in a currency the app can display, and
// otherwise leaves salary empty so it is never saved under the wrong unit.
func (p *Posting) setSalary(minimum, maximum float64, currency, interval string) {
	if minimum <= 0 && maximum <= 0 {
		return
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if !isAnnualInterval(interval) {
		p.Warnings = append(p.Warnings, "The posted salary is not annual, so it was left out.")
		return
	}
	if currency != "EUR" && currency != "USD" && currency != "GBP" {
		p.Warnings = append(p.Warnings, "The posted salary uses a currency War Room does not support, so it was left out.")
		return
	}
	if maximum > 0 && minimum > maximum {
		minimum, maximum = maximum, minimum
	}
	p.SalaryMin, p.SalaryMax, p.SalaryCurrency = positiveAmount(minimum), positiveAmount(maximum), currency
}

func positiveAmount(value float64) *int64 {
	if value <= 0 {
		return nil
	}
	amount := int64(value)
	return &amount
}

// isAnnualInterval treats a missing unit as annual only because boards that
// omit it label yearly ranges; any named shorter unit is refused.
func isAnnualInterval(interval string) bool {
	value := strings.ToLower(interval)
	for _, unit := range []string{"hour", "day", "week", "month"} {
		if strings.Contains(value, unit) {
			return false
		}
	}
	return true
}

func workArrangement(value string) string {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(value))
	switch {
	case strings.Contains(normalized, "remote"), strings.Contains(normalized, "telecommute"):
		return "remote"
	case strings.Contains(normalized, "hybrid"):
		return "hybrid"
	case strings.Contains(normalized, "onsite"), strings.Contains(normalized, "inoffice"):
		return "on_site"
	}
	return ""
}

// employmentType maps only permanent roles: "contract" does not say whether
// the contract is the B2B arrangement the app tracks.
func employmentType(value string) string {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(value))
	if strings.Contains(normalized, "fulltime") || strings.Contains(normalized, "permanent") {
		return "permanent"
	}
	return ""
}
