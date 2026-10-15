package computers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/gorilla/websocket"
)

func daemonIDForKey(publicKeyHex string) string {
	raw, err := hex.DecodeString(strings.TrimSpace(publicKeyHex))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func newClientSeed() (string, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return "", err
	}
	return hex.EncodeToString(seed), nil
}

func clientKey(seedHex string) (ed25519.PrivateKey, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("computer client key is damaged")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// deviceAuthorization signs one request the way every paired device does.
func deviceAuthorization(key ed25519.PrivateKey, purpose, daemonID, deviceID string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonceHex := hex.EncodeToString(nonce)
	signature := ed25519.Sign(key, auth.BuildSignaturePayload(purpose, daemonID, deviceID, timestamp, nonceHex))
	return fmt.Sprintf("%sv1:%s:%s:%s:%s:%s", auth.AuthorizationHeaderPrefix, deviceID, daemonID, timestamp, nonceHex, hex.EncodeToString(signature)), nil
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

// pair enrolls this computer's client key with the computer behind base,
// using the invite's one-time token, and checks the signed answer against
// the key the invite pinned.
func pair(ctx context.Context, base string, invite Invite, deviceID, deviceName string, key ed25519.PrivateKey) error {
	body, _ := json.Marshal(map[string]string{
		"enrollment_token":           invite.Token,
		"expected_daemon_id":         invite.DaemonID,
		"expected_daemon_public_key": invite.DaemonPublicKey,
		"device_id":                  deviceID,
		"device_name":                deviceName,
		"device_public_key":          hex.EncodeToString(key.Public().(ed25519.PublicKey)),
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/pair", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("pairing refused: %s", strings.TrimSpace(string(raw)))
	}
	var answer struct {
		OK                 bool   `json:"ok"`
		DaemonID           string `json:"daemon_id"`
		AssertionTimestamp string `json:"assertion_timestamp"`
		AssertionNonce     string `json:"assertion_nonce"`
		AssertionSignature string `json:"assertion_signature"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || !answer.OK {
		return errors.New("pairing answer is damaged")
	}
	assertion := auth.ServerAssertion{Timestamp: answer.AssertionTimestamp, NonceHex: answer.AssertionNonce, SignatureHex: answer.AssertionSignature}
	if answer.DaemonID != invite.DaemonID || !auth.VerifyServerAssertion(invite.DaemonPublicKey, "mewla-pair", assertion) {
		return errors.New("pairing answer was not signed by the invited computer")
	}
	return nil
}

func websocketURL(base string) string {
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + "/computer"
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + "/computer"
	}
	return base
}

// dial opens the /computer channel to an outbound peer on the first address
// that answers, and checks that the computer behind it holds the pinned key.
func dial(ctx context.Context, peer outboundPeer, hello Hello) (*websocket.Conn, Hello, string, error) {
	key, err := clientKey(peer.ClientSeedHex)
	if err != nil {
		return nil, Hello{}, "", err
	}
	var lastErr error = errors.New("no address to reach this computer")
	for _, base := range peer.URLs {
		if _, err := checkTransport(base); err != nil {
			lastErr = err
			continue
		}
		header := http.Header{}
		authorization, err := deviceAuthorization(key, auth.ComputerPurpose, peer.DaemonID, peer.DeviceID)
		if err != nil {
			return nil, Hello{}, "", err
		}
		header.Set("Authorization", authorization)
		dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
		conn, response, err := dialer.DialContext(ctx, websocketURL(base), header)
		if err != nil {
			if response != nil && response.StatusCode == http.StatusUnauthorized {
				lastErr = errors.New("this computer is no longer trusted there; add it again")
			} else {
				lastErr = err
			}
			continue
		}
		conn.SetReadLimit(maxFrameBytes)
		remote, err := exchangeHello(conn, hello)
		if err != nil {
			conn.Close()
			lastErr = err
			continue
		}
		if remote.DaemonID != peer.DaemonID || remote.Assertion == nil ||
			!auth.VerifyServerAssertion(peer.DaemonPublicKey, helloPurpose, *remote.Assertion) {
			conn.Close()
			lastErr = errors.New("the computer at this address is not the paired one")
			continue
		}
		return conn, remote, base, nil
	}
	return nil, Hello{}, "", lastErr
}

const helloPurpose = "mewla-computer-hello"

func exchangeHello(conn *websocket.Conn, hello Hello) (Hello, error) {
	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if err := conn.WriteJSON(frame{Type: "hello", Hello: &hello}); err != nil {
		return Hello{}, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	var answer frame
	if err := conn.ReadJSON(&answer); err != nil {
		return Hello{}, err
	}
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})
	if answer.Type == "error" {
		return Hello{}, errors.New(answer.Error)
	}
	if answer.Type != "hello" || answer.Hello == nil {
		return Hello{}, errors.New("unexpected answer from the other computer")
	}
	if answer.Hello.Protocol != ProtocolVersion {
		return Hello{}, versionMismatch(answer.Hello.Protocol, answer.Hello.Version)
	}
	return *answer.Hello, nil
}

func versionMismatch(protocol int, version string) error {
	if protocol < ProtocolVersion {
		return fmt.Errorf("the other computer runs Mewla %s; update it", version)
	}
	return fmt.Errorf("the other computer runs a newer Mewla (%s); update this one", version)
}
