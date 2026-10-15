package pairlink

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestConnectLinkLeavesOutTheDefaultSocketPath(t *testing.T) {
	key, token := strings.Repeat("ab", 32), strings.Repeat("cd", 32)
	link, err := ConnectLink("wss://quiet-otter.m.mewla.ai/ws", key, token)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != "mewla" || parsed.Host != "settings" {
		t.Fatalf("link = %q (%v)", link, err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parsed.Query().Get("p"))
	if err != nil {
		t.Fatal(err)
	}
	server := "wss://quiet-otter.m.mewla.ai"
	if payload[0] != 1 || int(payload[1])<<8|int(payload[2]) != len(server) || string(payload[3:3+len(server)]) != server {
		t.Fatalf("payload header = %v %q", payload[:3], payload[3:3+len(server)])
	}
	if len(payload) != 3+len(server)+64 {
		t.Fatalf("payload length = %d", len(payload))
	}
	if _, err := ConnectLink("wss://x/ws", "zz", token); err == nil {
		t.Fatal("bad public key accepted")
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"https://mewla.example.com":    "wss://mewla.example.com/ws",
		"http://127.0.0.1:9876":        "ws://127.0.0.1:9876/ws",
		"wss://mewla.example.com/sock": "wss://mewla.example.com/sock",
	} {
		if got, err := NormalizeEndpoint(in); err != nil || got != want {
			t.Fatalf("NormalizeEndpoint(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := NormalizeEndpoint("ftp://x"); err == nil {
		t.Fatal("ftp accepted")
	}
}
