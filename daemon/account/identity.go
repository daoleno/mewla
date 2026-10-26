package account

import (
	"context"
	"time"
)

// DeviceStart is an identity provider's answer to a device authorization
// request. Handle is a provider secret (for GitHub, the device code); it is
// stored server side and never logged or returned to the daemon.
type DeviceStart struct {
	Handle                  string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	Interval                time.Duration
	ExpiresAt               time.Time
}

// PollStatus is the state of one device authorization.
type PollStatus int

const (
	PollPending PollStatus = iota
	PollComplete
	PollDenied
	PollExpired
)

// Subject identifies a person at an identity provider. ID is stable and
// provider scoped (GitHub numeric user id); Login is a display name.
type Subject struct {
	ID    string
	Login string
	// CreatedAt is when the provider account was made, when the provider says;
	// zero when unknown.
	CreatedAt time.Time
}

// PollResult reports one device authorization poll. SlowDown on a pending
// result asks for a longer polling interval: Interval when it is longer than
// the current one, else the current interval plus 5s (RFC 8628).
type PollResult struct {
	Status   PollStatus
	Subject  Subject
	SlowDown bool
	Interval time.Duration
}

// IdentityProvider is a device-flow identity provider. Implementations must
// not log handles, user codes or tokens.
type IdentityProvider interface {
	// Name is the provider prefix of account subjects, e.g. "github".
	Name() string
	StartDevice(ctx context.Context) (DeviceStart, error)
	Poll(ctx context.Context, handle string) (PollResult, error)
}
