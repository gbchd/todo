package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/config"
	"github.com/gbchd/todo/internal/service/todo"
)

// hostRun records what `todo host` would have served, so the tests assert the
// settings the command resolved without binding a port.
type hostRun struct {
	called bool
	addr   string
	svc    *todo.Service
}

// runHostCLI drives `todo host` with $HOME pointed at a temp directory, which
// is where config.LoadHost looks for host.toml. clientDB is the --db path the
// root command is given; the host must ignore it.
func runHostCLI(t *testing.T, home, clientDB string, args ...string) (rec *hostRun, stderr string, code int) {
	t.Helper()
	t.Setenv("HOME", home)

	rec = &hostRun{}
	var outBuf, errBuf bytes.Buffer
	tui, serve, _ := noopLaunchers()
	launch := func(_ context.Context, svc *todo.Service, addr string, _ io.Writer) error {
		rec.called, rec.addr, rec.svc = true, addr, svc
		return nil
	}
	full := append([]string{"todo", "--db", clientDB, "host"}, args...)
	code = Run(context.Background(), full, strings.NewReader(""), &outBuf, &errBuf, config.Config{}, tui, serve, launch)
	return rec, errBuf.String(), code
}

func TestHost_DefaultsToLoopback(t *testing.T) {
	home := t.TempDir()
	rec, stderr, code := runHostCLI(t, home, newDB(t))
	require.Equal(t, 0, code, "stderr=%s", stderr)
	require.True(t, rec.called)
	assert.Equal(t, config.DefaultHostAddr, rec.addr)
}

// The host's database is the one host.toml names, not the client's --db: that
// separation is the whole reason host settings live in their own file.
func TestHost_UsesItsOwnDatabaseAndLeavesTheClientsAlone(t *testing.T) {
	home := t.TempDir()
	clientDB := newDB(t)

	rec, stderr, code := runHostCLI(t, home, clientDB)
	require.Equal(t, 0, code, "stderr=%s", stderr)
	require.True(t, rec.called)

	_, err := os.Stat(filepath.Join(home, ".todo", "todo.db"))
	require.NoError(t, err, "the host must open the database host.toml names")

	_, err = os.Stat(clientDB)
	assert.True(t, os.IsNotExist(err), "todo host must not open — or create — the client's database")
}

func TestHost_FlagsOverrideTheFile(t *testing.T) {
	home := t.TempDir()
	hostDB := filepath.Join(t.TempDir(), "host.db")
	require.NoError(t, config.SaveHostTo(filepath.Join(home, ".todo"),
		config.HostConfig{ListenAddr: "127.0.0.1:1234", DBPath: filepath.Join(home, "unused.db")}))

	rec, stderr, code := runHostCLI(t, home, newDB(t), "--addr", "127.0.0.1:9999", "--db", hostDB)
	require.Equal(t, 0, code, "stderr=%s", stderr)
	assert.Equal(t, "127.0.0.1:9999", rec.addr)

	_, err := os.Stat(hostDB)
	assert.NoError(t, err, "--db must pick the database the host opens")
}

func TestHost_ReadsTheListenAddressFromTheFile(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, config.SaveHostTo(filepath.Join(home, ".todo"),
		config.HostConfig{ListenAddr: "127.0.0.1:1234", DBPath: filepath.Join(home, "host.db")}))

	rec, stderr, code := runHostCLI(t, home, newDB(t))
	require.Equal(t, 0, code, "stderr=%s", stderr)
	assert.Equal(t, "127.0.0.1:1234", rec.addr)
}

// With no device credentials registered, a non-loopback bind would leave the
// task list open to anyone who can reach it, so the host refuses and says so —
// before opening or creating any database.
func TestHost_RefusesNonLoopbackWithoutCredentials(t *testing.T) {
	home := t.TempDir()
	rec, stderr, code := runHostCLI(t, home, newDB(t), "--addr", "0.0.0.0:8090")
	require.Equal(t, 1, code)
	assert.False(t, rec.called, "the host must not start")
	assert.Contains(t, stderr, "no device credentials are registered")
	assert.Contains(t, stderr, "0.0.0.0:8090")

	_, err := os.Stat(filepath.Join(home, ".todo", "todo.db"))
	assert.True(t, os.IsNotExist(err), "a refusal must not first create a database")
}

func TestHost_AllowsNonLoopbackOnceACredentialIsRegistered(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, config.SaveHostTo(filepath.Join(home, ".todo"), config.HostConfig{
		ListenAddr: "0.0.0.0:8090",
		DBPath:     filepath.Join(home, "host.db"),
		Clients:    []config.HostClient{{ID: "abc", Name: "laptop", SecretHash: "hashed"}},
	}))

	rec, stderr, code := runHostCLI(t, home, newDB(t))
	require.Equal(t, 0, code, "stderr=%s", stderr)
	assert.Equal(t, "0.0.0.0:8090", rec.addr)
}
