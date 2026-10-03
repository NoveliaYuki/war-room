package service

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/models"
)

var cvUploadLock sync.Mutex

// ErrCVVersionInUse indicates that a CV version is assigned to one or more jobs.
var ErrCVVersionInUse = errors.New("CV version is assigned to a job")

// ListCVVersions returns all versions, newest first.
func (s *JobService) ListCVVersions() ([]models.CVVersion, error) { return s.repo.ListCVVersions() }

// GetCVVersion returns a saved version by ID.
func (s *JobService) GetCVVersion(id string) (*models.CVVersion, error) {
	return s.repo.GetCVVersionByID(id)
}

// StoreCVVersion saves a new upload in the single CV history.
func (s *JobService) StoreCVVersion(filename string, content io.Reader, size int64, mimeType string) (*models.CVVersion, error) {
	filename, err := validateCVUpload(filename, size)
	if err != nil {
		return nil, err
	}
	cvUploadLock.Lock()
	defer cvUploadLock.Unlock()
	tempPath, digest, written, err := stageCVUpload(s.cfg.CVDir, content)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tempPath) }()
	if size > 0 && size != written {
		return nil, errors.New("CV upload size did not match request")
	}
	newBlob, err := installCVBlob(tempPath, s.cfg.CVDir, digest)
	if err != nil {
		return nil, err
	}
	version := newCVVersion(filename, digest, written, mimeType)
	if version.MimeType == "" {
		version.MimeType = "application/octet-stream"
	}
	if err := s.repo.InsertCVVersion(version); err != nil {
		if newBlob {
			s.removeUnreferencedCVBlob(version.StoredFilename)
		}
		return nil, fmt.Errorf("save CV version: %w", err)
	}
	s.mirrorAfterMutation()
	return version, nil
}

func validateCVUpload(filename string, size int64) (string, error) {
	if size < 0 || size > 50<<20 {
		return "", errors.New("CV upload exceeds the 50 MiB limit")
	}
	filename = portableFilename(strings.TrimSpace(filename))
	if filename == "" || filename == "." || strings.ContainsAny(filename, "\r\n") {
		return "", errors.New("invalid CV filename")
	}
	return filename, nil
}

func stageCVUpload(directory string, content io.Reader) (string, string, int64, error) {
	temp, err := os.CreateTemp(directory, ".cv-upload-*")
	if err != nil {
		return "", "", 0, fmt.Errorf("create temporary CV file: %w", err)
	}
	tempPath := temp.Name()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(content, (50<<20)+1))
	closeErr := temp.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return "", "", 0, fmt.Errorf("write CV upload: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return "", "", 0, fmt.Errorf("close CV upload: %w", closeErr)
	}
	if written == 0 || written > 50<<20 {
		_ = os.Remove(tempPath)
		if written == 0 {
			return "", "", 0, errors.New("CV file cannot be empty")
		}
		return "", "", 0, errors.New("CV upload exceeds the 50 MiB limit")
	}
	return tempPath, hex.EncodeToString(hash.Sum(nil)), written, nil
}

func installCVBlob(tempPath, directory, digest string) (bool, error) {
	destination := filepath.Join(directory, digest)
	// #nosec G304 -- destination uses the SHA-256 content address under configured CVDir.
	if existing, err := os.Open(destination); err == nil {
		defer func() { _ = existing.Close() }()
		if !sameFileHash(existing, digest) {
			return false, errors.New("stored CV hash collision or corrupted file")
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("check stored CV: %w", err)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return false, fmt.Errorf("install CV file: %w", err)
	}
	return true, nil
}

func newCVVersion(filename, digest string, size int64, mimeType string) *models.CVVersion {
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return &models.CVVersion{ID: uuid.NewString(), OriginalName: filename, StoredFilename: digest, FileSize: size, MimeType: mimeType, SHA256: digest, UploadedAt: time.Now().UTC().Unix()}
}

func (s *JobService) removeUnreferencedCVBlob(filename string) {
	var references int
	if err := s.repo.CountCVBlobReferences(filename, &references); err == nil && references == 0 {
		_ = os.Remove(filepath.Join(s.cfg.CVDir, filepath.Base(filename)))
	}
}

func sameFileHash(file *os.File, expected string) bool {
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false
	}
	return hex.EncodeToString(hash.Sum(nil)) == expected
}

// AssignCVVersion updates or clears the CV version assigned to a job.
func (s *JobService) AssignCVVersion(jobID string, versionID *string) error {
	if versionID != nil {
		version, err := s.repo.GetCVVersionByID(*versionID)
		if err != nil {
			return err
		}
		if version == nil {
			return sql.ErrNoRows
		}
	}
	if err := s.repo.AssignCVVersion(jobID, versionID); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

// DeleteCVVersion removes an unassigned version and its content-addressed file if unused.
func (s *JobService) DeleteCVVersion(id string) error {
	cvUploadLock.Lock()
	defer cvUploadLock.Unlock()
	version, err := s.repo.GetCVVersionByID(id)
	if err != nil {
		return err
	}
	if version == nil {
		return sql.ErrNoRows
	}
	if err := s.repo.DeleteCVVersion(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCVVersionInUse
		}
		return err
	}
	var references int
	if err := s.repo.CountCVBlobReferences(version.StoredFilename, &references); err == nil && references == 0 {
		_ = os.Remove(filepath.Join(s.cfg.CVDir, filepath.Base(version.StoredFilename)))
	}
	s.mirrorAfterMutation()
	return nil
}

// MigrateLegacyCVAttachments imports the legacy job-level PDF CV copies as one initial version.
func (s *JobService) MigrateLegacyCVAttachments() error {
	attachments, err := s.repo.GetAllAttachments()
	if err != nil {
		return err
	}
	pdfs := legacyCVAttachments(attachments)
	if len(pdfs) == 0 {
		return nil
	}
	chosen, digest, migrated, jobIDs, err := selectLegacyCV(s.cfg.AttachmentsDir, pdfs)
	if err != nil {
		return err
	}
	if len(migrated) < 2 {
		return nil
	}
	if err := installLegacyCVBlob(s.cfg, chosen, digest); err != nil {
		return err
	}
	version := legacyCVVersion(chosen, digest)
	created, err := s.repo.MigrateLegacyCVAttachments(version, migrated, jobIDs)
	if err != nil {
		return err
	}
	if created {
		if err := s.MirrorDatabaseToJSON(); err != nil {
			log.Printf("Warning: retained legacy CV files because recovery snapshot failed: %v", err)
			return nil
		}
		return s.cleanupMigratedCVFiles(migrated)
	}
	return nil
}

func legacyCVAttachments(attachments []models.Attachment) []models.Attachment {
	var pdfs []models.Attachment
	for _, attachment := range attachments {
		isPDF := strings.EqualFold(filepath.Ext(attachment.OriginalName), ".pdf") || strings.EqualFold(attachment.MimeType, "application/pdf")
		if attachment.StageID == nil && isPDF && hasCVFilenameMarker(attachment.OriginalName) {
			pdfs = append(pdfs, attachment)
		}
	}
	return pdfs
}

func hasCVFilenameMarker(filename string) bool {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(filename)), strings.ToLower(filepath.Ext(filename)))
	for _, marker := range []string{"cv", "resume", "curriculum"} {
		if strings.HasPrefix(name, marker) && (len(name) == len(marker) || isCVFilenameSeparator(rune(name[len(marker)]))) {
			return true
		}
	}
	return false
}

func isCVFilenameSeparator(character rune) bool {
	return character == ' ' || character == '.' || character == '_' || character == '-'
}

func selectLegacyCV(directory string, attachments []models.Attachment) (models.Attachment, string, []models.Attachment, []string, error) {
	chosen, digest, earliest := models.Attachment{}, "", int64(0)
	jobSet := make(map[string]bool, len(attachments))
	for index, attachment := range attachments {
		current, err := hashLegacyCV(directory, attachment)
		if err != nil {
			return models.Attachment{}, "", nil, nil, err
		}
		if index == 0 || attachment.CreatedAt < earliest {
			chosen, digest, earliest = attachment, current, attachment.CreatedAt
		}
		jobSet[attachment.JobID] = true
	}
	if len(attachments) < 2 {
		return models.Attachment{}, "", nil, nil, nil
	}
	jobIDs := make([]string, 0, len(jobSet))
	for id := range jobSet {
		jobIDs = append(jobIDs, id)
	}
	return chosen, digest, attachments, jobIDs, nil
}

func hashLegacyCV(directory string, attachment models.Attachment) (string, error) {
	name := filepath.Base(attachment.StoredFilename)
	if name != attachment.StoredFilename || name == "." {
		return "", fmt.Errorf("unsafe legacy CV attachment path")
	}
	// #nosec G304 -- name is a basename validated against the stored attachment filename.
	file, err := os.Open(filepath.Join(directory, name))
	if err != nil {
		return "", fmt.Errorf("open legacy CV attachment: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(hash, io.LimitReader(file, (50<<20)+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written > 50<<20 || written != attachment.FileSize {
		return "", fmt.Errorf("legacy CV attachment size mismatch")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func installLegacyCVBlob(cfg *config.Config, chosen models.Attachment, digest string) error {
	source := filepath.Join(cfg.AttachmentsDir, filepath.Base(chosen.StoredFilename))
	destination := filepath.Join(cfg.CVDir, digest)
	if _, err := os.Stat(destination); errors.Is(err, os.ErrNotExist) {
		return copyVerifiedFile(source, destination, digest, chosen.FileSize)
	} else if err != nil {
		return err
	}
	if err := verifyCVFile(destination, digest, chosen.FileSize); err != nil {
		return fmt.Errorf("verify existing CV blob: %w", err)
	}
	return nil
}

func legacyCVVersion(chosen models.Attachment, digest string) *models.CVVersion {
	return &models.CVVersion{ID: uuid.NewString(), Version: 1, OriginalName: chosen.OriginalName, StoredFilename: digest,
		FileSize: chosen.FileSize, MimeType: "application/pdf", SHA256: digest, UploadedAt: chosen.CreatedAt}
}

func (s *JobService) cleanupMigratedCVFiles(pdfs []models.Attachment) error {
	remaining, err := s.repo.GetAllAttachments()
	if err != nil {
		return fmt.Errorf("verify remaining attachment references: %w", err)
	}
	protected := make(map[string]bool, len(remaining))
	for _, attachment := range remaining {
		protected[filepath.Base(attachment.StoredFilename)] = true
	}
	for _, attachment := range pdfs {
		if !protected[filepath.Base(attachment.StoredFilename)] {
			removeStoredAttachment(s.cfg.AttachmentsDir, attachment.StoredFilename)
		}
	}
	return nil
}

func verifyCVFile(filename, expectedHash string, expectedSize int64) error {
	pathInfo, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if !pathInfo.Mode().IsRegular() || pathInfo.Size() != expectedSize {
		return errors.New("CV blob is not a regular file or has the wrong size")
	}
	// #nosec G304 -- filename is an internal path assembled from configured storage and a validated digest.
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != expectedSize || !sameFileHash(file, expectedHash) {
		return errors.New("CV blob size or checksum mismatch")
	}
	return nil
}

func copyVerifiedFile(sourcePath, destinationPath, expectedHash string, expectedSize int64) error {
	// #nosec G304 -- sourcePath uses a validated stored attachment basename in configured storage.
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	temp, err := os.CreateTemp(filepath.Dir(destinationPath), ".cv-migration-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temp, hash), source); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		return errors.New("legacy CV file changed during migration")
	}
	if err := os.Rename(tempPath, destinationPath); err != nil {
		if _, statErr := os.Stat(destinationPath); statErr == nil {
			return verifyCVFile(destinationPath, expectedHash, expectedSize)
		}
		return err
	}
	return nil
}
