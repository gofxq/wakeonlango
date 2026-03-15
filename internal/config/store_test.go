package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewStoreCreatesDefaultConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	cfg := store.Snapshot()
	if cfg.Title != "WOL 控制台" {
		t.Fatalf("cfg.Title = %q, want default title", cfg.Title)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected config file contents")
	}
}
