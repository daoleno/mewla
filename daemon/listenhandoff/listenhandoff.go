// Package listenhandoff carries the model relay's listening sockets across a
// daemon restart.
//
// Workers reach their model through loopback listeners the daemon owns (the
// Session routes and the machine gateway). When the daemon restarts, the
// listening socket itself must not close: a connection made while the old
// process drains or the new one starts then waits in the kernel's accept
// queue instead of being refused, and the new process answers it.
//
// The old process Holds each listener before it stops serving it (a
// duplicate of the socket that stays open after the server closes its own),
// then passes the held sockets on: through execve (ForExec) or to a child
// (ForChild). The new process takes each one back by its address (Listen)
// before it would bind the address itself, and Releases whatever it did not
// take back once its listeners are up.
//
// Linux and macOS only. Windows has no way to hand a listening socket to the
// next process here: a held socket is not kept, Listen binds anew, and a
// connection made in the gap is refused (the agents retry it).
package listenhandoff

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// EnvVar names the sockets a process inherited: "addr=fd" pairs separated by
// commas, such as "127.0.0.1:42403=3,127.0.0.1:3427=4".
const EnvVar = "MEWLA_LISTEN_FDS"

type registry struct {
	mu sync.Mutex
	// inherited are sockets this process was started with, by address, not
	// yet taken back.
	inherited map[string]*os.File
	// held are sockets this process keeps for the next one, by address.
	held map[string]*os.File
}

var process = newRegistry(os.Getenv(EnvVar))

func init() {
	// Children of this process (agents, helpers) inherit nothing of it.
	_ = os.Unsetenv(EnvVar)
}

func newRegistry(spec string) *registry {
	r := &registry{inherited: map[string]*os.File{}, held: map[string]*os.File{}}
	if !supported {
		return r
	}
	for _, pair := range strings.Split(spec, ",") {
		addr, fdText, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok {
			continue
		}
		fd, err := strconv.Atoi(fdText)
		if err != nil || fd < 3 || addr == "" {
			continue
		}
		file := os.NewFile(uintptr(fd), "listener "+addr)
		if file == nil {
			continue
		}
		markCloseOnExec(file)
		r.inherited[addr] = file
	}
	return r
}

// Listen returns the listening socket this process inherited for addr, or
// binds addr anew when it inherited none.
func Listen(network, addr string) (net.Listener, error) {
	return process.listen(network, addr)
}

func (r *registry) listen(network, addr string) (net.Listener, error) {
	if ln := r.take(network, addr); ln != nil {
		return ln, nil
	}
	return net.Listen(network, addr)
}

// take returns the inherited listener for addr, nil when there is none (or
// it is unusable, in which case it is closed).
func (r *registry) take(network, addr string) net.Listener {
	if network != "tcp" && network != "tcp4" {
		return nil
	}
	r.mu.Lock()
	file := r.inherited[addr]
	delete(r.inherited, addr)
	r.mu.Unlock()
	if file == nil {
		return nil
	}
	defer file.Close()
	ln, err := net.FileListener(file)
	if err != nil {
		return nil
	}
	if ln.Addr().String() != addr {
		_ = ln.Close()
		return nil
	}
	return ln
}

// Inherited reports whether this process still has an inherited socket for
// addr that it has not taken back.
func Inherited(addr string) bool {
	process.mu.Lock()
	defer process.mu.Unlock()
	_, ok := process.inherited[addr]
	return ok
}

// Release closes every inherited socket not taken back: no listener of this
// process serves that address any more, so a connection to it must be
// refused rather than wait forever.
func Release() {
	process.release()
}

func (r *registry) release() {
	r.mu.Lock()
	files := r.inherited
	r.inherited = map[string]*os.File{}
	r.mu.Unlock()
	for _, file := range files {
		_ = file.Close()
	}
}

// Hold keeps ln's socket listening after ln is closed, for the process that
// follows this one. It returns false where that is not possible (Windows, a
// listener that is not TCP).
func Hold(ln net.Listener) bool {
	return process.hold(ln)
}

func (r *registry) hold(ln net.Listener) bool {
	if !supported || ln == nil {
		return false
	}
	tcp, ok := ln.(*net.TCPListener)
	if !ok {
		return false
	}
	file, err := tcp.File()
	if err != nil {
		return false
	}
	addr := tcp.Addr().String()
	r.mu.Lock()
	previous := r.held[addr]
	r.held[addr] = file
	r.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	return true
}

// Held is the addresses of the sockets this process keeps for the next one.
func Held() []string {
	process.mu.Lock()
	defer process.mu.Unlock()
	addrs := make([]string, 0, len(process.held))
	for addr := range process.held {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	return addrs
}

// Drop closes the held sockets: no process follows this one, so connections
// waiting on them are refused now instead of when this process exits.
func Drop() {
	process.mu.Lock()
	files := process.held
	process.held = map[string]*os.File{}
	process.mu.Unlock()
	for _, file := range files {
		_ = file.Close()
	}
}

// ForExec readies the held sockets to survive execve and returns env with
// EnvVar naming them (any earlier value replaced). Call it immediately
// before the exec: until then the held sockets would also leak into any
// child the process starts.
func ForExec(env []string) []string {
	return process.forExec(env)
}

func (r *registry) forExec(env []string) []string {
	env = withoutVar(env)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.held) == 0 {
		return env
	}
	pairs := make([]string, 0, len(r.held))
	for _, addr := range sortedKeys(r.held) {
		file := r.held[addr]
		if !clearCloseOnExec(file) {
			continue
		}
		pairs = append(pairs, fmt.Sprintf("%s=%d", addr, file.Fd()))
	}
	if len(pairs) == 0 {
		return env
	}
	return append(env, EnvVar+"="+strings.Join(pairs, ","))
}

// ForChild returns env with EnvVar naming the held sockets as the child will
// see them, and the files to pass as the child's ExtraFiles (fd 3 onward).
// The parent keeps its own copies.
func ForChild(env []string) ([]string, []*os.File) {
	return process.forChild(env)
}

func (r *registry) forChild(env []string) ([]string, []*os.File) {
	env = withoutVar(env)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.held) == 0 {
		return env, nil
	}
	pairs := make([]string, 0, len(r.held))
	files := make([]*os.File, 0, len(r.held))
	for _, addr := range sortedKeys(r.held) {
		pairs = append(pairs, fmt.Sprintf("%s=%d", addr, 3+len(files)))
		files = append(files, r.held[addr])
	}
	return append(env, EnvVar+"="+strings.Join(pairs, ",")), files
}

func withoutVar(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, EnvVar+"=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func sortedKeys(files map[string]*os.File) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
