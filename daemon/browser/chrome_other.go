//go:build !linux

package browser

import (
	"context"
	"image"
)

type unavailableBackend struct{}

// NewBackend is the stub: the agent browser runs only where the agent screen does.
func NewBackend(func(string) (string, error), Screens) Backend { return unavailableBackend{} }
func (unavailableBackend) Capability(context.Context) Capability {
	return Capability{Reason: "The agent screen currently requires a Linux host"}
}
func (unavailableBackend) Start(context.Context, string) (Host, error) { return nil, ErrUnavailable }

var _ = image.Rectangle{}
