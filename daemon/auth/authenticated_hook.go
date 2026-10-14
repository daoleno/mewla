package auth

import "context"

type authenticatedHookKey struct{}

// WithAuthenticatedHook returns ctx carrying hook. The server calls
// NotifyAuthenticated after it verifies a request's device signature or
// capability, so a transport can tell paired devices from anonymous peers.
func WithAuthenticatedHook(ctx context.Context, hook func()) context.Context {
	return context.WithValue(ctx, authenticatedHookKey{}, hook)
}

// NotifyAuthenticated runs the hook in ctx, if any.
func NotifyAuthenticated(ctx context.Context) {
	if hook, ok := ctx.Value(authenticatedHookKey{}).(func()); ok && hook != nil {
		hook()
	}
}
