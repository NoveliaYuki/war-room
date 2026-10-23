package service

import (
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
)

// ErrGoogleBackupKeyMissing indicates that a local 256-bit backup key is required.
var ErrGoogleBackupKeyMissing = errors.New("google backup encryption key is missing or invalid")

func (d *DriveBackupService) encryptionKey() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(d.cfg.GoogleBackupEncryptionKey))
	if err != nil || len(key) != 32 {
		return nil, ErrGoogleBackupKeyMissing
	}
	return key, nil
}

// EncryptBackup seals a portable backup archive for download or Drive storage.
func (d *DriveBackupService) EncryptBackup(data []byte) ([]byte, error) {
	key, err := d.encryptionKey()
	if err != nil {
		return nil, err
	}
	return sealDriveData(key, backupEnvelopeMagic, backupEnvelopeAAD, data)
}

// DecryptBackup opens current portable backups and older Drive backup files.
func (d *DriveBackupService) DecryptBackup(data []byte) ([]byte, error) {
	key, err := d.encryptionKey()
	if err != nil {
		return nil, err
	}
	opened, err := openDriveData(key, backupEnvelopeMagic, backupEnvelopeAAD, data)
	if err == nil {
		return opened, nil
	}
	return openDriveData(key, backupEnvelopeMagic, legacyDriveBackupAAD, data)
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
