package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previousWriter, previousFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(previousWriter); log.SetFlags(previousFlags) })
	return &buf
}

func refusedLines(buf *bytes.Buffer) []string {
	var lines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, "auth: refused ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func getWithHeader(t *testing.T, url, header string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", header)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

// A phone whose request was refused leaves a line saying why. The case behind
// it: a request resent after a dropped connection carries the signature the
// daemon already accepted, is refused as a replay, and /auth-check answers 401.
func TestRefusedSignedRequestIsLoggedWithItsReason(t *testing.T) {
	buf := captureLog(t)
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	t.Run("a resent request is a replay, and the log says so", func(t *testing.T) {
		header := calendarAuthHeader(privateKey, manager.DaemonID(), deviceID, "mewla-probe")
		if resp := getWithHeader(t, httpServer.URL+"/auth-check", header); resp.StatusCode == http.StatusUnauthorized {
			t.Fatalf("the first request was refused: %d", resp.StatusCode)
		}
		time.Sleep(20 * time.Millisecond)
		if resp := getWithHeader(t, httpServer.URL+"/auth-check", header); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("the resent request got %d, want 401", resp.StatusCode)
		}
		lines := refusedLines(buf)
		if len(lines) != 1 {
			t.Fatalf("log lines = %q", lines)
		}
		for _, want := range []string{"GET /auth-check", "purpose=mewla-probe", "reason=replay", "device=" + deviceID, "first accepted"} {
			if !strings.Contains(lines[0], want) {
				t.Errorf("line %q lacks %q", lines[0], want)
			}
		}
		// Nothing that could sign or replay a request reaches the log.
		parts := strings.Split(strings.TrimPrefix(header, "Device "), ":")
		for _, secret := range []string{header, parts[4], parts[5]} {
			if strings.Contains(buf.String(), secret) {
				t.Fatalf("the log holds part of the Authorization header: %q", secret)
			}
		}
	})

	t.Run("the other reasons a phone is turned away", func(t *testing.T) {
		buf.Reset()
		_, strangerKey, _ := ed25519.GenerateKey(rand.Reader)
		cases := []struct{ name, header, want string }{
			{"bad signature", calendarAuthHeader(strangerKey, manager.DaemonID(), deviceID, "mewla-probe"), "reason=bad-signature device=" + deviceID},
			{"unknown device", calendarAuthHeader(strangerKey, manager.DaemonID(), "never-paired", "mewla-probe"), "reason=unknown-device device=never-paired"},
			{"garbage", "Device nonsense", "reason=malformed-header device=-"},
		}
		for _, tc := range cases {
			if resp := getWithHeader(t, httpServer.URL+"/auth-check", tc.header); resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s: status %d", tc.name, resp.StatusCode)
			}
			if got := buf.String(); !strings.Contains(got, tc.want) {
				t.Fatalf("%s: log %q lacks %q", tc.name, got, tc.want)
			}
		}
	})
}

// A client that keeps retrying a refused request must not fill the log.
func TestRefusalLogIsRateLimitedPerReasonAndDevice(t *testing.T) {
	buf := captureLog(t)
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	clock := time.Now()
	server.refusals.now = func() time.Time { return clock }
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	_, strangerKey, _ := ed25519.GenerateKey(rand.Reader)
	badSignature := func() {
		getWithHeader(t, httpServer.URL+"/auth-check", calendarAuthHeader(strangerKey, manager.DaemonID(), deviceID, "mewla-probe"))
	}
	for i := 0; i < 5; i++ {
		badSignature()
	}
	if lines := refusedLines(buf); len(lines) != 1 {
		t.Fatalf("5 refusals in a burst wrote %d lines: %q", len(lines), lines)
	}

	// A different reason from the same device is news.
	header := calendarAuthHeader(privateKey, manager.DaemonID(), deviceID, "mewla-probe")
	getWithHeader(t, httpServer.URL+"/auth-check", header)
	getWithHeader(t, httpServer.URL+"/auth-check", header)
	if lines := refusedLines(buf); len(lines) != 2 || !strings.Contains(lines[1], "reason=replay") {
		t.Fatalf("lines = %q", lines)
	}

	clock = clock.Add(refusalLogEvery + time.Second)
	badSignature()
	lines := refusedLines(buf)
	if len(lines) != 3 || !strings.Contains(lines[2], "[4 more since the last line]") {
		t.Fatalf("after the interval: %q", lines)
	}
}

func TestRefusalLogKeysAreBounded(t *testing.T) {
	var limiter refusalLog
	for i := 0; i < refusalLogKeys*3; i++ {
		limiter.allow("reason|device-" + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/676)) + "|p")
	}
	if len(limiter.last) > refusalLogKeys {
		t.Fatalf("%d keys held, want at most %d", len(limiter.last), refusalLogKeys)
	}
}
