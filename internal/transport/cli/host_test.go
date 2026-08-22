package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/config"
	"github.com/gbchd/todo/internal/credential"
	"github.com/gbchd/todo/internal/pairing"
	"github.com/gbchd/todo/internal/service/todo"
)

// hostRun records what `todo host` would have served, so the tests assert the
// settings the command resolved without binding a port.
type hostRun struct {
	called bool
	addr   string
	svc    *todo.Service
	creds  credential.Source
	pairs  *pairing.Store
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
	launch := func(_ context.Context, svc *todo.Service, addr string, creds credential.Source, pairs *pairing.Store, _ io.Writer) error {
		rec.called, rec.addr, rec.svc, rec.creds, rec.pairs = true, addr, svc, creds, pairs
		return nil
	}
	full := append([]string{"todo", "--db", clientDB, "host"}, args...)
	code = Run(t.Context(), full, strings.NewReader(""), &outBuf, &errBuf, config.Config{}, tui, serve, launch)
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

// runHostSubcommand drives `todo host clients` / `todo host revoke` with $HOME
// pointed at a temp directory, and returns what the operator would have seen.
func runHostSubcommand(t *testing.T, home string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	t.Setenv("HOME", home)

	var outBuf, errBuf bytes.Buffer
	tui, serve, host := noopLaunchers()
	full := append([]string{"todo", "--db", newDB(t), "host"}, args...)
	code = Run(t.Context(), full, strings.NewReader(""), &outBuf, &errBuf, config.Config{}, tui, serve, host)
	return outBuf.String(), errBuf.String(), code
}

// seedHost writes a host.toml registering one device per name, and returns the
// tokens those devices would be holding.
func seedHost(t *testing.T, home string, names ...string) map[string]string {
	t.Helper()
	cfg := config.HostConfig{ListenAddr: config.DefaultHostAddr, DBPath: filepath.Join(home, "host.db")}
	tokens := make(map[string]string, len(names))
	for _, name := range names {
		cred, token, err := credential.Issue()
		require.NoError(t, err, "issue credential")
		cfg.AddClient(name, cred)
		tokens[name] = token
	}
	require.NoError(t, config.SaveHostTo(filepath.Join(home, ".todo"), cfg))
	return tokens
}

func TestHostClients_ListsNamesAndWhenTheyWereAdded(t *testing.T) {
	home := t.TempDir()
	seedHost(t, home, "laptop", "desktop")

	stdout, stderr, code := runHostSubcommand(t, home, "clients")
	require.Equal(t, 0, code, "stderr=%s", stderr)
	assert.Contains(t, stdout, "NAME")
	assert.Contains(t, stdout, "ADDED")
	assert.Contains(t, stdout, "laptop")
	assert.Contains(t, stdout, "desktop")
	assert.Contains(t, stdout, time.Now().Format("2006-01-02"))
}

// The listing is meant to be safe to look at: nothing in it may be presentable
// as a credential, hash included.
func TestHostClients_NeverPrintsASecret(t *testing.T) {
	home := t.TempDir()
	tokens := seedHost(t, home, "laptop")

	stdout, stderr, code := runHostSubcommand(t, home, "clients")
	require.Equal(t, 0, code, "stderr=%s", stderr)

	cfg, err := config.LoadHostFrom(filepath.Join(home, ".todo"))
	require.NoError(t, err)
	assert.NotContains(t, stdout, cfg.Clients[0].SecretHash, "the stored hash must not be printed")
	assert.NotContains(t, stdout, tokens["laptop"], "the device's token must not be printed")
	assert.NotContains(t, strings.ToLower(stdout), "secret")
}

func TestHostClients_SaysSoWhenNoDeviceIsRegistered(t *testing.T) {
	home := t.TempDir()
	stdout, stderr, code := runHostSubcommand(t, home, "clients")
	require.Equal(t, 0, code, "stderr=%s", stderr)
	assert.Contains(t, stdout, "no devices registered")
}

// Revoking the lost laptop must leave the desktop registered — that is the
// difference between revoking a device and starting over.
func TestHostRevoke_RemovesOneDeviceAndLeavesTheRest(t *testing.T) {
	home := t.TempDir()
	seedHost(t, home, "laptop", "desktop")

	stdout, stderr, code := runHostSubcommand(t, home, "revoke", "laptop")
	require.Equal(t, 0, code, "stderr=%s", stderr)
	assert.Contains(t, stdout, "Revoked laptop")

	stdout, _, code = runHostSubcommand(t, home, "clients")
	require.Equal(t, 0, code)
	assert.NotContains(t, stdout, "laptop")
	assert.Contains(t, stdout, "desktop")
}

// The host resolves credentials by reading host.toml per request, so a device
// revoked while the host is running is rejected on its very next request.
func TestHostRevoke_TakesEffectForARunningHost(t *testing.T) {
	home := t.TempDir()
	tokens := seedHost(t, home, "laptop", "desktop")

	rec, stderr, code := runHostCLI(t, home, newDB(t))
	require.Equal(t, 0, code, "stderr=%s", stderr)
	require.NotNil(t, rec.creds, "the host must be given a way to resolve credentials")

	idOf := func(token string) string {
		id, _, ok := credential.SplitToken(token)
		require.True(t, ok)
		return id
	}
	_, ok := rec.creds(idOf(tokens["laptop"]))
	require.True(t, ok)

	_, stderr, code = runHostSubcommand(t, home, "revoke", "laptop")
	require.Equal(t, 0, code, "stderr=%s", stderr)

	_, ok = rec.creds(idOf(tokens["laptop"]))
	assert.False(t, ok, "the revoked device must stop resolving without a restart")
	_, ok = rec.creds(idOf(tokens["desktop"]))
	assert.True(t, ok, "every other device keeps working")
}

func TestHostRevoke_RejectsAnUnknownDevice(t *testing.T) {
	home := t.TempDir()
	seedHost(t, home, "laptop")

	_, stderr, code := runHostSubcommand(t, home, "revoke", "phone")
	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "phone")

	stdout, _, _ := runHostSubcommand(t, home, "clients")
	assert.Contains(t, stdout, "laptop", "a failed revoke must not remove anything")
}

func TestHostRevoke_RequiresADevice(t *testing.T) {
	home := t.TempDir()
	seedHost(t, home, "laptop")

	_, stderr, code := runHostSubcommand(t, home, "revoke")
	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "missing device")
}
