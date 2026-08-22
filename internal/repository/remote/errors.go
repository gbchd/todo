package remote

import (
	"errors"
	"fmt"
)

// The three failures that belong to the wire rather than to the domain. They
// are separate sentinels, not one "remote error", because the fix for each is
// somewhere else entirely: the network, the config file, and the installed
// version of todo. Every interface above the port is expected to be able to
// tell them apart with errors.Is, and to say which one happened.
//
// A domain error — ErrNotFound, *ValidationError, *ConflictError — never
// arrives wrapped in one of these: the host raised it, and a caller must not
// be able to tell that it crossed a network on the way back. *ConflictError is
// the fourth failure a paired device has to be able to name — another device
// got there first — and it deliberately stays a domain error, because it means
// the same thing and takes the same fix whether the race happened across a
// network or inside one SQLite transaction.
//
// Each of the three names the host in its message. The machine that has to be
// fixed is usually not the machine the command was typed on, and a user with
// two hosts in two shells has nothing else to tell the two answers apart by.
var (
	// ErrUnreachable means the request never got an answer: the host is down,
	// the address is wrong, the network dropped it, or the timeout expired.
	ErrUnreachable = errors.New("cannot reach the todo host")

	// ErrUnauthenticated means the host answered, and refused the credential
	// this device presented.
	ErrUnauthenticated = errors.New("the todo host rejected this device's credential")

	// ErrProtocolMismatch means the host answered, and does not speak the
	// protocol this build of todo writes.
	ErrProtocolMismatch = errors.New("this todo and the host do not speak the same protocol")
)

// unreachable wraps a transport-level failure.
func (r *Repository) unreachable(err error) error {
	return fmt.Errorf("%w at %s: %w", ErrUnreachable, r.baseURL, err)
}

// rejected wraps the host's own words about a credential it would not accept.
func (r *Repository) rejected(said string) error {
	return fmt.Errorf("%w: %s said: %s; run `todo pair %s <code>` to pair this device again",
		ErrUnauthenticated, r.baseURL, said, r.baseURL)
}

// mismatch wraps a disagreement about the protocol, carrying the host's words
// where it had any.
func (r *Repository) mismatch(said string) error {
	return fmt.Errorf("%w: %s said: %s; upgrade todo on this device or on the host",
		ErrProtocolMismatch, r.baseURL, said)
}
