package backend_test

import (
	"path/filepath"
	"testing"
	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
)

func TestInitDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := &config.Config{DBPath: dbPath}
	db, err := database.InitDB(cfg)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { closeTestResource(t, db) })

	err = db.Ping()
	if err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}
