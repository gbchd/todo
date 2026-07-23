package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFrom_CreatesDefaultsOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadFrom(dir)
	require.NoError(t, err)
	require.Equal(t, DefaultTUILayout, cfg.TUILayout)
	require.Equal(t, DefaultWebPort, cfg.WebPort)
	require.Equal(t, filepath.Join(dir, dbName), cfg.DBPath)

	_, err = LoadFrom(dir)
	require.NoError(t, err, "second LoadFrom")
}

func TestLoadFrom_ReadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	require.NoError(t, save(path, Config{DBPath: "/custom/path.db", TUILayout: "kanban", WebPort: 9000}))

	cfg, err := LoadFrom(dir)
	require.NoError(t, err)
	require.Equal(t, "/custom/path.db", cfg.DBPath)
	require.Equal(t, "kanban", cfg.TUILayout)
	require.Equal(t, 9000, cfg.WebPort)
}

func TestLoadFrom_FillsMissingFieldsWithDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	require.NoError(t, save(path, Config{}))

	cfg, err := LoadFrom(dir)
	require.NoError(t, err)
	require.Equal(t, DefaultTUILayout, cfg.TUILayout)
	require.Equal(t, DefaultWebPort, cfg.WebPort)
}
