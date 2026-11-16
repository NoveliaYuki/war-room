package backend_test

import (
	"os"
	"path/filepath"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
)

func TestInitDBRestoresFromInitialBackupWhenNoPersistentBackupExists(t *testing.T) {
	dir := t.TempDir()
	initialBackup := filepath.Join(dir, "seed.json")
	data := []byte(`[{"id":"seed-job","company_name":"Seed Co","position_title":"Engineer","status":"ongoing","salary_type":"unknown","salary_currency":"EUR","recruiter_type":"none","avatar_seed":"seed"}]`)
	if err := os.WriteFile(initialBackup, data, 0600); err != nil {
		t.Fatalf("write initial backup: %v", err)
	}

	db, err := database.InitDB(&config.Config{
		DBPath:            filepath.Join(dir, "jobs.db"),
		BackupPath:        filepath.Join(dir, "mutable", "backup.json"),
		InitialBackupPath: initialBackup,
	})
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })

	var company string
	if err := db.QueryRow("SELECT company_name FROM jobs WHERE id = ?", "seed-job").Scan(&company); err != nil {
		t.Fatalf("read restored seed job: %v", err)
	}
	if company != "Seed Co" {
		t.Fatalf("unexpected restored company: %q", company)
	}
}
