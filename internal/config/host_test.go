package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadHostFrom_CreatesDefaultsOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadHostFrom(dir)
	require.NoError(t, err)
	require.Equal(t, DefaultHostAddr, cfg.ListenAddr)
	require.Equal(t, filepath.Join(dir, dbName), cfg.DBPath)
	require.Empty(t, cfg.Clients)

	_, err = LoadHostFrom(dir)
	require.NoError(t, err, "second LoadHostFrom")
}

// The host file is separate from the client's so a machine that is both keeps
// the two databases apart.
func TestLoadHostFrom_IsSeparateFromTheClientConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadHostFrom(dir)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, hostFileName))
	require.NoError(t, err, "host.toml must exist")
	_, err = os.Stat(filepath.Join(dir, fileName))
	require.True(t, os.IsNotExist(err), "loading host settings must not create config.toml")
}

// The file will hold credential material, so no other account on the machine
// may read it.
func TestLoadHostFrom_WritesOwnerOnlyPermissions(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadHostFrom(dir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, hostFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestSaveHostTo_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := HostConfig{
		ListenAddr: "0.0.0.0:9000",
		DBPath:     "/custom/path.db",
		Clients:    []HostClient{{ID: "abc", Name: "laptop", SecretHash: "hashed", CreatedAt: "2026-08-22T10:00:00Z"}},
	}
	require.NoError(t, SaveHostTo(dir, want))

	got, err := LoadHostFrom(dir)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestLoadHostFrom_FillsMissingFieldsWithDefaults(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, SaveHostTo(dir, HostConfig{}))

	cfg, err := LoadHostFrom(dir)
	require.NoError(t, err)
	assert.Equal(t, DefaultHostAddr, cfg.ListenAddr)
	assert.Equal(t, filepath.Join(dir, dbName), cfg.DBPath)
}

func TestHostConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     HostConfig
		wantErr bool
	}{
		{"loopback IPv4", HostConfig{ListenAddr: "127.0.0.1:8090"}, false},
		{"loopback IPv6", HostConfig{ListenAddr: "[::1]:8090"}, false},
		{"localhost", HostConfig{ListenAddr: "localhost:8090"}, false},
		{"every interface", HostConfig{ListenAddr: "0.0.0.0:8090"}, true},
		{"bare port", HostConfig{ListenAddr: ":8090"}, true},
		{"a specific LAN address", HostConfig{ListenAddr: "192.168.1.10:8090"}, true},
		{"an unresolved hostname", HostConfig{ListenAddr: "host.example:8090"}, true},
		{
			"non-loopback with a credential registered",
			HostConfig{ListenAddr: "0.0.0.0:8090", Clients: []HostClient{{ID: "abc"}}},
			false,
		},
		{"unparseable", HostConfig{ListenAddr: "nonsense"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}

// The refusal has to say why, not just that.
func TestHostConfig_ValidateExplainsTheRefusal(t *testing.T) {
	err := HostConfig{ListenAddr: "0.0.0.0:8090"}.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no device credentials are registered")
	assert.Contains(t, err.Error(), "loopback")
	assert.True(t, strings.Contains(err.Error(), "0.0.0.0:8090"), "the message names the address")
}
