package repository

import (
	"fmt"
	"path/filepath"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/models"
)

// newAllUsersTestDB seeds two users, matching the shape of newCoverageDB in
// tests/unit but local to this package so these *AllUsers repository
// functions (used only by the install-wide disaster-recovery mirror) can be
// exercised directly.
func newAllUsersTestDB(t *testing.T) (*Repository, string, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.InitDB(&config.Config{DBPath: filepath.Join(dir, "all-users.db")})
	if err != nil {
		t.Fatalf("init database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const userA, userB = "all-users-a", "all-users-b"
	for _, id := range []string{userA, userB} {
		if _, err := db.Exec(`INSERT INTO users (id, google_sub, email, name, picture_url) VALUES (?, ?, ?, ?, ?)`,
			id, "sub-"+id, id+"@example.com", id, ""); err != nil {
			t.Fatalf("seed user %q: %v", id, err)
		}
	}
	return New(db), userA, userB
}

func TestListSearchPeriodsAllUsersSpansEveryAccount(t *testing.T) {
	repo, userA, userB := newAllUsersTestDB(t)
	periodA := &models.SearchPeriod{ID: "period-a", Name: "A", StartDate: "2026-01-01", EndDate: "2026-02-01"}
	periodB := &models.SearchPeriod{ID: "period-b", Name: "B", StartDate: "2026-01-01", EndDate: "2026-02-01"}
	if err := repo.CreateSearchPeriod(userA, periodA); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateSearchPeriod(userB, periodB); err != nil {
		t.Fatal(err)
	}
	all, err := repo.ListSearchPeriodsAllUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListSearchPeriodsAllUsers returned %d periods, want 2", len(all))
	}
}

func TestListTechnologiesAllUsersSpansEveryAccountWithOwnership(t *testing.T) {
	repo, userA, userB := newAllUsersTestDB(t)
	if err := repo.CreateTechnology(userA, models.Technology{ID: "tech-a", Name: "Go"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateTechnology(userB, models.Technology{ID: "tech-b", Name: "Rust"}); err != nil {
		t.Fatal(err)
	}
	all, err := repo.ListTechnologiesAllUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListTechnologiesAllUsers returned %d technologies, want 2", len(all))
	}
	for _, tech := range all {
		if tech.UserID == nil {
			t.Fatalf("ListTechnologiesAllUsers did not populate ownership: %+v", tech)
		}
	}
}

func TestListCVVersionsAllUsersSpansEveryAccount(t *testing.T) {
	repo, userA, userB := newAllUsersTestDB(t)
	versionA := &models.CVVersion{ID: "cv-a", Version: 1, OriginalName: "a.pdf", StoredFilename: "a.pdf", FileSize: 1, SHA256: "a", UploadedAt: 1}
	versionB := &models.CVVersion{ID: "cv-b", Version: 1, OriginalName: "b.pdf", StoredFilename: "b.pdf", FileSize: 1, SHA256: "b", UploadedAt: 1}
	if err := repo.InsertCVVersion(userA, versionA); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertCVVersion(userB, versionB); err != nil {
		t.Fatal(err)
	}
	all, err := repo.ListCVVersionsAllUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListCVVersionsAllUsers returned %d versions, want 2", len(all))
	}
}

func TestGetNextCVVersionAllUsersReflectsHighestSequence(t *testing.T) {
	repo, userA, userB := newAllUsersTestDB(t)
	if err := repo.InsertCVVersion(userA, &models.CVVersion{ID: "cv-seq-a", Version: 1, OriginalName: "a.pdf", StoredFilename: "a.pdf", FileSize: 1, SHA256: "a", UploadedAt: 1}); err != nil {
		t.Fatal(err)
	}
	for i, version := range []int{1, 2, 3} {
		if err := repo.InsertCVVersion(userB, &models.CVVersion{ID: fmt.Sprintf("cv-seq-b-%d", i), Version: version, OriginalName: "b.pdf", StoredFilename: fmt.Sprintf("b-%d.pdf", i), FileSize: 1, SHA256: fmt.Sprintf("b-%d", i), UploadedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	next, err := repo.GetNextCVVersionAllUsers()
	if err != nil {
		t.Fatal(err)
	}
	if next < 3 {
		t.Fatalf("GetNextCVVersionAllUsers = %d, want at least 3", next)
	}
}
