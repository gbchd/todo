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
// be able to tell that it crossed a network on the way back.
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

// unreachable wraps a transport-level failure. The host URL is in the message
// because the machine running this command is usually not the machine that
// needs fixing.
func (r *Repository) unreachable(err error) error {
	return fmt.Errorf("%w at %s: %w", ErrUnreachable, r.baseURL, err)
}
