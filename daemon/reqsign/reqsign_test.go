package reqsign

import (
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The account service and deployed daemons sign with these exact bytes. A
// change here breaks remote access for every installed daemon.
func TestAccountSchemeBytesAreStable(t *testing.T) {
	scheme := Scheme{Name: "MewlaMachine", Domain: "mewla-account-v1"}
	payload := scheme.Payload("PUT", "/v1/machine/name", "1700000000000", "00112233445566778899aabbccddeeff", []byte(`{"name":"x"}`))
	want := "mewla-account-v1\nPUT\n/v1/machine/name\n1700000000000\n00112233445566778899aabbccddeeff\n0229d37e33daae149bf40543a5ce1db4459d10f830d5139279aa2bfd5f6485a1"
	if got := string(payload); got != want {
		t.Fatalf("payload = %q", got)
	}
	if header := scheme.Header("aa", "1", "bb", "cc"); header != "MewlaMachine v1:aa:1:bb:cc" {
		t.Fatalf("header = %q", header)
	}
}

func signedRequest(t *testing.T, scheme Scheme, key ed25519.PrivateKey, at time.Time, nonce string, body string) *http.Request {
	t.Helper()
	timestamp := strconv.FormatInt(at.UnixMilli(), 10)
	request := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	payload := scheme.Payload(request.Method, request.URL.Path, timestamp, nonce, []byte(body))
	signature := hex.EncodeToString(ed25519.Sign(key, payload))
	request.Header.Set("Authorization", scheme.Header(hex.EncodeToString(key.Public().(ed25519.PublicKey)), timestamp, nonce, signature))
	return request
}

func TestVerifierRejectsReplayExpiredAndForeignDomain(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(nil)
	now := time.UnixMilli(1_700_000_000_000)
	scheme := Scheme{Name: "T", Domain: "d-v1"}
	verifier := &Verifier{
		Scheme: scheme, KeyBytes: 32, Now: func() time.Time { return now },
		Verify: func(k string, payload []byte, sig string) bool {
			pub, _ := hex.DecodeString(k)
			raw, _ := hex.DecodeString(sig)
			return ed25519.Verify(pub, payload, raw)
		},
	}
	const nonce = "00112233445566778899aabbccddeeff"
	if _, rejection := verifier.Authenticate(httptest.NewRecorder(), signedRequest(t, scheme, key, now, nonce, "{}")); rejection != nil {
		t.Fatalf("fresh request rejected: %+v", rejection)
	}
	if _, rejection := verifier.Authenticate(httptest.NewRecorder(), signedRequest(t, scheme, key, now, nonce, "{}")); rejection == nil || rejection.Status != 401 {
		t.Fatalf("replay = %+v", rejection)
	}
	if _, rejection := verifier.Authenticate(httptest.NewRecorder(), signedRequest(t, scheme, key, now.Add(-3*time.Minute), "ffeeddccbbaa99887766554433221100", "{}")); rejection == nil || rejection.Status != 401 {
		t.Fatalf("expired = %+v", rejection)
	}
	other := Scheme{Name: "T", Domain: "other-v1"}
	if _, rejection := verifier.Authenticate(httptest.NewRecorder(), signedRequest(t, other, key, now, "0123456789abcdef0123456789abcdef", "{}")); rejection == nil || rejection.Status != 401 {
		t.Fatalf("foreign domain = %+v", rejection)
	}
}

func TestRateLimiterWindowAndForget(t *testing.T) {
	limiter := RateLimiter{Limit: 2, Window: time.Hour}
	now := time.Unix(1000, 0)
	if !limiter.Allow("m:1", now) || !limiter.Allow("m:1", now) || limiter.Allow("m:1", now) {
		t.Fatal("limit not enforced")
	}
	limiter.Forget("m")
	if !limiter.Allow("m:1", now) {
		t.Fatal("forget did not clear")
	}
	if !limiter.Allow("m:1", now.Add(2*time.Hour)) {
		t.Fatal("window did not slide")
	}
}
