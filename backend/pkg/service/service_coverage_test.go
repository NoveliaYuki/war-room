package service

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteJobRemovesOwnedAttachmentFiles(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	attachment, err := fixture.service.StoreAttachment("owner-b", nil, "resume.pdf", strings.NewReader("resume data"), int64(len("resume data")), "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := fixture.service.GetAttachmentByID(attachment.ID)
	if err != nil || loaded == nil || loaded.ID != attachment.ID {
		t.Fatalf("attachment lookup=%+v err=%v", loaded, err)
	}
	if missing, err := fixture.service.GetAttachmentByID("missing"); err != nil || missing != nil {
		t.Fatalf("missing attachment=%+v err=%v", missing, err)
	}
	storedPath := filepath.Join(fixture.cfg.AttachmentsDir, attachment.StoredFilename)
	if _, err := os.Stat(storedPath); err != nil {
		t.Fatalf("stored attachment stat: %v", err)
	}
	if err := fixture.service.DeleteJob("owner-b"); err != nil {
		t.Fatalf("delete job with an attachment: %v", err)
	}
	if _, err := os.Stat(storedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("attachment file remains after job deletion, stat error=%v", err)
	}
}

func TestServiceLookupsReturnDatabaseErrors(t *testing.T) {
	fixture := newAttachmentOwnerFixture(t)
	version, err := fixture.service.StoreCVVersion("resume.pdf", strings.NewReader("cv"), 2, "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.DeleteJob("owner-a"); err == nil {
		t.Fatal("delete should report an attachment lookup error when its database is closed")
	}
	if _, err := fixture.service.GetAttachmentByID("missing"); err == nil {
		t.Fatal("attachment lookup should report a database error")
	}
	if _, err := fixture.service.GetCVVersion(version.ID); err == nil {
		t.Fatal("CV version lookup should report a database error")
	}
}

func TestStageImportedLogosWritesVerifiedFile(t *testing.T) {
	content := []byte("portable logo bytes")
	digest := sha256.Sum256(content)
	checksum := hex.EncodeToString(digest[:])
	logo := backupLogo{Domain: "example.test", Path: "logos/example.test.png", MimeType: "image/png", Size: int64(len(content)), SHA256: checksum}
	files, err := inspectFiles(makeTestArchive(t, map[string][]byte{"manifest.json": []byte("{}"), logo.Path: content}))
	if err != nil {
		t.Fatal(err)
	}
	staging := t.TempDir()
	used := map[string]bool{"manifest.json": true}
	if err := stageImportedLogos([]backupLogo{logo}, files, staging, used); err != nil {
		t.Fatalf("stage valid logo: %v", err)
	}
	if !used[logo.Path] {
		t.Fatalf("logo archive entry was not marked used: %v", used)
	}
	// #nosec G304 -- staging is created by t.TempDir and the logo filename is fixed.
	if data, err := os.ReadFile(filepath.Join(staging, "example.test.png")); err != nil || string(data) != string(content) {
		t.Fatalf("staged logo=%q err=%v", data, err)
	}
}

func TestStageImportedLogosRejectsInvalidReferences(t *testing.T) {
	logo, files := coverageImportedLogo(t)
	unsafe := logo
	unsafe.Path = "../logo.png"
	coverageAssertLogoStageError(t, []backupLogo{unsafe}, files, nil)
	coverageAssertLogoStageError(t, []backupLogo{logo}, map[string]*zip.File{}, nil)
}

func TestStageImportedLogosRejectsInvalidChecksumsAndReuse(t *testing.T) {
	logo, files := coverageImportedLogo(t)
	badHash := logo
	badHash.SHA256 = strings.Repeat("0", 64)
	coverageAssertLogoStageError(t, []backupLogo{badHash}, files, nil)
	coverageAssertLogoStageError(t, []backupLogo{logo, logo}, files, nil)
	coverageAssertLogoStageError(t, []backupLogo{logo}, files, map[string]bool{logo.Path: true})
}

func coverageImportedLogo(t *testing.T) (backupLogo, map[string]*zip.File) {
	t.Helper()
	content := []byte("portable logo bytes")
	digest := sha256.Sum256(content)
	logo := backupLogo{
		Domain: "example.test", Path: "logos/example.test.png", MimeType: "image/png",
		Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]),
	}
	files, err := inspectFiles(makeTestArchive(t, map[string][]byte{"manifest.json": []byte("{}"), logo.Path: content}))
	if err != nil {
		t.Fatal(err)
	}
	return logo, files
}

func coverageAssertLogoStageError(t *testing.T, logos []backupLogo, files map[string]*zip.File, used map[string]bool) {
	t.Helper()
	if used == nil {
		used = make(map[string]bool)
	}
	if err := stageImportedLogos(logos, files, t.TempDir(), used); err == nil {
		t.Fatal("invalid logo references should be rejected")
	}
}
