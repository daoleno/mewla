package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/remote"
)

const webTestToken = "0707070707070707070707070707070707070707070707070707070707070707"

type fakeWebEnv struct {
	running bool
	desktop bool
	reason  string
	openErr error
	opened  []string
	pairs   int
}

func (f *fakeWebEnv) env() webEnv {
	return webEnv{
		running: func(string) (bool, error) { return f.running, nil },
		pair: func(string) (*control.PairingInfo, error) {
			f.pairs++
			return &control.PairingInfo{
				Token:           webTestToken,
				ExpiresAt:       time.Now().Add(15 * time.Minute),
				DaemonID:        strings.Repeat("d", 64),
				DaemonPublicKey: strings.Repeat("a", 64),
				ShortLinks:      true,
			}, nil
		},
		desktop: func() (bool, string) { return f.desktop, f.reason },
		open: func(target string) error {
			f.opened = append(f.opened, target)
			return f.openErr
		},
	}
}

func webTestState(t *testing.T, signedIn bool, addresses ...string) string {
	t.Helper()
	stateDir := t.TempDir()
	book, err := addressbook.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := book.ReplaceDiscovered(addresses); err != nil {
		t.Fatal(err)
	}
	if signedIn {
		if err := remote.SaveConfig(stateDir, remote.Config{ServiceURL: "https://service.example", RemoteAccess: true, Machine: remote.Machine{
			MachineID: "mach_1", AccountLogin: "octo", Provider: "github", Hostname: "quiet-otter-1a2b.m.mewla.ai",
			Label: "quiet-otter-1a2b", RouteID: strings.Repeat("ab", 16), Grant: "grant",
			GrantExpiresAtMS: time.Now().Add(time.Hour).UnixMilli(),
			Relays:           []remote.Relay{{Name: "r1", ControlAddress: "127.0.0.1:1", ControlServerName: "relay.test", ClientDomain: "m.mewla.ai", ClientPort: 443}},
		}}); err != nil {
			t.Fatal(err)
		}
		if _, err := book.Add("https://quiet-otter-1a2b.m.mewla.ai", addressbook.SourceRemote); err != nil {
			t.Fatal(err)
		}
	}
	return stateDir
}

func wantLines(t *testing.T, text string, want []string, unwanted []string) {
	t.Helper()
	for _, line := range want {
		if !strings.Contains(text, line) {
			t.Fatalf("output missing %q:\n%s", line, text)
		}
	}
	for _, line := range unwanted {
		if strings.Contains(text, line) {
			t.Fatalf("output has %q:\n%s", line, text)
		}
	}
}

func TestWebWithoutMewlaRunningSaysHowToStartIt(t *testing.T) {
	fake := &fakeWebEnv{desktop: true}
	var out bytes.Buffer
	err := runWeb(webConfig{stateDir: t.TempDir()}, &out, fake.env())
	if !errors.Is(err, errMewlaNotRunning) {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(out.String(), "Mewla is not running. Start it with mewla -state-dir ") || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("output = %q", out.String())
	}
	if fake.pairs != 0 || len(fake.opened) != 0 {
		t.Fatalf("pairs = %d, opened = %v", fake.pairs, fake.opened)
	}
}

func TestWebOpensThisComputersShortLinkAndListsAddresses(t *testing.T) {
	stateDir := webTestState(t, false, "http://127.0.0.1:9876", "http://192.168.1.20:9876", "http://100.101.102.103:9876")
	fake := &fakeWebEnv{running: true, desktop: true}
	var out bytes.Buffer
	if err := runWeb(webConfig{stateDir: stateDir}, &out, fake.env()); err != nil {
		t.Fatal(err)
	}
	if len(fake.opened) != 1 || !strings.HasPrefix(fake.opened[0], "http://127.0.0.1:9876/p/") {
		t.Fatalf("opened = %v", fake.opened)
	}
	wantLines(t, out.String(), []string{
		"■ Mewla web UI\n",
		"  This computer   http://127.0.0.1:9876 · opened in your browser\n",
		"  Remote          off · reach it from other computers: mewla login\n",
		"  Tailscale       http://100.101.102.103:9876 · phone app only\n",
		"  Wi-Fi/LAN       http://192.168.1.20:9876 · phone app only\n",
	}, []string{"/p/", "\x1b["})
	if strings.Index(out.String(), "Tailscale") > strings.Index(out.String(), "Wi-Fi/LAN") {
		t.Fatalf("Tailscale should come before Wi-Fi/LAN:\n%s", out.String())
	}
}

func TestWebSignedInNamesTheRemoteAddressOnce(t *testing.T) {
	stateDir := webTestState(t, true, "http://127.0.0.1:9876")
	book, _ := addressbook.New(stateDir)
	if _, err := book.Add("https://mewla.example.com", addressbook.SourceManual); err != nil {
		t.Fatal(err)
	}
	fake := &fakeWebEnv{running: true, desktop: true}
	var out bytes.Buffer
	if err := runWeb(webConfig{stateDir: stateDir}, &out, fake.env()); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	wantLines(t, text, []string{
		"  Remote          https://quiet-otter-1a2b.m.mewla.ai\n",
		"                  Open it in a browser on another computer, then match\n",
		"its number here: mewla devices pending -state-dir ",
		"  HTTPS           https://mewla.example.com\n",
	}, []string{"mewla login"})
	if strings.Count(text, "quiet-otter-1a2b.m.mewla.ai") != 1 {
		t.Fatalf("remote address printed more than once:\n%s", text)
	}
}

func TestWebWithoutADesktopOnlyPrints(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      webConfig
		fake     fakeWebEnv
		header   string
		ssh      bool
		unwanted string
	}{
		{name: "ssh", fake: fakeWebEnv{running: true, reason: "over SSH"}, header: "Not opened: this is an SSH session\n", ssh: true},
		{name: "no display", fake: fakeWebEnv{running: true, reason: "no desktop session"}, header: "Not opened: no desktop session\n", unwanted: "ssh -L"},
		{name: "print", cfg: webConfig{print: true}, fake: fakeWebEnv{running: true, desktop: true}, header: "Open on this computer\n", unwanted: "ssh -L"},
		{name: "browser fails", fake: fakeWebEnv{running: true, desktop: true, openErr: errors.New("xdg-open: not found")}, header: "Not opened: could not start a browser: xdg-open: not found\n", unwanted: "ssh -L"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := webTestState(t, true, "http://127.0.0.1:9876")
			fake := tc.fake
			cfg := tc.cfg
			cfg.stateDir = stateDir
			var out bytes.Buffer
			if err := runWeb(cfg, &out, fake.env()); err != nil {
				t.Fatal(err)
			}
			if tc.name != "browser fails" && len(fake.opened) != 0 {
				t.Fatalf("opened %v without a desktop", fake.opened)
			}
			want := []string{
				"  This computer   http://127.0.0.1:9876\n",
				"  Remote          https://quiet-otter-1a2b.m.mewla.ai\n",
				tc.header,
				"  http://127.0.0.1:9876/p/BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc\n",
				"Pairs the browser that opens it first, once, until ",
			}
			if tc.ssh {
				want = append(want, "  ssh -L 9876:127.0.0.1:9876 <this computer>\n")
			}
			unwanted := []string{"opened in your browser"}
			if tc.unwanted != "" {
				unwanted = append(unwanted, tc.unwanted)
			}
			wantLines(t, out.String(), want, unwanted)
		})
	}
}

func TestWebWithoutLoopbackDoesNotOpen(t *testing.T) {
	stateDir := webTestState(t, false, "http://100.101.102.103:9876")
	fake := &fakeWebEnv{running: true, desktop: true}
	var out bytes.Buffer
	if err := runWeb(webConfig{stateDir: stateDir}, &out, fake.env()); err != nil {
		t.Fatal(err)
	}
	if len(fake.opened) != 0 {
		t.Fatalf("opened = %v", fake.opened)
	}
	wantLines(t, out.String(), []string{"not served here: Mewla does not listen on 127.0.0.1"}, []string{"Not opened"})
}

func TestWebIsTheCommandAndOpenIsNot(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"web", "-h"}, &out); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("web -h: err = %v", err)
	}
	if !strings.Contains(out.String(), "Usage: mewla web [flags]\n") || !strings.Contains(out.String(), "-print") {
		t.Fatalf("web help = %q", out.String())
	}
	if err := run([]string{"open"}, io.Discard); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("run(open) = %v", err)
	}
	var usage bytes.Buffer
	printTopLevelUsage(&usage)
	if !strings.Contains(usage.String(), "Connect a phone or browser\n  login") ||
		!strings.Contains(usage.String(), "  web       Open the web UI in this computer's browser and print its addresses\n") ||
		strings.Contains(usage.String(), "  open ") {
		t.Fatalf("usage = %s", usage.String())
	}
}

func TestDesktopSessionTreatsSSHAsHeadless(t *testing.T) {
	t.Setenv("DISPLAY", ":0")
	t.Setenv("SSH_CONNECTION", "10.0.0.2 50000 10.0.0.1 22")
	if ok, reason := desktopSession(); ok || reason != "over SSH" {
		t.Fatalf("desktopSession = %v, %q", ok, reason)
	}
}
