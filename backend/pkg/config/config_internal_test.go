package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAbsoluteDataDirectoryFallsBackWhenWorkingDirectoryDisappears(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	removedDirectory := t.TempDir()
	if err := os.Chdir(removedDirectory); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(workingDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()
	if err := os.RemoveAll(removedDirectory); err != nil {
		t.Fatal(err)
	}
	if got := absoluteDataDirectory(filepath.Join("relative", "data")); got != filepath.Join("relative", "data") {
		t.Fatalf("fallback path=%q", got)
	}
}

func TestCreateDataDirectoriesReturnsFilesystemErrors(t *testing.T) {
	root := t.TempDir()
	blockingFile := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := createDataDirectories(&Config{
		DataDir: blockingFile, AttachmentsDir: filepath.Join(root, "attachments"), LogosDir: filepath.Join(root, "logos"),
	}); err == nil {
		t.Fatal("directory creation under a file should fail")
	}
}
