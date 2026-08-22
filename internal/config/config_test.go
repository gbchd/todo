package config

import (
	"os"
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
	require.Equal(t, filepath.Join(dir, dbName), cfg.DBPath)
}

// Every config file written before backends existed has no backend block, and
// those machines are local machines. Reading one must not change what they do.
func TestLoadFrom_TreatsAConfigWithNoBackendBlockAsLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	require.NoError(t, save(path, struct {
		DBPath    string `toml:"db_path"`
		TUILayout string `toml:"tui_layout"`
		WebPort   int    `toml:"web_port"`
	}{DBPath: "/custom/path.db", TUILayout: "kanban", WebPort: 9000}))

	cfg, err := LoadFrom(dir)
	require.NoError(t, err)
	require.Equal(t, BackendLocal, cfg.Backend.Kind)
	require.False(t, cfg.Backend.Remote())
	require.Equal(t, "/custom/path.db", cfg.DBPath, "the rest of the file must be read exactly as before")
	require.Equal(t, "kanban", cfg.TUILayout)
	require.Equal(t, 9000, cfg.WebPort)
}

func TestLoadFrom_DefaultsToTheLocalBackend(t *testing.T) {
	cfg, err := LoadFrom(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, BackendLocal, cfg.Backend.Kind)
	require.False(t, cfg.Backend.Remote())
}

// This is the block `todo pair` writes and #18 reads to pick a backend.
func TestSaveTo_RoundTripsARemoteBackend(t *testing.T) {
	dir := t.TempDir()
	want := Config{
		DBPath:    filepath.Join(dir, dbName),
		TUILayout: DefaultTUILayout,
		WebPort:   DefaultWebPort,
		Backend: Backend{
			Kind:    BackendRemote,
			HostURL: "http://192.168.1.10:8090",
			Secret:  "clientid.clientsecret",
		},
	}
	require.NoError(t, SaveTo(dir, want))

	got, err := LoadFrom(dir)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.True(t, got.Backend.Remote())
}

// Once the file holds this device's credential, no other account on the
// machine may read it.
func TestSaveTo_WritesOwnerOnlyPermissions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, SaveTo(dir, Config{Backend: Backend{Kind: BackendRemote, Secret: "id.secret"}}))

	info, err := os.Stat(filepath.Join(dir, fileName))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
