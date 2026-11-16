package service

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/iotest"

	"war-room/backend/pkg/config"
)

const streamTestKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func newStreamTestDrive() *DriveBackupService {
	return NewDriveBackupService(nil, &config.Config{GoogleBackupEncryptionKey: streamTestKey}, nil)
}

func streamPayload(t *testing.T, size int) []byte {
	t.Helper()
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func sealStream(t *testing.T, drive *DriveBackupService, plain []byte) []byte {
	t.Helper()
	sealed, err := drive.EncryptBackup(plain)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func TestBackupStreamRoundTripsEveryChunkBoundary(t *testing.T) {
	drive := newStreamTestDrive()
	for _, size := range []int{0, 1, backupChunkSize - 1, backupChunkSize, backupChunkSize + 1, 2 * backupChunkSize, 2*backupChunkSize + 5} {
		plain := streamPayload(t, size)
		sealed := sealStream(t, drive, plain)
		if int64(len(sealed)) != EncryptedBackupSize(int64(size)) {
			t.Fatalf("size %d: sealed %d bytes, EncryptedBackupSize says %d", size, len(sealed), EncryptedBackupSize(int64(size)))
		}
		opened, err := drive.DecryptBackup(sealed)
		if err != nil || !bytes.Equal(opened, plain) {
			t.Fatalf("size %d: round trip failed: err=%v equal=%v", size, err, bytes.Equal(opened, plain))
		}
	}
}

func TestBackupStreamSealsDifferentlyEachTime(t *testing.T) {
	drive := newStreamTestDrive()
	plain := []byte("same plaintext")
	if bytes.Equal(sealStream(t, drive, plain), sealStream(t, drive, plain)) {
		t.Fatal("two backups of the same data must not be identical")
	}
}

func TestBackupStreamRejectsTruncationReorderingAndTrailingData(t *testing.T) {
	drive := newStreamTestDrive()
	plain := streamPayload(t, 3*backupChunkSize+10)
	sealed := sealStream(t, drive, plain)
	chunk := backupChunkSize + backupGCMOverhead
	cases := map[string][]byte{
		"missing everything after the header": sealed[:backupHeaderSize],
		"cut at a chunk boundary":             sealed[:backupHeaderSize+2*chunk],
		"cut inside the final chunk":          sealed[:len(sealed)-5],
		"cut to a bare tag":                   sealed[:backupHeaderSize+2*chunk+backupGCMOverhead-1],
		"trailing garbage":                    append(bytes.Clone(sealed), 0),
		"trailing full chunk":                 append(bytes.Clone(sealed), make([]byte, chunk)...),
		"flipped byte in the first chunk":     flipByte(sealed, backupHeaderSize+3),
		"flipped byte in the salt":            flipByte(sealed, len(backupStreamMagic)+1),
		"swapped first and second chunk":      swapChunks(sealed, chunk),
	}
	for name, data := range cases {
		if _, err := drive.DecryptBackup(data); !errors.Is(err, ErrBackupAuth) {
			t.Errorf("%s: error=%v, want ErrBackupAuth", name, err)
		}
	}
}

func flipByte(data []byte, index int) []byte {
	changed := bytes.Clone(data)
	changed[index] ^= 1
	return changed
}

func swapChunks(sealed []byte, chunk int) []byte {
	changed := bytes.Clone(sealed)
	first := changed[backupHeaderSize : backupHeaderSize+chunk]
	second := changed[backupHeaderSize+chunk : backupHeaderSize+2*chunk]
	swapped := append(bytes.Clone(second), first...)
	copy(changed[backupHeaderSize:], swapped)
	return changed
}

func TestBackupStreamRejectsWrongKeyAndMalformedInput(t *testing.T) {
	drive := newStreamTestDrive()
	sealed := sealStream(t, drive, []byte("payload"))
	other := NewDriveBackupService(nil, &config.Config{GoogleBackupEncryptionKey: "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVoxMjM0NTY="}, nil)
	if _, err := other.DecryptBackup(sealed); !errors.Is(err, ErrBackupAuth) {
		t.Fatalf("wrong key error=%v", err)
	}
	for name, data := range map[string][]byte{
		"empty":             nil,
		"short":             []byte("WRBACK"),
		"plain zip":         []byte("PK\x03\x04 not a backup at all"),
		"header only magic": []byte(backupStreamMagic),
	} {
		if _, err := drive.DecryptBackup(data); err == nil {
			t.Errorf("%s: malformed input must fail", name)
		}
	}
	if _, err := drive.DecryptBackup([]byte("PK\x03\x04 not a backup at all")); !errors.Is(err, ErrBackupFormat) {
		t.Fatalf("plain ZIP error=%v, want ErrBackupFormat", err)
	}
}

func TestBackupStreamReportsMissingKeyOnlyForRealBackups(t *testing.T) {
	broken := NewDriveBackupService(nil, &config.Config{GoogleBackupEncryptionKey: "invalid"}, nil)
	sealed := sealStream(t, newStreamTestDrive(), []byte("payload"))
	if err := broken.DecryptBackupTo(io.Discard, bytes.NewReader(sealed), 100); !errors.Is(err, ErrGoogleBackupKeyMissing) {
		t.Fatalf("backup with missing key error=%v", err)
	}
	if err := broken.DecryptBackupTo(io.Discard, bytes.NewReader([]byte("PK\x03\x04 not a backup")), 100); !errors.Is(err, ErrBackupFormat) {
		t.Fatalf("plain input with missing key error=%v", err)
	}
	if _, err := broken.EncryptBackupTo(io.Discard); !errors.Is(err, ErrGoogleBackupKeyMissing) {
		t.Fatalf("encrypt with missing key error=%v", err)
	}
}

func TestBackupStreamEnforcesPlaintextLimit(t *testing.T) {
	drive := newStreamTestDrive()
	sealed := sealStream(t, drive, streamPayload(t, 2*backupChunkSize))
	if err := drive.DecryptBackupTo(io.Discard, bytes.NewReader(sealed), backupChunkSize); !errors.Is(err, ErrBackupTooLarge) {
		t.Fatalf("streamed limit error=%v", err)
	}
	if err := drive.DecryptBackupTo(io.Discard, bytes.NewReader(sealed), 2*backupChunkSize); err != nil {
		t.Fatalf("exact limit must pass: %v", err)
	}
	legacy := legacySealForTest(t, bytes.Repeat([]byte("x"), 100))
	if err := drive.DecryptBackupTo(io.Discard, bytes.NewReader(legacy), 99); !errors.Is(err, ErrBackupTooLarge) {
		t.Fatalf("legacy limit error=%v", err)
	}
}

func legacySealForTest(t *testing.T, plain []byte) []byte {
	t.Helper()
	sealed, err := sealDriveData([]byte("0123456789abcdef0123456789abcdef"), backupEnvelopeMagic, backupEnvelopeAAD, plain)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func TestBackupStreamStillOpensLegacyFormats(t *testing.T) {
	drive := newStreamTestDrive()
	plain := []byte("legacy archive")
	for name, aad := range map[string]string{"portable": backupEnvelopeAAD, "drive": legacyDriveBackupAAD} {
		sealed, err := sealDriveData([]byte("0123456789abcdef0123456789abcdef"), backupEnvelopeMagic, aad, plain)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := drive.DecryptBackup(sealed)
		if err != nil || !bytes.Equal(opened, plain) {
			t.Fatalf("%s legacy backup: %q, %v", name, opened, err)
		}
		if _, err := drive.DecryptBackup(flipByte(sealed, len(sealed)-1)); !errors.Is(err, ErrBackupAuth) {
			t.Fatalf("%s tampered legacy backup error=%v", name, err)
		}
	}
}

type failAfterWriter struct {
	remaining int
	err       error
}

func (writer *failAfterWriter) Write(data []byte) (int, error) {
	if writer.remaining <= 0 {
		return 0, writer.err
	}
	writer.remaining--
	return len(data), nil
}

func TestBackupStreamWriterPropagatesDestinationErrors(t *testing.T) {
	boom := errors.New("disk full")
	key := bytes.Repeat([]byte{7}, 32)
	if _, err := newBackupStreamWriter(&failAfterWriter{err: boom}, key); !errors.Is(err, boom) {
		t.Fatalf("header write error=%v", err)
	}
	writer, err := newBackupStreamWriter(&failAfterWriter{remaining: 1, err: boom}, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(make([]byte, backupChunkSize+1)); !errors.Is(err, boom) {
		t.Fatalf("chunk write error=%v", err)
	}
	if err := writer.Close(); !errors.Is(err, boom) {
		t.Fatalf("close after failure error=%v", err)
	}
	closing, err := newBackupStreamWriter(&failAfterWriter{remaining: 1, err: boom}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := closing.Close(); !errors.Is(err, boom) {
		t.Fatalf("final chunk write error=%v", err)
	}
}

func TestBackupStreamWriterLifecycle(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	healthy, err := newBackupStreamWriter(io.Discard, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := healthy.Close(); err != nil {
		t.Fatal(err)
	}
	if err := healthy.Close(); err != nil {
		t.Fatalf("second close must be a no-op: %v", err)
	}
	if _, err := healthy.Write([]byte("late")); err == nil {
		t.Fatal("write after close must fail")
	}
	if _, err := newBackupStreamWriter(io.Discard, []byte("short")); err == nil {
		t.Fatal("an invalid key size must be rejected")
	}
}

func TestBackupStreamReaderPropagatesSourceErrors(t *testing.T) {
	drive := newStreamTestDrive()
	sealed := sealStream(t, drive, streamPayload(t, 2*backupChunkSize))
	boom := errors.New("connection reset")
	err := drive.DecryptBackupTo(io.Discard, io.MultiReader(bytes.NewReader(sealed[:backupHeaderSize+10]), iotest.ErrReader(boom)), 1<<30)
	if !errors.Is(err, boom) {
		t.Fatalf("mid-chunk read error=%v", err)
	}
	full := sealed[:backupHeaderSize+backupChunkSize+backupGCMOverhead]
	err = drive.DecryptBackupTo(io.Discard, io.MultiReader(bytes.NewReader(full), iotest.ErrReader(boom)), 1<<30)
	if !errors.Is(err, boom) {
		t.Fatalf("lookahead read error=%v", err)
	}
	key := bytes.Repeat([]byte{7}, 32)
	if _, err := newBackupStreamReader(bufio.NewReader(bytes.NewReader(sealed)), []byte("short")); err == nil {
		t.Fatal("an invalid key size must be rejected")
	}
	if _, err := newBackupStreamReader(bufio.NewReader(bytes.NewReader([]byte("short"))), key); !errors.Is(err, ErrBackupFormat) {
		t.Fatalf("short header error=%v", err)
	}
}

func TestSealArchiveFileStreamsToATempFile(t *testing.T) {
	drive := newStreamTestDrive()
	dir := t.TempDir()
	plain := streamPayload(t, backupChunkSize+123)
	file, size, err := drive.SealArchiveFile(dir, int64(len(plain)), func(out io.Writer) error {
		_, err := out.Write(plain)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := io.ReadAll(file)
	if err != nil || int64(len(sealed)) != size || size != EncryptedBackupSize(int64(len(plain))) {
		t.Fatalf("sealed %d bytes (reported %d, expected %d) err=%v", len(sealed), size, EncryptedBackupSize(int64(len(plain))), err)
	}
	opened, err := drive.DecryptBackup(sealed)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatalf("sealed file did not round trip: %v", err)
	}
	DiscardTempFile(file)
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("temp file not removed: %v", entries)
	}
}

func TestSealArchiveFileCleansUpAfterFailures(t *testing.T) {
	drive := newStreamTestDrive()
	dir := t.TempDir()
	boom := errors.New("export failed")
	for name, test := range map[string]struct {
		limit int64
		write func(io.Writer) error
		want  error
	}{
		"writer error": {limit: 10, write: func(io.Writer) error { return boom }, want: boom},
		"over limit":   {limit: 3, write: func(out io.Writer) error { _, err := out.Write([]byte("four")); return err }, want: ErrBackupTooLarge},
	} {
		if _, _, err := drive.SealArchiveFile(dir, test.limit, test.write); !errors.Is(err, test.want) {
			t.Errorf("%s: error=%v, want %v", name, err, test.want)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("failed seals left files behind: %v", entries)
	}
	if _, _, err := drive.SealArchiveFile(filepath.Join(dir, "missing"), 10, func(io.Writer) error { return nil }); err == nil {
		t.Fatal("a missing temp directory must fail")
	}
	broken := NewDriveBackupService(nil, &config.Config{GoogleBackupEncryptionKey: "invalid"}, nil)
	if _, _, err := broken.SealArchiveFile(dir, 10, func(io.Writer) error { return nil }); !errors.Is(err, ErrGoogleBackupKeyMissing) {
		t.Fatalf("missing key error=%v", err)
	}
}

func TestSealToFileReportsStorageFailures(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sealToFile(closed, key, 10, func(io.Writer) error { return nil }); err == nil {
		t.Fatal("a closed destination must fail")
	}
	file, err := os.CreateTemp(t.TempDir(), "open")
	if err != nil {
		t.Fatal(err)
	}
	defer DiscardTempFile(file)
	if _, err := sealToFile(file, []byte("short"), 10, func(io.Writer) error { return nil }); err == nil {
		t.Fatal("an invalid key must fail")
	}
}

func TestBoundedWriterStopsAtConfiguredLimit(t *testing.T) {
	var output bytes.Buffer
	writer := &boundedWriter{writer: &output, limit: 5}
	if n, err := writer.Write([]byte("1234")); n != 4 || err != nil {
		t.Fatalf("write before limit=(%d, %v), want (4, nil)", n, err)
	}
	if n, err := writer.Write([]byte("5678")); n != 1 || !errors.Is(err, ErrBackupTooLarge) {
		t.Fatalf("write beyond limit=(%d, %v), want (1, limit error)", n, err)
	}
	if output.String() != "12345" {
		t.Fatalf("bounded output=%q, want exactly five bytes", output.String())
	}
	if n, err := writer.Write([]byte("9")); n != 0 || !errors.Is(err, ErrBackupTooLarge) {
		t.Fatalf("write after limit=(%d, %v), want (0, limit error)", n, err)
	}
}

type alwaysFailWriter struct{ err error }

func (writer alwaysFailWriter) Write([]byte) (int, error) { return 0, writer.err }

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

func TestBoundedWriterPropagatesUnderlyingWriteFailures(t *testing.T) {
	writeErr := errors.New("disk full")
	tests := []struct {
		name   string
		writer io.Writer
		data   string
		limit  int64
		want   error
	}{
		{name: "underlying error before limit", writer: alwaysFailWriter{err: writeErr}, data: "x", limit: 5, want: writeErr},
		{name: "underlying error while enforcing limit", writer: alwaysFailWriter{err: writeErr}, data: "123456", limit: 5, want: writeErr},
		{name: "short write before limit", writer: shortWriter{}, data: "xy", limit: 5, want: io.ErrShortWrite},
		{name: "short write while enforcing limit", writer: shortWriter{}, data: "123456", limit: 5, want: io.ErrShortWrite},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer := &boundedWriter{writer: test.writer, limit: test.limit}
			if _, err := writer.Write([]byte(test.data)); !errors.Is(err, test.want) {
				t.Fatalf("write error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestSealToFileReportsUnseekableDestinations(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	key := bytes.Repeat([]byte{7}, 32)
	if _, err := sealToFile(writer, key, 10, func(out io.Writer) error { _, err := out.Write([]byte("tiny")); return err }); err == nil {
		t.Fatal("a destination that cannot be rewound must fail")
	}
}

func TestRemoveStaleBackupTempFilesOnlyRemovesStagingFiles(t *testing.T) {
	dir := t.TempDir()
	for name, remove := range map[string]bool{
		".war-room-backup-123.wrbackup": true,
		".war-room-import-456.zip":      true,
		"war-room.db":                   false,
		"backup.json":                   false,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		defer func(name string, remove bool) {
			_, err := os.Stat(filepath.Join(dir, name))
			if remove == (err == nil) {
				t.Errorf("%s: removed=%v, want %v", name, err != nil, remove)
			}
		}(name, remove)
	}
	RemoveStaleBackupTempFiles(dir)
}
