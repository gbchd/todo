package pairing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registrar returns a Register that mints predictable devices and counts how
// many times it ran, which is how the tests assert a code registers exactly
// one device however many times it is presented.
func registrar(calls *int) Register {
	return func(name string) (Device, error) {
		*calls++
		return Device{ID: "id-1", Name: name, Token: "id-1.secret"}, nil
	}
}

func openStore(t *testing.T, register Register) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(dir, register)
	code, err := NewCode()
	require.NoError(t, err)
	require.NoError(t, store.Open(code, Window))
	return store, code
}

func TestNewCode_IsSixCharactersFromTheUnambiguousAlphabet(t *testing.T) {
	seen := make(map[string]bool)
	for range 200 {
		code, err := NewCode()
		require.NoError(t, err)
		require.Len(t, code, CodeLength)
		for _, r := range code {
			assert.Contains(t, Alphabet, string(r), "code %q uses a character outside the alphabet", code)
		}
		seen[code] = true
	}
	// Not a test of the generator's quality — that is crypto/rand's job — but
	// a canary for a code that is constant or trivially cyclic.
	assert.Greater(t, len(seen), 190, "codes must not repeat")
}

// The point of the alphabet is that a person retyping a code cannot get it
// wrong in these particular ways.
func TestNewCode_AlphabetExcludesTheLookalikeLetters(t *testing.T) {
	for _, r := range "ILOU" {
		assert.NotContains(t, Alphabet, string(r))
	}
	assert.Len(t, Alphabet, 32, "the reduction in NewCode is only unbiased for a 32-character alphabet")
}

func TestRedeem_ConsumesTheCodeOnFirstSuccess(t *testing.T) {
	calls := 0
	store, code := openStore(t, registrar(&calls))

	device, ok := store.Redeem(code, "laptop")
	require.True(t, ok)
	assert.Equal(t, "id-1", device.ID)
	assert.Equal(t, "laptop", device.Name)
	assert.Equal(t, "id-1.secret", device.Token)
	assert.Equal(t, StatePaired, store.Outcome().State)

	// A second use of a consumed code registers nothing.
	_, ok = store.Redeem(code, "attacker")
	assert.False(t, ok, "a consumed code must not be redeemable again")
	assert.Equal(t, 1, calls, "exactly one device may be registered per code")
}

func TestRedeem_AcceptsACodeTheOperatorRetypedLoosely(t *testing.T) {
	calls := 0
	store, code := openStore(t, registrar(&calls))

	_, ok := store.Redeem(strings.ToLower(code[:3])+" -"+strings.ToLower(code[3:]), "laptop")
	assert.True(t, ok, "case, spaces and dashes must not decide whether pairing works")
}

func TestRedeem_FailsWithNoOfferOutstanding(t *testing.T) {
	calls := 0
	store := NewStore(t.TempDir(), registrar(&calls))

	_, ok := store.Redeem("ABC123", "laptop")
	assert.False(t, ok)
	assert.Equal(t, StateNone, store.Outcome().State)
	assert.Zero(t, calls)
}

func TestRedeem_FailsOnceTheWindowHasClosed(t *testing.T) {
	calls := 0
	dir := t.TempDir()
	store := NewStore(dir, registrar(&calls))
	code, err := NewCode()
	require.NoError(t, err)
	require.NoError(t, store.Open(code, -time.Second))

	_, ok := store.Redeem(code, "laptop")
	assert.False(t, ok, "the right code must not work after the window closed")
	assert.Equal(t, StateExpired, store.Outcome().State)
	assert.Zero(t, calls)
}

// An offer left open past its window reads as expired even though no request
// ever arrived to notice, which is what lets `todo host pair` stop waiting.
func TestOutcome_ReportsExpiryWithoutAnyRequest(t *testing.T) {
	store := NewStore(t.TempDir(), nil)
	require.NoError(t, store.Open("ABC123", 40*time.Millisecond))
	assert.Equal(t, StateOpen, store.Outcome().State)

	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, StateExpired, store.Outcome().State)
}

func TestRedeem_BurnsTheOfferAfterTooManyWrongCodes(t *testing.T) {
	calls := 0
	store, code := openStore(t, registrar(&calls))

	for i := range MaxAttempts {
		_, ok := store.Redeem(wrongCode(code), "attacker")
		require.False(t, ok)
		if i < MaxAttempts-1 {
			require.Equal(t, StateOpen, store.Outcome().State, "attempt %d must not have burned the offer yet", i+1)
		}
	}
	require.Equal(t, StateBurned, store.Outcome().State)
	assert.Equal(t, MaxAttempts, store.Outcome().Attempts)

	// The genuine code is worthless now: guessing fails closed rather than
	// leaving the operator's outstanding code usable.
	_, ok := store.Redeem(code, "laptop")
	assert.False(t, ok, "a burned offer must not accept the right code either")
	assert.Zero(t, calls)
}

// A registration that fails halfway must not leave the code usable, because
// whoever presented it already knows it.
func TestRedeem_BurnsTheOfferWhenRegistrationFails(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, func(string) (Device, error) { return Device{}, errors.New("disk full") })
	code, err := NewCode()
	require.NoError(t, err)
	require.NoError(t, store.Open(code, Window))

	_, ok := store.Redeem(code, "laptop")
	assert.False(t, ok)
	assert.Equal(t, StateBurned, store.Outcome().State)
}

// The command that prints codes never registers devices; only the process
// serving requests does.
func TestRedeem_IsRefusedByAStoreWithNoRegistrar(t *testing.T) {
	store, code := openStore(t, nil)
	_, ok := store.Redeem(code, "laptop")
	assert.False(t, ok)
}

func TestWithdraw_ClosesTheWindow(t *testing.T) {
	calls := 0
	store, code := openStore(t, registrar(&calls))

	require.NoError(t, store.Withdraw())
	assert.Equal(t, StateNone, store.Outcome().State)

	_, ok := store.Redeem(code, "laptop")
	assert.False(t, ok, "interrupting the pairing command must invalidate its code")
	assert.NoError(t, store.Withdraw(), "withdrawing nothing must succeed")
}

// Replacing a code an operator may at that moment be carrying to another
// machine would fail in a way they could not explain.
func TestOpen_RefusesWhileAnotherOfferIsOutstanding(t *testing.T) {
	store, _ := openStore(t, nil)

	err := store.Open("ABC123", Window)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already outstanding")
}

func TestOpen_ReplacesAnOfferThatIsAlreadyOver(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, nil)
	require.NoError(t, store.Open("ABC123", -time.Second))

	require.NoError(t, store.Open("XYZ789", Window), "an expired offer must not block the next one")
	assert.Equal(t, StateOpen, store.Outcome().State)
}

// The file coordinates two processes, so it lands on disk where the running
// host will look for it — readable by nobody else, and holding no code.
func TestOpen_WritesAnOwnerOnlyFileThatDoesNotContainTheCode(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, nil)
	code, err := NewCode()
	require.NoError(t, err)
	require.NoError(t, store.Open(code, Window))

	path := filepath.Join(dir, fileName)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), code, "a copy of this file must not be a pairing code")
}

// A second process reads the same offer, which is the whole reason it is a
// file: `todo host pair` opens the window and the running host closes it.
func TestStore_IsSharedBetweenProcesses(t *testing.T) {
	dir := t.TempDir()
	operator := NewStore(dir, nil)
	code, err := NewCode()
	require.NoError(t, err)
	require.NoError(t, operator.Open(code, Window))

	calls := 0
	host := NewStore(dir, registrar(&calls))
	_, ok := host.Redeem(code, "laptop")
	require.True(t, ok)

	assert.Equal(t, StatePaired, operator.Outcome().State)
	assert.Equal(t, "laptop", operator.Outcome().Device.Name)
}

// wrongCode returns a code of the right shape that is not the right one.
func wrongCode(code string) string {
	next := strings.IndexByte(Alphabet, code[0])
	return string(Alphabet[(next+1)%len(Alphabet)]) + code[1:]
}
