package jobimport

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"war-room/backend/pkg/models"
)

// Suggestion is one technology found in a posting. ID is empty for a starter
// entry the user's catalog does not contain yet.
type Suggestion struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases"`
	Recommended bool     `json:"recommended"`
}

var (
	optionalSection = regexp.MustCompile(`(?i)nice to have|bonus|preferred|desirable|a plus|good to have|advantage|plus points|valoraremos|se valorar[áa]|valorable|deseable|ser[áa] un plus`)
	skippedSection  = regexp.MustCompile(`(?i)benefit|perks|about (us|the company)|who we are|what we offer|equal opportunity|beneficios|ofrecemos|qui[ée]nes somos|sobre nosotros`)
	equivalentList  = regexp.MustCompile(`(?i)such as|e\.g\.|or similar|for example|\bor equivalent\b|\bor\b|\bcomo\b|\bo\b|por ejemplo`)
	boldLine        = regexp.MustCompile(`^\*\*[^*]+\*\*:?$`)
)

// exactCaseTerms are also ordinary words, so only their capitalized form counts.
var exactCaseTerms = map[string]bool{
	"Rust": true, "Swift": true, "Dart": true, "React": true, "Spark": true,
	"Helm": true, "Node": true, "Flask": true, "Rails": true,
}

// sentenceWords also open ordinary sentences, so a capitalized match at the
// start of one only counts when it is clearly a list entry.
var sentenceWords = map[string]bool{"Go": true, "R": true, "C": true}

// SuggestTechnologies finds concrete technologies in a posting. Mentions in
// requirement sections are recommended; optional, incidental, and either/or
// mentions stay suggestions the user must opt into.
func SuggestTechnologies(overview string, catalog []models.Technology) []Suggestion {
	entries := catalogEntries(catalog)
	found := map[string]*Suggestion{}
	section := ""
	for _, line := range strings.Split(overview, "\n") {
		if isSectionLine(strings.TrimSpace(line)) {
			section = line
		}
		scanLine(line, section, entries, found)
	}
	suggestions := make([]Suggestion, 0, len(found))
	for _, suggestion := range found {
		suggestions = append(suggestions, *suggestion)
	}
	sort.Slice(suggestions, func(a, b int) bool {
		if suggestions[a].Recommended != suggestions[b].Recommended {
			return suggestions[a].Recommended
		}
		return suggestions[a].Name < suggestions[b].Name
	})
	return suggestions
}

// isSectionLine also accepts bold lines and "Title:" lines, because many career
// sites never use real headings.
func isSectionLine(line string) bool {
	if strings.HasPrefix(line, "- ") {
		return false
	}
	return strings.HasPrefix(line, "#") || boldLine.MatchString(line) || strings.HasSuffix(line, ":")
}

func scanLine(line, section string, entries []Suggestion, found map[string]*Suggestion) {
	if skippedSection.MatchString(section) {
		return
	}
	var matched []Suggestion
	for _, entry := range entries {
		if entryMatches(line, entry) {
			matched = append(matched, entry)
		}
	}
	optional := optionalSection.MatchString(section) || optionalSection.MatchString(line) ||
		(len(matched) > 1 && equivalentList.MatchString(line))
	for _, entry := range matched {
		existing := found[entry.Name]
		if existing == nil {
			copied := entry
			copied.Recommended = !optional
			found[entry.Name] = &copied
			continue
		}
		existing.Recommended = existing.Recommended || !optional
	}
}

func entryMatches(line string, entry Suggestion) bool {
	for _, term := range append([]string{entry.Name}, entry.Aliases...) {
		if containsTerm(line, term) {
			return true
		}
	}
	return false
}

// containsTerm requires whole-token matches, so "go-to-market" is not Go, and
// short names must match case because "go" and "r" are not technologies.
func containsTerm(line, term string) bool {
	if term == "" {
		return false
	}
	haystack, needle := line, term
	if len(term) > 3 && !exactCaseTerms[term] {
		haystack, needle = strings.ToLower(line), strings.ToLower(term)
	}
	for offset := 0; offset < len(haystack); {
		index := strings.Index(haystack[offset:], needle)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(needle)
		ordinaryWord := sentenceWords[term] && startsSentence(line, start, end)
		if isWholeToken(haystack, start, end) && !ordinaryWord {
			return true
		}
		offset = start + 1
	}
	return false
}

func isWholeToken(text string, start, end int) bool {
	if start > 0 && isTokenRune(rune(text[start-1])) {
		return false
	}
	if end >= len(text) {
		return true
	}
	next := rune(text[end])
	if isTokenRune(next) || next == '+' || next == '#' {
		return false
	}
	if next == '-' || next == '.' {
		return end+1 >= len(text) || !isTokenRune(rune(text[end+1]))
	}
	return true
}

func isTokenRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value) || value == '_'
}

func startsSentence(line string, start, end int) bool {
	before := strings.TrimRight(line[:start], " \t*_-•")
	sentenceStart := before == "" || strings.HasSuffix(before, ".") || strings.HasSuffix(before, "!") ||
		strings.HasSuffix(before, "?") || strings.HasSuffix(before, ":")
	if !sentenceStart {
		return false
	}
	rest := strings.TrimLeft(line[end:], " ")
	return rest != "" && rest[0] != ',' && rest[0] != '/' && rest[0] != '('
}

// catalogEntries lists the user's technologies first, then starter entries
// their catalog lacks, so an empty catalog still gets useful suggestions.
func catalogEntries(catalog []models.Technology) []Suggestion {
	entries := make([]Suggestion, 0, len(catalog)+len(starterTechnologies))
	known := map[string]bool{}
	for _, item := range catalog {
		entries = append(entries, Suggestion{ID: item.ID, Name: item.Name, Aliases: item.Aliases})
		known[strings.ToLower(item.Name)] = true
		for _, alias := range item.Aliases {
			known[strings.ToLower(alias)] = true
		}
	}
	for _, starter := range starterTechnologies {
		if !known[strings.ToLower(starter.Name)] {
			entries = append(entries, Suggestion{Name: starter.Name, Aliases: starter.Aliases})
		}
	}
	return entries
}

var starterTechnologies = []Suggestion{
	{Name: "Go", Aliases: []string{"Golang"}}, {Name: "Python"}, {Name: "Java"}, {Name: "Kotlin"},
	{Name: "TypeScript"}, {Name: "JavaScript"}, {Name: "Rust"}, {Name: "C++"}, {Name: "C#"}, {Name: "Ruby"},
	{Name: "PHP"}, {Name: "Scala"}, {Name: "Swift"}, {Name: "Dart"}, {Name: "Elixir"}, {Name: "R"},
	{Name: "React"}, {Name: "Next.js"}, {Name: "Vue.js", Aliases: []string{"Vue"}}, {Name: "Angular"},
	{Name: "Svelte"}, {Name: "Node.js", Aliases: []string{"Node"}}, {Name: "Django"}, {Name: "Flask"},
	{Name: "FastAPI"}, {Name: "Spring Boot"}, {Name: "Ruby on Rails", Aliases: []string{"Rails"}},
	{Name: ".NET"}, {Name: "Laravel"}, {Name: "GraphQL"}, {Name: "gRPC"},
	{Name: "PostgreSQL", Aliases: []string{"Postgres"}}, {Name: "MySQL"}, {Name: "SQLite"},
	{Name: "MongoDB"}, {Name: "Redis"}, {Name: "Elasticsearch"}, {Name: "Cassandra"}, {Name: "DynamoDB"},
	{Name: "ClickHouse"}, {Name: "Snowflake"}, {Name: "BigQuery"}, {Name: "Databricks"},
	{Name: "Apache Kafka", Aliases: []string{"Kafka"}}, {Name: "RabbitMQ"}, {Name: "Apache Spark", Aliases: []string{"Spark"}},
	{Name: "Apache Airflow", Aliases: []string{"Airflow"}}, {Name: "dbt"},
	{Name: "Amazon Web Services", Aliases: []string{"AWS"}}, {Name: "Google Cloud Platform", Aliases: []string{"GCP", "Google Cloud"}},
	{Name: "Microsoft Azure", Aliases: []string{"Azure"}}, {Name: "Docker"}, {Name: "Kubernetes", Aliases: []string{"K8s"}},
	{Name: "Terraform"}, {Name: "Ansible"}, {Name: "Helm"}, {Name: "GitHub Actions"}, {Name: "Jenkins"},
	{Name: "Prometheus"}, {Name: "Grafana"}, {Name: "Datadog"}, {Name: "PyTorch"}, {Name: "TensorFlow"},
	{Name: "LangChain"}, {Name: "LangGraph"}, {Name: "OpenSearch"}, {Name: "Pinecone"}, {Name: "pgvector"},
	{Name: "Pandas"}, {Name: "NumPy"}, {Name: "Linux"},
}
