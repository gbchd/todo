package host

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gbchd/todo/internal/pairing"
)

const (
	// pairBurst and pairRefill set the global rate limit on pairing requests:
	// ten at once, then one every six seconds. Generous for the one device a
	// person is standing in front of, and useless to anyone working through a
	// thirty-bit keyspace — at this rate the five guesses an offer survives
	// take longer than the offer does.
	//
	// The limit is global rather than per-address on purpose. Per-address
	// limits are what an attacker with more than one address defeats for free,
	// and the legitimate traffic this route ever sees is one request from one
	// device.
	pairBurst  = 10
	pairRefill = time.Minute

	// pairMaxBody caps what will be read from an unauthenticated caller. The
	// body is a code and a device name; anything larger is not a pairing
	// attempt.
	pairMaxBody = 4 << 10

	// maxDeviceName bounds the label a device asks to be listed under, since
	// it is written into the operator's host.toml and printed by `todo host
	// clients`.
	maxDeviceName = 64

	// unnamedDevice is what a device that offered no usable name is listed as.
	// An empty name in the list would read like a bug in the host.
	unnamedDevice = "device"
)

// pairDevice serves the one route on the host that needs no credential, which
// makes it the most exposed surface in this package: it is reachable by
// anybody who can reach the address, and what it hands out is a long-lived
// credential.
//
// It answers exactly one of two things. On success, the device's token. On
// every single other path — the wrong method, a body that will not parse, no
// offer outstanding, an expired or spent or burned offer, a wrong code, the
// rate limit, a registration that failed — it answers with http.NotFound,
// which is byte for byte what this mux returns for a route that does not
// exist. A scan of the host therefore learns nothing: not that pairing exists,
// not that a window is open, not that a guess was close.
//
// Note what is deliberately absent: the protocol version header is not
// required here, though every task route requires it. Enforcing it would mean
// either answering a version mismatch with an explanation, which tells a
// scanner the route is real, or answering it with a 404, which tells the
// device's owner nothing useful. Letting an old client pair and then meet the
// clear "upgrade todo" message on its first task request is better than both.
func pairDevice(pairs *pairing.Store, limit *rateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		// The limit is checked before the offer is touched, so that flooding
		// the route cannot burn a code an operator is waiting on.
		if !limit.allow() {
			http.NotFound(w, r)
			return
		}

		var req pairing.Request
		if err := json.NewDecoder(io.LimitReader(r.Body, pairMaxBody)).Decode(&req); err != nil {
			http.NotFound(w, r)
			return
		}

		device, ok := pairs.Redeem(req.Code, deviceName(req.Name))
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, pairing.Response{
			Token:    device.Token,
			ClientID: device.ID,
			Name:     device.Name,
		})
	}
}

// deviceName cleans the label a device asked for. It is the one piece of
// attacker-supplied text that ends up in a file the operator reads, so
// unprintable characters are dropped and the length is bounded: a name that
// smuggles in newlines or escape sequences could otherwise forge extra rows in
// `todo host clients`.
func deviceName(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, strings.TrimSpace(name))

	if runes := []rune(cleaned); len(runes) > maxDeviceName {
		cleaned = string(runes[:maxDeviceName])
	}
	if strings.TrimSpace(cleaned) == "" {
		return unnamedDevice
	}
	return cleaned
}

// rateLimiter is a token bucket over the whole route, not per caller.
//
// Tokens rather than a fixed window because a fixed window lets an attacker
// spend the next window's whole allowance the instant it opens; the bucket
// spreads the same budget out. now is a field so tests can drive the refill
// without sleeping through it.
type rateLimiter struct {
	mu     sync.Mutex
	tokens float64
	burst  float64
	perSec float64
	last   time.Time
	now    func() time.Time
}

// newRateLimiter allows burst requests immediately and refills the bucket
// completely over per.
func newRateLimiter(burst int, per time.Duration) *rateLimiter {
	return &rateLimiter{
		tokens: float64(burst),
		burst:  float64(burst),
		perSec: float64(burst) / per.Seconds(),
		last:   time.Now(),
		now:    time.Now,
	}
}

// allow reports whether this request is within the limit, and spends a token
// if it is.
func (l *rateLimiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if elapsed := now.Sub(l.last).Seconds(); elapsed > 0 {
		l.tokens = math.Min(l.burst, l.tokens+elapsed*l.perSec)
	}
	l.last = now

	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
