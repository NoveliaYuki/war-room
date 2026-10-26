package repository

import (
	"database/sql"
	"path/filepath"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
)

func newClaimTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := database.InitDB(&config.Config{DBPath: filepath.Join(dir, "claim.db")})
	if err != nil {
		t.Fatalf("init database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestClaimUnownedDataSurfacesTableUpdateFailure(t *testing.T) {
	db := newClaimTestDB(t)
	if _, err := db.Exec(`DROP TABLE technologies`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := ClaimUnownedData(tx, "user-1"); err == nil {
		t.Fatal("expected claim to fail when a claimable table is missing")
	}
}

func TestClaimCVVersionSequenceSurfacesLookupFailure(t *testing.T) {
	db := newClaimTestDB(t)
	if _, err := db.Exec(`DROP TABLE cv_versions`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := claimCVVersionSequence(tx, "user-1"); err == nil {
		t.Fatal("expected sequence claim to fail when cv_versions is missing")
	}
}

func TestClaimCVVersionSequenceSurfacesSeedFailure(t *testing.T) {
	db := newClaimTestDB(t)
	if _, err := db.Exec(`DROP TABLE cv_version_sequence`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := claimCVVersionSequence(tx, "user-1"); err == nil {
		t.Fatal("expected sequence claim to fail when cv_version_sequence is missing")
	}
}
