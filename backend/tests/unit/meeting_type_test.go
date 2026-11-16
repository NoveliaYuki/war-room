package backend_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/service"
)

// TestMeetingTypeServiceValidationRejectsUnknownValues protects API meeting data.
func TestMeetingTypeServiceValidationRejectsUnknownValues(t *testing.T) {
	jobs, jobID, stage := newMeetingTypeServiceFixture(t)
	assertAcceptedMeetingTypes(t, jobs, stage.ID)
	assertRejectedMeetingTypes(t, jobs, stage.ID)
	assertMeetingScheduleUnchanged(t, jobs, jobID)
}

func newMeetingTypeServiceFixture(t *testing.T) (*service.JobService, string, *models.Stage) {
	t.Helper()
	_, jobs, _, _ := newCoverageApp(t)
	job, err := jobs.CreateJob(testUserID, models.CreateJobInput{
		CompanyName: "Acme", PositionTitle: "Engineer", CreateDefaultStages: ptr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	stage, err := jobs.CreateStage(testUserID, models.CreateStageInput{JobID: job.ID, StageType: models.StageHR})
	if err != nil {
		t.Fatal(err)
	}
	return jobs, job.ID, stage
}

func assertAcceptedMeetingTypes(t *testing.T, jobs *service.JobService, stageID string) {
	t.Helper()
	for _, meetingType := range []string{"video", "phone", "onsite"} {
		if err := jobs.ScheduleMeeting(testUserID, stageID, models.ScheduleMeetingInput{
			MeetingDate: "2030-01-02", MeetingTime: "09:30", MeetingType: &meetingType,
		}); err != nil {
			t.Errorf("ScheduleMeeting(%q): %v", meetingType, err)
		}
	}
}

func assertRejectedMeetingTypes(t *testing.T, jobs *service.JobService, stageID string) {
	t.Helper()
	invalid := "javascript:alert(1)"
	if err := jobs.ScheduleMeeting(testUserID, stageID, models.ScheduleMeetingInput{
		MeetingDate: "2030-01-03", MeetingTime: "10:00", MeetingType: &invalid,
	}); err == nil {
		t.Fatal("ScheduleMeeting accepted an unknown meeting type")
	}
	if _, err := jobs.UpdateStage(testUserID, stageID, models.UpdateStageInput{MeetingType: &invalid}); err == nil {
		t.Fatal("UpdateStage accepted an unknown meeting type")
	}
}

func assertMeetingScheduleUnchanged(t *testing.T, jobs *service.JobService, jobID string) {
	t.Helper()
	updated, err := jobs.GetFullJobDetails(testUserID, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Stages) != 1 || updated.Stages[0].MeetingType != "onsite" || updated.Stages[0].MeetingDate == nil || *updated.Stages[0].MeetingDate != "2030-01-02" || updated.Stages[0].MeetingTime == nil || *updated.Stages[0].MeetingTime != "09:30" {
		t.Fatalf("invalid schedule changed stage: %+v", updated.Stages)
	}
}

// TestMeetingTypeMigrationNormalizesAndConstrainsLegacyRows protects old SQLite databases.
func TestMeetingTypeMigrationNormalizesAndConstrainsLegacyRows(t *testing.T) {
	directory := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(directory, "legacy.db"), BackupPath: filepath.Join(directory, "backup.json")}
	createLegacyMeetingTypeDatabase(t, cfg.DBPath)
	migratedDB, err := database.InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestResource(t, migratedDB) })
	assertLegacyMeetingTypesNormalized(t, migratedDB)
	if _, err := migratedDB.Exec("UPDATE stages SET meeting_type = 'invalid' WHERE id = 'invalid-stage'"); err == nil {
		t.Fatal("migration did not constrain legacy stage updates")
	}
}

func createLegacyMeetingTypeDatabase(t *testing.T, databasePath string) {
	t.Helper()
	legacyDB, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacyDB.Exec(`
		CREATE TABLE jobs (
			id TEXT PRIMARY KEY,
			company_name TEXT NOT NULL DEFAULT 'Unknown',
			position_title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'waiting',
			salary_type TEXT NOT NULL DEFAULT 'unknown',
			salary_min INTEGER NULL,
			salary_max INTEGER NULL,
			salary_currency TEXT NOT NULL DEFAULT 'EUR',
			recruiter_type TEXT NOT NULL DEFAULT 'none',
			recruiter_name TEXT NULL,
			recruiter_agency TEXT NULL,
			recruiter_contact TEXT NULL,
			interviewers_json TEXT NOT NULL DEFAULT '[]',
			job_post_url TEXT NULL,
			avatar_seed TEXT NOT NULL DEFAULT 'seed',
			keyword_note TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			status_changed_at INTEGER NOT NULL DEFAULT 0,
			application_sent_date TEXT NULL,
			recruiter_first_contact_date TEXT NULL,
			created_at INTEGER NOT NULL DEFAULT (unixepoch()),
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		);
		CREATE TABLE stages (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL REFERENCES jobs(id),
			order_index INTEGER NOT NULL DEFAULT 0,
			stage_type TEXT NOT NULL DEFAULT 'HR',
			custom_title TEXT NULL,
			description TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			scheduled_at INTEGER NULL,
			notes TEXT NOT NULL DEFAULT '',
			meeting_type TEXT NULL DEFAULT 'video',
			created_at INTEGER NOT NULL DEFAULT (unixepoch())
		);
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL DEFAULT (unixepoch()));
		INSERT INTO jobs (id) VALUES ('legacy-job');
		INSERT INTO stages (id, job_id, meeting_type) VALUES ('invalid-stage', 'legacy-job', 'invalid'), ('null-stage', 'legacy-job', NULL);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertLegacyMeetingTypesNormalized(t *testing.T, migratedDB *sql.DB) {
	t.Helper()
	rows, err := migratedDB.Query("SELECT meeting_type FROM stages ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { closeTestResource(t, rows) }()
	for range 2 {
		var meetingType string
		if !rows.Next() {
			t.Fatal("migration removed a legacy stage")
		}
		if err := rows.Scan(&meetingType); err != nil {
			t.Fatal(err)
		}
		if meetingType != "video" {
			t.Fatalf("migrated meeting_type = %q, want video", meetingType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
