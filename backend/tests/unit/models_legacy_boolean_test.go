package backend_test

import (
	"encoding/json"
	"testing"

	"war-room/backend/pkg/models"
)

// TestLegacyIntegerFlagsDecodeAsBooleans protects recovery of older JSON snapshots.
func TestLegacyIntegerFlagsDecodeAsBooleans(t *testing.T) {
	var job models.Job
	if err := json.Unmarshal([]byte(`{"id":"legacy","is_referral":1,"interviewers_json":"[]"}`), &job); err != nil {
		t.Fatalf("decode legacy job: %v", err)
	}
	if !job.IsReferral {
		t.Fatal("legacy referral flag did not decode as true")
	}
	var question models.Question
	if err := json.Unmarshal([]byte(`{"id":"q","is_asked":0}`), &question); err != nil {
		t.Fatalf("decode legacy question: %v", err)
	}
	if question.IsAsked {
		t.Fatal("legacy asked flag did not decode as false")
	}
	encoded, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("encode job: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode encoded job: %v", err)
	}
	if string(fields["is_referral"]) != "true" {
		t.Fatalf("API referral field = %s, want true", fields["is_referral"])
	}
	if _, exists := fields["interviewers_json"]; exists {
		t.Fatal("storage-only interviewer JSON leaked into the API")
	}
}

// TestLegacyFlagsRejectInvalidIntegers rejects values outside SQLite's boolean domain.
func TestLegacyFlagsRejectInvalidIntegers(t *testing.T) {
	cases := []struct {
		input string
		isJob bool
	}{
		{input: `{"is_referral":2}`, isJob: true},
		{input: `{"is_referral":-1}`, isJob: true},
		{input: `{"is_asked":2}`},
	}
	for _, testCase := range cases {
		var err error
		if testCase.isJob {
			var job models.Job
			err = json.Unmarshal([]byte(testCase.input), &job)
		} else {
			var question models.Question
			err = json.Unmarshal([]byte(testCase.input), &question)
		}
		if err == nil {
			t.Errorf("accepted invalid boolean data %s", testCase.input)
		}
	}
}
