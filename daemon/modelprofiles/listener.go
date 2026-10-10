package modelprofiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// ListenerFile persists the loopback Router listen address (host:port only).
type ListenerFile struct {
	path    string
	dirSync func(string) error
	// Test seams (nil => real OS).
	readFile func(path string) ([]byte, error)
	hook     func(phase string) error
}

type listenerDocument struct {
	ListenAddr string `json:"listen_addr"`
}

// NewListenerFile constructs a listener-state owner at path.
func NewListenerFile(path string) (*ListenerFile, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%w: listener path is required", ErrInvalid)
	}
	return &ListenerFile{path: path}, nil
}

// Path returns the durable file path.
func (f *ListenerFile) Path() string {
	if f == nil {
		return ""
	}
	return f.path
}

// Load returns the persisted listen address, or "" when missing.
func (f *ListenerFile) Load() (string, error) {
	if f == nil {
		return "", fmt.Errorf("listener file is not configured")
	}
	raw, err := f.readBytes()
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var doc listenerDocument
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return "", fmt.Errorf("%w: listener state: %v", ErrRouteSnapshotInvalid, err)
	}
	addr := strings.TrimSpace(doc.ListenAddr)
	if addr == "" {
		return "", fmt.Errorf("%w: listener state missing listen_addr", ErrRouteSnapshotInvalid)
	}
	if _, _, err := splitHostPortStrict(addr); err != nil {
		return "", fmt.Errorf("%w: listener listen_addr: %v", ErrRouteSnapshotInvalid, err)
	}
	return addr, nil
}

// Save persists listenAddr at 0600.
func (f *ListenerFile) Save(listenAddr string) error {
	if f == nil {
		return fmt.Errorf("listener file is not configured")
	}
	listenAddr = strings.TrimSpace(listenAddr)
	if _, _, err := splitHostPortStrict(listenAddr); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	raw, err := json.MarshalIndent(listenerDocument{ListenAddr: listenAddr}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return f.atomicWrite(raw)
}

// RestoreBytes writes exact prior metadata bytes via temp+fsync+rename+dirsync.
func (f *ListenerFile) RestoreBytes(raw []byte) error {
	if f == nil {
		return fmt.Errorf("listener file is not configured")
	}
	return f.atomicWrite(append([]byte{}, raw...))
}

// RemoveDurable removes the listener file and dirsyncs the parent when present.
func (f *ListenerFile) RemoveDurable() error {
	if f == nil {
		return fmt.Errorf("listener file is not configured")
	}
	if f.hook != nil {
		if err := f.hook("before_remove"); err != nil {
			return err
		}
	}
	if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if f.hook != nil {
		if err := f.hook("after_remove"); err != nil {
			return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
		}
	}
	dir := filepath.Dir(f.path)
	if err := f.syncParentDir(dir); err != nil {
		return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
	}
	return nil
}

// SetDirSync installs a test seam for directory fsync after rename/remove.
func (f *ListenerFile) SetDirSync(fn func(dir string) error) {
	if f == nil {
		return
	}
	f.dirSync = fn
}

// SetReadFile installs a test seam for listener metadata reads.
func (f *ListenerFile) SetReadFile(fn func(path string) ([]byte, error)) {
	if f == nil {
		return
	}
	f.readFile = fn
}

// SetPersistHook installs a test failpoint. Phases: before_write, after_write,
// before_rename, after_rename, before_dirsync, after_dirsync, before_remove, after_remove.
func (f *ListenerFile) SetPersistHook(hook func(phase string) error) {
	if f == nil {
		return
	}
	f.hook = hook
}

func (f *ListenerFile) readBytes() ([]byte, error) {
	if f != nil && f.readFile != nil {
		return f.readFile(f.path)
	}
	return os.ReadFile(f.path)
}

func (f *ListenerFile) atomicWrite(data []byte) error {
	return writeAtomicFileWithSeams(f.path, data, 0o600, f.hook, f.dirSync)
}

func (f *ListenerFile) syncParentDir(dir string) error {
	if f != nil && f.dirSync != nil {
		return f.dirSync(dir)
	}
	return atomicfile.SyncDir(dir)
}

func splitHostPortStrict(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	if host == "" || port == "" {
		return "", "", fmt.Errorf("host and port are required")
	}
	if !isLoopbackHost(host) {
		return "", "", fmt.Errorf("listen host must be loopback")
	}
	return host, port, nil
}
