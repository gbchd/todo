package cli

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/config"
	"github.com/gbchd/todo/internal/credential"
	"github.com/gbchd/todo/internal/pairing"
	"github.com/gbchd/todo/internal/repository"
	"github.com/gbchd/todo/internal/service/todo"
	"github.com/gbchd/todo/internal/transport/host"
)

// syncBuffer is stdout for a command still running in another goroutine, which
// the test reads while it writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runAtHome drives a command to completion with $HOME pointed at home, which
// is where both config.toml and host.toml live. Unlike runCLI it passes no
// --db: these commands are the ones that do not use the client's database.
func runAtHome(t *testing.T, home string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	t.Setenv("HOME", home)

	var outBuf, errBuf bytes.Buffer
	tui, serve, launch := noopLaunchers()
	code = Run(context.Background(), append([]string{"todo"}, args...),
		strings.NewReader(""), &outBuf, &errBuf, config.Config{}, tui, serve, launch)
	return outBuf.String(), errBuf.String(), code
}

// startHostPair runs `todo host pair`, which blocks, and returns the code it
// printed along with a way to wait for it to finish. This is the shape of the
// real flow: the command stands there waiting while something else redeems.
func startHostPair(t *testing.T, home string) (code string, stdout *syncBuffer, cancel func(), wait func() int) {
	t.Helper()
	t.Setenv("HOME", home)

	ctx, cancelCtx := context.WithCancel(context.Background())
	out, errOut := &syncBuffer{}, &syncBuffer{}
	done := make(chan int, 1)

	tui, serve, launch := noopLaunchers()
	go func() {
		done <- Run(ctx, []string{"todo", "host", "pair"},
			strings.NewReader(""), out, errOut, config.Config{}, tui, serve, launch)
	}()
	t.Cleanup(cancelCtx)

	return waitForCode(t, out), out, cancelCtx, func() int {
		select {
		case exit := <-done:
			return exit
		case <-time.After(10 * time.Second):
			t.Fatal("todo host pair did not finish")
			return -1
		}
	}
}

var codeLine = regexp.MustCompile(`Pairing code: ([0-9A-Z]{6})\b`)

func waitForCode(t *testing.T, out *syncBuffer) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := codeLine.FindStringSubmatch(out.String()); m != nil {
			return m[1]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("todo host pair printed no code; stdout=%q", out.String())
	return ""
}

func TestHostPair_PrintsASixCharacterCodeFromTheUnambiguousAlphabet(t *testing.T) {
	home := t.TempDir()
	code, out, _, _ := startHostPair(t, home)

	require.Len(t, code, pairing.CodeLength)
	for _, r := range code {
		assert.Contains(t, pairing.Alphabet, string(r))
	}
	assert.Contains(t, out.String(), "todo pair http://"+config.DefaultHostAddr+" "+code,
		"the operator must be told exactly what to type on the device")
}

// Interrupting the command is one of the three ways the window closes, and it
// has to close all the way: the code the operator changed their mind about
// must be worthless afterwards.
func TestHostPair_WithdrawsTheCodeWhenTheOperatorInterrupts(t *testing.T) {
	home := t.TempDir()
	code, out, cancel, wait := startHostPair(t, home)

	dir := filepath.Join(home, ".todo")
	require.Equal(t, pairing.StateOpen, pairing.NewStore(dir, nil).Outcome().State)

	cancel()
	require.Equal(t, 0, wait())
	assert.Contains(t, out.String(), "no longer valid")

	// The store the running host would consult now has nothing to offer.
	_, ok := pairing.NewStore(dir, registerDevice).Redeem(code, "laptop")
	assert.False(t, ok, "an interrupted pairing code must not still work")
}

// The full host-side flow: a redeemed code registers a device, the waiting
// command says so, and the device shows up in the list the operator reads.
func TestHostPair_RegistersADeviceThatAppearsInHostClients(t *testing.T) {
	home := t.TempDir()
	code, out, _, wait := startHostPair(t, home)

	dir := filepath.Join(home, ".todo")
	device, ok := pairing.NewStore(dir, registerDevice).Redeem(code, "laptop")
	require.True(t, ok)
	require.NotEmpty(t, device.Token)

	require.Equal(t, 0, wait())
	assert.Contains(t, out.String(), "Paired laptop ("+device.ID+")")

	clients, _, exit := runAtHome(t, home, "host", "clients")
	require.Equal(t, 0, exit)
	assert.Contains(t, clients, "laptop")
	assert.Contains(t, clients, device.ID)
}

// pairedHost stands up a real host over a temporary database with a pairing
// window open, and returns its URL and the code.
func pairedHost(t *testing.T, home string) (url, code string) {
	t.Helper()
	dir := filepath.Join(home, ".todo")
	require.NoError(t, os.MkdirAll(dir, 0o700))

	repo, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "host.db"))
	require.NoError(t, err)
	t.Cleanup(func() { repo.Close() })

	store := pairing.NewStore(dir, registerDevice)
	code, err = pairing.NewCode()
	require.NoError(t, err)
	require.NoError(t, store.Open(code, pairing.Window))

	srv := httptest.NewServer(host.NewMux(todo.NewService(repo), store))
	t.Cleanup(srv.Close)
	return srv.URL, code
}

// End to end over HTTP: the device consumes the code, keeps the credential,
// and writes the backend block that will point it at the host from now on.
func TestPair_ObtainsACredentialAndWritesTheBackendBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	url, code := pairedHost(t, home)

	stdout, stderr, exit := runAtHome(t, home, "pair", "--name", "laptop", url, strings.ToLower(code))
	require.Equal(t, 0, exit, "stderr=%s", stderr)
	assert.Contains(t, stdout, "Paired with "+url)

	dir := filepath.Join(home, ".todo")
	cfg, err := config.LoadFrom(dir)
	require.NoError(t, err)
	assert.Equal(t, config.BackendRemote, cfg.Backend.Kind)
	assert.True(t, cfg.Backend.Remote())
	assert.Equal(t, url, cfg.Backend.HostURL)
	require.NotEmpty(t, cfg.Backend.Secret)

	// The secret written to the device is the one the host registered: the
	// point of pairing is that this holds without anyone typing it.
	hostCfg, err := config.LoadHostFrom(dir)
	require.NoError(t, err)
	require.Len(t, hostCfg.Clients, 1)
	assert.Equal(t, "laptop", hostCfg.Clients[0].Name)

	id, secret, ok := credential.SplitToken(cfg.Backend.Secret)
	require.True(t, ok)
	assert.Equal(t, hostCfg.Clients[0].ID, id)
	cred, registered := hostCfg.Credential(id)
	require.True(t, registered)
	assert.True(t, cred.Verify(secret), "the token the device kept must authenticate against the host")

	clients, _, exit := runAtHome(t, home, "host", "clients")
	require.Equal(t, 0, exit)
	assert.Contains(t, clients, "laptop")
}

func TestPair_RefusesASecondUseOfTheSameCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	url, code := pairedHost(t, home)

	_, stderr, exit := runAtHome(t, home, "pair", url, code)
	require.Equal(t, 0, exit, "stderr=%s", stderr)

	_, stderr, exit = runAtHome(t, home, "pair", url, code)
	assert.Equal(t, 1, exit)
	assert.Contains(t, stderr, "did not accept this pairing code")

	hostCfg, err := config.LoadHostFrom(filepath.Join(home, ".todo"))
	require.NoError(t, err)
	assert.Len(t, hostCfg.Clients, 1, "the second attempt must register nothing")
}

// A device about to read its tasks from a host has no use for a local database,
// and creating one would leave a stray empty file that looks like data.
func TestPair_DoesNotCreateALocalDatabase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	url, code := pairedHost(t, home)

	_, stderr, exit := runAtHome(t, home, "pair", url, code)
	require.Equal(t, 0, exit, "stderr=%s", stderr)

	_, err := os.Stat(filepath.Join(home, ".todo", "todo.db"))
	assert.True(t, os.IsNotExist(err), "todo pair must not open or create a task database")
}

func TestPair_ExplainsWhatItNeedsWhenAnArgumentIsMissing(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{{"pair"}, {"pair", "http://127.0.0.1:8090"}} {
		_, stderr, exit := runAtHome(t, home, args...)
		assert.Equal(t, 1, exit)
		assert.Contains(t, stderr, "todo pair <host url> <code>")
	}
}

func TestPair_ReportsAHostItCannotReach(t *testing.T) {
	home := t.TempDir()
	// A port nothing is listening on: httptest hands one back and closes it.
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()

	_, stderr, exit := runAtHome(t, home, "pair", url, "ABC123")
	assert.Equal(t, 1, exit)
	assert.Contains(t, stderr, "cannot reach the todo host at "+url,
		"an unreachable host must not read as a rejected code")
}

func TestNormalizeHostURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "a full url", in: "http://192.168.1.10:8090", want: "http://192.168.1.10:8090"},
		{name: "a trailing slash", in: "http://host.example:8090/", want: "http://host.example:8090"},
		{name: "no scheme", in: "192.168.1.10:8090", want: "http://192.168.1.10:8090"},
		{name: "behind a tls proxy", in: "https://todo.example", want: "https://todo.example"},
		{name: "a subpath", in: "https://todo.example/todo/", want: "https://todo.example/todo"},
		{name: "empty", in: "  ", wantErr: true},
		{name: "not http", in: "ftp://todo.example", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeHostURL(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
