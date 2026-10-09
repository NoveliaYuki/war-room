package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/models"
)

func TestValidateImportedSearchPeriods(t *testing.T) {
	validPeriod := models.SearchPeriod{ID: "march", Name: "March", StartDate: "2026-03-01", EndDate: "2026-03-31"}
	validJob := models.Job{ID: "job-1", SearchPeriodID: stringPointer("march")}
	for _, test := range []struct {
		name    string
		periods []models.SearchPeriod
		jobs    []models.Job
		valid   bool
	}{
		{name: "valid reference", periods: []models.SearchPeriod{validPeriod}, jobs: []models.Job{validJob}, valid: true},
		{name: "unassigned job", jobs: []models.Job{{ID: "job-2"}}, valid: true},
		{name: "missing ID", periods: []models.SearchPeriod{{Name: "March", StartDate: "2026-03-01", EndDate: "2026-03-31"}}},
		{name: "duplicate ID", periods: []models.SearchPeriod{validPeriod, validPeriod}},
		{name: "invalid dates", periods: []models.SearchPeriod{{ID: "bad", Name: "Bad", StartDate: "2026-02-30", EndDate: "2026-03-01"}}},
		{name: "overlap", periods: []models.SearchPeriod{validPeriod, {ID: "april", Name: "April", StartDate: "2026-03-31", EndDate: "2026-04-30"}}},
		{name: "missing job reference", periods: []models.SearchPeriod{validPeriod}, jobs: []models.Job{{ID: "job-3", SearchPeriodID: stringPointer("unknown")}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateImportedSearchPeriods(test.periods, test.jobs)
			if (err == nil) != test.valid {
				t.Fatalf("validation error=%v, valid=%v", err, test.valid)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func TestValidateExportArchiveSizeLimits(t *testing.T) {
	valid := []models.Job{{Attachments: []models.Attachment{{FileSize: 12}}}}
	if err := validateExportArchiveSize(valid, nil, []backupLogo{{Size: 8}}, t.TempDir()); err != nil {
		t.Fatalf("valid archive size: %v", err)
	}
	for name, job := range map[string]models.Job{
		"negative size":  {Attachments: []models.Attachment{{FileSize: -1}}},
		"oversized file": {Attachments: []models.Attachment{{FileSize: maxAttachmentBytes + 1}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateExportArchiveSize([]models.Job{job}, nil, nil, t.TempDir()); err == nil {
				t.Fatal("invalid attachment size should fail")
			}
		})
	}
	if err := validateExportArchiveSize(nil, nil, []backupLogo{{Size: maxLogoBytes + 1}}, t.TempDir()); err == nil {
		t.Fatal("oversized logo should fail")
	}
	entries := maxArchiveEntries
	var expanded uint64
	if err := addExportSize(&entries, &expanded, 0, maxAttachmentBytes); err == nil {
		t.Fatal("archive entry limit should fail")
	}
	expanded = maxExpandedBytes - maxManifestBytes
	entries = 0
	if err := addExportSize(&entries, &expanded, 1, maxAttachmentBytes); err == nil {
		t.Fatal("expanded archive limit should fail")
	}
}

func TestValidateExportArchiveIncludesUniqueCVBlobs(t *testing.T) {
	directory := t.TempDir()
	content := []byte("cv content")
	digestBytes := sha256.Sum256(content)
	digest := hex.EncodeToString(digestBytes[:])
	if err := os.WriteFile(filepath.Join(directory, digest), content, 0600); err != nil {
		t.Fatal(err)
	}
	versions := []models.CVVersion{{Version: 1, StoredFilename: digest, FileSize: int64(len(content)), SHA256: digest},
		{Version: 2, StoredFilename: digest, FileSize: int64(len(content)), SHA256: digest}}
	if err := validateExportArchiveSize(nil, versions, nil, directory); err != nil {
		t.Fatalf("valid shared blob: %v", err)
	}
	assertCVExportRejected(t, directory, models.CVVersion{Version: 1, StoredFilename: "../escape", FileSize: int64(len(content)), SHA256: digest})
	assertCVExportRejected(t, directory, models.CVVersion{Version: 1, StoredFilename: digest, FileSize: int64(len(content)), SHA256: strings.Repeat("z", 64)})
	assertCVExportRejected(t, directory, models.CVVersion{Version: 3, StoredFilename: strings.Repeat("a", 64), FileSize: 2, SHA256: strings.Repeat("a", 64)})
	conflicting := append(versions, models.CVVersion{Version: 3, StoredFilename: digest, FileSize: 1, SHA256: digest})
	if err := validateExportArchiveSize(nil, conflicting, nil, directory); err == nil {
		t.Fatal("conflicting metadata for shared blob should fail")
	}
}

func assertCVExportRejected(t *testing.T, directory string, version models.CVVersion) {
	t.Helper()
	if err := validateExportArchiveSize(nil, []models.CVVersion{version}, nil, directory); err == nil {
		t.Fatalf("CV version should be rejected: %+v", version)
	}
}

func TestCollectCachedLogosDeduplicatesAndValidatesFiles(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "example.test.png"), []byte("logo"), 0600); err != nil {
		t.Fatal(err)
	}
	domain := "example.test"
	logos, err := collectCachedLogos([]models.Job{
		{CompanyName: "Example", CompanyDomain: &domain},
		{CompanyName: "Example", CompanyDomain: &domain},
		{CompanyName: "No Logo"},
	}, directory)
	if err != nil || len(logos) != 1 || logos[0].MimeType != "image/png" || logos[0].Size != 4 {
		t.Fatalf("logos=%#v err=%v", logos, err)
	}
	if err := os.WriteFile(filepath.Join(directory, "large.test.png"), make([]byte, maxLogoBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := findCachedLogo("large.test", directory); err == nil {
		t.Fatal("oversized cached logo should fail")
	}
	if err := os.Mkdir(filepath.Join(directory, "folder.test.png"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := findCachedLogo("folder.test", directory); err == nil {
		t.Fatal("directory should not be accepted as a logo")
	}
}

func TestArchiveImportStageCleanupAndCommit(t *testing.T) {
	root := t.TempDir()
	attachments := filepath.Join(root, "attachments")
	cvs := filepath.Join(root, "cvs")
	staging := filepath.Join(attachments, ".import-test")
	if err := os.MkdirAll(staging, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(attachments, "new-attachment"), filepath.Join(cvs, "new-cv")} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stage := &archiveImportStage{attachmentsDir: attachments, cvDir: cvs, stagingDir: staging,
		createdFiles: []string{"new-attachment"}, createdCVFiles: []string{"new-cv"}}
	stage.cleanup()
	for _, path := range []string{staging, filepath.Join(attachments, "new-attachment"), filepath.Join(cvs, "new-cv")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("uncommitted import path %q remains: %v", path, err)
		}
	}

	committed := filepath.Join(attachments, "committed")
	if err := os.WriteFile(committed, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	stage = &archiveImportStage{attachmentsDir: attachments, stagingDir: filepath.Join(attachments, ".import-committed"),
		createdFiles: []string{"committed"}, committed: true}
	if err := os.Mkdir(stage.stagingDir, 0700); err != nil {
		t.Fatal(err)
	}
	stage.cleanup()
	if _, err := os.Stat(committed); err != nil {
		t.Fatalf("committed import file was removed: %v", err)
	}
}

func TestRegisterJobRecordIDsDetectsNestedDuplicates(t *testing.T) {
	job := &models.Job{ID: "job", Stages: []models.Stage{{ID: "stage", Questions: []models.Question{{ID: "question"}}}}}
	if err := registerJobRecordIDs(map[string]bool{}, job); err != nil {
		t.Fatalf("unique IDs rejected: %v", err)
	}
	for _, duplicate := range []string{"job", "stage", "question"} {
		if err := registerJobRecordIDs(map[string]bool{duplicate: true}, job); err == nil {
			t.Errorf("duplicate ID %q was accepted", duplicate)
		}
	}
}

func TestRemoveOldCVBlobsOnlyRemovesUnreferencedSafeFiles(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	version := storeCVTestVersion(t, fixture.service, "resume.pdf", "old CV")
	blobPath := filepath.Join(fixture.cfg.CVDir, version.StoredFilename)
	removeOldCVBlobs(fixture.repo, fixture.cfg.CVDir, []models.CVVersion{*version})
	if _, err := os.Stat(blobPath); err != nil {
		t.Fatalf("referenced blob removed: %v", err)
	}
	if err := fixture.repo.DeleteCVVersion(version.ID); err != nil {
		t.Fatal(err)
	}
	removeOldCVBlobs(fixture.repo, fixture.cfg.CVDir, []models.CVVersion{*version})
	if _, err := os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatalf("unreferenced blob remains: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	unsafe := *version
	unsafe.StoredFilename = "../" + filepath.Base(outside)
	removeOldCVBlobs(fixture.repo, fixture.cfg.CVDir, []models.CVVersion{unsafe})
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("unsafe cleanup removed unrelated file: %v", err)
	}
}

func TestArchiveExportHelpersValidateSourcesAndWriteChecksums(t *testing.T) {
	directory := t.TempDir()
	attachments := t.TempDir()
	logoBytes, attachmentBytes := []byte("logo-data"), []byte("resume-data")
	if err := os.WriteFile(filepath.Join(directory, "example.test.png"), logoBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attachments, "stored.pdf"), attachmentBytes, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := backupManifest{
		Format: backupFormat, Version: backupFormatVersion,
		Jobs:             []models.Job{{Attachments: []models.Attachment{{ID: "attachment-1", OriginalName: "resume.pdf", StoredFilename: "stored.pdf", FileSize: int64(len(attachmentBytes))}}}},
		AttachmentSHA256: make(map[string]string),
		Logos:            []backupLogo{{Domain: "example.test", Path: "logos/example.test.png", MimeType: "image/png", Size: int64(len(logoBytes))}},
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	writeTestArchive(t, archive, &manifest, attachments, directory)
	assertTestArchiveContents(t, output.Bytes(), manifest, attachmentBytes)
	if err := addArchiveAttachments(zip.NewWriter(io.Discard), []models.Job{{}}, nil, attachments); err != nil {
		t.Fatalf("empty attachment list: %v", err)
	}
}

func writeTestArchive(t *testing.T, archive *zip.Writer, manifest *backupManifest, attachments, logos string) {
	t.Helper()
	if err := writeArchiveFiles(archive, manifest, &config.Config{AttachmentsDir: attachments, LogosDir: logos}); err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveManifest(archive, *manifest); err != nil {
		t.Fatal(err)
	}
}

func assertTestArchiveContents(t *testing.T, data []byte, manifest backupManifest, attachmentBytes []byte) {
	t.Helper()
	if manifest.Jobs[0].Attachments[0].StoredFilename != "attachments/attachment-1/resume.pdf" {
		t.Fatalf("archive attachment path=%q", manifest.Jobs[0].Attachments[0].StoredFilename)
	}
	checksum := sha256.Sum256(attachmentBytes)
	if manifest.AttachmentSHA256["attachment-1"] != hex.EncodeToString(checksum[:]) {
		t.Fatalf("attachment checksum=%q", manifest.AttachmentSHA256["attachment-1"])
	}
	files, manifestFile, expanded, err := parseTestArchive(data)
	if err != nil || manifestFile == nil || len(files) != 3 || expanded == 0 {
		t.Fatalf("archive files=%d manifest=%v expanded=%d err=%v", len(files), manifestFile != nil, expanded, err)
	}
}

func TestArchiveExportRejectsChangedOrUnsafeSources(t *testing.T) {
	attachments, logos := t.TempDir(), t.TempDir()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	attachment := models.Attachment{ID: "a", OriginalName: "resume.pdf", StoredFilename: "../outside.pdf", FileSize: 1}
	if err := addArchiveAttachment(archive, &attachment, map[string]string{}, attachments); err == nil {
		t.Fatal("unsafe stored filename should fail")
	}
	attachment.StoredFilename = "missing.pdf"
	if err := addArchiveAttachment(archive, &attachment, map[string]string{}, attachments); err == nil {
		t.Fatal("missing source should fail")
	}
	logo := backupLogo{Domain: "example.test", Path: "logos/example.test.png", Size: 4}
	if err := addArchiveLogo(archive, &logo, logos); err == nil {
		t.Fatal("missing logo should fail")
	}
	if err := addArchiveLogos(archive, []backupLogo{logo}, logos); err == nil {
		t.Fatal("logo list should report a logo write failure")
	}
	if err := os.WriteFile(filepath.Join(attachments, "changed.pdf"), []byte("longer"), 0600); err != nil {
		t.Fatal(err)
	}
	attachment.StoredFilename, attachment.FileSize = "changed.pdf", 2
	if err := addArchiveAttachment(archive, &attachment, map[string]string{}, attachments); err == nil {
		t.Fatal("changed source size should fail")
	}
	if _, err := copyArchiveFile(io.Discard, filepath.Join(attachments, "changed.pdf"), 2, 2); err == nil {
		t.Fatal("copy with unexpected size should fail")
	}
}

func TestWriteArchiveManifestReturnsUnderlyingWriterErrors(t *testing.T) {
	if err := writeArchiveManifest(zip.NewWriter(&failingArchiveWriter{failAfter: 0}), backupManifest{Jobs: []models.Job{}}); err == nil {
		t.Fatal("archive finalization failure should be reported")
	}
}

func TestWriteArchiveManifestRejectsOversizedManifest(t *testing.T) {
	manifest := backupManifest{Jobs: []models.Job{{Description: strings.Repeat("x", maxManifestBytes+1)}}}
	if err := writeArchiveManifest(zip.NewWriter(io.Discard), manifest); err == nil || !strings.Contains(err.Error(), "100 MiB") {
		t.Fatalf("oversized manifest error=%v", err)
	}
}

func TestReadImportArchiveRejectsInvalidInputsAndManifests(t *testing.T) {
	for _, size := range []int64{0, -1, maxImportArchiveBytes + 1} {
		if _, _, err := readImportArchive(bytes.NewReader(nil), size); err == nil {
			t.Errorf("size %d should fail", size)
		}
	}
	if _, _, err := readImportArchive(bytes.NewReader([]byte("not zip")), int64(len("not zip"))); err == nil {
		t.Fatal("invalid ZIP should fail")
	}
	for name, entries := range map[string]map[string][]byte{
		"missing manifest":     {"other.txt": []byte("x")},
		"unsafe path":          {"../bad": []byte("x"), "manifest.json": validManifestBytes(t)},
		"unsupported manifest": {"manifest.json": manifestBytes(t, backupManifest{Format: "wrong", Version: 1, Jobs: []models.Job{}})},
		"missing jobs":         {"manifest.json": []byte(`{"format":"war-room-backup","version":1}`)},
		"invalid JSON":         {"manifest.json": []byte("{")},
	} {
		t.Run(name, func(t *testing.T) {
			archive := makeTestArchive(t, entries)
			if _, _, err := readImportArchive(bytes.NewReader(archive), int64(len(archive))); err == nil {
				t.Fatal("invalid archive should fail")
			}
		})
	}
	valid := makeTestArchive(t, map[string][]byte{"manifest.json": validManifestBytes(t)})
	manifest, files, err := readImportArchive(bytes.NewReader(valid), int64(len(valid)))
	if err != nil || manifest.Format != backupFormat || len(files) != 1 {
		t.Fatalf("manifest=%#v files=%d err=%v", manifest, len(files), err)
	}
}

func TestArchivePathAndImportMetadataValidation(t *testing.T) {
	assertUnsafeArchivePaths(t)
	assertArchiveAttachmentPaths(t)
	assertPortableFilename(t)
	assertImportedAttachmentMetadata(t)
	assertImportedIDRegistration(t)
}

func assertUnsafeArchivePaths(t *testing.T) {
	t.Helper()
	for _, name := range []string{"", "../file", "/absolute", `a\\b`, "a/../b"} {
		if safeArchivePath(name) {
			t.Errorf("unsafe path accepted: %q", name)
		}
	}
}

func assertArchiveAttachmentPaths(t *testing.T) {
	t.Helper()
	if got, ok := validArchiveAttachmentPath("attachments/id/file.pdf", "id"); !ok || got != "attachments/id/file.pdf" {
		t.Fatalf("valid attachment path=%q ok=%t", got, ok)
	}
	for _, value := range []string{"attachments/id/..", "attachments/other/file.pdf", "attachments/id/a/b"} {
		if _, ok := validArchiveAttachmentPath(value, "id"); ok {
			t.Errorf("invalid attachment path accepted: %q", value)
		}
	}
	for _, id := range []string{"", "a/b", `a\\b`} {
		if _, ok := validArchiveAttachmentPath("attachments/"+id+"/file.pdf", id); ok {
			t.Errorf("invalid ID accepted: %q", id)
		}
	}
}

func assertPortableFilename(t *testing.T) {
	t.Helper()
	if got := portableFilename(`C:\\folder\\resume.pdf`); got != "resume.pdf" {
		t.Fatalf("portable filename=%q", got)
	}
	if got := portableFilename("."); got != "attachment" {
		t.Fatalf("empty portable filename=%q", got)
	}
}

func assertImportedAttachmentMetadata(t *testing.T) {
	t.Helper()
	if validImportedAttachmentFile(nil, 0) || validImportedAttachmentFile(&zip.File{}, -1) || validImportedAttachmentFile(&zip.File{}, maxAttachmentBytes+1) {
		t.Fatal("invalid attachment file metadata accepted")
	}
}

func assertImportedIDRegistration(t *testing.T) {
	t.Helper()
	if err := registerImportedID(map[string]bool{"seen": true}, "seen"); err == nil {
		t.Fatal("duplicate ID should fail")
	}
	if err := registerImportedID(map[string]bool{}, ""); err == nil {
		t.Fatal("empty ID should fail")
	}
	seen := map[string]bool{}
	if err := registerImportedID(seen, "ok"); err != nil || !seen["ok"] {
		t.Fatalf("register ID err=%v seen=%v", err, seen)
	}
}

func TestValidateArchiveReferencesFindsInconsistentMetadata(t *testing.T) {
	manifest := &backupManifest{AttachmentSHA256: map[string]string{}, Logos: []backupLogo{}}
	if err := validateArchiveReferences(manifest, map[string]*zip.File{"manifest.json": {}}, &[]string{}, map[string]bool{"manifest.json": true}); err != nil {
		t.Fatal(err)
	}
	for name, setup := range map[string]func(*backupManifest, *[]string, map[string]bool, map[string]*zip.File){
		"attachment count": func(manifest *backupManifest, created *[]string, used map[string]bool, files map[string]*zip.File) {
			*created = append(*created, "one")
		},
		"logo count": func(manifest *backupManifest, created *[]string, used map[string]bool, files map[string]*zip.File) {
			used["logos/x.png"] = true
		},
		"unreferenced file": func(manifest *backupManifest, created *[]string, used map[string]bool, files map[string]*zip.File) {
			files["extra"] = &zip.File{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := &backupManifest{AttachmentSHA256: map[string]string{}, Logos: []backupLogo{}}
			created, used, files := []string{}, map[string]bool{"manifest.json": true}, map[string]*zip.File{"manifest.json": {}}
			setup(candidate, &created, used, files)
			if err := validateArchiveReferences(candidate, files, &created, used); err == nil {
				t.Fatal("inconsistent archive should fail")
			}
		})
	}
}

func TestStageImportedCVVersionsRejectsInvalidMetadata(t *testing.T) {
	cases := map[string]func(*backupManifest){
		"legacy versions":     func(manifest *backupManifest) { manifest.Version = 1 },
		"missing ID":          func(manifest *backupManifest) { manifest.CVVersions[0].ID = "" },
		"invalid size":        func(manifest *backupManifest) { manifest.CVVersions[0].FileSize = maxAttachmentBytes + 1 },
		"invalid checksum":    func(manifest *backupManifest) { manifest.CVVersions[0].SHA256 = "nope" },
		"unsafe path":         func(manifest *backupManifest) { manifest.CVVersions[0].Path = "cvs/../escape" },
		"sequence regression": func(manifest *backupManifest) { manifest.NextCVVersion = 1 },
		"missing selected version": func(manifest *backupManifest) {
			manifest.Jobs = []models.Job{{ID: "job", SelectedCVVersion: &models.CVVersion{ID: "missing"}}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := validCVImportManifest()
			mutate(&manifest)
			service := &JobService{cfg: &config.Config{CVDir: t.TempDir()}}
			if err := service.stageImportedCVVersions(&manifest, map[string]*zip.File{}, &[]string{}, map[string]bool{}); err == nil {
				t.Fatal("invalid CV metadata should be rejected")
			}
		})
	}
}

func TestStageImportedCVVersionsInstallsAndResolvesSelection(t *testing.T) {
	manifest := validCVImportManifest()
	manifest.Jobs = []models.Job{{ID: "job", SelectedCVVersion: &models.CVVersion{ID: "cv-1"}}}
	content := []byte("cv")
	files, err := inspectFiles(makeTestArchive(t, map[string][]byte{"manifest.json": []byte("{}"), manifest.CVVersions[0].Path: content}))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	service := &JobService{cfg: &config.Config{CVDir: directory}}
	created := []string{}
	used := map[string]bool{}
	if err := service.stageImportedCVVersions(&manifest, files, &created, used); err != nil {
		t.Fatal(err)
	}
	assertImportedCVStage(t, directory, content, created, used, &manifest)
}

func assertImportedCVStage(t *testing.T, directory string, content []byte, created []string, used map[string]bool, manifest *backupManifest) {
	t.Helper()
	version := manifest.CVVersions[0]
	if len(created) != 1 || created[0] != version.StoredFilename || !used[version.Path] {
		t.Fatalf("created=%v used=%v", created, used)
	}
	if manifest.Jobs[0].SelectedCVVersion == nil || manifest.Jobs[0].SelectedCVVersion.Version != 1 || manifest.NextCVVersion != 2 {
		t.Fatalf("finalized manifest=%+v", manifest)
	}
	// #nosec G304 -- created filename came from a validated SHA-256 digest in the import manifest.
	stored, err := os.ReadFile(filepath.Join(directory, created[0]))
	if err != nil || !bytes.Equal(stored, content) {
		t.Fatalf("stored bytes=%q err=%v", stored, err)
	}
}

func TestStageImportedCVVersionsRejectsDuplicateNumbers(t *testing.T) {
	manifest := validCVImportManifest()
	second := manifest.CVVersions[0]
	second.ID = "cv-2"
	manifest.CVVersions = append(manifest.CVVersions, second)
	manifest.NextCVVersion = 3
	files, err := inspectFiles(makeTestArchive(t, map[string][]byte{"manifest.json": []byte("{}"), second.Path: []byte("cv")}))
	if err != nil {
		t.Fatal(err)
	}
	service := &JobService{cfg: &config.Config{CVDir: t.TempDir()}}
	if err := service.stageImportedCVVersions(&manifest, files, &[]string{}, map[string]bool{}); err == nil {
		t.Fatal("duplicate version numbers should fail")
	}
}

func TestStageImportedCVVersionsHandlesExistingAndInvalidBlobs(t *testing.T) {
	manifest := validCVImportManifest()
	version := manifest.CVVersions[0]
	content := []byte("cv")
	files, err := inspectFiles(makeTestArchive(t, map[string][]byte{"manifest.json": []byte("{}"), version.Path: content}))
	if err != nil {
		t.Fatal(err)
	}
	service := &JobService{cfg: &config.Config{CVDir: t.TempDir()}}
	if err := os.WriteFile(filepath.Join(service.cfg.CVDir, version.SHA256), content, 0600); err != nil {
		t.Fatal(err)
	}
	created := []string{}
	if err := service.stageImportedCVVersions(&manifest, files, &created, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if len(created) != 0 {
		t.Fatalf("existing blob unexpectedly recreated: %v", created)
	}

	for _, scenario := range []struct {
		name  string
		files map[string][]byte
	}{
		{name: "missing archive blob", files: map[string][]byte{"manifest.json": []byte("{}")}},
		{name: "wrong content", files: map[string][]byte{"manifest.json": []byte("{}"), version.Path: []byte("zz")}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			candidate := validCVImportManifest()
			archiveFiles, err := inspectFiles(makeTestArchive(t, scenario.files))
			if err != nil {
				t.Fatal(err)
			}
			staging := &JobService{cfg: &config.Config{CVDir: t.TempDir()}}
			if err := staging.stageImportedCVVersions(&candidate, archiveFiles, &[]string{}, map[string]bool{}); err == nil {
				t.Fatal("invalid CV archive blob should fail")
			}
		})
	}
}

func inspectFiles(data []byte) (map[string]*zip.File, error) {
	files, _, _, err := parseTestArchive(data)
	return files, err
}

func validCVImportManifest() backupManifest {
	content := []byte("cv")
	digestBytes := sha256.Sum256(content)
	digest := hex.EncodeToString(digestBytes[:])
	return backupManifest{Version: backupFormatVersion, NextCVVersion: 2, CVVersions: []backupCVVersion{{CVVersion: models.CVVersion{
		ID: "cv-1", Version: 1, OriginalName: "resume.pdf", FileSize: int64(len(content)), SHA256: digest,
	}, Path: "cvs/" + digest}}}
}

func TestExtractArchiveAttachmentEnforcesExactSizeAndExclusiveCreation(t *testing.T) {
	file := zipFileFromBytes(t, "content")
	destination := filepath.Join(t.TempDir(), "extracted")
	if _, err := extractArchiveAttachment(file, destination, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := extractArchiveAttachment(file, destination, 7); err == nil {
		t.Fatal("existing destination should not be replaced")
	}
	if _, err := extractArchiveAttachment(file, filepath.Join(t.TempDir(), "short"), 2); err == nil {
		t.Fatal("size mismatch should fail")
	}
}

func parseTestArchive(data []byte) (map[string]*zip.File, *zip.File, uint64, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, 0, err
	}
	return inspectArchive(reader)
}

func makeTestArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, data := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func validManifestBytes(t *testing.T) []byte {
	t.Helper()
	return manifestBytes(t, backupManifest{Format: backupFormat, Version: backupFormatVersion, Jobs: []models.Job{}, AttachmentSHA256: map[string]string{}})
}

func manifestBytes(t *testing.T, manifest backupManifest) []byte {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func zipFileFromBytes(t *testing.T, data string) *zip.File {
	t.Helper()
	archive := makeTestArchive(t, map[string][]byte{"file": []byte(data)})
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	return reader.File[0]
}

type failingArchiveWriter struct {
	writes    int
	failAfter int
}

func (writer *failingArchiveWriter) Write(data []byte) (int, error) {
	if writer.writes >= writer.failAfter {
		return 0, io.ErrShortWrite
	}
	writer.writes++
	return len(data), nil
}
