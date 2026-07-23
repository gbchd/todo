package config

import (
	"path/filepath"
	"testing"
)

func TestLoadFrom_CreatesDefaultsOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.TUILayout != DefaultTUILayout {
		t.Errorf("TUILayout = %q, want %q", cfg.TUILayout, DefaultTUILayout)
	}
	if cfg.WebPort != DefaultWebPort {
		t.Errorf("WebPort = %d, want %d", cfg.WebPort, DefaultWebPort)
	}
	wantDB := filepath.Join(dir, dbName)
	if cfg.DBPath != wantDB {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, wantDB)
	}

	if _, err := LoadFrom(dir); err != nil {
		t.Fatalf("second LoadFrom: %v", err)
	}
}

func TestLoadFrom_ReadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	if err := save(path, Config{DBPath: "/custom/path.db", TUILayout: "kanban", WebPort: 9000}); err != nil {
		t.Fatalf("save: %v", err)
	}

	cfg, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.DBPath != "/custom/path.db" || cfg.TUILayout != "kanban" || cfg.WebPort != 9000 {
		t.Errorf("cfg = %+v, want values from file", cfg)
	}
}

func TestLoadFrom_FillsMissingFieldsWithDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	if err := save(path, Config{}); err != nil {
		t.Fatalf("save: %v", err)
	}

	cfg, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.TUILayout != DefaultTUILayout {
		t.Errorf("TUILayout = %q, want default", cfg.TUILayout)
	}
	if cfg.WebPort != DefaultWebPort {
		t.Errorf("WebPort = %d, want default", cfg.WebPort)
	}
}
