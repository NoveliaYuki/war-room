package database

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/models"
)

func TestRecoveryRestoreUsesSeedWhenMutableBackupIsMissing(t *testing.T) {
	db, cfg := recoveryCoverageDB(t)
	job := models.Job{ID: "seed-job", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing, AvatarSeed: "seed"}
	writeRecoveryCoverageBackup(t, cfg.InitialBackupPath, []models.Job{job})

	restoreFromBackupIfEmpty(db, cfg.BackupPath, cfg.InitialBackupPath, cfg.AttachmentsDir, cfg.CVDir)
	assertRecoveryCoverageJobCount(t, db, 1)
}

func TestRecoveryRestoreDoesNotLeavePartialRowsAfterFailure(t *testing.T) {
	db, cfg := recoveryCoverageDB(t)
	badJob := models.Job{ID: "bad-job", CompanyName: "Example", PositionTitle: "Engineer", Status: models.JobStatus("invalid"), AvatarSeed: "seed"}
	writeRecoveryCoverageBackup(t, cfg.BackupPath, []models.Job{badJob})

	restoreFromBackupIfEmpty(db, cfg.BackupPath, cfg.InitialBackupPath, cfg.AttachmentsDir, cfg.CVDir)
	assertRecoveryCoverageJobCount(t, db, 0)
}

func TestRecoveryRestoreIgnoresEmptyAndMalformedBackups(t *testing.T) {
	db, cfg := recoveryCoverageDB(t)
	for _, contents := range [][]byte{[]byte("[]"), []byte("invalid json")} {
		if err := os.WriteFile(cfg.BackupPath, contents, 0600); err != nil {
			t.Fatal(err)
		}
		restoreFromBackupIfEmpty(db, cfg.BackupPath, cfg.InitialBackupPath, cfg.AttachmentsDir, cfg.CVDir)
		assertRecoveryCoverageJobCount(t, db, 0)
	}
}

func TestRecoveryRestoreSkipsNonemptyDatabase(t *testing.T) {
	db, cfg := recoveryCoverageDB(t)
	job := models.Job{ID: "existing", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing, AvatarSeed: "seed"}
	if _, err := db.Exec(`INSERT INTO jobs (id, company_name, position_title, status, salary_type, salary_currency, recruiter_type, avatar_seed) VALUES (?, ?, ?, ?, 'unknown', 'EUR', 'none', ?)`, job.ID, job.CompanyName, job.PositionTitle, job.Status, job.AvatarSeed); err != nil {
		t.Fatal(err)
	}
	writeRecoveryCoverageBackup(t, cfg.BackupPath, []models.Job{{ID: "backup-job", CompanyName: "Other", PositionTitle: "Role", Status: models.StatusOngoing, AvatarSeed: "other"}})

	restoreFromBackupIfEmpty(db, cfg.BackupPath, cfg.InitialBackupPath, cfg.AttachmentsDir, cfg.CVDir)
	assertRecoveryCoverageJobCount(t, db, 1)
}

func TestRecoveryRestoreSkipsCatalogOnlyDatabase(t *testing.T) {
	db, cfg := recoveryCoverageDB(t)
	if _, err := db.Exec(`INSERT INTO technologies (id, name, normalized_name) VALUES ('existing-tech', 'Go', 'go')`); err != nil {
		t.Fatal(err)
	}
	job := models.Job{ID: "backup-job", CompanyName: "Example", PositionTitle: "Role", Status: models.StatusOngoing, AvatarSeed: "other"}
	writeRecoveryCoverageBackup(t, cfg.BackupPath, []models.Job{job})
	restoreFromBackupIfEmpty(db, cfg.BackupPath, cfg.InitialBackupPath, cfg.AttachmentsDir, cfg.CVDir)
	assertRecoveryCoverageJobCount(t, db, 0)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM technologies`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("catalog count=%d err=%v", count, err)
	}
}

func TestRecoverySnapshotAcceptsLegacyArrayAndRejectsMissingJobs(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "legacy.json")
	writeRecoveryCoverageBackup(t, backup, []models.Job{})
	snapshot, err := loadRecoverySnapshot(backup)
	if err != nil || snapshot.Jobs == nil || snapshot.NextCVVersion != 1 {
		t.Fatalf("legacy snapshot=%+v err=%v", snapshot, err)
	}
	if err := os.WriteFile(backup, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRecoverySnapshot(backup); err == nil {
		t.Fatal("snapshot without jobs should fail")
	}
	if _, err := loadRecoverySnapshot(t.TempDir()); err == nil {
		t.Fatal("directory read should fail")
	}
}

func recoveryCoverageDB(t *testing.T) (*sql.DB, *config.Config) {
	t.Helper()
	directory := t.TempDir()
	cfg := &config.Config{
		DataDir: directory, DBPath: filepath.Join(directory, "recovery.sqlite"),
		BackupPath: filepath.Join(directory, "backup.json"), InitialBackupPath: filepath.Join(directory, "seed.json"),
		AttachmentsDir: filepath.Join(directory, "attachments"), CVDir: filepath.Join(directory, "cvs"), LogosDir: filepath.Join(directory, "logos"),
	}
	db, err := InitDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, cfg
}

func writeRecoveryCoverageBackup(t *testing.T, path string, jobs []models.Job) {
	t.Helper()
	contents, err := json.Marshal(jobs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
}

func assertRecoveryCoverageJobCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM jobs").Scan(&count); err != nil || count != want {
		t.Fatalf("job count=%d want=%d err=%v", count, want, err)
	}
}
