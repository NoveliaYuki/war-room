package backend_test

import (
	"encoding/json"
	"testing"

	"war-room/backend/pkg/models"
)

func TestJobModel_Serialization(t *testing.T) {
	minVal := int64(37000)
	maxVal := int64(83000)
	recName := "Casey Example"

	job := models.Job{
		ID:             "synthetic-job",
		CompanyName:    "Example Company",
		PositionTitle:  "Software Engineer",
		Status:         models.StatusOngoing,
		SalaryType:     models.SalaryLimited,
		SalaryMin:      &minVal,
		SalaryMax:      &maxVal,
		SalaryCurrency: "EUR",
		RecruiterType:  models.RecruiterExternal,
		RecruiterName:  &recName,
		AvatarSeed:     "example-seed",
		KeywordNote:    "Synthetic role data",
		EmploymentType: models.EmploymentTypePermanentB2B,
		IsReferral:     false,
		OrderIndex:     1,
		CreatedAt:      100,
		UpdatedAt:      200,
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}

	var parsed models.Job
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal job: %v", err)
	}

	if parsed.ID != job.ID {
		t.Errorf("job ID did not round trip")
	}
	if parsed.CompanyName != "Example Company" || parsed.PositionTitle != "Software Engineer" {
		t.Errorf("synthetic job identity did not round trip")
	}
	if parsed.SalaryType != models.SalaryLimited || *parsed.SalaryMin != minVal || *parsed.SalaryMax != maxVal {
		t.Errorf("salary fields did not round trip")
	}
	if parsed.EmploymentType != models.EmploymentTypePermanentB2B {
		t.Errorf("employment type did not round trip")
	}
}

func TestStageTypesAndStatuses(t *testing.T) {
	validStages := []models.StageType{
		models.StageHR,
		models.StageTechnical,
		models.StageCultural,
		models.StageOfferDecision,
	}
	if len(validStages) != 4 {
		t.Errorf("Expected exactly 4 standard stage types, got %d", len(validStages))
	}

	validStatuses := []models.StageStatus{
		models.StageStatusPending,
		models.StageStatusCurrent,
		models.StageStatusCompleted,
		models.StageStatusSkipped,
	}
	if len(validStatuses) != 4 {
		t.Errorf("Expected exactly 4 stage statuses, got %d", len(validStatuses))
	}
}

func TestInterviewer_JSONEncoding(t *testing.T) {
	interviewers := []models.Interviewer{
		{Name: "Alex Example", Role: "Engineer", Notes: "Technical panel"},
		{Name: "Jordan Sample", Role: "Product", Notes: "Culture panel"},
	}

	data, err := json.Marshal(interviewers)
	if err != nil {
		t.Fatalf("marshal interviewers: %v", err)
	}

	var decoded []models.Interviewer
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal interviewers: %v", err)
	}

	if len(decoded) != 2 {
		t.Fatalf("expected 2 interviewers, got %d", len(decoded))
	}
	if decoded[0].Name != "Alex Example" || decoded[1].Role != "Product" {
		t.Errorf("interviewer JSON round trip returned unexpected data")
	}
}
