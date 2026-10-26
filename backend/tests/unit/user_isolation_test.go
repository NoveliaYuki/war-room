package backend_test

import (
	"database/sql"
	"strings"
	"testing"

	"war-room/backend/pkg/models"
	"war-room/backend/pkg/service"
)

// isolationFixture holds the two synthetic users and their seeded records
// used throughout TestUserDataIsolation's subtests.
type isolationFixture struct {
	jobs             *service.JobService
	userA, userB     string
	jobA, jobB       *models.Job
	techA, techB     models.Technology
	periodA, periodB *models.SearchPeriod
	cvA, cvB         *models.CVVersion
	attachmentB      *models.Attachment
}

func seedTwoUsers(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()
	const userA, userB = "user-a", "user-b"
	for _, user := range []struct{ id, sub, email string }{
		{userA, "sub-a", "a@example.com"},
		{userB, "sub-b", "b@example.com"},
	} {
		if _, err := db.Exec(`INSERT INTO users (id, google_sub, email, name, picture_url) VALUES (?, ?, ?, ?, ?)`,
			user.id, user.sub, user.email, user.email, ""); err != nil {
			t.Fatalf("seed user %q: %v", user.id, err)
		}
	}
	return userA, userB
}

func mustCreateJob(t *testing.T, jobs *service.JobService, userID string, input models.CreateJobInput) *models.Job {
	t.Helper()
	job, err := jobs.CreateJob(userID, input)
	if err != nil {
		t.Fatalf("create job for %s: %v", userID, err)
	}
	return job
}

func mustCreateTechnology(t *testing.T, jobs *service.JobService, userID, name string) models.Technology {
	t.Helper()
	tech, err := jobs.CreateTechnology(userID, name, nil)
	if err != nil {
		t.Fatalf("create technology for %s: %v", userID, err)
	}
	return tech
}

func mustCreateSearchPeriod(t *testing.T, jobs *service.JobService, userID, name string) *models.SearchPeriod {
	t.Helper()
	period, err := jobs.CreateSearchPeriod(userID, models.SearchPeriodInput{Name: name, StartDate: "2026-01-01", EndDate: "2026-02-01"})
	if err != nil {
		t.Fatalf("create search period for %s: %v", userID, err)
	}
	return period
}

func mustStoreCVVersion(t *testing.T, jobs *service.JobService, userID, filename, content string) *models.CVVersion {
	t.Helper()
	version, err := jobs.StoreCVVersion(userID, filename, strings.NewReader(content), int64(len(content)), "application/pdf")
	if err != nil {
		t.Fatalf("store CV version for %s: %v", userID, err)
	}
	return version
}

func mustStoreAttachment(t *testing.T, jobs *service.JobService, userID, jobID, filename, content string) *models.Attachment {
	t.Helper()
	attachment, err := jobs.StoreAttachment(userID, jobID, nil, filename, strings.NewReader(content), int64(len(content)), "text/plain")
	if err != nil {
		t.Fatalf("store attachment for %s: %v", userID, err)
	}
	return attachment
}

func newIsolationFixture(t *testing.T) isolationFixture {
	t.Helper()
	_, jobs, _, _, db := newCoverageDB(t)
	userA, userB := seedTwoUsers(t, db)

	jobA := mustCreateJob(t, jobs, userA, models.CreateJobInput{CompanyName: "A Corp", PositionTitle: "Engineer", CreateDefaultStages: ptr(false)})
	jobB := mustCreateJob(t, jobs, userB, models.CreateJobInput{CompanyName: "B Corp", PositionTitle: "Designer", CreateDefaultStages: ptr(false)})
	techA := mustCreateTechnology(t, jobs, userA, "Go")
	techB := mustCreateTechnology(t, jobs, userB, "Go")
	periodA := mustCreateSearchPeriod(t, jobs, userA, "A Search")
	periodB := mustCreateSearchPeriod(t, jobs, userB, "B Search")
	cvA := mustStoreCVVersion(t, jobs, userA, "resume-a.pdf", "a-content")
	cvB := mustStoreCVVersion(t, jobs, userB, "resume-b.pdf", "b-content")
	assertIndependentCVNumbering(t, cvA, cvB)
	attachmentB := mustStoreAttachment(t, jobs, userB, jobB.ID, "notes-b.txt", "b-attachment")

	return isolationFixture{
		jobs: jobs, userA: userA, userB: userB,
		jobA: jobA, jobB: jobB, techA: techA, techB: techB,
		periodA: periodA, periodB: periodB, cvA: cvA, cvB: cvB, attachmentB: attachmentB,
	}
}

func assertIndependentCVNumbering(t *testing.T, cvA, cvB *models.CVVersion) {
	t.Helper()
	if cvA.Version != 1 || cvB.Version != 1 {
		t.Fatalf("expected each user's CV library to number independently from 1, got A=%d B=%d", cvA.Version, cvB.Version)
	}
}

// TestUserDataIsolation pins the multi-user contract for Phase 4 of the
// Google Sign-In rework: every JobService read, list, update, and delete is
// scoped to the calling user, and a record that belongs to a different user
// behaves exactly as if it does not exist. Two synthetic users are seeded
// directly in the database (this predates any real multi-account sign-in,
// which only ever allows one real account) so the test can prove isolation
// without depending on the HTTP session layer built in later phases.
func TestUserDataIsolation(t *testing.T) {
	fixture := newIsolationFixture(t)

	t.Run("jobs list excludes other user's jobs", func(t *testing.T) { assertJobsListExcludesOtherUser(t, fixture) })
	t.Run("job lookup across users is not found", func(t *testing.T) { assertJobLookupAcrossUsersNotFound(t, fixture) })
	t.Run("job update across users does not mutate", func(t *testing.T) { assertJobUpdateAcrossUsersNoop(t, fixture) })
	t.Run("job delete across users does not remove", func(t *testing.T) { assertJobDeleteAcrossUsersNoop(t, fixture) })
	t.Run("technology catalogs are independent per user", func(t *testing.T) { assertTechnologyCatalogsIndependent(t, fixture) })
	t.Run("search periods are independent per user", func(t *testing.T) { assertSearchPeriodsIndependent(t, fixture) })
	t.Run("CV library lookup across users is not found", func(t *testing.T) { assertCVLookupAcrossUsersNotFound(t, fixture) })
	t.Run("attachment lookup across users is not found", func(t *testing.T) { assertAttachmentLookupAcrossUsersNotFound(t, fixture) })
}

func assertJobsListExcludesOtherUser(t *testing.T, fixture isolationFixture) {
	listA, err := fixture.jobs.GetAllJobs(fixture.userA, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range listA {
		if job.ID == fixture.jobB.ID {
			t.Fatalf("user A's job list leaked user B's job %q", fixture.jobB.ID)
		}
	}
}

func assertJobLookupAcrossUsersNotFound(t *testing.T, fixture isolationFixture) {
	if _, err := fixture.jobs.GetFullJobDetails(fixture.userA, fixture.jobB.ID); err == nil {
		t.Fatal("user A could read user B's job")
	}
}

func assertJobUpdateAcrossUsersNoop(t *testing.T, fixture isolationFixture) {
	title := "Hijacked"
	if _, err := fixture.jobs.UpdateJob(fixture.userA, fixture.jobB.ID, models.UpdateJobInput{PositionTitle: &title}); err == nil {
		t.Fatal("user A could update user B's job")
	}
	stillB, err := fixture.jobs.GetFullJobDetails(fixture.userB, fixture.jobB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillB.PositionTitle != "Designer" {
		t.Fatalf("user B's job was mutated by user A: position_title=%q", stillB.PositionTitle)
	}
}

func assertJobDeleteAcrossUsersNoop(t *testing.T, fixture isolationFixture) {
	if err := fixture.jobs.DeleteJob(fixture.userA, fixture.jobB.ID); err == nil {
		t.Fatal("user A could delete user B's job")
	}
	if _, err := fixture.jobs.GetFullJobDetails(fixture.userB, fixture.jobB.ID); err != nil {
		t.Fatalf("user B's job was deleted by user A: %v", err)
	}
}

func assertTechnologyCatalogsIndependent(t *testing.T, fixture isolationFixture) {
	listA, err := fixture.jobs.ListTechnologies(fixture.userA)
	if err != nil {
		t.Fatal(err)
	}
	if len(listA) != 1 || listA[0].ID != fixture.techA.ID {
		t.Fatalf("user A's technology catalog = %+v, want only %+v", listA, fixture.techA)
	}
	if fixture.techA.ID == fixture.techB.ID {
		t.Fatal("users sharing a technology name were assigned the same technology row")
	}
}

func assertSearchPeriodsIndependent(t *testing.T, fixture isolationFixture) {
	listA, err := fixture.jobs.ListSearchPeriods(fixture.userA)
	if err != nil {
		t.Fatal(err)
	}
	for _, period := range listA {
		if period.ID == fixture.periodB.ID {
			t.Fatalf("user A's search periods leaked user B's period %q", fixture.periodB.ID)
		}
	}
}

func assertCVLookupAcrossUsersNotFound(t *testing.T, fixture isolationFixture) {
	listA, err := fixture.jobs.ListCVVersions(fixture.userA)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range listA {
		if version.ID == fixture.cvB.ID {
			t.Fatalf("user A's CV library leaked user B's CV version %q", fixture.cvB.ID)
		}
	}
	if _, err := fixture.jobs.GetCVVersion(fixture.userA, fixture.cvB.ID); err == nil {
		t.Fatal("user A could read user B's CV version")
	}
}

func assertAttachmentLookupAcrossUsersNotFound(t *testing.T, fixture isolationFixture) {
	if _, err := fixture.jobs.GetAttachmentByID(fixture.userA, fixture.attachmentB.ID); err == nil {
		t.Fatal("user A could read user B's attachment")
	}
	if _, err := fixture.jobs.GetAttachmentByID(fixture.userB, fixture.attachmentB.ID); err != nil {
		t.Fatalf("user B could not read their own attachment: %v", err)
	}
}
