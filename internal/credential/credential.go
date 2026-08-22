// Package credential issues and verifies the per-device credentials that let
// a `todo host` tell one of its own devices from anyone else who can reach the
// address it listens on.
//
// A device holds one opaque string, its token. The host holds a record with no
// usable secret in it: an id and a password hash. The two halves of the token
// are what connect them — the id selects exactly one record, the secret is
// checked against that record's hash and nothing else. See docs/adr/0003.
package credential

import (
	"cmp"
	"crypto/rand"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// separator joins the id and the secret into the single string a device
// stores. It is a character neither half can contain: both are base32 text
// from crypto/rand, so splitting on the first one is unambiguous and the
// device never has to keep — or a user to copy — two values that must stay
// together.
const separator = "."

// DefaultCost is the bcrypt work factor Issue uses. It is deliberately the
// library default rather than the minimum: the secret's own entropy is the
// primary defence, and the work factor is what remains if a host's config file
// is read by someone who then wants to use it. The price is paid once per
// request, which on a personal host is small beside the round-trip that
// carried it.
const DefaultCost = bcrypt.DefaultCost

// Credential is one registered device as authentication sees it: the id a
// token names and the hash a secret is checked against. Neither field can be
// replayed as a credential, which is what lets the record sit in a config file
// on disk.
type Credential struct {
	ID         string
	SecretHash string
}

// Source returns the credential registered under id, and reports whether any
// device is registered under it at all.
//
// It is a function rather than a snapshot so that the set of registered
// devices can be read afresh per request: revoking a lost device has to take
// effect without restarting the host. A Source that cannot read its store must
// report false — failing closed — because reporting an error the caller might
// mistake for "not registered" is the same outcome, and failing open is not an
// option.
type Source func(id string) (Credential, bool)

// Issue mints a credential for a new device: the record the host stores and
// the token the device keeps. The secret is returned only inside that token
// and is retained nowhere, so a lost token cannot be recovered — only revoked
// and replaced.
//
// This is the seam pairing mounts on: pairing chooses when a device may be
// registered and how the token reaches it, and calls this for the credential
// itself.
func Issue() (Credential, string, error) {
	return Issuer{}.Issue()
}

// Issuer mints credentials at a chosen bcrypt work factor.
//
// It exists for one reason: a test suite that makes hundreds of authenticated
// requests pays DefaultCost on every one of them, which turns a second of
// testing into minutes. Its zero value is DefaultCost, so the production path
// — Issue, above — cannot be weakened by forgetting to set anything; only a
// caller that deliberately writes a Cost down gets anything else, and the only
// callers that do are tests. Verification needs no such seam: bcrypt reads the
// work factor back out of the hash it is checking against.
type Issuer struct {
	// Cost is the bcrypt work factor; zero means DefaultCost.
	Cost int
}

// Issue mints a credential hashed at i.Cost.
func (i Issuer) Issue() (Credential, string, error) {
	id, secret := rand.Text(), rand.Text()
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), cmp.Or(i.Cost, DefaultCost))
	if err != nil {
		return Credential{}, "", fmt.Errorf("hashing device secret: %w", err)
	}
	return Credential{ID: id, SecretHash: string(hash)}, id + separator + secret, nil
}

// SplitToken splits a device token into the id that selects a credential and
// the secret that proves it. It reports false for anything that is not two
// non-empty halves, which is the only structural check made: whether the id
// names a device is Verify's question, and answering it here would let a
// caller learn the difference between a malformed token and a valid one for an
// id that does not exist.
func SplitToken(token string) (id, secret string, ok bool) {
	id, secret, ok = strings.Cut(token, separator)
	if !ok || id == "" || secret == "" {
		return "", "", false
	}
	return id, secret, true
}

// Verify reports whether secret is the one this credential was issued with.
//
// Exactly one hash comparison happens per request, because the token named
// exactly one credential: there is no loop over registered devices, so the
// work done does not grow with the number of devices and cannot vary with
// which one presented itself.
func (c Credential) Verify(secret string) bool {
	return bcrypt.CompareHashAndPassword([]byte(c.SecretHash), []byte(secret)) == nil
}

// VerifyAbsent does the work Verify does — the same function, the same work
// factor — against the hash of a secret nobody was ever given, and so always
// reports false.
//
// An unknown id must cost what a known one costs. Skipping the hash when the
// lookup misses would make "no such device" the fast answer and turn the host
// into an oracle for which ids are registered, which is the one thing a device
// id is not allowed to leak.
func VerifyAbsent(secret string) bool {
	return absent().Verify(secret)
}

// absent is a credential for a secret that was generated, hashed, and
// discarded unread, so nothing can satisfy it. It is computed once and lazily:
// once because it is a constant, lazily so that a host that never sees an
// unknown id never pays for it, and generated rather than written into the
// source so that no two installations share it.
var absent = sync.OnceValue(func() Credential {
	// GenerateFromPassword fails only on an out-of-range cost, and cost is a
	// constant this package owns. An empty hash would still verify to false.
	hash, _ := bcrypt.GenerateFromPassword([]byte(rand.Text()), DefaultCost)
	return Credential{SecretHash: string(hash)}
})
