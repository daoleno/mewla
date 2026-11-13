package connections

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"golang.org/x/oauth2"
)

// A sign-in's state ends in a MAC under a key this daemon keeps in its vault.
// A restart forgets pending sign-ins (their PKCE verifiers live in memory), so
// a return that comes after one has an unknown state; the MAC tells this
// daemon's own stale state (Plugins says it expired) from another computer's
// on the same loopback port (the copy page, so it can be pasted there).
const (
	oauthStateKeyRef = "oauth-state-key"
	oauthStateRandom = 43 // oauth2.GenerateVerifier: 32 bytes, base64url
	oauthStateMAC    = 32 // hex characters
)

type oauthStateKey struct {
	once sync.Once
	key  []byte
}

func (m *Manager) oauthStateKey() []byte {
	m.stateKey.once.Do(func() {
		if raw, ok, err := m.vault.Get(oauthStateKeyRef); err == nil && ok {
			if key, err := hex.DecodeString(raw); err == nil && len(key) == 32 {
				m.stateKey.key = key
				return
			}
		}
		key := make([]byte, 32)
		_, _ = rand.Read(key)
		// A vault that refuses keeps the key for this run only: a return after a
		// restart then reads as another computer's, as it did before.
		_ = m.vault.Set(oauthStateKeyRef, hex.EncodeToString(key))
		m.stateKey.key = key
	})
	return m.stateKey.key
}

func (m *Manager) oauthStateMAC(random string) string {
	mac := hmac.New(sha256.New, m.oauthStateKey())
	mac.Write([]byte(random))
	return hex.EncodeToString(mac.Sum(nil))[:oauthStateMAC]
}

// newOAuthState is a fresh state this daemon can recognise after a restart.
func (m *Manager) newOAuthState() string {
	random := oauth2.GenerateVerifier()
	return random + m.oauthStateMAC(random)
}

// issuedOAuthState reports whether state (or the state a web return wraps,
// which ends it) was issued by this daemon.
func (m *Manager) issuedOAuthState(state string) bool {
	if len(state) < oauthStateRandom+oauthStateMAC {
		return false
	}
	own := state[len(state)-oauthStateRandom-oauthStateMAC:]
	random, mac := own[:oauthStateRandom], own[oauthStateRandom:]
	return hmac.Equal([]byte(mac), []byte(m.oauthStateMAC(random)))
}
