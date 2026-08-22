package host

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gbchd/todo/internal/credential"
)

const (
	// ProtocolVersionHeader names the wire contract a request was written
	// against. The path prefix already carries a version, but a path can only
	// say "this route exists"; a client that reaches a route it no longer
	// understands otherwise gets a 404 that reads like a broken URL. The
	// header is what turns that into an answer the user can act on.
	ProtocolVersionHeader = "Todo-Protocol-Version"

	// ProtocolVersion is the version this build speaks. It is exported so a
	// client's HTTP repository sets exactly what the host checks.
	ProtocolVersion = "1"
)

// RequireProtocolVersion rejects any request that does not name a protocol
// version this build speaks, and says so in words the person reading them can
// act on: the fix is always to upgrade todo on the machine that sent it.
//
// It is the outermost middleware, ahead of authentication, because "your
// client is too old" stays true — and stays the more useful thing to say —
// even when the credential that came with it is stale, malformed, or absent.
// A client old enough to disagree about the protocol may well disagree about
// how credentials are presented, and answering that with "unauthorized" would
// send its owner looking in the wrong place.
func RequireProtocolVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch got := r.Header.Get(ProtocolVersionHeader); got {
		case ProtocolVersion:
			next.ServeHTTP(w, r)
		case "":
			writeJSON(w, http.StatusBadRequest, errorBody{Error: fmt.Sprintf(
				"this request named no protocol version, so it did not come from a todo client this host understands; this host speaks version %s — upgrade todo on the machine that sent it",
				ProtocolVersion)})
		default:
			writeJSON(w, http.StatusBadRequest, errorBody{Error: fmt.Sprintf(
				"this host speaks protocol version %s and the request asked for version %s; upgrade todo on the machine that sent it",
				ProtocolVersion, got)})
		}
	})
}

// bearerPrefix is the one authorization scheme the host accepts. The token
// after it is credential.Issue's second return value, stored verbatim by the
// device.
const bearerPrefix = "Bearer "

// Authenticate rejects every request that does not carry a token naming a
// registered device, so that reaching the address the host listens on grants
// nothing by itself.
//
// It wraps only the task API. A route registered outside that subtree — the
// pairing route, which by definition serves a device that has no credential
// yet — never reaches this.
func Authenticate(src credential.Source) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), bearerPrefix)
			if !ok || token == "" {
				denied(w, "this request carried no device credential; pair this machine with the host before using it")
				return
			}
			if !verify(src, token) {
				denied(w, "this host rejected the device credential; it may have been revoked — pair this machine with the host again")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// verify checks a token against the one credential it names.
//
// The two failing paths deliberately converge: an id nobody is registered
// under still runs a full hash verification, against a hash nothing satisfies,
// and reaches the same answer by the same route as a registered id with the
// wrong secret. Returning early on the lookup miss would leave the host
// answering "is this device id registered?" in the time it takes to reply,
// which is a question no unauthenticated caller may ask.
func verify(src credential.Source, token string) bool {
	id, secret, ok := credential.SplitToken(token)
	if !ok {
		return false
	}
	cred, registered := src(id)
	if !registered {
		return credential.VerifyAbsent(secret)
	}
	return cred.Verify(secret)
}

// denied answers with 401 and one of two messages. Which of them is chosen
// turns only on whether a token was presented at all — never on why one was
// rejected — so that a caller learns whether to send a credential, and nothing
// about which credentials would work.
func denied(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeJSON(w, http.StatusUnauthorized, errorBody{Error: msg})
}
