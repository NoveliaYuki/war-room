package models

import (
	"encoding/json"
	"testing"
)

func TestJobAndQuestionDecodeStoredBooleanFormats(t *testing.T) {
	for _, value := range []struct {
		json  string
		want  bool
		valid bool
	}{
		{json: ``, want: false, valid: true},
		{json: `true`, want: true, valid: true},
		{json: `false`, want: false, valid: true},
		{json: `1`, want: true, valid: true},
		{json: `0`, want: false, valid: true},
		{json: `2`, valid: false},
		{json: `"true"`, valid: false},
		{json: `null`, valid: false},
	} {
		assertStoredBoolean(t, value.json, value.want, value.valid)
	}
	for _, malformed := range []string{`{`, `{"is_referral":`, `{"is_asked":`} {
		var job Job
		if err := json.Unmarshal([]byte(malformed), &job); err == nil {
			t.Errorf("malformed job JSON %q should fail", malformed)
		}
		var question Question
		if err := json.Unmarshal([]byte(malformed), &question); err == nil {
			t.Errorf("malformed question JSON %q should fail", malformed)
		}
	}
}

func assertStoredBoolean(t *testing.T, raw string, want, valid bool) {
	t.Helper()
	jobJSON, questionJSON := `{"id":"job"}`, `{"id":"question"}`
	if raw != "" {
		jobJSON = `{"id":"job","is_referral":` + raw + `}`
		questionJSON = `{"id":"question","is_asked":` + raw + `}`
	}
	var job Job
	jobErr := json.Unmarshal([]byte(jobJSON), &job)
	var question Question
	questionErr := json.Unmarshal([]byte(questionJSON), &question)
	if valid {
		if jobErr != nil || questionErr != nil || job.IsReferral != want || question.IsAsked != want {
			t.Errorf("value %s decoded job=%t err=%v question=%t err=%v", raw, job.IsReferral, jobErr, question.IsAsked, questionErr)
		}
		return
	}
	if jobErr == nil || questionErr == nil {
		t.Errorf("value %s should be rejected by both models", raw)
	}
}
