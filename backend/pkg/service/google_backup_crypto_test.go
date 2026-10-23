package service

import (
	"bytes"
	"testing"

	"war-room/backend/pkg/config"
)

func TestGoogleBackupEncryptionRoundTripAndAuthentication(t *testing.T) {
	cfg := &config.Config{GoogleBackupEncryptionKey: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}
	drive := NewDriveBackupService(cfg, nil)
	plain := []byte("portable archive")
	ciphertext, err := drive.EncryptBackup(plain)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := drive.DecryptBackup(ciphertext)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatalf("decrypted=%q error=%v", opened, err)
	}
	legacy, err := sealDriveData([]byte("0123456789abcdef0123456789abcdef"), backupEnvelopeMagic, legacyDriveBackupAAD, plain)
	if err != nil {
		t.Fatal(err)
	}
	opened, err = drive.DecryptBackup(legacy)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatalf("legacy Drive backup decrypted=%q error=%v", opened, err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := drive.DecryptBackup(ciphertext); err == nil {
		t.Fatal("tampered ciphertext must not decrypt")
	}
	if _, err := drive.DecryptBackup([]byte("not encrypted")); err == nil {
		t.Fatal("invalid envelope must fail")
	}
}

func TestGoogleBackupEncryptionRequiresValidKey(t *testing.T) {
	drive := NewDriveBackupService(&config.Config{GoogleBackupEncryptionKey: "invalid"}, nil)
	if _, err := drive.EncryptBackup([]byte("backup")); err != ErrGoogleBackupKeyMissing {
		t.Fatalf("invalid encryption key error=%v", err)
	}
	if _, err := drive.DecryptBackup([]byte("backup")); err != ErrGoogleBackupKeyMissing {
		t.Fatalf("invalid key error=%v", err)
	}
}

func TestDriveCipherRejectsInvalidKeySizes(t *testing.T) {
	if _, err := sealDriveData([]byte("short"), backupEnvelopeMagic, "backup", nil); err == nil {
		t.Fatal("seal must reject an invalid AES key size")
	}
	if _, err := openDriveData([]byte("short"), backupEnvelopeMagic, "backup", nil); err == nil {
		t.Fatal("open must reject an invalid AES key size")
	}
}
