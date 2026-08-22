# Device credentials are hashed, per-device secrets

A `todo host` may be reachable from outside the machine it runs on, so
possession of its URL must grant nothing. Every request to the task API carries
two things: a bearer token naming one registered device, and a protocol
version. Neither is optional and neither has a default.

The credential is per device and named, so that losing one laptop costs that
laptop its access and nothing else. The host stores an id and a password hash;
the secret exists only on the device. Reading `host.toml` — the thing an
attacker who got as far as the filesystem would read — yields no working
credential.

## The token is `<id>.<secret>`

The device stores one opaque string. The host splits it on the first `.`, looks
up exactly one credential by the id half, and verifies the secret half against
that credential's hash.

The alternative — one secret, compared against every registered credential in
turn — was rejected on two counts. It makes the work per request grow with the
number of devices, and it makes the work *vary* with which device presented
itself: the tenth device is verified more slowly than the first, which is a
signal about the store's contents that an unauthenticated caller should not be
able to take. Naming the credential up front makes the verification a single
constant-time comparison against a single hash, always.

That leaves the id itself as a thing the host could leak. If an unregistered id
returned early, the reply time would answer "is this id registered?" for anyone
who cared to ask. So an unknown id runs the *same* verification, at the same
work factor, against the hash of a secret that was generated, hashed, and
discarded unread. Both paths run one bcrypt comparison and both reach the same
401 with the same message. The dummy hash is generated per process rather than
written into the source, so no two installations share it.

It is generated *eagerly*, when `Authenticate` assembles the handler, and this
is not an optimisation. Computing it on the first miss would move the cost of
the dummy hash onto exactly one request — the first unknown id a freshly
started host sees — making that request twice the price of a known one and
handing back the same "is this id registered?" answer the dummy hash exists to
withhold. The work has to be done before any request can observe it, so it is
done while there is nothing to observe it.

`credential.Absent` exists only to make that equivalence something the code
states rather than something a reader has to verify by inspection: the
credential a miss falls back to is an ordinary `Credential`, checked with the
same `Verify`, on the same type, at the same cost.

## bcrypt, at the library default cost

The secret is 128 bits from `crypto/rand`, so an offline attacker who reads
`host.toml` is not going to guess it whatever the work factor. The work factor
is defence in depth: it is what remains if the secret turns out to be weaker
than intended, or if a future version lets a human choose one.

The cost is paid on every request — roughly 90ms on a laptop — and that is the
real price of this decision. It is charged against a round trip that already
crossed a network, on a host serving one person's task list, where request
volume is measured in dozens per day. A verified-token cache would buy the
latency back and would have to be invalidated on revoke, which is the one
operation that must never be even slightly stale. Not worth it. If it ever
becomes worth it, the cache goes behind `credential.Verify` and nothing else
changes.

## Considered options

**OAuth2 client_credentials** is the standard answer and would give token
expiry, rotation, and scopes. It was rejected because every part of it is
machinery for a problem this app does not have. There is one user, no third
parties, no delegated authority, and nothing to scope: a device either reaches
the task list or it does not. The flow's real product is a short-lived access
token, which buys containment of a leaked credential — but a leaked credential
here is a leaked *device*, and the answer to that is revocation, which is
already immediate. What it would cost is a token endpoint, an expiry clock on
both sides, refresh handling, and a client that can fail in a new way (its
token expired mid-command) for a benefit measured against threats that do not
apply. Device secrets are long-lived until revoked, and the revoke command is
what ends them.

**An always-on bootstrap secret** — one shared value, printed at first start or
written into the config, that any device may use to enrol itself — was rejected
because it is a permanent skeleton key. It never expires, it is the same on
every device that has ever seen it, and it cannot be revoked without revoking
everything, since revoking it is indistinguishable from changing it. Worse, it
is at its most exposed exactly when it is least needed: sitting in a config file
for months after the last device was paired, still able to mint new ones. The
enrolment window should be open only while a human is deliberately holding it
open, which is what host-initiated pairing does.

**Out-of-band secret files** — generate a credential on the host, copy the file
to the device by scp or a password manager — were rejected because they push the
one genuinely dangerous step onto the user and give them no way to do it well.
The secret ends up in a shell history, a clipboard, a Downloads folder, or a
chat message to oneself, and there is no moment at which it stops being valid
because nobody deletes the copy. It also fails the story this feature exists
for: adding a device should be one command on the host and one on the device,
not a file transfer between them.

## The protocol version is a header, not just a path

The API path already carries `/api/v1`, but a path can only say "this route
exists". A client built against a version the host has dropped would get a 404,
which reads like a broken URL or a misconfigured proxy — the two things a user
would then go and check, neither of which is wrong. So every request names the
version it was written against in `Todo-Protocol-Version`, and a missing or
unrecognised value is a 400 whose message says which version the host speaks and
that the fix is to upgrade `todo` on the machine that sent the request.

The check sits *outside* authentication. A client old enough to disagree about
the protocol may well disagree about how credentials are presented, and
answering that with "your credential was rejected" would send its owner to
re-pair a machine that needs upgrading instead. "Your client is too old" stays
both true and more useful when the credential is stale, malformed, or absent.

## Consequences

- **Authentication is a middleware over the versioned subtree, not over the
  whole mux.** `NewMux` mounts the task API under one pattern and wraps only
  that. A route registered on the outer mux — pairing, which by definition
  serves a device that has no credential yet — is reachable without one. That
  separation is load-bearing and is why the two muxes exist.
- **The credential store is read per request, not cached at startup.** Revoking
  a device that was lost this morning has to take effect this morning; a
  revocation that waits for the operator to remember to restart the host is not
  a revocation. Re-parsing a few lines of TOML costs nothing beside the hash
  verification that follows it. A file that cannot be read denies the request.
- **A rejected credential is one answer, not several.** Unknown id, wrong
  secret, malformed token, and revoked device all produce the same 401 and the
  same message. Only "no credential was presented at all" is distinguished,
  because that one tells the user to send something rather than to send
  something else.
- **The secret is never recoverable.** Nothing retains it after `Issue`
  returns. A device that loses its token is revoked and paired again; there is
  no rotate-in-place.
- **A device name is a label, not an identity.** Two laptops may both be called
  "laptop". `todo host revoke` accepts a name when it is unambiguous and refuses
  to guess when it is not, naming the ids to disambiguate with.
- **`todo host clients` has no column that could ever hold a secret.** It shows
  name, id, and when the device was added. The id is half a token and the half
  that proves nothing, so the listing is safe to read aloud.
