package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/config"
	"github.com/gbchd/todo/internal/service/todo"
	"github.com/gbchd/todo/internal/transport/host"
	"github.com/gbchd/todo/internal/transport/host/hosttest"
	"github.com/gbchd/todo/internal/transport/tui"
)

// startTestHost runs a real host in this process and returns the config a
// device paired with it would have written.
func startTestHost(t *testing.T) config.Config {
	t.Helper()
	h := hosttest.StartFresh(t)
	return config.Config{
		DBPath:  filepath.Join(t.TempDir(), "client-local.db"),
		Backend: config.Backend{Kind: config.BackendRemote, HostURL: h.URL, Secret: h.Token},
	}
}

// runWith drives the entry point with a whole config, so the backend block is
// resolved exactly as it is in production. No --db is passed unless a test
// passes one itself.
func runWith(t *testing.T, cfg config.Config, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	tui, serve, hostLauncher := noopLaunchers()
	code = Run(t.Context(), append([]string{"todo"}, args...),
		strings.NewReader(""), &outBuf, &errBuf, cfg, tui, serve, hostLauncher)
	return outBuf.String(), errBuf.String(), code
}

// TestRemoteBackend_CLIBehavesAsItDoesLocally is the payoff the whole feature
// is for: on a paired device the commands are the same commands, the output is
// the same output, and nothing on this machine holds the tasks.
func TestRemoteBackend_CLIBehavesAsItDoesLocally(t *testing.T) {
	cfg := startTestHost(t)

	stdout, stderr, code := runWith(t, cfg, "add", "Buy milk", "--priority", "high")
	require.Equal(t, 0, code, "stderr=%s", stderr)
	assert.Contains(t, stdout, "Added task #1: Buy milk")

	stdout, _, code = runWith(t, cfg, "add", "Pay rent", "--due", "2026-08-01")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Added task #2: Pay rent")

	stdout, _, code = runWith(t, cfg, "list")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Buy milk")
	assert.Contains(t, stdout, "Pay rent")

	stdout, _, code = runWith(t, cfg, "done", "1")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Task #1 done: Buy milk")

	stdout, _, code = runWith(t, cfg, "show", "1")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "done")

	stdout, _, code = runWith(t, cfg, "edit", "2", "--title", "Pay the rent")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Updated task #2: Pay the rent")

	stdout, _, code = runWith(t, cfg, "delete", "2", "--force")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Deleted task #2")

	_, stderr, code = runWith(t, cfg, "show", "2")
	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "task #2 not found", "a missing task must read the same as it does locally")

	assert.NoFileExists(t, cfg.DBPath, "a paired device must not have created a local database")
}

// TestRemoteBackend_SubtasksAndFilters covers the features most likely to
// degrade silently on the wire: the tri-state parent filter and the rest of
// the filter vocabulary.
func TestRemoteBackend_SubtasksAndFilters(t *testing.T) {
	cfg := startTestHost(t)

	_, stderr, code := runWith(t, cfg, "add", "Ship the release")
	require.Equal(t, 0, code, stderr)
	_, _, code = runWith(t, cfg, "add", "Write the notes", "--parent", "1", "--priority", "high")
	require.Equal(t, 0, code)
	_, _, code = runWith(t, cfg, "add", "Tag the commit", "--parent", "1")
	require.Equal(t, 0, code)
	_, _, code = runWith(t, cfg, "add", "Unrelated", "--priority", "low", "--due", "2026-01-01")
	require.Equal(t, 0, code)

	stdout, _, code := runWith(t, cfg, "list")
	require.Equal(t, 0, code)
	assert.NotContains(t, stdout, "Write the notes", "subtasks stay hidden until asked for, as they do locally")
	assert.Contains(t, stdout, "Ship the release")

	stdout, _, code = runWith(t, cfg, "list", "--parent", "1")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Write the notes")
	assert.Contains(t, stdout, "Tag the commit")
	assert.NotContains(t, stdout, "Unrelated")

	stdout, _, code = runWith(t, cfg, "list", "--all")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Write the notes")
	assert.Contains(t, stdout, "Unrelated")

	stdout, _, code = runWith(t, cfg, "list", "--priority", "high", "--all")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Write the notes")
	assert.NotContains(t, stdout, "Unrelated")

	stdout, _, code = runWith(t, cfg, "list", "--due-before", "2026-06-01", "--all")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Unrelated")
	assert.NotContains(t, stdout, "Ship the release")

	stdout, _, code = runWith(t, cfg, "list", "--status", "open", "--sort", "priority", "--all")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Ship the release")

	_, stderr, code = runWith(t, cfg, "list", "--priority", "urgent")
	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "invalid priority urgent", "a rejected filter must read as it does locally")
}

// TestRemoteBackend_SecretFromTheEnvironment: a config file that carries the
// backend but not the credential still works, which is the point of the
// environment variable.
func TestRemoteBackend_SecretFromTheEnvironment(t *testing.T) {
	cfg := startTestHost(t)
	t.Setenv(config.SecretEnv, cfg.Backend.Secret)
	cfg.Backend.Secret = ""

	stdout, stderr, code := runWith(t, cfg, "add", "From the environment")
	require.Equal(t, 0, code, "stderr=%s", stderr)
	assert.Contains(t, stdout, "Added task #1")
}

// TestRemoteBackend_EnvironmentOverridesAStaleFileSecret pins the precedence
// itself: with both present, the environment is the one that is used.
func TestRemoteBackend_EnvironmentOverridesAStaleFileSecret(t *testing.T) {
	cfg := startTestHost(t)
	t.Setenv(config.SecretEnv, cfg.Backend.Secret)
	cfg.Backend.Secret = "revoked.long-ago"

	_, stderr, code := runWith(t, cfg, "add", "From the environment")
	require.Equal(t, 0, code, "stderr=%s", stderr)

	// And the other way round, so the test cannot pass by ignoring both.
	t.Setenv(config.SecretEnv, "revoked.long-ago")
	_, stderr, code = runWith(t, cfg, "list")
	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "credential", "stderr=%s", stderr)
}

// TestRemoteBackend_DBPathIsAHardError: the flag is refused, the command does
// nothing, and the message names the fix.
func TestRemoteBackend_DBPathIsAHardError(t *testing.T) {
	cfg := startTestHost(t)
	local := filepath.Join(t.TempDir(), "old.db")

	_, stderr, code := runWith(t, cfg, "--db", local, "list")

	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "--local", "the message must name the fix")
	assert.Contains(t, stderr, cfg.Backend.HostURL)
	assert.NoFileExists(t, local, "a refused command must not have created the database it refused to use")
}

// TestLocalOverride_ReachesAnExistingLocalDatabase is the escape hatch that
// makes switching to a host feel reversible: the old file is still there,
// still readable, and still exactly as it was.
func TestLocalOverride_ReachesAnExistingLocalDatabase(t *testing.T) {
	cfg := startTestHost(t)
	old := filepath.Join(t.TempDir(), "old.db")
	seed(t, old, todo.Task{
		Title: "left behind on this laptop", Status: todo.StatusOpen, Priority: todo.PriorityNone,
	})
	before, err := os.ReadFile(old)
	require.NoError(t, err)

	stdout, stderr, code := runWith(t, cfg, "--local", "--db", old, "list")
	require.Equal(t, 0, code, "stderr=%s", stderr)
	assert.Contains(t, stdout, "left behind on this laptop")

	// And the host's list is untouched by it: two task lists, not one merged.
	stdout, _, code = runWith(t, cfg, "list")
	require.Equal(t, 0, code)
	assert.NotContains(t, stdout, "left behind on this laptop",
		"the local file must not have been migrated to the host")

	after, err := os.ReadFile(old)
	require.NoError(t, err)
	assert.Equal(t, before, after, "reading a local file must not have rewritten it")
}

// TestLocalOverride_UsesTheConfiguredPathWhenGivenNoneOfItsOwn: --local on its
// own is enough, because config.toml still names this machine's database.
func TestLocalOverride_UsesTheConfiguredPath(t *testing.T) {
	cfg := startTestHost(t)
	seed(t, cfg.DBPath, todo.Task{Title: "this machine's own", Status: todo.StatusOpen, Priority: todo.PriorityNone})

	stdout, stderr, code := runWith(t, cfg, "--local", "list")

	require.Equal(t, 0, code, "stderr=%s", stderr)
	assert.Contains(t, stdout, "this machine's own")
}

// TestRemoteBackend_UnreachableHostFailsFast: the terminal comes back, with a
// non-zero exit and a message naming the host that did not answer.
func TestRemoteBackend_UnreachableHostFailsFast(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()

	cfg := config.Config{
		DBPath:  filepath.Join(t.TempDir(), "unused.db"),
		Backend: config.Backend{Kind: config.BackendRemote, HostURL: url, Secret: "id.secret"},
	}

	_, stderr, code := runWith(t, cfg, "list")

	require.Equal(t, 1, code)
	assert.Contains(t, stderr, url, "the message must name the host")
	assert.NoFileExists(t, cfg.DBPath, "a failed remote command must not fall back to a local file")
}

// TestRemoteBackend_OutputMatchesALocalInstall is the strongest form of "the
// CLI behaves as it does locally" available: the same session is run twice,
// once against a local database and once against a host, and the two
// transcripts are compared byte for byte. Anything the wire quietly changed —
// an ordering, a rollup count, a filter that stopped filtering — shows up as a
// diff rather than as an assertion nobody wrote.
func TestRemoteBackend_OutputMatchesALocalInstall(t *testing.T) {
	session := [][]string{
		{"add", "Ship the release", "--priority", "high"},
		{"add", "Write the notes", "--parent", "1"},
		{"add", "Tag the commit", "--parent", "1", "--priority", "low"},
		{"add", "Unrelated", "--due", "2026-08-01"},
		{"list"},
		{"list", "--all"},
		{"list", "--parent", "1"},
		{"list", "--sort", "priority", "--all"},
		{"done", "2"},
		{"list", "--all"},
		{"list", "--status", "done"},
		{"edit", "3", "--priority", "high", "--due", "2026-09-01"},
		{"start", "4"},
		{"list", "--all"},
		{"reopen", "2"},
		{"delete", "3", "--force"},
		{"list", "--all"},
		{"show", "9999"},
	}

	transcript := func(cfg config.Config) string {
		var b strings.Builder
		for _, args := range session {
			stdout, stderr, code := runWith(t, cfg, args...)
			b.WriteString(strings.Join(args, " ") + "\n")
			b.WriteString(stdout)
			b.WriteString(stderr)
			b.WriteString("exit " + strconv.Itoa(code) + "\n\n")
		}
		return b.String()
	}

	local := config.Config{DBPath: newDB(t)}
	remote := startTestHost(t)

	assert.Equal(t, transcript(local), transcript(remote),
		"a remote client's transcript must be indistinguishable from a local one")
}

// remoteConfig is the config a device paired with url would have.
func remoteConfig(t *testing.T, url, secret string) config.Config {
	t.Helper()
	return config.Config{
		DBPath:  filepath.Join(t.TempDir(), "client-local.db"),
		Backend: config.Backend{Kind: config.BackendRemote, HostURL: url, Secret: secret},
	}
}

// TestRemoteBackend_EachFailureSaysWhichOneItIs walks the four things that can
// go wrong on a paired device through the CLI, which is where a user meets
// them. Each has its fix in a different place, so each has to arrive named and
// with the next step in it — and none of them may leave a database behind on a
// machine that is supposed to keep nothing.
func TestRemoteBackend_EachFailureSaysWhichOneItIs(t *testing.T) {
	tests := []struct {
		name string
		want []string
		args []string
		cfg  func(t *testing.T) config.Config
	}{
		{
			name: "the host is unreachable",
			args: []string{"list"},
			want: []string{"cannot reach the todo host", "--local"},
			cfg: func(t *testing.T) config.Config {
				dead := httptest.NewServer(http.NotFoundHandler())
				url := dead.URL
				dead.Close()
				return remoteConfig(t, url, "id.secret")
			},
		},
		{
			name: "the credential was rejected",
			args: []string{"list"},
			want: []string{"rejected this device's credential", "todo pair"},
			cfg: func(t *testing.T) config.Config {
				return remoteConfig(t, hosttest.StartFresh(t).URL, "someone-else.revoked")
			},
		},
		{
			name: "the two builds disagree",
			args: []string{"list"},
			want: []string{"do not speak the same protocol", "upgrade todo"},
			cfg: func(t *testing.T) config.Config {
				mangle := func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						r.Header.Set(host.ProtocolVersionHeader, "99")
						next.ServeHTTP(w, r)
					})
				}
				srv := httptest.NewServer(mangle(host.NewMux(hosttest.NewService(t), nil, host.RequireProtocolVersion)))
				t.Cleanup(srv.Close)
				return remoteConfig(t, srv.URL, "id.secret")
			},
		},
		{
			name: "another device got there first",
			args: []string{"done", "1"},
			want: []string{"task #1 changed while this command was working on it", "run the command again"},
			cfg: func(t *testing.T) config.Config {
				// Every write is refused as a lost version race, which is what
				// a task being edited from another device continuously looks
				// like from here: the adapter retries, runs out of retries,
				// and the conflict is the user's to see.
				conflict := func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodPatch {
							next.ServeHTTP(w, r)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusConflict)
						fmt.Fprint(w, `{"error":"conflict","conflict":{"task_id":1,"expected_version":1,"actual_version":99}}`)
					})
				}
				h := hosttest.StartFresh(t, conflict)
				cfg := remoteConfig(t, h.URL, h.Token)
				_, _, code := runWith(t, cfg, "add", "contended")
				require.Equal(t, 0, code, "seed")
				return cfg
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg(t)
			_, stderr, code := runWith(t, cfg, tc.args...)

			require.Equal(t, 1, code, "a failure must exit non-zero; stderr=%s", stderr)
			for _, want := range tc.want {
				assert.Contains(t, stderr, want)
			}
			assert.NoFileExists(t, cfg.DBPath, "a failed remote command must not fall back to a local file")
		})
	}
}

// TestRemoteBackend_TUIRefusesToStartOnAnUnreachableHost drives the real TUI
// launcher, not a stub: `todo tui` against a host that does not answer must
// come back to the shell the way `todo list` does, rather than opening an
// empty screen the user then has to quit.
func TestRemoteBackend_TUIRefusesToStartOnAnUnreachableHost(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	cfg := remoteConfig(t, url, "id.secret")

	var outBuf, errBuf bytes.Buffer
	_, serve, hostLauncher := noopLaunchers()
	code := Run(t.Context(), []string{"todo", "tui"},
		strings.NewReader(""), &outBuf, &errBuf, cfg, tui.Run, serve, hostLauncher)

	require.Equal(t, 1, code)
	assert.Contains(t, errBuf.String(), url, "the message must name the host, as `todo list` does")
	assert.Contains(t, errBuf.String(), "cannot reach the todo host")
}

// TestBackendSelection_Precedence pins the whole order in one place: the file
// says what this machine is, the environment may replace the secret it says so
// with, and --local overrides the decision entirely for one command. Each
// source is checked while the wider one says something different, so the test
// cannot pass by consulting only one of them.
func TestBackendSelection_Precedence(t *testing.T) {
	h := hosttest.StartFresh(t)
	cfg := remoteConfig(t, h.URL, "stale-in-the-file")
	seed(t, cfg.DBPath, todo.Task{
		Title: "this machine's own", Status: todo.StatusOpen, Priority: todo.PriorityNone,
	})
	_, err := h.Svc.AddTask(t.Context(), todo.NewTask{Title: "the host's"})
	require.NoError(t, err, "seed the host")

	t.Run("the file alone chooses the host", func(t *testing.T) {
		withFileSecret := cfg
		withFileSecret.Backend.Secret = h.Token
		stdout, stderr, code := runWith(t, withFileSecret, "list")
		require.Equal(t, 0, code, "stderr=%s", stderr)
		assert.Contains(t, stdout, "the host's")
		assert.NotContains(t, stdout, "this machine's own")
	})

	t.Run("the environment replaces the file's secret", func(t *testing.T) {
		t.Setenv(config.SecretEnv, h.Token)
		stdout, stderr, code := runWith(t, cfg, "list")
		require.Equal(t, 0, code, "the stale secret in the file must not have been used; stderr=%s", stderr)
		assert.Contains(t, stdout, "the host's")
	})

	t.Run("--local overrides both", func(t *testing.T) {
		t.Setenv(config.SecretEnv, h.Token)
		stdout, stderr, code := runWith(t, cfg, "--local", "list")
		require.Equal(t, 0, code, "stderr=%s", stderr)
		assert.Contains(t, stdout, "this machine's own")
		assert.NotContains(t, stdout, "the host's")
	})
}
