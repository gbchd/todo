// Package pairing implements the short, deliberate window during which a
// `todo host` will hand a credential to a device that does not have one yet.
//
// A pairing code is six characters, because a person has to read it off one
// screen and type it into another. Six characters is roughly thirty bits,
// which is not enough on its own — so it is never on its own. Four defences
// stand together and each is load-bearing:
//
//  1. the code is valid for a few minutes, and the window also closes on
//     success and whenever the operator stops the command that opened it;
//  2. it is consumed by the first success, so a second use finds nothing;
//  3. a handful of wrong guesses burns it outright, so guessing fails closed
//     rather than eventually succeeding;
//  4. the transport rate-limits pairing requests globally, so the guesses
//     cannot arrive quickly enough for even the burn limit to be reached at
//     speed.
//
// None of the four may be relaxed on the reasoning that another one covers it.
// See docs/adr/0003.
//
// The outstanding offer lives in a file because the two halves of pairing are
// two processes: `todo host pair` opens the window and waits, while the
// already-running `todo host` is the one a device actually talks to. The file
// carries owner-only permissions and holds a hash of the code rather than the
// code, so that a copy of it — in a backup, on a shared filesystem — is not a
// pairing code. It is transient runtime state rather than settings, which is
// why it is JSON next to the TOML the operator edits.
package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// Alphabet is Crockford's base32: the decimal digits and the uppercase
	// letters, less I, L, O and U. What is left cannot be misread — there is no
	// O to confuse with 0 and no I or l to confuse with 1 — which is what makes
	// a code practical to read off a screen and retype.
	Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

	// CodeLength is short on purpose: the code is typed by a person, and its
	// entropy is not what makes pairing safe. See the package comment.
	CodeLength = 6

	// Window is how long an offer stands if nothing else closes it. Long
	// enough to walk to another machine, short enough that a code read over
	// the operator's shoulder is useless by the time it is used.
	Window = 3 * time.Minute

	// MaxAttempts is how many wrong codes an outstanding offer survives. On
	// the next one it is burned rather than merely rejected, so a guessing run
	// costs the attacker the target and the operator one re-run of `todo host
	// pair`.
	MaxAttempts = 5

	// Path is the host route that redeems a code. It is deliberately outside
	// the versioned API prefix: the device asking has no credential yet, so the
	// route cannot sit behind authentication.
	Path = "/pair"

	fileName = "pairing.json"
)

// Request is what a device sends to redeem a code. Name is the label the
// device asks to be listed under on the host; the host is free to clean it up.
type Request struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Response is what the host returns on success, and the only response the
// pairing route ever gives that is not a plain 404. Token is the device's
// credential, returned this once and stored nowhere on the host.
type Response struct {
	Token    string `json:"token"`
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
}

// Device is a newly registered device: what the host recorded, plus the token
// only its owner ever sees.
type Device struct {
	ID    string
	Name  string
	Token string
}

// Register mints a credential for a device pairing under name and records it
// with the host. It is injected rather than called directly so that this
// package owns when a device may be registered and nothing about where the
// host keeps its credentials.
type Register func(name string) (Device, error)

// State is how an offer stands. Every state but StateOpen is terminal: the
// window is shut and the route is back to answering as if it did not exist.
type State string

const (
	// StateNone means no offer has been made, or the last one was withdrawn.
	StateNone State = "none"
	// StateOpen means a code is outstanding and may still be redeemed.
	StateOpen State = "open"
	// StatePaired means a device redeemed the code and holds a credential.
	StatePaired State = "paired"
	// StateBurned means the offer was destroyed by wrong guesses rather than
	// used. It is distinct from expiry because it is worth telling an operator
	// that someone was guessing.
	StateBurned State = "burned"
	// StateExpired means the window closed with nothing having redeemed it.
	StateExpired State = "expired"
)

// Outcome is an offer as the operator waiting on it sees it.
type Outcome struct {
	State    State
	Device   Device // ID and Name on StatePaired; never the token, which is not stored
	Attempts int
}

// record is the on-disk offer. The code itself is not in it: a hash is enough
// to check a guess against, and nothing that reads this file later can pair
// with what it finds.
type record struct {
	State      State     `json:"state"`
	CodeHash   string    `json:"code_hash"`
	ExpiresAt  time.Time `json:"expires_at"`
	Attempts   int       `json:"attempts"`
	ClientID   string    `json:"client_id,omitempty"`
	ClientName string    `json:"client_name,omitempty"`
}

// Store is the outstanding offer, shared between the command that opens the
// window and the running host that closes it.
//
// A Store built without a register function can open and withdraw offers but
// never redeem one, which is exactly what `todo host pair` needs and all it is
// allowed to do: only the process serving requests may register a device.
//
// One host process is assumed. The mutex orders the redemptions of the process
// that serves them; a second host serving the same directory is not a
// supported configuration and never has been, since both would also be writing
// the same host.toml.
type Store struct {
	path     string
	register Register
	mu       sync.Mutex
}

// NewStore returns the offer store kept in dir, which is the app's config
// directory. register may be nil; see the Store comment.
func NewStore(dir string, register Register) *Store {
	return &Store{path: filepath.Join(dir, fileName), register: register}
}

// NewCode returns a fresh pairing code from a cryptographically secure source.
//
// Each character is one random byte reduced into Alphabet. The reduction is
// unbiased because the alphabet has exactly 32 characters and 256 is a
// multiple of it, so every character is equally likely — a property worth
// stating, because it would quietly stop holding if the alphabet ever changed
// length.
func NewCode() (string, error) {
	buf := make([]byte, CodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate pairing code: %w", err)
	}
	code := make([]byte, CodeLength)
	for i, b := range buf {
		code[i] = Alphabet[int(b)%len(Alphabet)]
	}
	return string(code), nil
}

// Open offers code for window, and refuses if another offer is already
// outstanding rather than replacing it: silently invalidating a code an
// operator is at that moment carrying to another machine is the confusing
// failure this avoids.
func (s *Store) Open(code string, window time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec, err := s.read(); err == nil && state(rec) == StateOpen {
		return errors.New("a pairing code is already outstanding on this host; finish that pairing, or wait for it to expire, before starting another")
	}
	return s.write(record{
		State:     StateOpen,
		CodeHash:  hashCode(code),
		ExpiresAt: time.Now().Add(window).UTC(),
	})
}

// Withdraw closes the window whatever state it is in. It is what makes
// interrupting `todo host pair` shut the door rather than leave it ajar, so it
// must succeed when there is nothing to withdraw.
func (s *Store) Withdraw() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("withdraw pairing code: %w", err)
	}
	return nil
}

// Outcome reports how the offer stands, for the command waiting on it. An
// unreadable or absent file is StateNone: there is nothing to wait for either
// way.
func (s *Store) Outcome() Outcome {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.read()
	if err != nil {
		return Outcome{State: StateNone}
	}
	return Outcome{
		State:    state(rec),
		Device:   Device{ID: rec.ClientID, Name: rec.ClientName},
		Attempts: rec.Attempts,
	}
}

// Redeem exchanges code for a newly registered device named deviceName, and
// reports false for every reason it could not: no offer outstanding, an
// expired or already-consumed or burned one, a wrong code, or a registration
// that failed. The caller is required to answer all of those identically —
// distinguishing them is how a scan learns that a pairing window is open.
//
// Success consumes the offer before returning, so the second use of a code
// finds a terminal state and fails like any other wrong guess. A wrong code
// costs the offer one of its MaxAttempts, and the last one burns it.
//
// Registration happens here, under the lock, rather than in the caller: it is
// the step that must happen exactly once per code, and holding the lock across
// it is what guarantees two simultaneous correct guesses cannot both be
// granted.
func (s *Store) Redeem(code, deviceName string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.read()
	if err != nil {
		return Device{}, false
	}
	switch state(rec) {
	case StateOpen:
	case StateExpired:
		rec.State = StateExpired
		s.write(rec) //nolint:errcheck // the offer is closed either way; nothing to tell the device
		return Device{}, false
	default:
		return Device{}, false
	}

	if subtle.ConstantTimeCompare([]byte(hashCode(code)), []byte(rec.CodeHash)) != 1 {
		rec.Attempts++
		if rec.Attempts >= MaxAttempts {
			rec.State = StateBurned
		}
		s.write(rec) //nolint:errcheck // a failed write leaves the offer as it was, which is no worse than the guess
		return Device{}, false
	}

	if s.register == nil {
		return Device{}, false
	}
	device, err := s.register(deviceName)
	if err != nil {
		// The code was right, so it is spent; a registration that failed
		// halfway must not leave it usable for a retry by someone else.
		rec.State = StateBurned
		s.write(rec) //nolint:errcheck // see above
		return Device{}, false
	}

	rec.State, rec.ClientID, rec.ClientName = StatePaired, device.ID, device.Name
	s.write(rec) //nolint:errcheck // the credential is already registered; the record is only what the waiting command reads
	return device, true
}

// state resolves an offer that is nominally open but past its window. Expiry
// is computed on every read rather than swept by a timer, so a host that was
// not running when the window closed still treats the offer as shut.
func state(rec record) State {
	if rec.State == StateOpen && time.Now().After(rec.ExpiresAt) {
		return StateExpired
	}
	return rec.State
}

func (s *Store) read() (record, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return record{}, err
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil {
		return record{}, fmt.Errorf("parse %s: %w", s.path, err)
	}
	return rec, nil
}

func (s *Store) write(rec record) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode pairing offer: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	return nil
}

// hashCode reduces a code to what is stored and compared.
//
// SHA-256 rather than a password hash, deliberately: a password hash buys work
// factor against an offline attack on a long-lived secret, and this secret
// lives for minutes and survives five guesses. The defences that matter here
// are the window and the lockout, and making every guess cost the host a
// hundred milliseconds would turn the route into its own denial of service.
func hashCode(code string) string {
	sum := sha256.Sum256([]byte(normalize(code)))
	return hex.EncodeToString(sum[:])
}

// normalize is what lets a person retype a code without being punished for
// how they read it: case is ignored, spaces and dashes they added are
// dropped, and the three letters the alphabet excludes are folded onto the
// digits they look like. It runs on both sides of the comparison, so the
// stored hash is always of the canonical form.
func normalize(code string) string {
	var b strings.Builder
	b.Grow(len(code))
	for _, r := range strings.ToUpper(code) {
		switch r {
		case ' ', '-', '_', '\t':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		b.WriteRune(r)
	}
	return b.String()
}
