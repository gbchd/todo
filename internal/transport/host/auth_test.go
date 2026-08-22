package host

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/todo/internal/credential"
)

// registered is a credential.Source over a fixed set of devices — the shape
// host.toml's Clients take once they reach authentication.
func registered(creds ...credential.Credential) credential.Source {
	return func(id string) (credential.Credential, bool) {
		for _, c := range creds {
			if c.ID == id {
				return c, true
			}
		}
		return credential.Credential{}, false
	}
}

func issue(t *testing.T) (credential.Credential, string) {
	t.Helper()
	cred, token, err := credential.Issue()
	require.NoError(t, err, "issue credential")
	return cred, token
}

// newGuardedMux builds the host the way host.Run does: the protocol version
// checked outermost, then the credential, then the task API.
func newGuardedMux(t *testing.T, src credential.Source) http.Handler {
	t.Helper()
	return newTestMux(t, RequireProtocolVersion, Authenticate(src))
}

// request drives the guarded mux with whatever headers a test wants to set,
// including none.
func request(t *testing.T, mux http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// authed is the header set a correctly configured client sends.
func authed(token string) map[string]string {
	return map[string]string{
		"Authorization":       "Bearer " + token,
		ProtocolVersionHeader: ProtocolVersion,
	}
}

func TestAuth_AcceptsARegisteredDevice(t *testing.T) {
	cred, token := issue(t)
	mux := newGuardedMux(t, registered(cred))

	rec := request(t, mux, http.MethodGet, apiPrefix+"/tasks", authed(token))
	assert.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
}

// Possession of the URL alone must grant nothing.
func TestAuth_RejectsARequestWithNoCredential(t *testing.T) {
	cred, _ := issue(t)
	mux := newGuardedMux(t, registered(cred))

	rec := request(t, mux, http.MethodGet, apiPrefix+"/tasks", map[string]string{ProtocolVersionHeader: ProtocolVersion})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
	assert.Contains(t, decodeError(t, rec).Error, "no device credential")
}

// Every write verb is guarded, not just the one a test happened to pick.
func TestAuth_GuardsEveryTaskRoute(t *testing.T) {
	cred, _ := issue(t)
	mux := newGuardedMux(t, registered(cred))

	routes := []struct{ method, path string }{
		{http.MethodGet, apiPrefix + "/tasks"},
		{http.MethodPost, apiPrefix + "/tasks"},
		{http.MethodGet, apiPrefix + "/tasks/1"},
		{http.MethodPatch, apiPrefix + "/tasks/1"},
		{http.MethodDelete, apiPrefix + "/tasks/1"},
	}
	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			rec := request(t, mux, r.method, r.path, map[string]string{ProtocolVersionHeader: ProtocolVersion})
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

// The time a rejection takes must not give the difference away either, and
// that includes the first unknown id a freshly started host ever sees.
// Authenticate mints the credential a lookup miss is checked against while the
// handler is being assembled, so no request pays for it; minting it on the
// first miss instead would make that request cost two password hashes where a
// known id costs one.
func TestAuth_TheFirstUnknownIDCostsWhatAKnownOneCosts(t *testing.T) {
	cred, _ := issue(t)
	mux := newGuardedMux(t, registered(cred))

	// Deliberately the very first request this mux serves.
	firstUnknown := timeRejection(t, mux, "nosuchdevice.wrong")

	known := firstUnknown
	for range 3 {
		known = min(known, timeRejection(t, mux, cred.ID+".wrong"))
	}

	assert.Less(t, firstUnknown, known*3/2,
		"the first unknown id took %s against %s for a known one, which is a readable answer to \"is this device registered?\"",
		firstUnknown, known)
}

// timeRejection measures one rejected request end to end, which is one
// password hash plus noise nothing here is sensitive to.
func timeRejection(t *testing.T, mux http.Handler, token string) time.Duration {
	t.Helper()
	start := time.Now()
	rec := request(t, mux, http.MethodGet, apiPrefix+"/tasks", authed(token))
	elapsed := time.Since(start)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	return elapsed
}

// An unknown id and a wrong secret must be one answer, not two: the difference
// between them is exactly the fact a scan would be looking for.
func TestAuth_RejectsBadCredentialsIdentically(t *testing.T) {
	cred, token := issue(t)
	other, _ := issue(t)
	_, otherToken := issue(t)
	mux := newGuardedMux(t, registered(cred))

	tests := []struct {
		name  string
		token string
	}{
		{"unknown device id", other.ID + ".whatever"},
		{"wrong secret for a known id", cred.ID + ".wrong"},
		{"another device's whole token", otherToken},
		{"no separator", "justonestring"},
		{"empty secret", cred.ID + "."},
	}

	var bodies []string
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := request(t, mux, http.MethodGet, apiPrefix+"/tasks", authed(tc.token))
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			bodies = append(bodies, decodeError(t, rec).Error)
		})
	}
	for _, body := range bodies {
		assert.Equal(t, bodies[0], body, "every rejected credential must produce the same message")
	}

	// And the token that does work still works, so the rejections above are
	// about the credentials and not about the host.
	rec := request(t, mux, http.MethodGet, apiPrefix+"/tasks", authed(token))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuth_RejectsANonBearerScheme(t *testing.T) {
	cred, token := issue(t)
	mux := newGuardedMux(t, registered(cred))

	rec := request(t, mux, http.MethodGet, apiPrefix+"/tasks", map[string]string{
		"Authorization":       "Basic " + token,
		ProtocolVersionHeader: ProtocolVersion,
	})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// Revoking the laptop must not cost the desktop its access, and must take
// effect on the next request rather than on the next restart.
func TestAuth_RevokingOneDeviceLeavesTheOthersWorking(t *testing.T) {
	laptop, laptopToken := issue(t)
	desktop, desktopToken := issue(t)

	devices := []credential.Credential{laptop, desktop}
	mux := newGuardedMux(t, func(id string) (credential.Credential, bool) {
		return registered(devices...)(id)
	})

	require.Equal(t, http.StatusOK, request(t, mux, http.MethodGet, apiPrefix+"/tasks", authed(laptopToken)).Code)
	require.Equal(t, http.StatusOK, request(t, mux, http.MethodGet, apiPrefix+"/tasks", authed(desktopToken)).Code)

	devices = []credential.Credential{desktop}

	assert.Equal(t, http.StatusUnauthorized,
		request(t, mux, http.MethodGet, apiPrefix+"/tasks", authed(laptopToken)).Code,
		"the revoked device must be rejected on its very next request")
	assert.Equal(t, http.StatusOK,
		request(t, mux, http.MethodGet, apiPrefix+"/tasks", authed(desktopToken)).Code,
		"every other device keeps working")
}

// A store with nothing in it — a fresh host, or one whose config file could
// not be read, since a Source that fails reports the same "not registered" —
// denies every token rather than waving anything through.
func TestAuth_FailsClosedWhenNoDeviceIsRegistered(t *testing.T) {
	_, token := issue(t)
	mux := newGuardedMux(t, registered())

	assert.Equal(t, http.StatusUnauthorized, request(t, mux, http.MethodGet, apiPrefix+"/tasks", authed(token)).Code)
}

func TestProtocolVersion_RejectsAMissingVersion(t *testing.T) {
	cred, token := issue(t)
	mux := newGuardedMux(t, registered(cred))

	rec := request(t, mux, http.MethodGet, apiPrefix+"/tasks", map[string]string{"Authorization": "Bearer " + token})
	require.Equal(t, http.StatusBadRequest, rec.Code, "a missing version is not a missing route")
	assert.NotEqual(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, decodeError(t, rec).Error, "upgrade todo")
}

func TestProtocolVersion_RejectsAnUnrecognisedVersion(t *testing.T) {
	cred, token := issue(t)
	mux := newGuardedMux(t, registered(cred))

	headers := authed(token)
	headers[ProtocolVersionHeader] = "99"
	rec := request(t, mux, http.MethodGet, apiPrefix+"/tasks", headers)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeError(t, rec).Error
	assert.Contains(t, body, "upgrade todo")
	assert.Contains(t, body, "99", "the message names the version the client asked for")
	assert.Contains(t, body, ProtocolVersion, "and the one this host speaks")
}

// A client too old to be understood is told to upgrade, not told its
// credential is bad: the second message would send its owner looking in the
// wrong place.
func TestProtocolVersion_IsCheckedBeforeTheCredential(t *testing.T) {
	cred, _ := issue(t)
	mux := newGuardedMux(t, registered(cred))

	rec := request(t, mux, http.MethodGet, apiPrefix+"/tasks", map[string]string{ProtocolVersionHeader: "99"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decodeError(t, rec).Error, "upgrade todo")
}

// The guards sit above the handlers and must not flatten what they return: a
// missing task, a rejected field, and a stale version stay three answers.
func TestGuardedMux_StillMapsDomainErrorsOntoDistinctStatusCodes(t *testing.T) {
	cred, token := issue(t)
	svc := newTestService(t, filepath.Join(t.TempDir(), "todo.db"))
	mux := NewMux(svc, nil, RequireProtocolVersion, Authenticate(registered(cred)))

	do := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		for k, v := range authed(token) {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	created := do(http.MethodPost, apiPrefix+"/tasks", `{"title":"Buy milk"}`)
	require.Equal(t, http.StatusCreated, created.Code, "body=%s", created.Body.String())
	task := decodeTask(t, created)

	assert.Equal(t, http.StatusNotFound, do(http.MethodGet, apiPrefix+"/tasks/9999", "").Code)
	assert.Equal(t, http.StatusBadRequest, do(http.MethodPost, apiPrefix+"/tasks", `{"title":""}`).Code)
	assert.Equal(t, http.StatusConflict,
		do(http.MethodPatch, apiPrefix+"/tasks/"+strconv.FormatInt(task.ID, 10), `{"title":"Buy oat milk","expected_version":99}`).Code)
}
