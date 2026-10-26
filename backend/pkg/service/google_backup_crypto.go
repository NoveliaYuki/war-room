package service

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	storeEnvelopeMagic   = "WRSTORE\x01"
	backupEnvelopeMagic  = "WRBACKUP\x01"
	backupEnvelopeAAD    = "war-room-backup-v1"
	legacyDriveBackupAAD = "war-room-drive-backup-v1"
	accountEnvelopeMagic = "WRACCOUNT\x01"
	accountEnvelopeAAD   = "war-room-drive-account-v1"
)

// ErrGoogleBackupKeyMissing indicates that a local 256-bit backup key is required.
var ErrGoogleBackupKeyMissing = errors.New("google backup encryption key is missing or invalid")

// ErrAuthTokenKeyMissing indicates that AUTH_TOKEN_ENCRYPTION_KEY, which
// protects stored Google refresh/access tokens at rest, is missing or
// invalid. This is a separate secret from GOOGLE_BACKUP_ENCRYPTION_KEY
// (which only protects portable .wrbackup export/import files) so that
// rotating one key never invalidates the other.
var ErrAuthTokenKeyMissing = errors.New("auth token encryption key is missing or invalid")

func (d *DriveBackupService) encryptionKey() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(d.cfg.GoogleBackupEncryptionKey))
	if err != nil || len(key) != 32 {
		return nil, ErrGoogleBackupKeyMissing
	}
	return key, nil
}

func (d *DriveBackupService) authTokenKey() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(d.cfg.AuthTokenEncryptionKey))
	if err != nil || len(key) != 32 {
		return nil, ErrAuthTokenKeyMissing
	}
	return key, nil
}

// EncryptBackup seals a small portable backup held in memory. Large archives
// must use EncryptBackupTo so they never have to fit in memory.
func (d *DriveBackupService) EncryptBackup(data []byte) ([]byte, error) {
	var sealed bytes.Buffer
	writer, err := d.EncryptBackupTo(&sealed)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return sealed.Bytes(), nil
}

// DecryptBackup opens a small encrypted backup held in memory. It accepts
// chunked backups and the older single-shot formats.
func (d *DriveBackupService) DecryptBackup(data []byte) ([]byte, error) {
	var plain bytes.Buffer
	if err := d.DecryptBackupTo(&plain, bytes.NewReader(data), int64(len(data))); err != nil {
		return nil, err
	}
	return plain.Bytes(), nil
}

func sealDriveData(key []byte, magic, aad string, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create backup cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create backup authenticator: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("create backup nonce: %w", err)
	}
	result := make([]byte, 0, len(magic)+len(nonce)+len(plaintext)+aead.Overhead())
	result = append(result, magic...)
	result = append(result, nonce...)
	result = aead.Seal(result, nonce, plaintext, []byte(aad))
	return result, nil
}

func openDriveData(key []byte, magic, aad string, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create backup cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create backup authenticator: %w", err)
	}
	minimumSize := len(magic) + aead.NonceSize() + aead.Overhead()
	if len(ciphertext) < minimumSize || string(ciphertext[:len(magic)]) != magic {
		return nil, errors.New("encrypted backup has an invalid format")
	}
	nonceStart := len(magic)
	nonceEnd := nonceStart + aead.NonceSize()
	plaintext, err := aead.Open(nil, ciphertext[nonceStart:nonceEnd], ciphertext[nonceEnd:], []byte(aad))
	if err != nil {
		return nil, errors.New("encrypted backup authentication failed")
	}
	return plaintext, nil
}
