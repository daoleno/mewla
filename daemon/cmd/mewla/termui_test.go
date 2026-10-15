package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
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
