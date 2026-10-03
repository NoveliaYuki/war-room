package backend_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
	"war-room/backend/pkg/service"
)

type testArchiveEntry struct {
	name string
	data []byte
}

func makeTestArchive(t *testing.T, entries ...testArchiveEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range entries {
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func testManifest(t *testing.T, jobs []models.Job) []byte {
	t.Helper()
	value := map[string]any{"format": "war-room-backup", "version": 1, "jobs": jobs, "attachment_sha256": map[string]string{}}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestArchiveRoundTripWithAttachmentAndLogo(t *testing.T) {
	archive, jobID, attachmentID, stages := roundTripSource(t)
	verifyRoundTripTarget(t, archive, jobID, attachmentID, stages)
}

func requireArchiveNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func roundTripSource(t *testing.T) ([]byte, string, string, int) {
	t.Helper()
	_, source, _, cfg := newCoverageApp(t)
	job, err := source.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer", CompanyDomain: ptr("example.org"), Interviewers: []models.Interviewer{{Name: "Alex", Role: "Lead"}}})
	requireArchiveNoError(t, err)
	attachment, err := source.StoreAttachment(job.ID, nil, "resume.txt", strings.NewReader("resume"), 6, "text/plain")
	requireArchiveNoError(t, err)
	_, err = source.CreateQuestion(models.CreateQuestionInput{StageID: job.Stages[0].ID, Question: "What is the team size?"})
	requireArchiveNoError(t, err)
	requireArchiveNoError(t, os.WriteFile(filepath.Join(cfg.LogosDir, "example.org.png"), []byte("logo"), 0600))
	var output bytes.Buffer
	requireArchiveNoError(t, source.ExportArchive(&output))
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	requireArchiveNoError(t, err)
	if len(archive.File) != 3 {
		t.Fatalf("archive files = %d", len(archive.File))
	}
	return output.Bytes(), job.ID, attachment.ID, len(job.Stages)
}

func verifyRoundTripTarget(t *testing.T, archive []byte, jobID, attachmentID string, stages int) {
	t.Helper()
	_, target, _, targetCfg := newCoverageApp(t)
	prior, err := target.CreateJob(models.CreateJobInput{CompanyName: "Previous", PositionTitle: "Analyst"})
	requireArchiveNoError(t, err)
	oldAttachment, err := target.StoreAttachment(prior.ID, nil, "old.txt", strings.NewReader("old"), 3, "text/plain")
	requireArchiveNoError(t, err)
	requireArchiveNoError(t, os.WriteFile(filepath.Join(targetCfg.LogosDir, "example.org.png"), []byte("old-logo"), 0600))
	requireArchiveNoError(t, target.ImportArchive(bytes.NewReader(archive), int64(len(archive)), false))
	if _, err := os.Stat(filepath.Join(targetCfg.AttachmentsDir, oldAttachment.StoredFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old attachment was not removed: %v", err)
	}
	imported, err := target.GetFullJobDetails(jobID)
	requireArchiveNoError(t, err)
	if imported == nil {
		t.Fatal("imported process is missing")
	}
	if len(imported.Attachments) != 1 || len(imported.Stages) != stages || len(imported.Stages[0].Questions) != 1 {
		t.Fatalf("imported process = %#v", imported)
	}
	verifyImportedFiles(t, targetCfg, imported, attachmentID)
	requireArchiveNoError(t, target.ImportArchive(bytes.NewReader(archive), int64(len(archive)), false))
}

func verifyImportedFiles(t *testing.T, cfg *config.Config, imported *models.Job, attachmentID string) {
	t.Helper()
	if imported.Attachments[0].ID != attachmentID {
		t.Fatalf("attachment ID changed: %q", imported.Attachments[0].ID)
	}
	content, err := os.ReadFile(filepath.Join(cfg.AttachmentsDir, imported.Attachments[0].StoredFilename))
	if err != nil || string(content) != "resume" {
		t.Fatalf("imported attachment = %q, %v", content, err)
	}
	logo, err := os.ReadFile(filepath.Join(cfg.LogosDir, "example.org.png"))
	if err != nil || string(logo) != "logo" {
		t.Fatalf("imported logo = %q, %v", logo, err)
	}
}

func TestEmptyArchiveRequiresConfirmation(t *testing.T) {
	_, jobs, _, _ := newCoverageApp(t)
	var output bytes.Buffer
	if err := jobs.ExportArchive(&output); err != nil {
		t.Fatal(err)
	}
	if err := jobs.ImportArchive(bytes.NewReader(output.Bytes()), int64(output.Len()), false); !errors.Is(err, service.ErrEmptyBackupRequiresConfirmation) {
		t.Fatalf("empty import error = %v", err)
	}
	if err := jobs.ImportArchive(bytes.NewReader(output.Bytes()), int64(output.Len()), true); err != nil {
		t.Fatalf("confirmed empty import: %v", err)
	}
}

func TestImportRejectsMalformedArchives(t *testing.T) {
	manifest := testManifest(t, []models.Job{})
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"invalid zip", []byte("not a zip")},
		{"no files", makeTestArchive(t)},
		{"missing manifest", makeTestArchive(t, testArchiveEntry{"other.txt", []byte("x")})},
		{"duplicate path", makeTestArchive(t, testArchiveEntry{"manifest.json", manifest}, testArchiveEntry{"manifest.json", manifest})},
		{"parent path", makeTestArchive(t, testArchiveEntry{"../manifest.json", manifest})},
		{"absolute path", makeTestArchive(t, testArchiveEntry{"/manifest.json", manifest})},
		{"backslash path", makeTestArchive(t, testArchiveEntry{"bad\\path", manifest})},
		{"invalid json", makeTestArchive(t, testArchiveEntry{"manifest.json", []byte("{")})},
		{"unsupported version", makeTestArchive(t, testArchiveEntry{"manifest.json", []byte(`{"format":"war-room-backup","version":99,"jobs":[]}`)})},
		{"no process list", makeTestArchive(t, testArchiveEntry{"manifest.json", []byte(`{"format":"war-room-backup","version":1}`)})},
		{"unreferenced file", makeTestArchive(t, testArchiveEntry{"manifest.json", manifest}, testArchiveEntry{"extra", []byte("x")})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, jobs, _, _ := newCoverageApp(t)
			if err := jobs.ImportArchive(bytes.NewReader(test.data), int64(len(test.data)), true); err == nil {
				t.Fatal("expected malformed archive to fail")
			}
		})
	}
}

func TestImportAcceptsLegacyVersionOneBackup(t *testing.T) {
	_, jobs, _, _ := newCoverageApp(t)
	archive := makeTestArchive(t, testArchiveEntry{"manifest.json", []byte(`{"format":"war-room-backup","version":1,"jobs":[],"attachment_sha256":{}}`)})
	if err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true); err != nil {
		t.Fatalf("import legacy v1 archive: %v", err)
	}
}

func TestImportRejectsInvalidMeetingDates(t *testing.T) {
	_, jobs, _, _ := newCoverageApp(t)
	manifest := []byte(`{"format":"war-room-backup","version":1,"jobs":[{"stages":[{"meeting_date":"2031-02-30"}]}]}`)
	archive := makeTestArchive(t, testArchiveEntry{"manifest.json", manifest})
	err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true)
	if !errors.Is(err, service.ErrInvalidMeetingDate) {
		t.Fatalf("invalid meeting date import error = %v", err)
	}
}

func TestImportRejectsMissingAttachmentAndInvalidLogo(t *testing.T) {
	_, jobs, _, _ := newCoverageApp(t)
	job := models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		Attachments: []models.Attachment{{ID: "attachment-1", StoredFilename: "attachments/attachment-1/resume.txt", OriginalName: "resume.txt", FileSize: 6}}}
	data := makeTestArchive(t, testArchiveEntry{"manifest.json", testManifest(t, []models.Job{job})})
	if err := jobs.ImportArchive(bytes.NewReader(data), int64(len(data)), true); err == nil {
		t.Fatal("expected missing attachment to fail")
	}
	logoManifest := map[string]any{"format": "war-room-backup", "version": 1, "jobs": []models.Job{}, "attachment_sha256": map[string]string{},
		"logos": []map[string]any{{"domain": "../unsafe", "path": "logos/../unsafe.png", "mime_type": "image/png", "size": 4, "sha256": "bad"}}}
	encoded, err := json.Marshal(logoManifest)
	if err != nil {
		t.Fatal(err)
	}
	data = makeTestArchive(t, testArchiveEntry{"manifest.json", encoded})
	if err := jobs.ImportArchive(bytes.NewReader(data), int64(len(data)), true); err == nil {
		t.Fatal("expected unsafe logo to fail")
	}
}

func TestExportRejectsMissingFiles(t *testing.T) {
	_, jobs, _, cfg := newCoverageApp(t)
	job, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer"})
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := jobs.StoreAttachment(job.ID, nil, "resume.txt", strings.NewReader("resume"), 6, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(cfg.AttachmentsDir, attachment.StoredFilename)); err != nil {
		t.Fatal(err)
	}
	if err := jobs.ExportArchive(io.Discard); err == nil {
		t.Fatal("expected missing attachment to fail export")
	}
}

func TestImportRejectsInvalidRecords(t *testing.T) {
	base := models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing}
	tests := []struct {
		name string
		jobs []models.Job
	}{
		{"missing ID", []models.Job{{CompanyName: "Example", PositionTitle: "Engineer"}}},
		{"duplicate job ID", []models.Job{base, base}},
		{"duplicate stage ID", []models.Job{{ID: "job-1", Stages: []models.Stage{{ID: "stage-1"}, {ID: "stage-1"}}}}},
		{"duplicate question ID", []models.Job{{ID: "job-1", Stages: []models.Stage{{ID: "stage-1", Questions: []models.Question{{ID: "question-1"}, {ID: "question-1"}}}}}}},
		{"duplicate attachment ID", []models.Job{{ID: "job-1", Attachments: []models.Attachment{{ID: "attachment-1"}, {ID: "attachment-1"}}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, jobs, _, _ := newCoverageApp(t)
			archive := makeTestArchive(t, testArchiveEntry{"manifest.json", testManifest(t, test.jobs)})
			if err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true); err == nil {
				t.Fatal("expected invalid record to fail")
			}
		})
	}
}

func TestImportRejectsBadAttachmentMetadata(t *testing.T) {
	attachment := models.Attachment{ID: "attachment-1", OriginalName: "resume.txt", StoredFilename: "attachments/attachment-1/resume.txt", FileSize: 6}
	job := models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing, Attachments: []models.Attachment{attachment}}
	fileData := []byte("resume")
	hash := sha256.Sum256(fileData)
	checksums := map[string]string{"attachment-1": hex.EncodeToString(hash[:])}
	tests := []struct {
		name        string
		edit        func(*models.Job, map[string]string)
		includeFile bool
	}{
		{"missing checksum", func(_ *models.Job, hashes map[string]string) { delete(hashes, "attachment-1") }, true},
		{"wrong checksum", func(_ *models.Job, hashes map[string]string) { hashes["attachment-1"] = "bad" }, true},
		{"wrong size", func(job *models.Job, _ map[string]string) { job.Attachments[0].FileSize = 7 }, true},
		{"unsafe path", func(job *models.Job, _ map[string]string) { job.Attachments[0].StoredFilename = "../resume.txt" }, true},
		{"missing file", func(_ *models.Job, _ map[string]string) {}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, jobs, _, _ := newCoverageApp(t)
			copyJob := job
			copyJob.Attachments = append([]models.Attachment(nil), job.Attachments...)
			hashes := map[string]string{"attachment-1": checksums["attachment-1"]}
			test.edit(&copyJob, hashes)
			manifest := map[string]any{"format": "war-room-backup", "version": 1, "jobs": []models.Job{copyJob}, "attachment_sha256": hashes}
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			entries := []testArchiveEntry{{"manifest.json", encoded}}
			if test.includeFile {
				entries = append(entries, testArchiveEntry{attachment.StoredFilename, fileData})
			}
			archive := makeTestArchive(t, entries...)
			if err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true); err == nil {
				t.Fatal("expected invalid attachment to fail")
			}
		})
	}
}

func TestImportRollsBackLogoWhenDatabaseFails(t *testing.T) {
	_, source, _, cfg := newCoverageApp(t)
	if _, err := source.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer", CompanyDomain: ptr("example.org")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.LogosDir, "example.org.png"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	var exported bytes.Buffer
	if err := source.ExportArchive(&exported); err != nil {
		t.Fatal(err)
	}
	_, target, _, targetCfg, db := newCoverageDB(t)
	logoPath := filepath.Join(targetCfg.LogosDir, "example.org.png")
	if err := os.WriteFile(logoPath, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE jobs"); err != nil {
		t.Fatal(err)
	}
	if err := target.ImportArchive(bytes.NewReader(exported.Bytes()), int64(exported.Len()), true); err == nil {
		t.Fatal("expected database import to fail")
	}
	// #nosec G304 -- logoPath is inside this test's temporary directory.
	logo, err := os.ReadFile(logoPath)
	if err != nil || string(logo) != "old" {
		t.Fatalf("logo rollback = %q, %v", logo, err)
	}
}

func TestExportRejectsInvalidCachedLogo(t *testing.T) {
	_, jobs, _, cfg := newCoverageApp(t)
	if _, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer", CompanyDomain: ptr("example.org")}); err != nil {
		t.Fatal(err)
	}
	logoPath := filepath.Join(cfg.LogosDir, "example.org.png")
	if err := os.Mkdir(logoPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := jobs.ExportArchive(io.Discard); err == nil {
		t.Fatal("expected directory logo to fail export")
	}
	if err := os.Remove(logoPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logoPath, bytes.Repeat([]byte("x"), 5<<20+1), 0600); err != nil {
		t.Fatal(err)
	}
	if err := jobs.ExportArchive(io.Discard); err == nil {
		t.Fatal("expected oversized logo to fail export")
	}
}

func sendBackupImport(t *testing.T, mux *http.ServeMux, archive []byte, allowEmpty bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("backup", "backup.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(archive); err != nil {
		t.Fatal(err)
	}
	if allowEmpty {
		if err := form.WriteField("allow_empty", "true"); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func TestBackupHandlers(t *testing.T) {
	mux, jobs, _, _ := newCoverageApp(t)
	if _, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer"}); err != nil {
		t.Fatal(err)
	}
	exportRequest := httptest.NewRequest(http.MethodGet, "/api/backup/export", nil)
	exportResponse := httptest.NewRecorder()
	mux.ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || exportResponse.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export status = %d", exportResponse.Code)
	}
	if response := sendBackupImport(t, mux, exportResponse.Body.Bytes(), false); response.Code != http.StatusOK {
		t.Fatalf("import status = %d: %s", response.Code, response.Body.String())
	}
	if response := sendBackupImport(t, mux, []byte("invalid"), false); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid ZIP status = %d", response.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", strings.NewReader("not multipart"))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid form status = %d", response.Code)
	}
	empty := makeTestArchive(t, testArchiveEntry{"manifest.json", testManifest(t, []models.Job{})})
	if response := sendBackupImport(t, mux, empty, false); response.Code != http.StatusConflict {
		t.Fatalf("unconfirmed empty import = %d", response.Code)
	}
	if response := sendBackupImport(t, mux, empty, true); response.Code != http.StatusOK {
		t.Fatalf("confirmed empty import = %d", response.Code)
	}
	missingFile := httptest.NewRequest(http.MethodPost, "/api/backup/import", strings.NewReader(""))
	missingFile.Header.Set("Content-Type", "multipart/form-data; boundary=empty")
	missingResponse := httptest.NewRecorder()
	mux.ServeHTTP(missingResponse, missingFile)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing file status = %d", missingResponse.Code)
	}
}

func TestImportRejectsDatabaseConstraints(t *testing.T) {
	base := models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing}
	tests := []struct {
		name string
		edit func(*models.Job)
	}{
		{"invalid status", func(job *models.Job) { job.Status = "invalid" }},
		{"invalid stage type", func(job *models.Job) {
			job.Stages = []models.Stage{{ID: "stage-1", StageType: "invalid", Status: models.StageStatusPending}}
		}},
		{"invalid attachment stage", func(job *models.Job) {
			job.Attachments = []models.Attachment{{ID: "attachment-1", OriginalName: "resume.txt", StoredFilename: "attachments/attachment-1/resume.txt", FileSize: 6, StageID: ptr("missing-stage")}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, jobs, _, _ := newCoverageApp(t)
			job := base
			test.edit(&job)
			manifest := map[string]any{"format": "war-room-backup", "version": 1, "jobs": []models.Job{job}, "attachment_sha256": map[string]string{}}
			entries := []testArchiveEntry{}
			if len(job.Attachments) > 0 {
				data := []byte("resume")
				hash := sha256.Sum256(data)
				manifest["attachment_sha256"] = map[string]string{"attachment-1": hex.EncodeToString(hash[:])}
				entries = append(entries, testArchiveEntry{"attachments/attachment-1/resume.txt", data})
			}
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			entries = append([]testArchiveEntry{{"manifest.json", encoded}}, entries...)
			archive := makeTestArchive(t, entries...)
			if err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true); err == nil {
				t.Fatal("expected constraint failure")
			}
		})
	}
}

func TestImportRejectsInvalidLogoContent(t *testing.T) {
	tests := []struct {
		name    string
		file    []byte
		size    int64
		hash    string
		include bool
	}{
		{"missing logo", []byte("logo"), 4, "bad", false},
		{"wrong size", []byte("logo"), 5, "bad", true},
		{"wrong checksum", []byte("logo"), 4, "bad", true},
		{"zero size", []byte("logo"), 0, "bad", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, jobs, _, _ := newCoverageApp(t)
			manifest := map[string]any{"format": "war-room-backup", "version": 1, "jobs": []models.Job{}, "attachment_sha256": map[string]string{},
				"logos": []map[string]any{{"domain": "example.org", "path": "logos/example.org.png", "mime_type": "image/png", "size": test.size, "sha256": test.hash}}}
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			entries := []testArchiveEntry{{"manifest.json", encoded}}
			if test.include {
				entries = append(entries, testArchiveEntry{"logos/example.org.png", test.file})
			}
			archive := makeTestArchive(t, entries...)
			if err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true); err == nil {
				t.Fatal("expected invalid logo content to fail")
			}
		})
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestExportRejectsWriterAndSizeFailures(t *testing.T) {
	_, jobs, repo, _ := newCoverageApp(t)
	if err := jobs.ExportArchive(errorWriter{}); err == nil {
		t.Fatal("expected writer error")
	}
	job, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer"})
	if err != nil {
		t.Fatal(err)
	}
	attachment := &models.Attachment{ID: "huge-attachment", JobID: job.ID, OriginalName: "huge.txt", StoredFilename: "huge.txt", FileSize: 51 << 20, MimeType: "text/plain"}
	if err := repo.InsertAttachment(attachment); err != nil {
		t.Fatal(err)
	}
	if err := jobs.ExportArchive(io.Discard); err == nil {
		t.Fatal("expected oversized attachment to fail")
	}
	if err := repo.DeleteAttachment(attachment.ID); err != nil {
		t.Fatal(err)
	}
	if err := jobs.ExportArchive(errorWriter{}); err == nil {
		t.Fatal("expected archive write error")
	}
}

func TestBackupExportHandlerReportsMissingFile(t *testing.T) {
	mux, jobs, _, cfg := newCoverageApp(t)
	job, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer"})
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := jobs.StoreAttachment(job.ID, nil, "resume.txt", strings.NewReader("resume"), 6, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(cfg.AttachmentsDir, attachment.StoredFilename)); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/backup/export", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("missing file export status = %d", response.Code)
	}
}

func TestRepositoryReplaceRejectsInvalidRecords(t *testing.T) {
	_, _, repo, _ := newCoverageApp(t)
	base := models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing}
	if err := repo.ReplaceAllJobs([]models.Job{base}); err != nil {
		t.Fatal(err)
	}
	bad := base
	bad.Status = "invalid"
	if err := repo.ReplaceAllJobs([]models.Job{bad}); err == nil {
		t.Fatal("expected invalid status to fail")
	}
	good, err := repo.GetJobByID(base.ID)
	if err != nil || good == nil {
		t.Fatal("failed replacement discarded existing data")
	}
}

func TestImportRejectsMetadataInconsistencies(t *testing.T) {
	attachment := models.Attachment{ID: "attachment-1", OriginalName: "resume.txt", StoredFilename: "attachments/attachment-1/resume.txt", FileSize: 6}
	job := models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing, Attachments: []models.Attachment{attachment}}
	content := []byte("resume")
	hash := sha256.Sum256(content)
	goodHash := hex.EncodeToString(hash[:])
	tests := []struct {
		name     string
		manifest map[string]any
		files    []testArchiveEntry
	}{
		{"extra checksum", map[string]any{"jobs": []models.Job{}, "attachment_sha256": map[string]string{"extra": goodHash}}, nil},
		{"extra attachment", map[string]any{"jobs": []models.Job{}, "attachment_sha256": map[string]string{}}, []testArchiveEntry{{attachment.StoredFilename, content}}},
		{"extra logo", map[string]any{"jobs": []models.Job{}, "attachment_sha256": map[string]string{}}, []testArchiveEntry{{"logos/example.org.png", content}}},
		{"missing logo metadata", map[string]any{"jobs": []models.Job{}, "attachment_sha256": map[string]string{}, "logos": []map[string]any{{"domain": "example.org", "path": "logos/example.org.png", "mime_type": "image/png", "size": 6, "sha256": goodHash}}}, nil},
		{"extra checksum for valid file", map[string]any{"jobs": []models.Job{job}, "attachment_sha256": map[string]string{"attachment-1": goodHash, "extra": goodHash}}, []testArchiveEntry{{attachment.StoredFilename, content}}},
		{"invalid logo media type", map[string]any{"jobs": []models.Job{}, "attachment_sha256": map[string]string{}, "logos": []map[string]any{{"domain": "example.org", "path": "logos/example.org.png", "mime_type": "text/plain", "size": 6, "sha256": goodHash}}}, []testArchiveEntry{{"logos/example.org.png", content}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, jobs, _, _ := newCoverageApp(t)
			test.manifest["format"] = "war-room-backup"
			test.manifest["version"] = 1
			encoded, err := json.Marshal(test.manifest)
			if err != nil {
				t.Fatal(err)
			}
			entries := append([]testArchiveEntry{{"manifest.json", encoded}}, test.files...)
			archive := makeTestArchive(t, entries...)
			if err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true); err == nil {
				t.Fatal("expected inconsistent metadata to fail")
			}
		})
	}
}

func TestImportRejectsSymlinkEntry(t *testing.T) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	header := &zip.FileHeader{Name: "manifest.json"}
	header.SetMode(os.ModeSymlink | 0777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(testManifest(t, []models.Job{})); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	_, jobs, _, _ := newCoverageApp(t)
	if err := jobs.ImportArchive(bytes.NewReader(output.Bytes()), int64(output.Len()), true); err == nil {
		t.Fatal("expected symlink entry to fail")
	}
}

func TestRepositoryImportRejectsBrokenSchema(t *testing.T) {
	for _, table := range []string{"jobs", "stages", "stage_questions", "attachments"} {
		t.Run(table, func(t *testing.T) {
			_, _, repo, _, db := newCoverageDB(t)
			queries := map[string]string{
				"jobs": "DROP TABLE jobs", "stages": "DROP TABLE stages",
				"stage_questions": "DROP TABLE stage_questions", "attachments": "DROP TABLE attachments",
			}
			if _, err := db.Exec(queries[table]); err != nil {
				t.Fatal(err)
			}
			if err := repo.ReplaceAllJobs(nil); err == nil {
				t.Fatal("expected invalid schema to reject import")
			}
		})
	}
}

func TestRepositoryImportRejectsInvalidNestedRows(t *testing.T) {
	base := models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing}
	tests := []struct {
		name string
		edit func(*models.Job)
	}{
		{"invalid stage", func(job *models.Job) {
			job.Stages = []models.Stage{{ID: "stage-1", StageType: "invalid", Status: models.StageStatusPending}}
		}},
		{"duplicate question", func(job *models.Job) {
			job.Stages = []models.Stage{{ID: "stage-1", StageType: models.StageHR, Status: models.StageStatusPending,
				Questions: []models.Question{{ID: "question-1", Question: "First"}, {ID: "question-1", Question: "Second"}}}}
		}},
		{"missing attachment stage", func(job *models.Job) {
			job.Attachments = []models.Attachment{{ID: "attachment-1", OriginalName: "resume.txt", StoredFilename: "saved.txt", FileSize: 6, StageID: ptr("missing")}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, repo, _ := newCoverageApp(t)
			job := base
			test.edit(&job)
			if err := repo.ReplaceAllJobs([]models.Job{job}); err == nil {
				t.Fatal("expected invalid nested row to fail")
			}
		})
	}
}

func TestRepositoryImportRejectsClosedDatabase(t *testing.T) {
	_, _, _, _, db := newCoverageDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	if err := repo.ReplaceAllJobs(nil); err == nil {
		t.Fatal("expected closed database to fail")
	}
}

func TestArchiveRejectsUnsafeStoredFilesAndExpandedLimit(t *testing.T) {
	for _, storedName := range []string{"../escape.txt", ".", ""} {
		t.Run(storedName, func(t *testing.T) {
			_, jobs, repo, _ := newCoverageApp(t)
			job, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer"})
			if err != nil {
				t.Fatal(err)
			}
			attachment := &models.Attachment{ID: "attachment-1", JobID: job.ID, OriginalName: "resume.txt", StoredFilename: storedName, FileSize: 1}
			if err := repo.InsertAttachment(attachment); err != nil {
				t.Fatal(err)
			}
			if err := jobs.ExportArchive(io.Discard); err == nil {
				t.Fatal("expected unsafe stored filename to fail")
			}
		})
	}
	_, jobs, repo, _ := newCoverageApp(t)
	job, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer"})
	if err != nil {
		t.Fatal(err)
	}
	for index := range 21 {
		attachment := &models.Attachment{ID: fmt.Sprintf("attachment-%d", index), JobID: job.ID,
			OriginalName: "large.txt", StoredFilename: fmt.Sprintf("large-%d.txt", index), FileSize: 50 << 20}
		if err := repo.InsertAttachment(attachment); err != nil {
			t.Fatal(err)
		}
	}
	if err := jobs.ExportArchive(io.Discard); err == nil {
		t.Fatal("expected expanded size limit to fail")
	}
}

func TestImportRejectsInvalidDeclaredSizeAndPaths(t *testing.T) {
	_, jobs, _, _ := newCoverageApp(t)
	for _, size := range []int64{-1, 0, 500<<20 + 1} {
		if err := jobs.ImportArchive(bytes.NewReader(nil), size, true); err == nil {
			t.Fatalf("expected invalid size %d to fail", size)
		}
	}
	for _, name := range []string{"./manifest.json", "a/../manifest.json", "a//b", "..", "../a", "/a", "a\\b"} {
		t.Run(name, func(t *testing.T) {
			archive := makeTestArchive(t, testArchiveEntry{"manifest.json", testManifest(t, []models.Job{})}, testArchiveEntry{name, []byte("x")})
			if err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true); err == nil {
				t.Fatal("expected unsafe archive path to fail")
			}
		})
	}
}

func TestArchiveReportsStorageFailures(t *testing.T) {
	empty := makeTestArchive(t, testArchiveEntry{"manifest.json", testManifest(t, []models.Job{})})
	t.Run("export snapshot", func(t *testing.T) {
		_, jobs, _, _, db := newCoverageDB(t)
		if _, err := db.Exec("DROP TABLE jobs"); err != nil {
			t.Fatal(err)
		}
		if err := jobs.ExportArchive(io.Discard); err == nil {
			t.Fatal("expected unreadable database to fail export")
		}
	})
	t.Run("existing attachments", func(t *testing.T) {
		_, jobs, _, _, db := newCoverageDB(t)
		if _, err := db.Exec("DROP TABLE attachments"); err != nil {
			t.Fatal(err)
		}
		if err := jobs.ImportArchive(bytes.NewReader(empty), int64(len(empty)), true); err == nil {
			t.Fatal("expected unreadable attachments to fail import")
		}
	})
	t.Run("attachment staging", func(t *testing.T) {
		_, jobs, _, cfg := newCoverageApp(t)
		if err := os.Remove(cfg.AttachmentsDir); err != nil {
			t.Fatal(err)
		}
		if err := jobs.ImportArchive(bytes.NewReader(empty), int64(len(empty)), true); err == nil {
			t.Fatal("expected unavailable attachment directory to fail import")
		}
	})
	t.Run("logo staging", func(t *testing.T) {
		_, jobs, _, cfg := newCoverageApp(t)
		if err := os.Remove(cfg.LogosDir); err != nil {
			t.Fatal(err)
		}
		if err := jobs.ImportArchive(bytes.NewReader(empty), int64(len(empty)), true); err == nil {
			t.Fatal("expected unavailable logo directory to fail import")
		}
	})
	t.Run("recovery snapshot", func(t *testing.T) {
		_, jobs, _, cfg := newCoverageApp(t)
		cfg.BackupPath = cfg.DataDir
		if err := jobs.ImportArchive(bytes.NewReader(empty), int64(len(empty)), true); err != nil {
			t.Fatalf("database import should survive snapshot failure: %v", err)
		}
	})
}

func archiveWithUnsupportedCompression(t *testing.T, entries ...testArchiveEntry) []byte {
	t.Helper()
	archive := makeTestArchive(t, entries...)
	for _, header := range []struct {
		signature []byte
		offset    int
	}{
		{[]byte{'P', 'K', 3, 4}, 8},
		{[]byte{'P', 'K', 1, 2}, 10},
	} {
		position := bytes.Index(archive, header.signature)
		if position < 0 {
			t.Fatal("missing ZIP header")
		}
		binary.LittleEndian.PutUint16(archive[position+header.offset:], 99)
	}
	return archive
}

func TestImportRejectsUnsupportedCompression(t *testing.T) {
	_, jobs, _, _ := newCoverageApp(t)
	manifest := testManifest(t, []models.Job{})
	archive := archiveWithUnsupportedCompression(t, testArchiveEntry{"manifest.json", manifest})
	if err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true); err == nil {
		t.Fatal("expected unreadable manifest to fail")
	}
	content := []byte("resume")
	hash := sha256.Sum256(content)
	job := models.Job{ID: "job-1", CompanyName: "Example", PositionTitle: "Engineer", Status: models.StatusOngoing,
		Attachments: []models.Attachment{{ID: "attachment-1", OriginalName: "resume.txt", StoredFilename: "attachments/attachment-1/resume.txt", FileSize: 6}}}
	metadata := map[string]any{"format": "war-room-backup", "version": 1, "jobs": []models.Job{job},
		"attachment_sha256": map[string]string{"attachment-1": hex.EncodeToString(hash[:])}}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	archive = makeTestArchive(t, testArchiveEntry{"manifest.json", encoded}, testArchiveEntry{"attachments/attachment-1/resume.txt", content})
	position := bytes.LastIndex(archive, []byte{'P', 'K', 1, 2})
	if position < 0 {
		t.Fatal("missing attachment header")
	}
	binary.LittleEndian.PutUint16(archive[position+10:], 99)
	if err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true); err == nil {
		t.Fatal("expected unreadable attachment to fail")
	}
}

func TestImportRollsBackNewLogoOnDatabaseFailure(t *testing.T) {
	_, source, _, cfg := newCoverageApp(t)
	if _, err := source.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer", CompanyDomain: ptr("example.org")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.LogosDir, "example.org.png"), []byte("logo"), 0600); err != nil {
		t.Fatal(err)
	}
	var exported bytes.Buffer
	if err := source.ExportArchive(&exported); err != nil {
		t.Fatal(err)
	}
	_, target, _, targetCfg, db := newCoverageDB(t)
	if _, err := db.Exec("DROP TABLE jobs"); err != nil {
		t.Fatal(err)
	}
	if err := target.ImportArchive(bytes.NewReader(exported.Bytes()), int64(exported.Len()), true); err == nil {
		t.Fatal("expected database failure")
	}
	if _, err := os.Stat(filepath.Join(targetCfg.LogosDir, "example.org.png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new logo survived rollback: %v", err)
	}
}

func TestImportSkipsUnsafeOldAttachmentFilename(t *testing.T) {
	_, jobs, repo, _ := newCoverageApp(t)
	job, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Old", PositionTitle: "Engineer"})
	if err != nil {
		t.Fatal(err)
	}
	attachment := &models.Attachment{ID: "old-attachment", JobID: job.ID, OriginalName: "old.txt", StoredFilename: "../outside.txt", FileSize: 0}
	if err := repo.InsertAttachment(attachment); err != nil {
		t.Fatal(err)
	}
	empty := makeTestArchive(t, testArchiveEntry{"manifest.json", testManifest(t, []models.Job{})})
	if err := jobs.ImportArchive(bytes.NewReader(empty), int64(len(empty)), true); err != nil {
		t.Fatalf("import with legacy unsafe filename: %v", err)
	}
}

func TestExportRejectsInvalidAttachmentIdentifier(t *testing.T) {
	_, jobs, repo, cfg := newCoverageApp(t)
	job, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.AttachmentsDir, "saved.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	attachment := &models.Attachment{ID: "bad/id", JobID: job.ID, OriginalName: "resume.txt", StoredFilename: "saved.txt", FileSize: 1}
	if err := repo.InsertAttachment(attachment); err != nil {
		t.Fatal(err)
	}
	if err := jobs.ExportArchive(io.Discard); err == nil {
		t.Fatal("expected invalid attachment ID to fail")
	}
}

func TestImportRejectsOversizedManifest(t *testing.T) {
	manifest := bytes.Repeat([]byte(" "), 100<<20+1)
	archive := makeTestArchive(t, testArchiveEntry{"manifest.json", manifest})
	_, jobs, _, _ := newCoverageApp(t)
	if err := jobs.ImportArchive(bytes.NewReader(archive), int64(len(archive)), true); err == nil {
		t.Fatal("expected oversized manifest to fail")
	}
}

func TestExportNormalizesEmptyAttachmentName(t *testing.T) {
	_, jobs, repo, cfg := newCoverageApp(t)
	job, err := jobs.CreateJob(models.CreateJobInput{CompanyName: "Example", PositionTitle: "Engineer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.AttachmentsDir, "saved.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	attachment := &models.Attachment{ID: "attachment-1", JobID: job.ID, OriginalName: "", StoredFilename: "saved.txt", FileSize: 1}
	if err := repo.InsertAttachment(attachment); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := jobs.ExportArchive(&output); err != nil {
		t.Fatal(err)
	}
}

func TestExportSkipsCompanyWithoutDomain(t *testing.T) {
	_, jobs, repo, _ := newCoverageApp(t)
	job := &models.Job{ID: "unknown-company", CompanyName: "", PositionTitle: "Engineer", Status: models.StatusOngoing,
		SalaryType: models.SalaryUnknown, SalaryCurrency: "EUR", RecruiterType: models.RecruiterNone, AvatarSeed: "unknown"}
	if err := repo.InsertJob(job); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := jobs.ExportArchive(&output); err != nil {
		t.Fatal(err)
	}
}
