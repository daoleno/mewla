//go:build !linux

package browser

import "context"

type unavailableBackend struct{}

func NewBackend(func(string) (string, error)) Backend { return unavailableBackend{} }
func (unavailableBackend) Capability(context.Context) Capability {
	return Capability{Reason: "Managed Browser currently requires a Linux host"}
}
func (unavailableBackend) Start(context.Context, string) (Host, error) { return nil, ErrUnavailable }
