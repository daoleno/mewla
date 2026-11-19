package acmecert

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Watcher serves the certificate a Keeper keeps in Dir, read only: it loads
// Dir/cert.pem and key.pem, reloads them when they change (what the Keeper
// wrote last wins, whatever its lifetime), and reports the Keeper's status
// file. The seed is served only while Dir holds no unexpired pair. It holds
// no credential and changes nothing in Dir.
type Watcher struct {
	dir, seedCert, seedKey string
	every                  time.Duration
	logf                   func(string, ...any)
	reload                 chan struct{}

	mu       sync.Mutex
	cert     *tls.Certificate
	fromSeed bool
	loaded   []byte // the cert.pem and key.pem last loaded from dir
	renewer  Status // from status.json
	readErr  string
}

// NewWatcher loads what dir holds now. every is how often it looks again
// (default one minute).
func NewWatcher(dir, seedCert, seedKey string, every time.Duration, logf func(string, ...any)) (*Watcher, error) {
	if dir == "" {
		return nil, errors.New("acmecert: a directory is required")
	}
	if every <= 0 {
		every = time.Minute
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	w := &Watcher{dir: dir, seedCert: seedCert, seedKey: seedKey, every: every, logf: logf, reload: make(chan struct{}, 1)}
	if seedCert != "" {
		seed, err := loadPair(seedCert, seedKey)
		if err != nil {
			logf("acmecert: seed certificate unreadable: %v", err)
		} else {
			w.cert, w.fromSeed = seed, true
		}
	}
	w.look()
	return w, nil
}

func (w *Watcher) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cert == nil {
		return nil, fmt.Errorf("no certificate in %s yet", w.dir)
	}
	return w.cert, nil
}

// Status is the served certificate's expiry with the renewer's report.
func (w *Watcher) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.renewer
	st.NotAfter = time.Time{}
	if w.cert != nil {
		st.NotAfter = w.cert.Leaf.NotAfter
	}
	if w.readErr != "" && st.LastError == "" {
		st.LastError = w.readErr
	}
	return st
}

// Reload asks Run to look at once.
func (w *Watcher) Reload() {
	select {
	case w.reload <- struct{}{}:
	default:
	}
}

func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(w.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-w.reload:
		}
		w.look()
	}
}

// look loads the certificate when its files changed and a pair parses, and
// reads the status file.
func (w *Watcher) look() {
	var renewer Status
	if raw, err := os.ReadFile(filepath.Join(w.dir, StatusFile)); err == nil {
		_ = json.Unmarshal(raw, &renewer)
	}
	certPath, keyPath := filepath.Join(w.dir, CertFile), filepath.Join(w.dir, KeyFile)
	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.renewer = renewer
	if certErr != nil || keyErr != nil {
		if !errors.Is(certErr, os.ErrNotExist) || !errors.Is(keyErr, os.ErrNotExist) {
			w.readErr = fmt.Sprintf("reading %s: %v %v", w.dir, certErr, keyErr)
		}
		return
	}
	both := append(append([]byte{}, certPEM...), keyPEM...)
	if bytes.Equal(both, w.loaded) {
		return
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		// A pair caught between its two writes: the next look takes it.
		w.readErr = fmt.Sprintf("loading %s: %v", w.dir, err)
		return
	}
	w.loaded, w.readErr = both, ""
	if w.fromSeed && !time.Now().Before(cert.Leaf.NotAfter) {
		w.logf("acmecert: %s holds an expired certificate; serving the seed", w.dir)
		return
	}
	w.cert, w.fromSeed = &cert, false
	w.logf("acmecert: serving %v from %s, serial %s, valid until %s", cert.Leaf.DNSNames, w.dir, cert.Leaf.SerialNumber.Text(16), cert.Leaf.NotAfter.UTC().Format(time.RFC3339))
}
