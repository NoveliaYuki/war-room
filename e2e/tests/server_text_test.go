package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// serverTextPatterns find the English text the backend sends to the interface.
var serverTextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:ErrorJSON|publicError|writeJSONError)\(w,\s*[^,]+,\s*"((?:[^"\\]|\\.)+)"`),
	regexp.MustCompile(`http\.Error\(w,\s*"((?:[^"\\]|\\.)+)"`),
	regexp.MustCompile(`newError\(Code\w+,\s*"((?:[^"\\]|\\.)+)"\)`),
	regexp.MustCompile(`Message:\s*"((?:[^"\\]|\\.)+)"`),
	regexp.MustCompile(`Warnings = append\([^,]+,\s*"((?:[^"\\]|\\.)+)"\)`),
	regexp.MustCompile(`"error":\s*"((?:[^"\\]|\\.)+)"`),
	regexp.MustCompile(`fmt\.Errorf\("%w: ([^"%]+)", ErrInvalidField`),
}

// echoedErrorPattern finds service errors whose own message a handler sends.
var echoedErrorPattern = regexp.MustCompile(`var (Err\w+) = errors\.New\("((?:[^"\\]|\\.)+)"\)`)

var defaultStagePattern = regexp.MustCompile(`\{models\.Stage\w+, "([^"]+)", "([^"]+)"\}`)

// untranslatedServerText is sent only to requests the interface never shows as text.
var untranslatedServerText = map[string]string{
	"Unknown company logo not tracked": "answers an <img> request for a logo",
	"Logo not found":                   "answers an <img> request for a logo",
	"invalid_grant":                    "is Google's OAuth error code, which the backend reads and logs",
}

// TestEveryServerMessageHasATranslation fails when the backend gains a message,
// job import warning, or default stage text that the interface cannot translate.
func TestEveryServerMessageHasATranslation(t *testing.T) {
	translations := readRepositoryFile(t, "frontend/public/js/utils/serverMessages.js")
	for _, message := range serverMessages(t) {
		if _, exempt := untranslatedServerText[message]; exempt {
			continue
		}
		if !strings.Contains(translations, `"`+message+`"`) {
			t.Errorf("server message %q has no entry in frontend/public/js/utils/serverMessages.js", message)
		}
	}
	labels := readRepositoryFile(t, "frontend/public/js/utils/labels.js")
	for _, text := range defaultStageTexts(t) {
		if !strings.Contains(labels, `"`+text+`"`) {
			t.Errorf("default stage text %q has no translation key in frontend/public/js/utils/labels.js", text)
		}
	}
}

// serverMessages lists every literal message the backend can send to the interface.
func serverMessages(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var messages []string
	add := func(message string) {
		if !seen[message] {
			seen[message] = true
			messages = append(messages, message)
		}
	}
	echoed := echoedServiceErrors(t)
	for _, source := range backendSources(t) {
		for _, pattern := range serverTextPatterns {
			for _, match := range pattern.FindAllStringSubmatch(source, -1) {
				add(match[1])
			}
		}
		for _, match := range echoedErrorPattern.FindAllStringSubmatch(source, -1) {
			if echoed[match[1]] {
				add(match[2])
			}
		}
	}
	if len(messages) < 50 {
		t.Fatalf("found only %d server messages; the source patterns no longer match the backend", len(messages))
	}
	return messages
}

// echoedServiceErrors names the service errors whose message handlers send as-is.
func echoedServiceErrors(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, source := range backendSources(t) {
		for _, match := range regexp.MustCompile(`service\.(Err\w+)\.Error\(\)`).FindAllStringSubmatch(source, -1) {
			names[match[1]] = true
		}
		if list := regexp.MustCompile(`validationErrors = \[\]error\{([^}]*)\}`).FindStringSubmatch(source); list != nil {
			for _, name := range strings.Split(list[1], ",") {
				names[strings.TrimSpace(name)] = true
			}
		}
	}
	return names
}

// defaultStageTexts lists the titles and descriptions the backend gives default stages.
func defaultStageTexts(t *testing.T) []string {
	t.Helper()
	source := readRepositoryFile(t, "backend/pkg/service/job_service.go")
	var texts []string
	for _, match := range defaultStagePattern.FindAllStringSubmatch(source, -1) {
		texts = append(texts, match[1], match[2])
	}
	if len(texts) == 0 {
		t.Fatal("found no default stages in backend/pkg/service/job_service.go")
	}
	return texts
}

// backendSources returns the backend's non-test Go sources.
func backendSources(t *testing.T) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(repositoryPath("backend"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			paths = append(paths, path)
		}
		return err
	})
	if err != nil {
		t.Fatalf("find backend sources: %v", err)
	}
	sources := make([]string, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path) // #nosec G304 -- reads this repository's backend sources.
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sources = append(sources, string(content))
	}
	return sources
}

func readRepositoryFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(repositoryPath(path)) // #nosec G304 -- reads a fixed file in this repository.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func repositoryPath(path string) string {
	return filepath.Join("..", "..", filepath.FromSlash(path))
}

// decodeJSONValue converts a value returned by page evaluation into target.
func decodeJSONValue(t *testing.T, value any, target any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode page value: %v", err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatalf("decode page value: %v", err)
	}
}
