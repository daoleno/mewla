package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
)

// Security review 11-23 (PR B, item 5): a /pair answer signed only a nonce
// and time the daemon chose, so whoever recorded one answer could play the
// computer to any later app. An app's own challenge is signed now; an app that
// sends none (older) gets the answer it always had.
func TestPairSignsTheAppsChallenge(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	host := httptest.NewServer(http.HandlerFunc(srv.handlePair))
	t.Cleanup(host.Close)

	pair := func(token, device, challenge string) (int, map[string]any) {
		t.Helper()
		public, _, _ := ed25519.GenerateKey(rand.Reader)
		body, _ := json.Marshal(map[string]any{
			"enrollment_token":           token,
			"expected_daemon_id":         manager.DaemonID(),
			"expected_daemon_public_key": manager.PublicKeyHex(),
			"device_id":                  device,
			"device_name":                device,
			"device_public_key":          hex.EncodeToString(public),
			"challenge":                  challenge,
		})
		response, err := http.Post(host.URL, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var answer map[string]any
		_ = json.NewDecoder(response.Body).Decode(&answer)
		return response.StatusCode, answer
	}
	proofOf := func(answer map[string]any) auth.ServerAssertion {
		proof, _ := answer["proof"].(map[string]any)
		text := func(key string) string { value, _ := proof[key].(string); return value }
		if text("purpose") != string(auth.PairChallengePurpose) {
			t.Fatalf("proof purpose = %q", text("purpose"))
		}
		return auth.ServerAssertion{Timestamp: text("timestamp"), NonceHex: text("nonce"), SignatureHex: text("signature")}
	}

	// A bad challenge is refused before the one-time code is spent.
	first, _ := manager.IssuePairingToken(time.Minute)
	if status, _ := pair(first.Value, "phone-a", "zz"); status != http.StatusBadRequest {
		t.Fatalf("bad challenge: status %d", status)
	}
	challenge := "00112233445566778899aabbccddeeff"
	status, answer := pair(first.Value, "phone-a", challenge)
	if status != http.StatusOK {
		t.Fatalf("pair: status %d %v", status, answer)
	}
	proof := proofOf(answer)
	if !auth.VerifyChallengeAssertionFor(manager.PublicKeyHex(), auth.PairChallengePurpose, challenge, proof) {
		t.Fatal("the proof does not answer the app's challenge")
	}
	if auth.VerifyChallengeAssertionFor(manager.PublicKeyHex(), auth.PairChallengePurpose, "ffeeddccbbaa99887766554433221100", proof) {
		t.Fatal("a recorded proof answered another challenge")
	}
	// The answer older apps check is still there.
	old := auth.ServerAssertion{Timestamp: answer["assertion_timestamp"].(string), NonceHex: answer["assertion_nonce"].(string), SignatureHex: answer["assertion_signature"].(string)}
	if !auth.VerifyServerAssertion(manager.PublicKeyHex(), "mewla-pair", old) {
		t.Fatal("the old assertion is gone")
	}

	// An older app sends no challenge and gets no proof.
	second, _ := manager.IssuePairingToken(time.Minute)
	status, answer = pair(second.Value, "phone-b", "")
	if status != http.StatusOK || answer["proof"] != nil {
		t.Fatalf("older app: status %d proof %v", status, answer["proof"])
	}
}

// A paired device's probe is answered over the nonce of its own signed
// request too.
func TestAuthCheckSignsTheDevicesNonce(t *testing.T) {
	manager, privateKey := enrolledTestDevice(t)
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	host := httptest.NewServer(http.HandlerFunc(srv.handleAuthCheck))
	t.Cleanup(host.Close)
	header := calendarAuthHeader(privateKey, manager.DaemonID(), "device-direct", "mewla-probe")
	request, _ := http.NewRequest(http.MethodGet, host.URL, nil)
	request.Header.Set("Authorization", header)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var answer struct {
		Proof struct{ Purpose, Timestamp, Nonce, Signature string } `json:"proof"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&answer) != nil {
		t.Fatalf("auth-check: %d", response.StatusCode)
	}
	nonce := auth.AuthorizationNonce(header)
	signed := auth.ServerAssertion{Timestamp: answer.Proof.Timestamp, NonceHex: answer.Proof.Nonce, SignatureHex: answer.Proof.Signature}
	if answer.Proof.Purpose != string(auth.ProbeChallengePurpose) || !auth.VerifyChallengeAssertionFor(manager.PublicKeyHex(), auth.ProbeChallengePurpose, nonce, signed) {
		t.Fatalf("the probe's proof does not answer the request's nonce: %+v", answer.Proof)
	}
}
