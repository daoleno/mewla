package sessionhost

import (
	"strings"
	"testing"
)

func filterAll(chunks ...string) string {
	var f queryFilter
	var out strings.Builder
	for _, chunk := range chunks {
		out.Write(f.write([]byte(chunk)))
	}
	return out.String()
}

func TestQueryFilterDropsQueries(t *testing.T) {
	for _, q := range []string{
		"\x1b[c", "\x1b[0c", "\x1b[>c", "\x1b[>0c", "\x1b[=c", // device attributes
		"\x1bZ",                                        // DECID
		"\x1b[5n", "\x1b[6n", "\x1b[?6n", "\x1b[?996n", // status and position
		"\x1b[?2004$p", "\x1b[4$p", // DECRQM
		"\x1b[>q", "\x1b[>0q", // XTVERSION
		"\x1b[?u",                          // Kitty keyboard flags
		"\x1b[?4m",                         // XTQMODKEYS
		"\x1b[14t", "\x1b[18t", "\x1b[16t", // window reports
		"\x1b]11;?\x07", "\x1b]10;?\x1b\\", "\x1b]4;1;?\x07", "\x1b]12;?\x07", // colors
		"\x1b]52;c;?\x07",                     // clipboard read
		"\x1b]21;foreground=?\x1b\\",          // Kitty colors
		"\x1bP+q544e\x1b\\", "\x1bP$qm\x1b\\", // XTGETTCAP, DECRQSS
		"\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\", // Kitty graphics
	} {
		if got := filterAll("a" + q + "b"); got != "ab" {
			t.Errorf("filter(%q) = %q, want the query dropped", q, got)
		}
	}
}

func TestQueryFilterKeepsEverythingElse(t *testing.T) {
	for _, s := range []string{
		"plain text\r\n",
		"\x1b[31mred\x1b[0m", "\x1b[2J\x1b[H", "\x1b[?1049h", "\x1b[?2004l",
		"\x1b[>4;2m", "\x1b[>1u", "\x1b[<u", "\x1b[=5;1u", // modifyOtherKeys, Kitty push/pop/set
		"\x1b[2 q",                    // cursor style
		"\x1b[>0n",                    // XTMODKEYS reset, not a report
		"\x1b[?1c",                    // Linux console cursor, not attributes
		"\x1b[22;0t", "\x1b[8;24;80t", // title stack, resize
		"\x1b]0;title\x07", "\x1b]2;title\x1b\\", "\x1b]7;file://h/tmp\x07",
		"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\",
		"\x1b]11;#000000\x07", "\x1b]4;1;rgb:ff/00/00\x07", "\x1b]104\x07",
		"\x1b]52;c;aGVsbG8=\x07", "\x1b]7501;state=idle\x07",
		"\x1bPq#0;2;0;0;0#0~~\x1b\\", // sixel
		"\x1b7\x1b8\x1bc\x1b(B\x1b=\x1b>",
		"日本語 ✓",
	} {
		if got := filterAll(s); got != s {
			t.Errorf("filter(%q) = %q, want it unchanged", s, got)
		}
	}
}

func TestQueryFilterAcrossWrites(t *testing.T) {
	in := "x\x1b[?2004h\x1b]11;?\x1b\\y\x1b[6nz\x1b]0;title\x07\x1b[1;31mw"
	want := "x\x1b[?2004hy" + "z\x1b]0;title\x07\x1b[1;31mw"
	// Every split point, including inside each sequence.
	for i := 0; i <= len(in); i++ {
		for j := i; j <= len(in); j++ {
			if got := filterAll(in[:i], in[i:j], in[j:]); got != want {
				t.Fatalf("split at %d,%d: %q, want %q", i, j, got, want)
			}
		}
	}
}

func TestQueryFilterNeverHoldsLongStrings(t *testing.T) {
	var f queryFilter
	clip := "\x1b]52;c;" + strings.Repeat("QUFB", 200)
	if got := string(f.write([]byte(clip))); got != clip {
		t.Fatalf("a long clipboard write was held: got %d of %d bytes", len(got), len(clip))
	}
	if got := string(f.write([]byte("\x07after"))); got != "\x07after" {
		t.Fatalf("end of the long string = %q", got)
	}
	title := "\x1b]2;" + strings.Repeat("t", 40)
	if got := string(f.write([]byte(title))); got != title {
		t.Fatalf("a title was held: %q", got)
	}
	csi := "\x1b[" + strings.Repeat("1;", 40)
	if got := string(f.write([]byte(csi + "m"))); got != csi+"m" {
		t.Fatalf("a long CSI = %q", got)
	}
}

func TestQueryFilterInterruptedSequences(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[12\x1b[6nok":   "\x1b[12ok",   // ESC cuts a CSI; the next query still goes
		"\x1b[1\x18x":        "\x1b[1\x18x", // CAN cancels in the viewer too
		"\x1b]11;?\x1b[6nok": "\x1b]11;?ok", // ESC that is not ST ends the OSC
		"\x1b]0;t\x1b[6nok":  "\x1b]0;tok",  // a passed OSC ended the same way
		"\x1b_Gq\x1b[31mred": "\x1b[31mred", // dropped APC ended by ESC
		"\x1b\x1b[6n":        "\x1b",
		"a\r\x1b[1\r2Hb":     "a\r\r\x1b[12Hb", // C0 inside CSI acts at once
	} {
		if got := filterAll(in); got != want {
			t.Errorf("filter(%q) = %q, want %q", in, got, want)
		}
	}
}
