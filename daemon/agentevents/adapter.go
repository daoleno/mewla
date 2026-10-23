package agentevents

import "sync"

// Adapter is one agent's side of the contract. Core code holds adapters only
// through a Registry and reads what they declare; it never names an agent.
type Adapter interface {
	// Descriptor declares what the adapter can do and at what level.
	Descriptor() Descriptor
	// PrepareLaunch returns the command to launch the agent with, adding
	// whatever lets Mewla follow it from its first moment (a session id, a
	// hook file). A command the adapter cannot safely extend comes back
	// unchanged: the Worker is then followed by the fallback.
	PrepareLaunch(command string) (string, error)
}

// Registry maps a provider name (an executor's declared provider) to its
// adapter. The names appear here and in each adapter's own package, nowhere
// else.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry { return &Registry{adapters: map[string]Adapter{}} }

// Register installs the adapter for a provider, replacing any earlier one.
func (r *Registry) Register(provider string, a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[provider] = a
}

// For returns the provider's adapter; ok is false for an agent without one,
// which is followed by the fallback alone.
func (r *Registry) For(provider string) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[provider]
	return a, ok
}
