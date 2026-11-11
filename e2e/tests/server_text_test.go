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
}

// invalidFieldPattern finds the details sent after "request contains an invalid field value: ".
var invalidFieldPattern = regexp.MustCompile(`fmt\.Errorf\("%w: ([^"%]+)", ErrInvalidField`)

// translationKeyPattern finds the quoted message keys of a translation map in serverMessages.js.
var translationKeyPattern = regexp.MustCompile(`(?m)^\s*"((?:[^"\\]|\\.)+)":`)

// echoedErrorPattern finds service errors whose own message a handler sends.
var echoedErrorPattern = regexp.MustCompile(`var (Err\w+) = errors\.New\("((?:[^"\\]|\\.)+)"\)`)

var defaultStagePattern = regexp.MustCompile(`\{models\.Stage\w+, "([^"]+)", "([^"]+)"\}`)

// historyActionPatterns find the English card history entries the database and the demo store record.
var historyActionPatterns = map[string]*regexp.Regexp{
	"backend/pkg/database/db.go": regexp.MustCompile(`VALUES \(NEW\.id, [^,]+, '((?:changed|created)[^']*)'\)`),
	"frontend/demo/demoStore.js": regexp.MustCompile(`(?:recordDemoHistory\(job, |\["\w+", )"((?:changed|created)[^"]*)"`),
}

// untranslatedServerText is sent only to requests the interface never shows as text.
var untranslatedServerText = map[string]string{
	"Unknown company logo not tracked": "answers an <img> request for a logo",
	"Logo not found":                   "answers an <img> request for a logo",
	"invalid_grant":                    "is Google's OAuth error code, which the backend reads and logs",
}

// TestEveryServerMessageHasATranslation fails when the backend gains a message,
// job import warning, card history entry, or default stage text that the interface cannot translate.
func TestEveryServerMessageHasATranslation(t *testing.T) {
	translations := readRepositoryFile(t, "frontend/public/js/utils/serverMessages.js")
	messageKeys := translationKeys(t, translations, "MESSAGE_KEYS")
	invalidFieldKeys := translationKeys(t, translations, "INVALID_FIELD_KEYS")
	messages, details := serverMessages(t)
	for _, message := range messages {
		if _, exempt := untranslatedServerText[message]; !exempt && !messageKeys[message] {
			t.Errorf("server message %q has no entry in MESSAGE_KEYS in frontend/public/js/utils/serverMessages.js", message)
		}
	}
	for _, detail := range details {
		if !invalidFieldKeys[detail] && !strings.HasSuffix(detail, " must use YYYY-MM-DD") {
			t.Errorf("invalid field detail %q has no entry in INVALID_FIELD_KEYS in frontend/public/js/utils/serverMessages.js", detail)
		}
	}
	assertGeneratedTextTranslated(t)
}

// assertGeneratedTextTranslated checks the default stage text and card history entries the app generates.
func assertGeneratedTextTranslated(t *testing.T) {
	t.Helper()
	labels := readRepositoryFile(t, "frontend/public/js/utils/labels.js")
	for _, text := range defaultStageTexts(t) {
		if !strings.Contains(labels, `"`+text+`"`) {
			t.Errorf("default stage text %q has no translation key in frontend/public/js/utils/labels.js", text)
		}
	}
	for _, action := range historyActions(t) {
		if !strings.Contains(labels, `"`+action+`": "history.`) && !strings.Contains(labels, action+`: "history.`) {
			t.Errorf("card history entry %q has no translation key in frontend/public/js/utils/labels.js", action)
		}
	}
}

// historyActions lists the card history entries the database and the demo store record.
func historyActions(t *testing.T) []string {
	t.Helper()
	var actions []string
	for path, pattern := range historyActionPatterns {
		matches := pattern.FindAllStringSubmatch(readRepositoryFile(t, path), -1)
		if len(matches) == 0 {
			t.Fatalf("found no history entries in %s; the pattern no longer matches it", path)
		}
		for _, match := range matches {
			actions = append(actions, match[1])
		}
	}
	return actions
}

// translationKeys returns the messages a named translation map in serverMessages.js lists.
func translationKeys(t *testing.T, source, name string) map[string]bool {
	t.Helper()
	block := regexp.MustCompile(`const ` + name + ` = new Map\(Object\.entries\(\{([^}]*)\}\)\);`).FindStringSubmatch(source)
	if block == nil {
		t.Fatalf("found no %s map in frontend/public/js/utils/serverMessages.js", name)
	}
	keys := map[string]bool{}
	for _, match := range translationKeyPattern.FindAllStringSubmatch(block[1], -1) {
		keys[match[1]] = true
	}
	return keys
}

// serverMessages lists every literal message the backend can send to the interface,
// and separately the details it sends after "request contains an invalid field value: ".
func serverMessages(t *testing.T) (messages, details []string) {
	t.Helper()
	seen := map[*[]string]map[string]bool{&messages: {}, &details: {}}
	add := func(list *[]string, text string) {
		if !seen[list][text] {
			seen[list][text] = true
			*list = append(*list, text)
		}
	}
	echoed := echoedServiceErrors(t)
	for _, source := range backendSources(t) {
		for _, pattern := range serverTextPatterns {
			for _, match := range pattern.FindAllStringSubmatch(source, -1) {
				add(&messages, match[1])
			}
		}
		for _, match := range invalidFieldPattern.FindAllStringSubmatch(source, -1) {
			add(&details, match[1])
		}
		for _, match := range echoedErrorPattern.FindAllStringSubmatch(source, -1) {
			if echoed[match[1]] {
				add(&messages, match[2])
			}
		}
	}
	if len(messages) < 50 || len(details) == 0 {
		t.Fatalf("found only %d server messages and %d invalid field details; the source patterns no longer match the backend", len(messages), len(details))
	}
	return messages, details
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
