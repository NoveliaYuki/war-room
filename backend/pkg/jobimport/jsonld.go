package jobimport

import (
	"bytes"
	"encoding/json"
	"html"
	"strconv"
	"strings"
	"time"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// postingFromJSONLD reads the schema.org JobPosting that career sites publish
// for Google Jobs.
func postingFromJSONLD(page []byte) *Posting {
	root, err := nethtml.Parse(bytes.NewReader(page))
	if err != nil {
		return nil
	}
	for _, script := range jsonLDScripts(root) {
		var value any
		if json.Unmarshal([]byte(script), &value) != nil {
			continue
		}
		if job := findJobPosting(value, 0); job != nil {
			return mapJobPosting(job)
		}
	}
	return nil
}

func jsonLDScripts(n *nethtml.Node) []string {
	var scripts []string
	if n.DataAtom == atom.Script && strings.EqualFold(attribute(n, "type"), "application/ld+json") && n.FirstChild != nil {
		scripts = append(scripts, n.FirstChild.Data)
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		scripts = append(scripts, jsonLDScripts(child)...)
	}
	return scripts
}

// findJobPosting searches arrays and @graph containers with bounded depth.
func findJobPosting(value any, depth int) map[string]any {
	if depth > 4 {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if job := findJobPosting(item, depth+1); job != nil {
				return job
			}
		}
	case map[string]any:
		if hasType(typed["@type"], "JobPosting") {
			return typed
		}
		return findJobPosting(typed["@graph"], depth+1)
	}
	return nil
}

func hasType(value any, name string) bool {
	switch typed := value.(type) {
	case string:
		return typed == name
	case []any:
		for _, item := range typed {
			if item == name {
				return true
			}
		}
	}
	return false
}

func mapJobPosting(job map[string]any) *Posting {
	description := html.UnescapeString(text(job["description"]))
	posting := &Posting{
		PositionTitle: collapseSpace(html.UnescapeString(text(job["title"]))), Source: "structured_data",
		CompanyOverview: htmlToMarkdown(description),
		WorkArrangement: workArrangement(text(job["jobLocationType"])),
		EmploymentType:  employmentType(joined(job["employmentType"])),
	}
	applyOrganization(posting, job["hiringOrganization"])
	posting.setLocation(locationName(job["jobLocation"]))
	applySalary(posting, job["baseSalary"])
	if expired(text(job["validThrough"])) {
		posting.Warnings = append(posting.Warnings, "This posting's closing date has passed.")
	}
	return posting
}

func applyOrganization(posting *Posting, value any) {
	if name, ok := value.(string); ok {
		posting.CompanyName = collapseSpace(name)
		return
	}
	organization, _ := value.(map[string]any)
	posting.CompanyName = collapseSpace(html.UnescapeString(text(organization["name"])))
	for _, key := range []string{"sameAs", "url"} {
		if domain := domainFrom(text(organization[key])); domain != "" {
			posting.CompanyDomain = domain
			return
		}
	}
}

func applySalary(posting *Posting, value any) {
	salary, _ := value.(map[string]any)
	amount, ok := salary["value"].(map[string]any)
	if !ok {
		return
	}
	minimum, maximum := number(amount["minValue"]), number(amount["maxValue"])
	if minimum == 0 && maximum == 0 {
		minimum = number(amount["value"])
		maximum = minimum
	}
	posting.setSalary(minimum, maximum, text(salary["currency"]), text(amount["unitText"]))
}

func locationName(value any) string {
	if list, ok := value.([]any); ok && len(list) > 0 {
		value = list[0]
	}
	location, _ := value.(map[string]any)
	address, _ := location["address"].(map[string]any)
	var parts []string
	for _, key := range []string{"addressLocality", "addressCountry"} {
		if part := text(address[key]); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ", ")
}

func expired(value string) bool {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Before(time.Now())
		}
	}
	return false
}

func text(value any) string {
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
}

func joined(value any) string {
	list, ok := value.([]any)
	if !ok {
		return text(value)
	}
	parts := make([]string, 0, len(list))
	for _, item := range list {
		parts = append(parts, text(item))
	}
	return strings.Join(parts, " ")
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		parsed, _ := strconv.ParseFloat(strings.ReplaceAll(typed, ",", ""), 64)
		return parsed
	}
	return 0
}
