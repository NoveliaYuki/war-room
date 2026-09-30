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

func TestValidateExportArchiveSizeLimits(t *testing.T) {
	valid := []models.Job{{Attachments: []models.Attachment{{FileSize: 12}}}}
	if err := validateExportArchiveSize(valid, []backupLogo{{Size: 8}}); err != nil {
		t.Fatalf("valid archive size: %v", err)
	}
	for name, job := range map[string]models.Job{
		"negative size":  {Attachments: []models.Attachment{{FileSize: -1}}},
		"oversized file": {Attachments: []models.Attachment{{FileSize: maxAttachmentBytes + 1}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateExportArchiveSize([]models.Job{job}, nil); err == nil {
				t.Fatal("invalid attachment size should fail")
			}
		})
	}
	if err := validateExportArchiveSize(nil, []backupLogo{{Size: maxLogoBytes + 1}}); err == nil {
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
