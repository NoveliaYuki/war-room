package service

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
)

const (
	backupFormat          = "war-room-backup"
	backupFormatVersion   = 4
	maxImportArchiveBytes = 500 << 20
	maxExpandedBytes      = 1 << 30
	maxManifestBytes      = 100 << 20
	maxArchiveEntries     = 10001
	maxAttachmentBytes    = 50 << 20
	maxLogoBytes          = 5 << 20
)

// ErrEmptyBackupRequiresConfirmation indicates an empty backup needs explicit user confirmation.
var ErrEmptyBackupRequiresConfirmation = errors.New("empty backup import requires confirmation")

type backupManifest struct {
	Format           string                `json:"format"`
	Version          int                   `json:"version"`
	ExportedAt       time.Time             `json:"exported_at"`
	Jobs             []models.Job          `json:"jobs"`
	SearchPeriods    []models.SearchPeriod `json:"search_periods,omitempty"`
	Technologies     []models.Technology   `json:"technologies,omitempty"`
	NextCVVersion    int                   `json:"next_cv_version,omitempty"`
	AttachmentSHA256 map[string]string     `json:"attachment_sha256"`
	Logos            []backupLogo          `json:"logos,omitempty"`
	CVVersions       []backupCVVersion     `json:"cv_versions,omitempty"`
}

type backupCVVersion struct {
	models.CVVersion
	Path string `json:"path"`
}

type backupLogo struct {
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// ExportArchive writes a portable ZIP backup containing process data and uploaded files.
func (s *JobService) ExportArchive(output io.Writer) error {
	cvUploadLock.Lock()
	defer cvUploadLock.Unlock()
	snapshot, err := loadExportSnapshot(s.repo)
	if err != nil {
		return fmt.Errorf("load export snapshot: %w", err)
	}
	if snapshot.jobs == nil {
		snapshot.jobs = []models.Job{}
	}
	logos, err := collectCachedLogos(snapshot.jobs, s.cfg.LogosDir)
	if err != nil {
		return err
	}
	if err := validateExportArchiveSize(snapshot.jobs, snapshot.versions, logos, s.cfg.CVDir); err != nil {
		return err
	}

	archive := zip.NewWriter(output)
	manifest := backupManifest{Format: backupFormat, Version: backupFormatVersion, ExportedAt: time.Now().UTC(), Jobs: snapshot.jobs, SearchPeriods: snapshot.searchPeriods, Technologies: snapshot.technologies, NextCVVersion: snapshot.nextCVVersion, AttachmentSHA256: make(map[string]string), Logos: logos}
	for _, version := range snapshot.versions {
		manifest.CVVersions = append(manifest.CVVersions, backupCVVersion{CVVersion: version, Path: "cvs/" + version.StoredFilename})
	}
	if err := writeArchiveFiles(archive, &manifest, s.cfg); err != nil {
		_ = archive.Close()
		return err
	}
	return writeArchiveManifest(archive, manifest)
}

type exportSnapshot struct {
	jobs          []models.Job
	technologies  []models.Technology
	searchPeriods []models.SearchPeriod
	versions      []models.CVVersion
	nextCVVersion int
}

func loadExportSnapshot(repo *repository.Repository) (exportSnapshot, error) {
	var result exportSnapshot
	err := repo.WithReadSnapshot(func(snapshot *repository.Repository) error {
		var err error
		result.jobs, err = loadFullJobSnapshot(snapshot)
		if err == nil {
			result.technologies, err = snapshot.ListTechnologies()
		}
		if err == nil {
			result.searchPeriods, err = snapshot.ListSearchPeriods()
		}
		if err == nil {
			result.versions, err = snapshot.ListCVVersions()
		}
		if err == nil {
			result.nextCVVersion, err = snapshot.GetNextCVVersion()
		}
		return err
	})
	return result, err
}

func writeArchiveFiles(archive *zip.Writer, manifest *backupManifest, cfg *config.Config) error {
	if err := addArchiveAttachments(archive, manifest.Jobs, manifest.AttachmentSHA256, cfg.AttachmentsDir); err != nil {
		return err
	}
	if err := addArchiveCVVersions(archive, manifest.CVVersions, cfg.CVDir); err != nil {
		return err
	}
	return addArchiveLogos(archive, manifest.Logos, cfg.LogosDir)
}

func addArchiveCVVersions(archive *zip.Writer, versions []backupCVVersion, cvDirectory string) error {
	seen := map[string]bool{}
	for _, version := range versions {
		name, err := backupStoredFilename(version.StoredFilename)
		if err != nil || version.Path != "cvs/"+name {
			return errors.New("CV backup contains unsafe file path")
		}
		if seen[version.Path] {
			continue
		}
		seen[version.Path] = true
		if err := verifyCVBlob(filepath.Join(cvDirectory, name), version.SHA256, version.FileSize); err != nil {
			return fmt.Errorf("CV version %d is unavailable or changed: %w", version.Version, err)
		}
		entry, createErr := archive.Create(version.Path)
		var digest string
		if createErr == nil {
			digest, createErr = copyArchiveFile(entry, filepath.Join(cvDirectory, name), version.FileSize, maxAttachmentBytes)
		}
		if createErr != nil {
			return createErr
		}
		if !strings.EqualFold(digest, version.SHA256) {
			return fmt.Errorf("CV version %d failed checksum validation during export", version.Version)
		}
	}
	return nil
}

func writeArchiveManifest(archive *zip.Writer, manifest backupManifest) error {
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		_ = archive.Close()
		return fmt.Errorf("encode archive manifest: %w", err)
	}
	if len(manifestBytes) > maxManifestBytes {
		_ = archive.Close()
		return errors.New("backup manifest exceeds the 100 MiB limit")
	}
	manifestEntry, err := archive.Create("manifest.json")
	if err == nil {
		_, err = manifestEntry.Write(manifestBytes)
	}
	closeErr := archive.Close()
	if err != nil {
		return fmt.Errorf("write archive manifest: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("finish backup archive: %w", closeErr)
	}
	return nil
}

func validateExportArchiveSize(jobs []models.Job, versions []models.CVVersion, logos []backupLogo, cvDirectory string) error {
	entries := 1
	var expanded uint64
	for _, job := range jobs {
		for _, attachment := range job.Attachments {
			if err := addExportSize(&entries, &expanded, attachment.FileSize, maxAttachmentBytes); err != nil {
				return err
			}
		}
	}
	for _, logo := range logos {
		if err := addExportSize(&entries, &expanded, logo.Size, maxLogoBytes); err != nil {
			return err
		}
	}
	return validateCVExportSize(versions, cvDirectory, &entries, &expanded)
}

func validateCVExportSize(versions []models.CVVersion, cvDirectory string, entries *int, expanded *uint64) error {
	seenBlobs := make(map[string]models.CVVersion)
	for _, version := range versions {
		filename, err := backupStoredFilename(version.StoredFilename)
		if err != nil || len(version.SHA256) != 64 || filename != strings.ToLower(version.SHA256) {
			return errors.New("CV version has invalid stored file metadata")
		}
		if previous, exists := seenBlobs[filename]; exists {
			if previous.SHA256 != version.SHA256 || previous.FileSize != version.FileSize {
				return errors.New("CV versions contain conflicting metadata for a shared blob")
			}
			continue
		}
		if err := verifyCVBlob(filepath.Join(cvDirectory, filename), version.SHA256, version.FileSize); err != nil {
			return fmt.Errorf("CV version %d is unavailable or changed: %w", version.Version, err)
		}
		seenBlobs[filename] = version
		if err := addExportSize(entries, expanded, version.FileSize, maxAttachmentBytes); err != nil {
			return err
		}
	}
	return nil
}

func verifyCVBlob(filename, expectedHash string, expectedSize int64) error {
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != expectedSize || expectedSize <= 0 || expectedSize > maxAttachmentBytes {
		return errors.New("CV blob size or file type is invalid")
	}
	// #nosec G304 -- filename is an internal content-addressed path within configured CV storage.
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maxAttachmentBytes+1))
	if err != nil {
		return err
	}
	if written != expectedSize || hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		return errors.New("CV blob checksum mismatch")
	}
	return nil
}

func addExportSize(entries *int, expanded *uint64, size, fileLimit int64) error {
	*entries++
	if *entries > maxArchiveEntries || size < 0 || size > fileLimit {
		return errors.New("data exceeds the file count or size limit for ZIP backups")
	}
	if uint64(size) > maxExpandedBytes-maxManifestBytes-*expanded {
		return errors.New("data exceeds the 1 GiB expanded ZIP backup limit")
	}
	*expanded += uint64(size)
	return nil
}

func collectCachedLogos(jobs []models.Job, logosDir string) ([]backupLogo, error) {
	logoService := NewLogoService(&config.Config{LogosDir: logosDir})
	seenDomains := make(map[string]bool)
	logos := make([]backupLogo, 0)
	for _, job := range jobs {
		explicitDomain := ""
		if job.CompanyDomain != nil {
			explicitDomain = *job.CompanyDomain
		}
		domain := logoService.DeduceDomain(job.CompanyName, explicitDomain)
		if domain == "" || seenDomains[domain] {
			continue
		}
		seenDomains[domain] = true
		logo, found, err := findCachedLogo(domain, logosDir)
		if err != nil {
			return nil, err
		}
		if found {
			logos = append(logos, logo)
		}
	}
	return logos, nil
}

func findCachedLogo(domain, logosDir string) (backupLogo, bool, error) {
	for _, format := range cachedLogoFormats {
		filePath := filepath.Join(logosDir, domain+format.extension)
		info, err := os.Lstat(filePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return backupLogo{}, false, fmt.Errorf("read cached logo for %q", domain)
		}
		if info.Size() <= 0 || info.Size() > maxLogoBytes {
			return backupLogo{}, false, fmt.Errorf("cached logo for %q exceeds the 5 MiB ZIP backup limit", domain)
		}
		return backupLogo{Domain: domain, Path: path.Join("logos", domain+format.extension), MimeType: format.mimeType, Size: info.Size()}, true, nil
	}
	return backupLogo{}, false, nil
}

var cachedLogoFormats = []struct{ extension, mimeType string }{
	{".png", "image/png"}, {".svg", "image/svg+xml"}, {".jpg", "image/jpeg"}, {".webp", "image/webp"},
}

func addArchiveLogos(archive *zip.Writer, logos []backupLogo, logosDir string) error {
	for index := range logos {
		if err := addArchiveLogo(archive, &logos[index], logosDir); err != nil {
			return err
		}
	}
	return nil
}

func addArchiveLogo(archive *zip.Writer, logo *backupLogo, logosDir string) error {
	filePath := filepath.Join(logosDir, logo.Domain+filepath.Ext(logo.Path))
	info, err := os.Lstat(filePath)
	if err != nil || !info.Mode().IsRegular() || info.Size() != logo.Size {
		return fmt.Errorf("cached logo for %q is unavailable or has changed", logo.Domain)
	}
	entry, err := archive.Create(logo.Path)
	if err != nil {
		return fmt.Errorf("add company logo to archive: %w", err)
	}
	// #nosec G304 -- the logo path is derived from a normalized domain and known extension.
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open cached logo for export: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(entry, hash), io.LimitReader(file, maxLogoBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written != logo.Size || written > maxLogoBytes {
		return fmt.Errorf("read cached logo for %q", logo.Domain)
	}
	logo.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return nil
}

func addArchiveAttachments(archive *zip.Writer, jobs []models.Job, checksums map[string]string, attachmentsDir string) error {
	for jobIndex := range jobs {
		attachments := jobs[jobIndex].Attachments
		for attachmentIndex := range attachments {
			if err := addArchiveAttachment(archive, &attachments[attachmentIndex], checksums, attachmentsDir); err != nil {
				return err
			}
		}
	}
	return nil
}

func addArchiveAttachment(archive *zip.Writer, attachment *models.Attachment, checksums map[string]string, attachmentsDir string) error {
	storedName, err := backupStoredFilename(attachment.StoredFilename)
	if err != nil {
		return fmt.Errorf("invalid stored attachment filename: %w", err)
	}
	filePath := filepath.Join(attachmentsDir, storedName)
	info, err := os.Lstat(filePath)
	if err != nil || !info.Mode().IsRegular() || info.Size() != attachment.FileSize {
		return fmt.Errorf("attachment %q is unavailable or has changed", attachment.OriginalName)
	}
	archivePath := path.Join("attachments", attachment.ID, portableFilename(attachment.OriginalName))
	if _, ok := validArchiveAttachmentPath(archivePath, attachment.ID); !ok {
		return errors.New("attachment has an invalid identifier")
	}
	entry, err := archive.Create(archivePath)
	if err != nil {
		return fmt.Errorf("add attachment to archive: %w", err)
	}
	checksum, err := copyArchiveFile(entry, filePath, attachment.FileSize, maxAttachmentBytes)
	if err != nil {
		return fmt.Errorf("read attachment %q for export: %w", attachment.OriginalName, err)
	}
	attachment.StoredFilename = archivePath
	checksums[attachment.ID] = checksum
	return nil
}

func copyArchiveFile(entry io.Writer, filePath string, expectedSize, limit int64) (string, error) {
	// #nosec G304 -- callers validate that the path is inside the configured file directory.
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(entry, hash), io.LimitReader(file, limit+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written != expectedSize || written > limit {
		return "", errors.New("archive file is unavailable or changed")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ImportArchive validates an archive and replaces all saved processes atomically.
func (s *JobService) ImportArchive(input io.ReaderAt, size int64, allowEmpty bool) error {
	cvUploadLock.Lock()
	defer cvUploadLock.Unlock()
	manifest, files, err := readImportArchive(input, size)
	if err != nil {
		return err
	}
	if len(manifest.Jobs) == 0 && !allowEmpty {
		return ErrEmptyBackupRequiresConfirmation
	}
	return s.applyImportedArchive(&manifest, files)
}

func (s *JobService) applyImportedArchive(manifest *backupManifest, files map[string]*zip.File) error {
	oldAttachments, err := s.repo.GetAllAttachments()
	if err != nil {
		return fmt.Errorf("read existing attachment list: %w", err)
	}
	oldCVVersions, err := s.repo.ListCVVersions()
	if err != nil {
		return fmt.Errorf("read existing CV version list: %w", err)
	}
	stage, err := newArchiveImportStage(s.cfg)
	if err != nil {
		return err
	}
	defer stage.cleanup()
	if err := s.stageArchiveImport(manifest, files, stage); err != nil {
		return err
	}
	stage.committed = true
	if manifest.Version == 1 {
		if err := s.MigrateLegacyCVAttachments(); err != nil {
			log.Printf("Warning: failed to convert legacy CV attachments after import: %v", err)
		}
	}
	if err := s.MirrorDatabaseToJSON(); err != nil {
		log.Printf("Warning: failed to refresh recovery snapshot after import: %v", err)
		return nil
	}
	return s.cleanupReplacedArchiveFiles(oldAttachments, oldCVVersions)
}

type archiveImportStage struct {
	attachmentsDir string
	cvDir          string
	stagingDir     string
	createdFiles   []string
	createdCVFiles []string
	committed      bool
}

func newArchiveImportStage(cfg *config.Config) (*archiveImportStage, error) {
	directory, err := os.MkdirTemp(cfg.AttachmentsDir, ".import-")
	if err != nil {
		return nil, fmt.Errorf("prepare import staging: %w", err)
	}
	return &archiveImportStage{attachmentsDir: cfg.AttachmentsDir, cvDir: cfg.CVDir, stagingDir: directory}, nil
}

func (stage *archiveImportStage) cleanup() {
	_ = os.RemoveAll(stage.stagingDir)
	if stage.committed {
		return
	}
	for _, filename := range stage.createdFiles {
		_ = os.Remove(filepath.Join(stage.attachmentsDir, filename))
	}
	for _, filename := range stage.createdCVFiles {
		_ = os.Remove(filepath.Join(stage.cvDir, filename))
	}
}

func (s *JobService) stageArchiveImport(manifest *backupManifest, files map[string]*zip.File, stage *archiveImportStage) error {
	usedEntries := map[string]bool{"manifest.json": true}
	if err := s.stageImportedAttachments(manifest, files, stage.stagingDir, &stage.createdFiles, usedEntries); err != nil {
		return err
	}
	if err := s.stageImportedCVVersions(manifest, files, &stage.createdCVFiles, usedEntries); err != nil {
		return err
	}
	return s.commitImportedArchive(manifest, files, &stage.createdFiles, usedEntries)
}

func (s *JobService) cleanupReplacedArchiveFiles(oldAttachments []models.Attachment, oldCVVersions []models.CVVersion) error {
	remaining, err := s.repo.GetAllAttachments()
	if err != nil {
		log.Printf("Warning: retained old attachments because references could not be checked: %v", err)
		return nil
	}
	protected := make(map[string]bool, len(remaining))
	for _, attachment := range remaining {
		protected[filepath.Base(attachment.StoredFilename)] = true
	}
	removeOldAttachments(s.cfg.AttachmentsDir, oldAttachments, protected)
	removeOldCVBlobs(s.repo, s.cfg.CVDir, oldCVVersions)
	return nil
}

func removeOldAttachments(directory string, attachments []models.Attachment, protected map[string]bool) {
	for _, attachment := range attachments {
		if protected[filepath.Base(attachment.StoredFilename)] {
			continue
		}
		if filename, err := backupStoredFilename(attachment.StoredFilename); err == nil {
			_ = os.Remove(filepath.Join(directory, filename))
		}
	}
}

func removeOldCVBlobs(repo *repository.Repository, directory string, versions []models.CVVersion) {
	for _, version := range versions {
		var references int
		if err := repo.CountCVBlobReferences(version.StoredFilename, &references); err != nil || references > 0 {
			continue
		}
		if filename, err := backupStoredFilename(version.StoredFilename); err == nil {
			_ = os.Remove(filepath.Join(directory, filename))
		}
	}
}

func (s *JobService) commitImportedArchive(manifest *backupManifest, files map[string]*zip.File, createdFiles *[]string, usedEntries map[string]bool) error {
	logoStagingDir, err := os.MkdirTemp(s.cfg.LogosDir, ".import-")
	if err != nil {
		return fmt.Errorf("prepare logo import staging: %w", err)
	}
	defer func() { _ = os.RemoveAll(logoStagingDir) }()
	if err := stageImportedLogos(manifest.Logos, files, logoStagingDir, usedEntries); err != nil {
		return err
	}
	if err := validateArchiveReferences(manifest, files, createdFiles, usedEntries); err != nil {
		return err
	}
	installedLogos, err := installImportedLogos(manifest.Logos, logoStagingDir, s.cfg.LogosDir)
	if err != nil {
		return err
	}
	versions := make([]models.CVVersion, 0, len(manifest.CVVersions))
	for _, cvVersion := range manifest.CVVersions {
		versions = append(versions, cvVersion.CVVersion)
	}
	if manifest.NextCVVersion <= 0 {
		manifest.NextCVVersion = 1
		for _, version := range versions {
			if version.Version >= manifest.NextCVVersion {
				manifest.NextCVVersion = version.Version + 1
			}
		}
	}
	if err := s.repo.ReplaceAllWithSearchPeriods(manifest.Jobs, manifest.SearchPeriods, manifest.Technologies, versions, true, manifest.NextCVVersion); err != nil {
		rollbackImportedLogos(installedLogos)
		return fmt.Errorf("replace saved processes: %w", err)
	}
	return nil
}

func backupStoredFilename(storedFilename string) (string, error) {
	filename := filepath.Base(storedFilename)
	if filename != storedFilename || filename == "." || filename == string(filepath.Separator) {
		return "", fmt.Errorf("unsafe stored filename %q", storedFilename)
	}
	return filename, nil
}

func readImportArchive(input io.ReaderAt, size int64) (backupManifest, map[string]*zip.File, error) {
	if size <= 0 || size > maxImportArchiveBytes {
		return backupManifest{}, nil, errors.New("backup file must be between 1 byte and 500 MiB")
	}
	archive, err := zip.NewReader(input, size)
	if err != nil {
		return backupManifest{}, nil, errors.New("file is not a valid War Room backup ZIP")
	}
	files, manifestFile, expandedSize, err := inspectArchive(archive)
	if err != nil {
		return backupManifest{}, nil, err
	}
	if expandedSize > maxExpandedBytes {
		return backupManifest{}, nil, errors.New("backup expands beyond the 1 GiB import limit")
	}
	manifest, err := decodeBackupManifest(manifestFile)
	return manifest, files, err
}

func decodeBackupManifest(manifestFile *zip.File) (backupManifest, error) {
	manifestBytes, err := readArchiveEntry(manifestFile, maxManifestBytes)
	if err != nil {
		return backupManifest{}, fmt.Errorf("read backup manifest: %w", err)
	}
	var manifest backupManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return backupManifest{}, errors.New("backup manifest is invalid JSON")
	}
	if manifest.Format != backupFormat || manifest.Version < 1 || manifest.Version > backupFormatVersion {
		return backupManifest{}, fmt.Errorf("unsupported backup format or version (%q, %d)", manifest.Format, manifest.Version)
	}
	if manifest.Jobs == nil {
		return backupManifest{}, errors.New("backup manifest has no process list")
	}
	if err := validateImportedMeetingDates(manifest.Jobs); err != nil {
		return backupManifest{}, err
	}
	if err := validateImportedSearchPeriods(manifest.SearchPeriods, manifest.Jobs); err != nil {
		return backupManifest{}, err
	}
	return manifest, nil
}

func validateImportedSearchPeriods(periods []models.SearchPeriod, jobs []models.Job) error {
	known := make(map[string]bool, len(periods))
	sorted := append([]models.SearchPeriod(nil), periods...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left].StartDate < sorted[right].StartDate })
	previousEnd := ""
	hasPreviousPeriod := false
	for _, period := range sorted {
		if strings.TrimSpace(period.ID) == "" || known[period.ID] {
			return errors.New("backup contains a missing or duplicate search period ID")
		}
		if _, err := normalizeSearchPeriod(models.SearchPeriodInput{Name: period.Name, StartDate: period.StartDate, EndDate: period.EndDate}); err != nil {
			return fmt.Errorf("invalid search period in backup: %w", err)
		}
		if hasPreviousPeriod && searchPeriodsOverlap(previousEnd, period.StartDate) {
			return errors.New("backup contains overlapping search periods")
		}
		known[period.ID] = true
		previousEnd = period.EndDate
		hasPreviousPeriod = true
	}
	for _, job := range jobs {
		if job.SearchPeriodID != nil && !known[*job.SearchPeriodID] {
			return fmt.Errorf("job %q refers to a missing search period", job.ID)
		}
	}
	return nil
}

func searchPeriodsOverlap(previousEnd, currentStart string) bool {
	return previousEnd == "" || currentStart <= previousEnd
}

func validateImportedMeetingDates(jobs []models.Job) error {
	for _, job := range jobs {
		for _, stage := range job.Stages {
			if stage.MeetingDate != nil && *stage.MeetingDate != "" {
				if err := validateMeetingDate(*stage.MeetingDate); err != nil {
					return fmt.Errorf("invalid meeting date in backup: %w", err)
				}
			}
		}
	}
	return nil
}

func (s *JobService) stageImportedAttachments(manifest *backupManifest, files map[string]*zip.File, stagingDir string, createdFiles *[]string, usedEntries map[string]bool) error {
	seenIDs := make(map[string]bool)
	for jobIndex := range manifest.Jobs {
		job := &manifest.Jobs[jobIndex]
		if err := registerJobRecordIDs(seenIDs, job); err != nil {
			return err
		}
		for attachmentIndex := range job.Attachments {
			attachment := &job.Attachments[attachmentIndex]
			if err := registerImportedID(seenIDs, attachment.ID); err != nil {
				return err
			}
			if err := s.stageImportedAttachment(manifest, job, attachment, files, stagingDir, createdFiles, usedEntries); err != nil {
				return err
			}
		}
	}
	return nil
}

func registerJobRecordIDs(seenIDs map[string]bool, job *models.Job) error {
	if err := registerImportedID(seenIDs, job.ID); err != nil {
		return err
	}
	for _, stage := range job.Stages {
		if err := registerImportedID(seenIDs, stage.ID); err != nil {
			return err
		}
		for _, question := range stage.Questions {
			if err := registerImportedID(seenIDs, question.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateArchiveReferences(manifest *backupManifest, files map[string]*zip.File, createdFiles *[]string, usedEntries map[string]bool) error {
	if len(manifest.AttachmentSHA256) != len(*createdFiles) {
		return errors.New("backup contains inconsistent attachment metadata")
	}
	logoAndCVFileCount := len(usedEntries) - len(*createdFiles) - 1
	uniqueCVPaths := map[string]bool{}
	for _, version := range manifest.CVVersions {
		uniqueCVPaths[version.Path] = true
	}
	if len(manifest.Logos) != logoAndCVFileCount-len(uniqueCVPaths) {
		return errors.New("backup contains inconsistent company logo metadata")
	}
	if len(usedEntries) != len(files) {
		return errors.New("backup contains unreferenced files")
	}
	return nil
}

func (s *JobService) stageImportedCVVersions(manifest *backupManifest, files map[string]*zip.File, createdFiles *[]string, usedEntries map[string]bool) error {
	if manifest.Version == 1 && len(manifest.CVVersions) > 0 {
		return errors.New("legacy backup unexpectedly contains CV versions")
	}
	versionsByID := make(map[string]models.CVVersion, len(manifest.CVVersions))
	paths := make(map[string]string)
	pathSizes := make(map[string]int64)
	for index := range manifest.CVVersions {
		if err := s.stageOneImportedCVVersion(&manifest.CVVersions[index], files, createdFiles, usedEntries, versionsByID, paths, pathSizes); err != nil {
			return err
		}
	}
	return finalizeImportedCVManifest(manifest, versionsByID)
}

func (s *JobService) stageOneImportedCVVersion(version *backupCVVersion, files map[string]*zip.File, createdFiles *[]string, usedEntries map[string]bool, versionsByID map[string]models.CVVersion, paths map[string]string, pathSizes map[string]int64) error {
	filename, err := validateImportedCVVersion(version)
	if err != nil {
		return err
	}
	if _, exists := versionsByID[version.ID]; exists {
		return errors.New("backup contains duplicate CV version IDs")
	}
	if err := registerImportedCVPath(version, filename, paths, pathSizes); err != nil {
		return err
	}
	version.StoredFilename = filename
	versionsByID[version.ID] = version.CVVersion
	if usedEntries[version.Path] {
		return nil
	}
	if err := s.installImportedCVBlob(*version, filename, files, createdFiles); err != nil {
		return err
	}
	usedEntries[version.Path] = true
	return nil
}

func validateImportedCVVersion(version *backupCVVersion) (string, error) {
	if invalidImportedCVFields(version) {
		return "", errors.New("backup contains invalid CV version metadata")
	}
	if _, err := hex.DecodeString(version.SHA256); err != nil {
		return "", errors.New("backup contains invalid CV checksum")
	}
	filename, err := backupStoredFilename(strings.TrimPrefix(version.Path, "cvs/"))
	if err != nil || version.Path != "cvs/"+filename || filename != strings.ToLower(version.SHA256) {
		return "", errors.New("backup contains an unsafe CV path")
	}
	return filename, nil
}

func invalidImportedCVFields(version *backupCVVersion) bool {
	return version.ID == "" || version.Version <= 0 || version.OriginalName == "" || version.FileSize < 0 || version.FileSize > maxAttachmentBytes || len(version.SHA256) != 64
}

func registerImportedCVPath(version *backupCVVersion, filename string, paths map[string]string, pathSizes map[string]int64) error {
	if prior, exists := paths[version.Path]; exists && (prior != version.SHA256 || pathSizes[version.Path] != version.FileSize) {
		return errors.New("backup contains conflicting duplicate CV blobs")
	}
	paths[version.Path] = version.SHA256
	pathSizes[version.Path] = version.FileSize
	return nil
}

func (s *JobService) installImportedCVBlob(version backupCVVersion, filename string, files map[string]*zip.File, createdFiles *[]string) error {
	zipFile := files[version.Path]
	// #nosec G115 -- FileSize is validated as nonnegative and bounded before this call.
	if zipFile == nil || zipFile.UncompressedSize64 != uint64(version.FileSize) {
		return fmt.Errorf("CV version %d file is missing or has an invalid size", version.Version)
	}
	destination := filepath.Join(s.cfg.CVDir, filename)
	// #nosec G304 -- filename is validated as a digest basename under configured CVDir.
	if existing, err := os.Open(destination); err == nil {
		defer func() { _ = existing.Close() }()
		if !sameFileHash(existing, version.SHA256) {
			return errors.New("existing CV blob failed checksum validation")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return extractAndInstallImportedCV(zipFile, destination, filename, version.FileSize, version.SHA256, createdFiles)
}

func extractAndInstallImportedCV(zipFile *zip.File, destination, filename string, size int64, digest string, createdFiles *[]string) error {
	tempPath := filepath.Join(filepath.Dir(destination), ".cv-import-"+uuid.NewString())
	actualHash, err := extractArchiveAttachment(zipFile, tempPath, size)
	if err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if !strings.EqualFold(actualHash, digest) {
		_ = os.Remove(tempPath)
		return errors.New("CV version failed its checksum check")
	}
	if err := os.Rename(tempPath, destination); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("install imported CV version: %w", err)
	}
	*createdFiles = append(*createdFiles, filename)
	return nil
}

func finalizeImportedCVManifest(manifest *backupManifest, versionsByID map[string]models.CVVersion) error {
	if err := validateImportedCVNumbers(manifest.CVVersions); err != nil {
		return err
	}
	maxVersion := 0
	for _, version := range manifest.CVVersions {
		if version.Version > maxVersion {
			maxVersion = version.Version
		}
	}
	if manifest.NextCVVersion == 0 {
		manifest.NextCVVersion = maxVersion + 1
	}
	if manifest.NextCVVersion < maxVersion+1 {
		return errors.New("backup CV sequence is lower than its saved versions")
	}
	for jobIndex := range manifest.Jobs {
		job := &manifest.Jobs[jobIndex]
		if job.SelectedCVVersion == nil {
			continue
		}
		version, ok := versionsByID[job.SelectedCVVersion.ID]
		if !ok {
			return fmt.Errorf("job %q references a missing CV version", job.ID)
		}
		job.SelectedCVVersion = &version
	}
	return nil
}

func validateImportedCVNumbers(versions []backupCVVersion) error {
	seenNumbers := make(map[int]bool, len(versions))
	for _, version := range versions {
		if seenNumbers[version.Version] {
			return errors.New("backup contains duplicate CV version numbers")
		}
		seenNumbers[version.Version] = true
	}
	return nil
}

func stageImportedLogos(logos []backupLogo, files map[string]*zip.File, stagingDir string, usedEntries map[string]bool) error {
	seenDomains := make(map[string]bool)
	for _, logo := range logos {
		if err := stageImportedLogo(logo, files, stagingDir, seenDomains, usedEntries); err != nil {
			return err
		}
	}
	return nil
}

func stageImportedLogo(logo backupLogo, files map[string]*zip.File, stagingDir string, seenDomains, usedEntries map[string]bool) error {
	archivePath, extension, ok := validArchiveLogo(logo)
	if !ok || seenDomains[logo.Domain] || usedEntries[archivePath] {
		return errors.New("backup contains an unsafe or duplicate company logo")
	}
	zipFile := files[archivePath]
	if zipFile == nil || logo.Size <= 0 || logo.Size > maxLogoBytes || zipFile.UncompressedSize64 != uint64(logo.Size) {
		return fmt.Errorf("company logo for %q is missing or has an invalid size", logo.Domain)
	}
	stagingPath := filepath.Join(stagingDir, logo.Domain+extension)
	actualHash, err := extractArchiveAttachment(zipFile, stagingPath, logo.Size)
	if err != nil || !strings.EqualFold(actualHash, logo.SHA256) {
		return fmt.Errorf("company logo for %q failed validation", logo.Domain)
	}
	seenDomains[logo.Domain] = true
	usedEntries[archivePath] = true
	return nil
}

func validArchiveLogo(logo backupLogo) (string, string, bool) {
	if logo.Domain == "" || normalizeLogoHost(logo.Domain) != logo.Domain {
		return "", "", false
	}
	for _, format := range cachedLogoFormats {
		archivePath := path.Join("logos", logo.Domain+format.extension)
		if logo.Path == archivePath && logo.MimeType == format.mimeType {
			return archivePath, format.extension, true
		}
	}
	return "", "", false
}

type installedLogo struct {
	destination string
	previous    string
}

func installImportedLogos(logos []backupLogo, stagingDir, logosDir string) ([]installedLogo, error) {
	installed := make([]installedLogo, 0, len(logos))
	for _, logo := range logos {
		_, extension, _ := validArchiveLogo(logo)
		destination := filepath.Join(logosDir, logo.Domain+extension)
		previous := filepath.Join(stagingDir, ".previous-"+logo.Domain+extension)
		if _, err := os.Lstat(destination); err == nil {
			if err := os.Rename(destination, previous); err != nil {
				rollbackImportedLogos(installed)
				return nil, fmt.Errorf("preserve existing logo for %q: %w", logo.Domain, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			rollbackImportedLogos(installed)
			return nil, fmt.Errorf("inspect existing logo for %q: %w", logo.Domain, err)
		} else {
			previous = ""
		}
		staged := filepath.Join(stagingDir, logo.Domain+extension)
		if err := os.Rename(staged, destination); err != nil {
			if previous != "" {
				_ = os.Rename(previous, destination)
			}
			rollbackImportedLogos(installed)
			return nil, fmt.Errorf("install imported logo for %q: %w", logo.Domain, err)
		}
		installed = append(installed, installedLogo{destination: destination, previous: previous})
	}
	return installed, nil
}

func rollbackImportedLogos(installed []installedLogo) {
	for index := len(installed) - 1; index >= 0; index-- {
		logo := installed[index]
		_ = os.Remove(logo.destination)
		if logo.previous != "" {
			_ = os.Rename(logo.previous, logo.destination)
		}
	}
}

func (s *JobService) stageImportedAttachment(manifest *backupManifest, job *models.Job, attachment *models.Attachment, files map[string]*zip.File, stagingDir string, createdFiles *[]string, usedEntries map[string]bool) error {
	archivePath, ok := validArchiveAttachmentPath(attachment.StoredFilename, attachment.ID)
	if !ok || usedEntries[archivePath] {
		return errors.New("backup contains an unsafe or duplicate attachment path")
	}
	zipFile := files[archivePath]
	if !validImportedAttachmentFile(zipFile, attachment.FileSize) {
		return fmt.Errorf("attachment %q is missing or has an invalid size", attachment.OriginalName)
	}
	expectedHash, ok := manifest.AttachmentSHA256[attachment.ID]
	if !ok {
		return fmt.Errorf("attachment %q has no checksum", attachment.OriginalName)
	}
	newName := fmt.Sprintf("%s-%d%s", uuid.NewString(), time.Now().UnixNano(), safeAttachmentExtension(attachment.OriginalName))
	tempPath := filepath.Join(stagingDir, newName)
	actualHash, err := extractArchiveAttachment(zipFile, tempPath, attachment.FileSize)
	if err != nil {
		return fmt.Errorf("extract attachment %q: %w", attachment.OriginalName, err)
	}
	if !strings.EqualFold(actualHash, expectedHash) {
		return fmt.Errorf("attachment %q failed its checksum check", attachment.OriginalName)
	}
	if err := os.Rename(tempPath, filepath.Join(s.cfg.AttachmentsDir, newName)); err != nil {
		return fmt.Errorf("stage attachment %q: %w", attachment.OriginalName, err)
	}
	*createdFiles = append(*createdFiles, newName)
	attachment.StoredFilename = newName
	attachment.JobID = job.ID
	usedEntries[archivePath] = true
	return nil
}

func validImportedAttachmentFile(file *zip.File, size int64) bool {
	return file != nil && size >= 0 && size <= maxAttachmentBytes && file.UncompressedSize64 == uint64(size)
}

func registerImportedID(seenIDs map[string]bool, id string) error {
	if id == "" || seenIDs[id] {
		return errors.New("backup contains a missing or duplicate record ID")
	}
	seenIDs[id] = true
	return nil
}

func inspectArchive(archive *zip.Reader) (map[string]*zip.File, *zip.File, uint64, error) {
	if len(archive.File) == 0 || len(archive.File) > maxArchiveEntries {
		return nil, nil, 0, errors.New("backup has an invalid number of files")
	}
	files := make(map[string]*zip.File, len(archive.File))
	var manifest *zip.File
	var expanded uint64
	for _, file := range archive.File {
		if err := inspectArchiveFile(file, files, expanded); err != nil {
			return nil, nil, 0, err
		}
		expanded += file.UncompressedSize64
		files[file.Name] = file
		if file.Name == "manifest.json" {
			manifest = file
		}
	}
	if manifest == nil {
		return nil, nil, 0, errors.New("backup is missing manifest.json")
	}
	return files, manifest, expanded, nil
}

func inspectArchiveFile(file *zip.File, files map[string]*zip.File, expanded uint64) error {
	name := file.Name
	if !safeArchivePath(name) {
		return errors.New("backup contains an unsafe file path")
	}
	if _, duplicate := files[name]; duplicate {
		return errors.New("backup contains duplicate file paths")
	}
	if file.Mode()&os.ModeType != 0 && !file.Mode().IsRegular() {
		return errors.New("backup contains a non-regular file")
	}
	if ^uint64(0)-expanded < file.UncompressedSize64 {
		return errors.New("backup size is invalid")
	}
	return nil
}

func safeArchivePath(name string) bool {
	return name != "" && !strings.Contains(name, "\\") && !strings.HasPrefix(name, "/") && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../")
}

func readArchiveEntry(file *zip.File, limit int64) ([]byte, error) {
	if limit < 0 || file.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("manifest exceeds the size limit")
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("manifest could not be read within the size limit")
	}
	return data, nil
}

func validArchiveAttachmentPath(value, id string) (string, bool) {
	if id == "" || strings.ContainsAny(id, `/\\`) {
		return "", false
	}
	parts := strings.Split(value, "/")
	if len(parts) != 3 || parts[0] != "attachments" || parts[1] != id || parts[2] == "" || parts[2] == "." || parts[2] == ".." {
		return "", false
	}
	return value, true
}

func extractArchiveAttachment(file *zip.File, destination string, expectedSize int64) (string, error) {
	input, err := file.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = input.Close() }()
	// #nosec G304 -- destination is a generated staging path within the configured upload directory.
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(input, expectedSize+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || written != expectedSize {
		_ = os.Remove(destination)
		return "", errors.New("attachment data is incomplete")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func portableFilename(value string) string {
	name := filepath.Base(strings.ReplaceAll(value, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		return "attachment"
	}
	return name
}
