package host

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/credential"
	"github.com/gbchd/todo/internal/pairing"
)

// pairingMux builds a host whose pairing route is live, and returns the store
// so a test can open, inspect, or close the window the way `todo host pair`
// would from the other process.
func pairingMux(t *testing.T) (http.Handler, *pairing.Store) {
	t.Helper()
	store := pairing.NewStore(t.TempDir(), func(name string) (pairing.Device, error) {
		return pairing.Device{ID: "client-1", Name: name, Token: "client-1.secret"}, nil
	})
	svc := newTestService(t, filepath.Join(t.TempDir(), "todo.db"))
	// The task API is wrapped exactly as `todo host` wraps it, so the tests
	// also pin that pairing is reachable without a credential or a protocol
	// version while everything else is not.
	noCredentials := func(string) (credential.Credential, bool) { return credential.Credential{}, false }
	return NewMux(svc, store, RequireProtocolVersion, Authenticate(noCredentials)), store
}

func postPair(t *testing.T, mux http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, pairing.Path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func pairBody(code, name string) string {
	b, _ := json.Marshal(pairing.Request{Code: code, Name: name})
	return string(b)
}

func openOffer(t *testing.T, store *pairing.Store) string {
	t.Helper()
	code, err := pairing.NewCode()
	require.NoError(t, err)
	require.NoError(t, store.Open(code, pairing.Window))
	return code
}

// A device with no credential must be able to reach this one route, since
// having no credential is the situation pairing exists to fix.
func TestPair_HandsACredentialToADeviceThatHasNone(t *testing.T) {
	mux, store := pairingMux(t)
	code := openOffer(t, store)

	rec := postPair(t, mux, pairBody(code, "laptop"))
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	var got pairing.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "client-1.secret", got.Token)
	assert.Equal(t, "client-1", got.ClientID)
	assert.Equal(t, "laptop", got.Name)
}

// The load-bearing property of this route: whenever there is nothing to pair
// with, it must be impossible to tell it apart from a URL the host has never
// heard of. Not a similar 404 — the same bytes, the same headers.
func TestPair_IsIndistinguishableFromANonexistentRoute(t *testing.T) {
	mux, store := pairingMux(t)

	absent := httptest.NewRecorder()
	mux.ServeHTTP(absent, httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/no-such-route", strings.NewReader("")))
	require.Equal(t, http.StatusNotFound, absent.Code, "the baseline must be a genuine 404")

	openOffer(t, store)
	// Ordered rather than a map, because the last case has to run with the
	// window shut and the earlier ones with it open.
	cases := []struct {
		name  string
		body  string
		setup func()
	}{
		{name: "a wrong code", body: pairBody("XXXXXX", "laptop")},
		{name: "an empty code", body: pairBody("", "laptop")},
		{name: "a body that will not parse", body: "{"},
		{name: "no body at all", body: ""},
		{name: "no offer outstanding", body: pairBody("XXXXXX", "laptop"), setup: func() {
			require.NoError(t, store.Withdraw())
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			assertSameResponse(t, absent, postPair(t, mux, tt.body))
		})
	}

	t.Run("the wrong method", func(t *testing.T) {
		// A method-scoped route would have the mux answer 405 here, which
		// announces that the route is real.
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(
			t.Context(), http.MethodGet, pairing.Path, nil))
		assertSameResponse(t, absent, rec)
	})
}

// assertSameResponse holds two responses to be byte-identical in everything a
// caller can observe.
func assertSameResponse(t *testing.T, want, got *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, want.Code, got.Code, "status")
	assert.Equal(t, want.Body.String(), got.Body.String(), "body")
	assert.Equal(t, want.Header(), got.Header(), "headers")
}

func TestPair_RefusesASecondUseOfAConsumedCode(t *testing.T) {
	mux, store := pairingMux(t)
	code := openOffer(t, store)

	require.Equal(t, http.StatusOK, postPair(t, mux, pairBody(code, "laptop")).Code)

	again := postPair(t, mux, pairBody(code, "attacker"))
	assert.Equal(t, http.StatusNotFound, again.Code, "a code is spent by the device that used it")
}

func TestPair_RefusesAnExpiredCode(t *testing.T) {
	mux, store := pairingMux(t)
	code, err := pairing.NewCode()
	require.NoError(t, err)
	require.NoError(t, store.Open(code, -time.Second))

	assert.Equal(t, http.StatusNotFound, postPair(t, mux, pairBody(code, "laptop")).Code)
}

// Guessing must cost the attacker the target rather than eventually paying
// off: the outstanding code is destroyed, not merely the guesses rejected.
func TestPair_BurnsTheCodeAfterRepeatedWrongGuesses(t *testing.T) {
	mux, store := pairingMux(t)
	code := openOffer(t, store)

	for range pairing.MaxAttempts {
		require.Equal(t, http.StatusNotFound, postPair(t, mux, pairBody("XXXXXX", "attacker")).Code)
	}
	assert.Equal(t, http.StatusNotFound, postPair(t, mux, pairBody(code, "laptop")).Code,
		"the operator's own code must be dead once it has been guessed at")
	assert.Equal(t, pairing.StateBurned, store.Outcome().State)
}

// The rate limit is global and applies before the offer is touched, so a flood
// cannot even reach a code, let alone burn it.
func TestPair_RateLimitsPairingRequestsGlobally(t *testing.T) {
	mux, store := pairingMux(t)

	// Spend the whole bucket while nothing is outstanding, so the lockout
	// plays no part in what follows.
	for range pairBurst {
		require.Equal(t, http.StatusNotFound, postPair(t, mux, "").Code)
	}

	code := openOffer(t, store)
	assert.Equal(t, http.StatusNotFound, postPair(t, mux, pairBody(code, "laptop")).Code,
		"a request beyond the rate limit must be refused, right code or not")
	assert.Equal(t, pairing.StateOpen, store.Outcome().State,
		"a rate-limited request must not consume or burn the outstanding offer")
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	now := time.Now()
	limit := newRateLimiter(2, time.Minute)
	limit.now = func() time.Time { return now }
	limit.last = now

	assert.True(t, limit.allow())
	assert.True(t, limit.allow())
	assert.False(t, limit.allow(), "the burst is spent")

	now = now.Add(29 * time.Second)
	assert.False(t, limit.allow(), "less than one token has accrued")

	now = now.Add(2 * time.Second)
	assert.True(t, limit.allow(), "a token accrues once the refill interval has passed")
	assert.False(t, limit.allow(), "and only one")

	now = now.Add(time.Hour)
	assert.True(t, limit.allow())
	assert.True(t, limit.allow())
	assert.False(t, limit.allow(), "the bucket never fills past its burst")
}

func TestDeviceName_CleansWhatADeviceAsksToBeCalled(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a plain name", "laptop", "laptop"},
		{"surrounding space", "  laptop \n", "laptop"},
		{"empty", "", unnamedDevice},
		{"only space", "   ", unnamedDevice},
		{"forged rows", "laptop\nevil\tid\t2026", "laptopevilid2026"},
		{"an escape sequence", "lap\x1b[31mtop", "lap[31mtop"},
		{"far too long", strings.Repeat("x", 500), strings.Repeat("x", maxDeviceName)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, deviceName(tt.in))
		})
	}
}

// Pairing is mounted outside the task API, so nothing it is exempt from leaks
// onto the routes that are not.
func TestPair_DoesNotWeakenTheTaskAPI(t *testing.T) {
	mux, _ := pairingMux(t)

	rec := doJSON(t, mux, http.MethodGet, apiPrefix+"/tasks", nil)
	assert.NotEqual(t, http.StatusOK, rec.Code, "the task API still needs a protocol version and a credential")
}
