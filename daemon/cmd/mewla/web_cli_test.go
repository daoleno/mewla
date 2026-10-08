package main

import (
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

func TestNormalizeWebOriginAcceptsLoopbackHTTPAndHTTPS(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:9876":          "http://127.0.0.1:9876",
		"http://localhost:9876/":         "http://localhost:9876",
		"https://mewla.example.ts.net":     "https://mewla.example.ts.net",
		"https://mewla.example.ts.net:443": "https://mewla.example.ts.net",
	}
	for raw, want := range cases {
		got, err := normalizeWebOrigin(raw)
		if err != nil || got != want {
			t.Fatalf("%s: got %q err %v", raw, got, err)
		}
	}
	for _, raw := range []string{"http://192.168.1.2:9876", "ws://127.0.0.1:9876", "http://127.0.0.1:9876/ws"} {
		if _, err := normalizeWebOrigin(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestBuildWebPairingURLCarriesTheConnectLinkInTheFragment(t *testing.T) {
	publicKey := strings.Repeat("ab", 32)
	token := strings.Repeat("cd", 32)
	webURL, err := buildWebPairingURL("http://127.0.0.1:9876", publicKey, auth.PairingToken{
		Value:     token,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(webURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "http" || parsed.Host != "127.0.0.1:9876" || parsed.Path != "/" || parsed.RawQuery != "" {
		t.Fatalf("web URL = %q", webURL)
	}
	fragment, err := url.ParseQuery(parsed.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	link := fragment.Get("pair")
	if !strings.HasPrefix(link, "mewla://settings?p=") {
		t.Fatalf("pair link = %q", link)
	}
}

func TestRunWebCommandRejectsUnexpectedArguments(t *testing.T) {
	if err := runWebCommand([]string{"extra"}, io.Discard, io.Discard); err == nil {
		t.Fatal("accepted a positional argument")
	}
}
