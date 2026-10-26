package service

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	// backupStreamMagic marks the chunked backup format. The older single-shot
	// format keeps backupEnvelopeMagic and is still accepted on import.
	backupStreamMagic = "WRBACKUP\x02"
	backupStreamInfo  = "war-room-backup-v2"
	backupSaltSize    = 16
	backupChunkSize   = 1 << 20
	backupGCMOverhead = 16
	backupHeaderSize  = len(backupStreamMagic) + backupSaltSize
	backupFinalChunk  = 1
	legacyNonceSize   = 12
)

var (
	// ErrBackupTooLarge reports that a backup exceeds the allowed plaintext size.
	ErrBackupTooLarge = errors.New("backup exceeds the allowed size")
	// ErrBackupFormat reports bytes that are not a War Room encrypted backup.
	ErrBackupFormat = errors.New("encrypted backup has an invalid format")
	// ErrBackupAuth reports a wrong key, tampering, or a truncated backup.
	ErrBackupAuth = errors.New("encrypted backup authentication failed")
)

// EncryptedBackupSize returns the exact byte length of a chunked backup that
// seals plainSize bytes, so callers can announce Content-Length up front.
func EncryptedBackupSize(plainSize int64) int64 {
	chunks := plainSize / backupChunkSize
	if plainSize == 0 || plainSize%backupChunkSize != 0 {
		chunks++
	}
	return int64(backupHeaderSize) + plainSize + chunks*backupGCMOverhead
}

// backupStreamAEAD derives the per-file AES-256-GCM key from the master key and
// the random salt so no two backups ever share a key and counter nonces are safe.
func backupStreamAEAD(masterKey, salt []byte) (cipher.AEAD, error) {
	if len(masterKey) != 32 {
		return nil, ErrGoogleBackupKeyMissing
	}
	fileKey, err := hkdf.Key(sha256.New, masterKey, salt, backupStreamInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("derive backup key: %w", err)
	}
	block, err := aes.NewCipher(fileKey)
	if err != nil {
		return nil, fmt.Errorf("create backup cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create backup authenticator: %w", err)
	}
	return aead, nil
}

// chunkSealer holds the state shared by the writer and reader: the chunk
// counter becomes the nonce and the header plus final flag become the AAD.
type chunkSealer struct {
	aead    cipher.AEAD
	header  []byte
	counter uint64
}

func (c *chunkSealer) nonceAndAAD(final bool) ([]byte, []byte) {
	nonce := make([]byte, c.aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], c.counter)
	aad := make([]byte, len(c.header)+1)
	copy(aad, c.header)
	if final {
		aad[len(c.header)] = backupFinalChunk
	}
	c.counter++
	return nonce, aad
}

// backupStreamWriter encrypts everything written to it into fixed-size chunks.
// The last chunk is held back until Close so it can be flagged as final.
type backupStreamWriter struct {
	dst    io.Writer
	sealer chunkSealer
	buffer []byte
	out    []byte
	err    error
	closed bool
}

// newBackupStreamWriter writes the header to dst and returns the encrypting writer.
func newBackupStreamWriter(dst io.Writer, masterKey []byte) (*backupStreamWriter, error) {
	salt := make([]byte, backupSaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("create backup salt: %w", err)
	}
	aead, err := backupStreamAEAD(masterKey, salt)
	if err != nil {
		return nil, err
	}
	header := append([]byte(backupStreamMagic), salt...)
	if _, err := dst.Write(header); err != nil {
		return nil, err
	}
	return &backupStreamWriter{
		dst:    dst,
		sealer: chunkSealer{aead: aead, header: header},
		buffer: make([]byte, 0, backupChunkSize),
		out:    make([]byte, 0, backupChunkSize+backupGCMOverhead),
	}, nil
}

func (w *backupStreamWriter) Write(data []byte) (int, error) {
	if w.err != nil || w.closed {
		return 0, errors.Join(w.err, errors.New("backup writer is closed"))
	}
	written := 0
	for len(data) > 0 {
		if len(w.buffer) == backupChunkSize {
			if w.err = w.flush(false); w.err != nil {
				return written, w.err
			}
		}
		take := min(backupChunkSize-len(w.buffer), len(data))
		w.buffer = append(w.buffer, data[:take]...)
		data = data[take:]
		written += take
	}
	return written, nil
}

func (w *backupStreamWriter) flush(final bool) error {
	nonce, aad := w.sealer.nonceAndAAD(final)
	w.out = w.sealer.aead.Seal(w.out[:0], nonce, w.buffer, aad)
	w.buffer = w.buffer[:0]
	_, err := w.dst.Write(w.out)
	return err
}

// Close seals the remaining bytes as the final chunk. It does not close dst.
func (w *backupStreamWriter) Close() error {
	if w.closed {
		return w.err
	}
	w.closed = true
	if w.err == nil {
		w.err = w.flush(true)
	}
	return w.err
}

// backupStreamReader authenticates and decrypts a chunked backup one chunk at a
// time. A chunk is only released after its tag verifies, and the final flag is
// derived from whether more bytes follow, so truncation and trailing data fail.
type backupStreamReader struct {
	src      *bufio.Reader
	sealer   chunkSealer
	cipher   []byte
	plain    []byte
	pending  []byte
	finished bool
}

// newBackupStreamReader reads the header from src and returns the decrypting reader.
func newBackupStreamReader(src *bufio.Reader, masterKey []byte) (*backupStreamReader, error) {
	header := make([]byte, backupHeaderSize)
	if _, err := io.ReadFull(src, header); err != nil || string(header[:len(backupStreamMagic)]) != backupStreamMagic {
		return nil, ErrBackupFormat
	}
	aead, err := backupStreamAEAD(masterKey, header[len(backupStreamMagic):])
	if err != nil {
		return nil, err
	}
	return &backupStreamReader{
		src:    src,
		sealer: chunkSealer{aead: aead, header: header},
		cipher: make([]byte, backupChunkSize+backupGCMOverhead),
		plain:  make([]byte, 0, backupChunkSize),
	}, nil
}

func (r *backupStreamReader) Read(out []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.finished {
			return 0, io.EOF
		}
		if err := r.nextChunk(); err != nil {
			return 0, err
		}
	}
	n := copy(out, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *backupStreamReader) nextChunk() error {
	read, err := io.ReadFull(r.src, r.cipher)
	final := false
	switch {
	case err == nil:
		_, peekErr := r.src.Peek(1)
		if peekErr != nil && !errors.Is(peekErr, io.EOF) {
			return peekErr
		}
		final = peekErr != nil
	case errors.Is(err, io.ErrUnexpectedEOF):
		final = true
	case errors.Is(err, io.EOF):
		return ErrBackupAuth
	default:
		return err
	}
	if read < backupGCMOverhead {
		return ErrBackupAuth
	}
	nonce, aad := r.sealer.nonceAndAAD(final)
	plain, openErr := r.sealer.aead.Open(r.plain[:0], nonce, r.cipher[:read], aad)
	if openErr != nil {
		return ErrBackupAuth
	}
	r.plain, r.pending, r.finished = plain, plain, final
	return nil
}

// EncryptBackupTo returns a writer that seals a portable backup into dst using
// constant memory. The caller must Close it to write the final chunk.
func (d *DriveBackupService) EncryptBackupTo(dst io.Writer) (io.WriteCloser, error) {
	key, err := d.encryptionKey()
	if err != nil {
		return nil, err
	}
	return newBackupStreamWriter(dst, key)
}

// DecryptBackupTo authenticates and decrypts a portable backup from src into
// dst, stopping with ErrBackupTooLarge past maxPlain bytes. Chunked backups
// stream in constant memory; older single-shot backups must be read whole.
// Nothing written to dst is trustworthy until the call returns nil.
func (d *DriveBackupService) DecryptBackupTo(dst io.Writer, src io.Reader, maxPlain int64) error {
	buffered := bufio.NewReader(src)
	magic, err := buffered.Peek(len(backupStreamMagic))
	if err != nil {
		return ErrBackupFormat
	}
	chunked := string(magic) == backupStreamMagic
	if !chunked && string(magic) != backupEnvelopeMagic {
		return ErrBackupFormat
	}
	key, err := d.encryptionKey()
	if err != nil {
		return err
	}
	if !chunked {
		return decryptLegacyBackupTo(dst, buffered, key, maxPlain)
	}
	reader, err := newBackupStreamReader(buffered, key)
	if err != nil {
		return err
	}
	return copyBounded(dst, reader, maxPlain)
}

// decryptLegacyBackupTo opens the single-shot format, which cannot be
// authenticated without holding the whole ciphertext in memory.
func decryptLegacyBackupTo(dst io.Writer, src io.Reader, key []byte, maxPlain int64) error {
	limit := maxPlain + int64(len(backupEnvelopeMagic)) + legacyNonceSize + backupGCMOverhead
	sealed, err := io.ReadAll(io.LimitReader(src, limit+1))
	if err != nil {
		return err
	}
	if int64(len(sealed)) > limit {
		return ErrBackupTooLarge
	}
	plain, err := openLegacyBackup(key, sealed)
	if err != nil {
		return err
	}
	_, err = dst.Write(plain)
	return err
}

func openLegacyBackup(key, sealed []byte) ([]byte, error) {
	plain, err := openDriveData(key, backupEnvelopeMagic, backupEnvelopeAAD, sealed)
	if err == nil {
		return plain, nil
	}
	plain, err = openDriveData(key, backupEnvelopeMagic, legacyDriveBackupAAD, sealed)
	if err != nil {
		return nil, ErrBackupAuth
	}
	return plain, nil
}

// copyBounded copies src to dst and fails once more than limit bytes arrive.
func copyBounded(dst io.Writer, src io.Reader, limit int64) error {
	copied, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return err
	}
	if copied > limit {
		return ErrBackupTooLarge
	}
	return nil
}

// SealArchiveFile runs write against an encrypting writer that spills into a
// private temp file in dir, so an archive is never held in memory. Plaintext
// beyond limit fails with ErrBackupTooLarge. On success the returned file is
// rewound and sized for Content-Length; the caller must DiscardTempFile it.
func (d *DriveBackupService) SealArchiveFile(dir string, limit int64, write func(io.Writer) error) (*os.File, int64, error) {
	key, err := d.encryptionKey()
	if err != nil {
		return nil, 0, err
	}
	file, err := os.CreateTemp(dir, ".war-room-backup-*.wrbackup")
	if err != nil {
		return nil, 0, fmt.Errorf("prepare backup file: %w", err)
	}
	size, err := sealToFile(file, key, limit, write)
	if err != nil {
		DiscardTempFile(file)
		return nil, 0, err
	}
	return file, size, nil
}

func sealToFile(file *os.File, key []byte, limit int64, write func(io.Writer) error) (int64, error) {
	sealer, err := newBackupStreamWriter(file, key)
	if err != nil {
		return 0, err
	}
	if err := write(&boundedWriter{writer: sealer, limit: limit}); err != nil {
		return 0, err
	}
	if err := sealer.Close(); err != nil {
		return 0, fmt.Errorf("seal backup archive: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// RemoveStaleBackupTempFiles deletes backup staging files that a crash left in
// dir. Call it before serving, while no transfer can be using them.
func RemoveStaleBackupTempFiles(dir string) {
	for _, pattern := range []string{".war-room-backup-*.wrbackup", ".war-room-import-*.zip"} {
		stale, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, name := range stale {
			_ = os.Remove(name)
		}
	}
}

// DiscardTempFile closes and removes a temp file created by this package.
func DiscardTempFile(file *os.File) {
	_ = file.Close()
	_ = os.Remove(file.Name())
}

// boundedWriter forwards at most limit bytes and then fails with ErrBackupTooLarge.
type boundedWriter struct {
	writer  io.Writer
	limit   int64
	written int64
}

func (writer *boundedWriter) Write(data []byte) (int, error) {
	remaining := writer.limit - writer.written
	if int64(len(data)) > remaining {
		if remaining <= 0 {
			return 0, ErrBackupTooLarge
		}
		length := int(remaining)
		written, err := writer.writer.Write(data[:length])
		writer.written += int64(written)
		if err != nil {
			return written, err
		}
		if written < length {
			return written, io.ErrShortWrite
		}
		return written, ErrBackupTooLarge
	}
	written, err := writer.writer.Write(data)
	writer.written += int64(written)
	if err == nil && written < len(data) {
		err = io.ErrShortWrite
	}
	return written, err
}
