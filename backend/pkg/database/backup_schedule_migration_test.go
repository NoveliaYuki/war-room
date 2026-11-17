package database

import "testing"

func TestBackupScheduleMigrationLeavesExistingAccountsOff(t *testing.T) {
	db := openMigrationTestDB(t)
	mustMigrationExec(t, db, schemaSQL)
	mustMigrationSchema(t, db)
	mustMigrationExec(t, db, "DROP TABLE backup_schedules; DELETE FROM schema_migrations WHERE version = 25")
	mustMigrationExec(t, db, `INSERT INTO users (id, google_sub, email) VALUES ('existing-user', 'sub-1', 'existing@example.com')`)
	mustMigrationExec(t, db, `INSERT INTO google_drive_accounts (user_id, credentials_encrypted, folder_id) VALUES ('existing-user', x'00', 'folder-1')`)

	mustMigrationSchema(t, db)

	var schedules int
	if err := db.QueryRow("SELECT COUNT(*) FROM backup_schedules").Scan(&schedules); err != nil || schedules != 0 {
		t.Fatalf("existing accounts must start without a schedule (Off), count=%d err=%v", schedules, err)
	}
	var folder string
	if err := db.QueryRow("SELECT folder_id FROM google_drive_accounts WHERE user_id = 'existing-user'").Scan(&folder); err != nil || folder != "folder-1" {
		t.Fatalf("migration should preserve Drive accounts, folder=%q err=%v", folder, err)
	}
	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != 25 {
		t.Fatalf("schema version = %d, err=%v; want 25", version, err)
	}
}

func TestBackupScheduleMigrationConstrainsRowsAndCascades(t *testing.T) {
	db := openMigrationTestDB(t)
	mustMigrationExec(t, db, schemaSQL)
	mustMigrationSchema(t, db)
	mustMigrationExec(t, db, `INSERT INTO users (id, google_sub, email) VALUES ('user-1', 'sub-1', 'one@example.com')`)
	mustMigrationExec(t, db, `INSERT INTO backup_schedules (user_id, frequency, weekday, minute_of_day, time_zone, next_run_at) VALUES ('user-1', 'weekly', 1, 540, 'Europe/Madrid', 1700000000)`)

	for name, statement := range map[string]string{
		"unknown frequency":       `UPDATE backup_schedules SET frequency = 'hourly'`,
		"weekday out of range":    `UPDATE backup_schedules SET weekday = 7`,
		"minute out of range":     `UPDATE backup_schedules SET minute_of_day = 1440`,
		"empty time zone":         `UPDATE backup_schedules SET time_zone = ''`,
		"off with a next run":     `UPDATE backup_schedules SET frequency = 'off'`,
		"scheduled with no next":  `UPDATE backup_schedules SET next_run_at = NULL`,
		"schedule for nobody":     `INSERT INTO backup_schedules (user_id) VALUES ('missing-user')`,
		"duplicate user schedule": `INSERT INTO backup_schedules (user_id) VALUES ('user-1')`,
	} {
		if _, err := db.Exec("PRAGMA foreign_keys = ON; " + statement); err == nil {
			t.Errorf("%s: statement should be rejected", name)
		}
	}

	mustMigrationExec(t, db, "PRAGMA foreign_keys = ON; DELETE FROM users WHERE id = 'user-1'")
	var remaining int
	if err := db.QueryRow("SELECT COUNT(*) FROM backup_schedules").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("deleting a user should delete its schedule, remaining=%d err=%v", remaining, err)
	}
}

func TestBackupScheduleMigrationReportsTableAndIndexFailures(t *testing.T) {
	for _, failure := range []string{"CREATE TABLE IF NOT EXISTS backup_schedules", "CREATE INDEX IF NOT EXISTS idx_backup_schedules_due"} {
		t.Run(failure, func(t *testing.T) {
			db := openMigrationTestDB(t)
			mustMigrationExec(t, db, schemaSQL)
			if err := migrateBackupSchedules(migrationTestExecutor{db: db, failExec: failure}); err == nil {
				t.Fatalf("migration did not report failure for %q", failure)
			}
		})
	}
}

func TestBackupScheduleMigrationDispatchRejectsUnknownVersion(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := runBackupScheduleSchemaMigration(db, 26); err == nil {
		t.Fatal("unknown migration version should be rejected")
	}
}
