package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	tcli "github.com/urfave/cli/v3"

	"github.com/gbchd/todo/internal/config"
	"github.com/gbchd/todo/internal/credential"
	"github.com/gbchd/todo/internal/pairing"
)

const (
	// pairPollInterval is how often `todo host pair` re-reads the offer while
	// it waits. The operator is walking to another machine, so a fraction of a
	// second is far below what they can notice, and re-reading a hundred bytes
	// of JSON costs nothing.
	pairPollInterval = 250 * time.Millisecond

	// pairRequestTimeout bounds the one HTTP request `todo pair` makes. A host
	// that is unreachable must fail the command, not wedge the terminal.
	pairRequestTimeout = 15 * time.Second
)

// hostPairCommand opens a pairing window and waits in front of the operator.
//
// It is a separate process from the running `todo host`, which is why the
// offer goes through a file rather than a variable: the command that prints
// the code is not the one a device talks to. Blocking rather than opening the
// window and returning is the point — the window closes when this command
// ends, so an operator who changes their mind presses Ctrl-C and there is
// nothing left outstanding.
func hostPairCommand(stdout, stderr io.Writer) *tcli.Command {
	return &tcli.Command{
		Name:  "pair",
		Usage: "print a pairing code for a new device and wait for it",
		Action: func(ctx context.Context, _ *tcli.Command) error {
			dir, err := config.Dir()
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			hostCfg, err := config.LoadHost()
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			code, err := pairing.NewCode()
			if err != nil {
				return reportErr(stderr, err, 0)
			}

			// No registrar: this command opens and withdraws the window, and
			// the running host is the only process that may turn a redeemed
			// code into a credential. See pairing.Store.
			store := pairing.NewStore(dir, nil)
			if err := store.Open(code, pairing.Window); err != nil {
				return reportErr(stderr, err, 0)
			}
			// However this command ends — paired, expired, interrupted, or
			// failed — the window is shut behind it.
			defer store.Withdraw() //nolint:errcheck // nothing useful remains to be done about a failed cleanup

			printPairingCode(stdout, code, hostCfg.ListenAddr)
			return awaitPairing(ctx, store, stdout, stderr)
		},
	}
}

// printPairingCode shows the code and the exact command to run with it.
// Spelling out the second command matters: the whole point of pairing is that
// nobody has to work out what to type or copy a secret by hand.
func printPairingCode(stdout io.Writer, code, listenAddr string) {
	fmt.Fprintf(stdout, "Pairing code: %s\n\n", code)
	fmt.Fprintf(stdout, "It is valid for %s, can be used once, and is burned after %d wrong tries.\n",
		pairing.Window, pairing.MaxAttempts)

	target, note := pairTarget(listenAddr)
	fmt.Fprintf(stdout, "On the new device run:\n\n    todo pair %s %s\n\n", target, code)
	if note != "" {
		fmt.Fprintf(stdout, "%s\n\n", note)
	}
	fmt.Fprintln(stdout, "Waiting for the device... (Ctrl-C withdraws the code)")
}

// pairTarget turns the address the host listens on into the URL to type on the
// device, plus the line that has to go with it when there is no such URL.
//
// A wildcard address — ":8090", "0.0.0.0:8090", "[::]:8090" — is exactly what
// an operator binds to make the host reachable from other machines, and is the
// one address no other machine can connect to. Echoing it back as a URL hands
// them a command that cannot work and no clue why, so the host part is left as
// a placeholder for the address they know and this command does not: which of
// the machine's interfaces the new device can see.
func pairTarget(listenAddr string) (target, note string) {
	addr, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		// Not an address this can take apart; print it as it stands rather
		// than inventing one. Whether the host will bind it is its problem.
		return "http://" + listenAddr, ""
	}
	if ip := net.ParseIP(addr); addr == "" || (ip != nil && ip.IsUnspecified()) {
		return "http://<this host's address>:" + port,
			"This host listens on every network interface: use the address the new device can reach it at."
	}
	return "http://" + net.JoinHostPort(addr, port), ""
}

// awaitPairing blocks until the offer reaches a terminal state or the operator
// interrupts, and reports which. Polling rather than watching the file:
// the state being waited on changes at most once, the wait is measured in
// minutes, and a poll needs no filesystem notification machinery to be correct
// on every platform.
func awaitPairing(ctx context.Context, store *pairing.Store, stdout, stderr io.Writer) error {
	ticker := time.NewTicker(pairPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(stdout, "\nPairing canceled. The code is no longer valid.")
			return nil
		case <-ticker.C:
			switch outcome := store.Outcome(); outcome.State {
			case pairing.StatePaired:
				fmt.Fprintf(stdout, "Paired %s (%s). It now appears in `todo host clients`.\n",
					outcome.Device.Name, outcome.Device.ID)
				return nil
			case pairing.StateBurned:
				return reportErr(stderr, fmt.Errorf(
					"the pairing code was withdrawn after %d wrong tries; nothing was registered — run `todo host pair` again",
					outcome.Attempts), 0)
			case pairing.StateExpired, pairing.StateNone:
				return reportErr(stderr, errors.New(
					"the pairing code expired before a device used it; run `todo host pair` again"), 0)
			case pairing.StateOpen:
			}
		}
	}
}

// registerDevice is the host's half of a redeemed code: mint a credential,
// record it in host.toml, and hand the device the only copy of its token.
//
// It is passed to the pairing store rather than called by it directly, so the
// store decides when a device may be registered and this decides what
// registering means. A device registered here is usable immediately, because
// the host resolves credentials by re-reading host.toml per request.
func registerDevice(name string) (pairing.Device, error) {
	cred, token, err := credential.Issue()
	if err != nil {
		return pairing.Device{}, err
	}
	cfg, err := config.LoadHost()
	if err != nil {
		return pairing.Device{}, err
	}
	client := cfg.AddClient(name, cred)
	if err := config.SaveHost(cfg); err != nil {
		return pairing.Device{}, err
	}
	return pairing.Device{ID: client.ID, Name: client.Name, Token: token}, nil
}

// pairCommandName is matched in the root Before hook, so it lives next to the
// command it names.
const pairCommandName = "pair"

// pairCommand is the device's half: consume a code, keep the credential that
// comes back, and write the backend block that points this machine at the
// host from now on.
//
// It writes the config rather than printing something to paste, because a
// secret a person copies is a secret that ends up in a shell history.
func pairCommand(stdout, stderr io.Writer) *tcli.Command {
	return &tcli.Command{
		Name:      pairCommandName,
		Usage:     "pair this device with a todo host",
		ArgsUsage: "<host url> <code>",
		Flags: []tcli.Flag{
			&tcli.StringFlag{Name: "name", Usage: "how this device is listed on the host (defaults to its hostname)"},
		},
		Action: func(ctx context.Context, cmd *tcli.Command) error {
			hostURL, code := cmd.Args().Get(0), cmd.Args().Get(1)
			if hostURL == "" || code == "" {
				return reportErr(stderr, errors.New(
					"missing arguments: todo pair <host url> <code>, using the code `todo host pair` printed"), 0)
			}
			base, err := normalizeHostURL(hostURL)
			if err != nil {
				return reportErr(stderr, err, 0)
			}

			resp, err := redeemPairingCode(ctx, base, code, deviceLabel(cmd.String("name")))
			if err != nil {
				return reportErr(stderr, err, 0)
			}

			cfg, err := config.Load()
			if err != nil {
				return reportErr(stderr, err, 0)
			}
			cfg.Backend = config.Backend{Kind: config.BackendRemote, HostURL: base, Secret: resp.Token}
			if err := config.Save(cfg); err != nil {
				return reportErr(stderr, err, 0)
			}

			fmt.Fprintf(stdout, "Paired with %s as %q.\n", base, resp.Name)
			fmt.Fprintln(stdout, "This device now reads and writes the host's task list. Your local tasks are untouched.")
			return nil
		},
	}
}

// deviceLabel falls back to the machine's hostname, which is almost always the
// name its owner would have typed anyway.
func deviceLabel(flag string) string {
	if flag != "" {
		return flag
	}
	if name, err := os.Hostname(); err == nil {
		return name
	}
	return ""
}

// normalizeHostURL accepts what a person would type — with or without a
// scheme, with or without a trailing slash — and returns the base every
// request is built from. A bare host defaults to http because the host does
// not terminate TLS itself; a deployment that has TLS is behind a proxy and
// its operator will have typed https.
func normalizeHostURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", errors.New("missing host url")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("%q is not a host url; it should look like http://192.168.1.10:8090", raw)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%q is not a host url; it should look like http://192.168.1.10:8090", raw)
	}
	return parsed.Scheme + "://" + parsed.Host + strings.TrimRight(parsed.Path, "/"), nil
}

// pairMaxResponse bounds what is read back from a URL the user typed, which is
// not necessarily a todo host at all.
const pairMaxResponse = 4 << 10

// redeemPairingCode makes the one request pairing needs.
//
// The host answers a refused code with a plain 404, identical to the one it
// gives for a route that does not exist, and that is deliberate: it must not
// tell a scanner whether pairing is open. The cost is paid here, in a message
// that has to cover every refusal at once — so it names all of them rather
// than guessing at which applies.
func redeemPairingCode(ctx context.Context, base, code, name string) (pairing.Response, error) {
	body, err := json.Marshal(pairing.Request{Code: code, Name: name})
	if err != nil {
		return pairing.Response{}, fmt.Errorf("encode pairing request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, pairRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+pairing.Path, bytes.NewReader(body))
	if err != nil {
		return pairing.Response{}, fmt.Errorf("build pairing request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return pairing.Response{}, fmt.Errorf("cannot reach the todo host at %s: %w", base, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return pairing.Response{}, fmt.Errorf(
			"%s did not accept this pairing code; it may have expired, been used already, been mistyped, or no pairing may be in progress — run `todo host pair` on the host and try again",
			base)
	}

	var out pairing.Response
	if err := json.NewDecoder(io.LimitReader(resp.Body, pairMaxResponse)).Decode(&out); err != nil {
		return pairing.Response{}, fmt.Errorf("cannot read the reply from %s: %w", base, err)
	}
	if out.Token == "" {
		return pairing.Response{}, fmt.Errorf("%s replied without a credential; is it a todo host?", base)
	}
	return out, nil
}
