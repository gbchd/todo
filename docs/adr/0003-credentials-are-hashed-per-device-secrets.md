# Device credentials are hashed, per-device secrets, handed over by pairing

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

## Pairing is a window a human holds open

A device with no credential still has to get one, and "one command on the host,
one on the device" means it comes over the same network the tasks do.
`todo host pair` prints a six-character code and waits; the device posts that
code to `/pair`; the host answers once, with a freshly issued token.

Six characters from a 32-character alphabet is about thirty bits. That is not
enough on its own — and it is never on its own. Four defences hold it up, and
they are joint rather than layered: each answers a different question, and to
the other three questions its answer is "that is not what I am for".

1. **The window is short.** Three minutes if nothing else closes it, and
   something else usually does: it closes on the first success, and it closes
   when the operator interrupts `todo host pair`, because the command that
   opened it is the command that withdraws it. This is what makes a code read
   over the operator's shoulder, or left on a screen they walked away from,
   worthless — and nothing else does.
2. **The code is single use.** The first success consumes the offer before it
   returns, so a code that already paired a device pairs nothing else. Without
   this a code that worked keeps working, and the operator has no reason ever
   to look at it again.
3. **Wrong guesses burn it.** Five wrong codes destroy the offer rather than
   merely rejecting the fifth. This is the defence that bounds *guessing*: it
   turns thirty bits with unlimited tries into thirty bits with five, and it
   fails closed — the attacker's reward for guessing is that nobody pairs.
4. **Pairing requests are globally rate-limited.** Ten at once, then one every
   six seconds. Its job is not the guessing, which (3) already bounds; its job
   is that (3) is also a weapon. Five requests can destroy an offer an operator
   is standing there waiting on, so the limit is checked *before* the offer is
   touched and a flood cannot burn a code. It is global rather than per
   address, because a per-address limit is the kind an attacker with more than
   one address defeats for free, and the legitimate traffic this route ever
   sees is one request from one device.

So none of the four may be relaxed on the grounds that another one covers it.
Loosening the window does not become safe because the code is single use;
raising the attempt limit for an operator who mistypes does not become safe
because requests are rate-limited. The short code is what makes pairing usable,
and these four together are the entire reason a short code is defensible.

## The pairing route answers as a route that does not exist

Every refusal is `http.NotFound` — the wrong method, a body that will not
parse, no offer outstanding, an expired or spent or burned offer, a wrong code,
the rate limit, a registration that failed. Not a similar 404: the same status,
the same headers, the same bytes the mux returns for a path it does not route.
The route is registered without a method for the same reason, so that a GET
reaches the handler and is answered with a 404 rather than having the mux reply
405, which would announce that the route is real.

The point is that a scan of the host learns nothing: not that this build knows
what pairing is, not that a window is open right now, not that a guess was
close. An enrolment endpoint that hands out long-lived credentials is the most
exposed surface in this design, and the cheapest thing it can give away is the
knowledge that it exists.

The protocol version header is deliberately *not* required here, though every
task route requires it. Enforcing it would mean either explaining a version
mismatch — which tells a scanner the route is real — or answering it with the
404, which tells the device's owner nothing they can act on. Letting an old
client pair and then meet the clear "upgrade todo" message on its first task
request is better than both.

The cost is paid by the honest device: `todo pair` cannot be told which refusal
it hit, so its error message has to name all of them at once — expired, already
used, mistyped, or no pairing in progress. That is the price of the property,
and it is the right way round: the message a legitimate user reads is longer,
and the message an attacker reads does not exist.

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

**Idempotency keys for pairing retries** — the device mints a key, sends it
alongside the code, and a repeated request carrying the same key gets the same
token back instead of a 404 — were rejected. The failure they would rescue is
real but small: the host registered the device and the reply was lost, leaving a
device with no token and a host with a client entry nobody holds. The recovery
for that today is already correct and takes one command — run `todo host pair`
again, pair the device, and revoke the stranded entry, which is a credential
nobody possesses. What the key would buy is not worth what it costs.

It costs the property. "A code is single use" would become "a code is single use
unless you ask for the same one again", which a reader has to reason about
rather than rely on, and every future change to pairing would have to be checked
against the exception. It costs server state: the host would have to remember
the key *and the token it returned* — the one secret this design goes out of its
way never to store — for long enough to be useful, past the point where the
offer itself is consumed. And it costs the indistinguishability above: a route
that remembers keys is a route with a second thing it can answer differently,
and the whole `/pair` design is that it has exactly one.

The general point: pairing is not an operation that wants to be retried. It is a
human standing at a machine for three minutes. When it fails, the human is right
there, and asking them to run it again is a better answer than a mechanism that
makes a single-use secret usable twice.

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
- **The four pairing defences are one mechanism, not a stack.** The window, the
  single use, the attempt lockout, and the global rate limit each cover
  something the others do not, and the six-character code is only defensible
  with all four. Changing any of them is a change to the whole argument.
- **`todo pair` cannot say why a code was refused.** The host answers every
  refusal identically on purpose, so the device's message names every reason at
  once. A friendlier message here would be a worse host.
- **A lost pairing reply costs one re-run.** There is no retry mechanism and no
  idempotency key: `todo host pair` again, and revoke the stranded client entry
  if one was left behind.
- **A device name is a label, not an identity.** Two laptops may both be called
  "laptop". `todo host revoke` accepts a name when it is unambiguous and refuses
  to guess when it is not, naming the ids to disambiguate with.
- **`todo host clients` has no column that could ever hold a secret.** It shows
  name, id, and when the device was added. The id is half a token and the half
  that proves nothing, so the listing is safe to read aloud.
