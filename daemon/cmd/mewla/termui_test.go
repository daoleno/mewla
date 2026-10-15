package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/daoleno/mewla/daemon/link"
	"rsc.io/qr"
)

func TestTermStyleIsPlainOffATerminal(t *testing.T) {
	var out bytes.Buffer
	s := styleForWriter(&out)
	if got := s.title("Pair your phone") + s.dim("hint") + s.cmd("mewla"); got != "■ Pair your phonehintmewla" {
		t.Fatalf("plain style = %q", got)
	}
	color := termStyle{color: true}
	if got := color.bold("x"); got != "\x1b[1mx\x1b[0m" {
		t.Fatalf("bold = %q", got)
	}
	if got := color.seal(); !strings.Contains(got, "38;5;166") {
		t.Fatalf("seal = %q", got)
	}
	if got := s.check(false, "tmux", "missing"); got != "  ○ tmux             missing" {
		t.Fatalf("check = %q", got)
	}
}

func TestRenderQRKeepsAQuietZoneAndFitsQRColumns(t *testing.T) {
	text := "mewla://settings?p=AQAid3NzOi8vbWV3bGEuZXhhbXBsZS5jb20"
	for _, style := range []termStyle{{}, {color: true}} {
		var out bytes.Buffer
		if err := renderQR(&out, style, text); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
		if style.color {
			if !strings.Contains(lines[0], "48;5;231m▀") || strings.Contains(lines[0], "38;5;16;") {
				t.Fatalf("color quiet row = %q", lines[0])
			}
			continue
		}
		width := utf8.RuneCountInString(lines[0])
		if width != qrColumns(text) {
			t.Fatalf("row width = %d, qrColumns = %d", width, qrColumns(text))
		}
		if strings.TrimSpace(lines[0]) != "" {
			t.Fatalf("first row is not quiet: %q", lines[0])
		}
		for _, line := range lines {
			if !strings.HasPrefix(line, "    ") || !strings.HasSuffix(line, "  ") {
				t.Fatalf("row without quiet columns: %q", line)
			}
		}
	}
}

func TestNumberMenu(t *testing.T) {
	options := []menuOption{{label: "Sign in", note: "(recommended)"}, {label: "Later"}}
	for input, want := range map[string]int{"\n": 0, "2\n": 1, "9\n1\n": 0, "": -1} {
		var out bytes.Buffer
		if got := chooseOption(bufio.NewReader(strings.NewReader(input)), &out, options); got != want {
			t.Fatalf("input %q: choice = %d, want %d\n%s", input, got, want, out.String())
		}
		if !strings.Contains(out.String(), "  1. Sign in       (recommended)\n  2. Later\n") {
			t.Fatalf("menu = %q", out.String())
		}
		if input == "9\n1\n" && !strings.Contains(out.String(), "Type a number from 1 to 2.") {
			t.Fatalf("no retry hint: %q", out.String())
		}
	}
}

func TestCompactLinkPairingQRFitsAnEightyColumnTerminal(t *testing.T) {
	for relays := 1; relays <= 3; relays++ {
		payload := link.PairingPayload{
			Version:         link.CompactPairingVersion,
			DaemonPublicKey: strings.Repeat("a1", 32),
			EnrollmentToken: strings.Repeat("b2", 32),
			RouteID:         strings.Repeat("c3", 16),
			TransportPin:    strings.Repeat("d4", 32),
			ExpiresAtMS:     time.Now().Add(15 * time.Minute).UnixMilli(),
			Signature:       strings.Repeat("e5", 64),
		}
		for index := range relays {
			domain := []string{"m.mewla.ai", "us-east.link.example", "eu-west.link.example"}[index]
			payload.Candidates = append(payload.Candidates, link.PairingCandidate{
				Name:         []string{"main", "us-east", "eu-west"}[index],
				AdmissionURL: "wss://" + strings.Repeat("f6", 16) + "." + domain + "/ws",
				StableURL:    "wss://quiet-otter-3f9a." + domain + "/ws",
			})
		}
		encoded, err := link.EncodeCompactPairing(payload)
		if err != nil {
			t.Fatal(err)
		}
		text := link.CompactPairingLink(encoded)
		code, err := encodeQR(text)
		if err != nil {
			t.Fatal(err)
		}
		columns, rows := qrColumns(text), (code.Size+2*qrQuietModules+1)/2
		t.Logf("%d relay(s): %d characters, %d modules, %d columns, %d rows", relays, len(text), code.Size, columns, rows)
		// Production has one relay; rows stay within 35 for up to two.
		if columns > 80 || (relays <= 2 && rows > 35) {
			t.Fatalf("%d relay(s): QR is %d columns by %d rows", relays, columns, rows)
		}
		if plain, _ := qr.Encode(text, qr.L); plain != nil && plain.Size <= code.Size {
			t.Fatalf("mixed segments did not shrink the code: %d vs %d modules", code.Size, plain.Size)
		}
	}
}

func TestLinkPairingNamesTheAppVersionAndKeepsTheOlderLink(t *testing.T) {
	links := pairingLinks{
		expiresAt:    time.Now().Add(15 * time.Minute),
		phoneAddress: "Remote access, https://quiet-otter-3f9a.m.mewla.ai",
		phoneLink:    link.CompactPairingLink("AN43KVROR7TFJ6KA"),
		phoneLegacy:  "mewla://settings?p=e30&v=2",
	}
	var screen, details bytes.Buffer
	printPairing(&screen, pairingStatus{remoteURL: "https://quiet-otter-3f9a.m.mewla.ai"}, links, "mewla pair --details")
	printPairingDetails(&details, "Pair a new phone or browser.", links)
	if !strings.Contains(screen.String(), "  Needs the app 0.2.17 or later; older app: mewla pair --details\n") ||
		!strings.Contains(screen.String(), "Works from any network.") {
		t.Fatalf("screen = %q", screen.String())
	}
	if !strings.Contains(details.String(), "App before 0.2.17, paste:  mewla://settings?p=e30&v=2\n") {
		t.Fatalf("details = %q", details.String())
	}
}
