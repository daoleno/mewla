package codextrust

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/daoleno/mewla/daemon/shellquote"
)

var awkwardDirs = []string{
	"/plain/dir",
	"/with space/dir",
	`/quote"and'apostrophe`,
	`/back\slash`,
	"/semi;colon & $(sub) `tick` #hash",
	"/ünï/日本語",
}

// The flag survives the launch-line splitter and Codex's TOML parser with every
// directory spelled exactly.
func TestFlagRoundTripsThroughTheShellAndTOML(t *testing.T) {
	for _, dir := range awkwardDirs {
		fields, ok := shellquote.Fields("codex " + Flag(dir, dir+"-real"))
		if !ok || len(fields) != 3 || fields[1] != "--config" {
			t.Fatalf("%q: fields=%q ok=%v", dir, fields, ok)
		}
		var cfg struct {
			Projects map[string]struct {
				TrustLevel string `toml:"trust_level"`
			} `toml:"projects"`
		}
		if _, err := toml.Decode(fields[2], &cfg); err != nil {
			t.Fatalf("%q: %v\n%s", dir, err, fields[2])
		}
		if len(cfg.Projects) != 2 || cfg.Projects[dir].TrustLevel != "trusted" || cfg.Projects[dir+"-real"].TrustLevel != "trusted" {
			t.Fatalf("%q: decoded %+v", dir, cfg.Projects)
		}
	}
}

func TestForwardedCarriesOnlyTheTrustOverride(t *testing.T) {
	dir := awkwardDirs[2]
	args := "--model gpt-6 -c check_for_update_on_startup=false " + Flag(dir) + " --dangerously-bypass-hook-trust"
	forwarded := Forwarded(args)
	if forwarded != " "+Flag(dir) {
		t.Fatalf("forwarded %q, want only %q", forwarded, Flag(dir))
	}
	for _, none := range []string{"", "--model gpt-6", "-c model=x", "--config 'projects.\"/a\".trust_level=\"trusted\"'", "-c 'oops", "-c a; rm -rf /"} {
		if got := Forwarded(none); got != "" {
			t.Errorf("Forwarded(%q) = %q, want none", none, got)
		}
	}
	if !strings.HasPrefix(Forwarded("-c "+shellquote.Word(Value("/a"))), " --config ") {
		t.Fatal("the short -c spelling is not forwarded")
	}
}
