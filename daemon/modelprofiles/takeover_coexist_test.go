package modelprofiles

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// fakeMagpie does to ~/.codex/config.toml what Magpie (github.com/yetone/magpie)
// does on the owner's Mac: on connect it remembers the model_provider it found
// and points Codex at its own provider, keeping its [model_providers.magpie]
// table for good; on disconnect it writes the remembered value back, whatever
// the config holds by then.
type fakeMagpie struct {
	t    *testing.T
	path string
	left string
}

var topProviderLine = regexp.MustCompile(`(?m)^model_provider\s*=\s*"([^"]*)"\s*$`)

func (m *fakeMagpie) connect() {
	m.t.Helper()
	raw := string(readFileBytes(m.t, m.path))
	if match := topProviderLine.FindStringSubmatch(raw); match != nil {
		m.left = match[1]
		raw = topProviderLine.ReplaceAllString(raw, `model_provider = "magpie"`)
	} else {
		m.left = ""
		raw = `model_provider = "magpie"` + "\n" + raw
	}
	if !strings.Contains(raw, "[model_providers.magpie]") {
		raw += "\n[model_providers.magpie]\nname = \"magpie\"\nbase_url = \"http://127.0.0.1:3435/v1\"\nwire_api = \"responses\"\n"
	}
	m.write(raw)
}

func (m *fakeMagpie) disconnect() {
	m.t.Helper()
	raw := string(readFileBytes(m.t, m.path))
	if m.left == "" {
		raw = topProviderLine.ReplaceAllString(raw, "")
	} else {
		raw = topProviderLine.ReplaceAllString(raw, `model_provider = "`+m.left+`"`)
	}
	m.write(raw)
}

func (m *fakeMagpie) write(raw string) {
	m.t.Helper()
	if err := os.WriteFile(m.path, []byte(raw), 0o600); err != nil {
		m.t.Fatal(err)
	}
}

const coexistUserConfig = `#model_provider = "packycode"
model_provider = "freeride-xcode"
model = "gpt-6-astra"

[model_providers.freeride-xcode]
name = "freeride-xcode"
base_url = "https://xcode.example/v1"
wire_api = "responses"
requires_openai_auth = true

[projects."/Users/me"]
trust_level = "trusted"
`

// codexWouldStart is Codex's own check at start: the model_provider it names
// must be a built-in or have a [model_providers.<name>] table ("Error: Model
// provider `mewla-gateway` not found" otherwise).
func codexWouldStart(t *testing.T, path string) (string, bool) {
	t.Helper()
	raw := string(readFileBytes(t, path))
	match := topProviderLine.FindStringSubmatch(raw)
	if match == nil {
		return "openai", true
	}
	name := match[1]
	switch name {
	case "openai", "oss", "ollama", "lmstudio":
		return name, true
	}
	return name, strings.Contains(raw, "[model_providers."+name+"]")
}

func coexistFixture(t *testing.T) (*Takeover, string, *fakeMagpie) {
	t.Helper()
	takeover, _, path := takeoverFixture(t)
	if err := os.WriteFile(path, []byte(coexistUserConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return takeover, path, &fakeMagpie{t: t, path: path}
}

// The owner's 11-26 sequence: Mewla's gateway on, Magpie's Codex setting on,
// Mewla's Codex switched back to the official login, Magpie's setting off.
// Codex was left naming mewla-gateway with no table: "Model provider
// mewla-gateway not found".
func TestTakeoverMagpieOnMewlaOffMagpieOffLeavesCodexWorking(t *testing.T) {
	takeover, path, magpie := coexistFixture(t)
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	magpie.connect()
	if _, err := takeover.Disable(); err != nil {
		t.Fatal(err)
	}
	magpie.disconnect()
	takeover.Heal()
	name, ok := codexWouldStart(t, path)
	if !ok {
		t.Fatalf("Codex points at %q, which has no table:\n%s", name, readFileBytes(t, path))
	}
	if name != "freeride-xcode" {
		t.Fatalf("Codex points at %q, want the user's own freeride-xcode", name)
	}
}

// A daemon restart while Magpie is connected must not take Codex back from
// Magpie (Repair used to rewrite any single model_provider line).
func TestTakeoverRestartWhileMagpieConnectedLeavesMagpieInFront(t *testing.T) {
	takeover, path, magpie := coexistFixture(t)
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	magpie.connect()
	if _, err := takeover.Repair(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	if name, _ := codexWouldStart(t, path); name != "magpie" {
		t.Fatalf("restart took Codex from Magpie: model_provider %q", name)
	}
	magpie.disconnect()
	if name, ok := codexWouldStart(t, path); name != GatewayProviderName || !ok {
		t.Fatalf("after Magpie let go Codex points at %q (table %v), want Mewla's gateway", name, ok)
	}
	if status := takeover.Status(); status.State != TakeoverStateActive {
		t.Fatalf("status %s (%s), want active", status.State, status.Detail)
	}
	if _, err := takeover.Disable(); err != nil {
		t.Fatal(err)
	}
	if got := string(readFileBytes(t, path)); !strings.Contains(got, `model_provider = "freeride-xcode"`) {
		t.Fatalf("disable did not give the user's provider back:\n%s", got)
	}
}

// The owner's Mac as it was found: takeover off, Codex naming mewla-gateway
// with no table. Heal alone gives Codex his own provider back.
func TestTakeoverHealGivesBackADanglingMewlaProvider(t *testing.T) {
	takeover, path, magpie := coexistFixture(t)
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	magpie.connect()
	if _, err := takeover.Disable(); err != nil {
		t.Fatal(err)
	}
	magpie.disconnect()
	if name, ok := codexWouldStart(t, path); ok || name != GatewayProviderName {
		t.Fatalf("setup: want the dangling mewla-gateway, got %q (table %v)", name, ok)
	}
	before := readFileBytes(t, path)
	takeover.Heal()
	after := string(readFileBytes(t, path))
	if name, ok := codexWouldStart(t, path); !ok || name != "freeride-xcode" {
		t.Fatalf("heal left Codex on %q (table %v):\n%s", name, ok, after)
	}
	// Only the provider line changed; Magpie's table and everything else stay.
	if want := strings.Replace(string(before), `model_provider = "mewla-gateway"`, `model_provider = "freeride-xcode"`, 1); after != want {
		t.Fatalf("heal changed more than the provider:\n%s", after)
	}
	takeover.Heal()
	if again := string(readFileBytes(t, path)); again != after {
		t.Fatal("a second heal changed the config again")
	}
}

// Heal never touches a provider that is not Mewla's, on or off.
func TestTakeoverHealLeavesOtherProvidersAlone(t *testing.T) {
	takeover, path, magpie := coexistFixture(t)
	takeover.Heal()
	if got := string(readFileBytes(t, path)); got != coexistUserConfig {
		t.Fatalf("heal changed a config Mewla never touched:\n%s", got)
	}
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	magpie.connect()
	held := readFileBytes(t, path)
	takeover.Heal()
	if got := readFileBytes(t, path); string(got) != string(held) {
		t.Fatalf("heal took Codex from Magpie:\n%s", got)
	}
}

// While on, a config that names Mewla but lost its table (a tool rewrote the
// file without it) gets the table back.
func TestTakeoverHealPutsTheTableBackWhileOn(t *testing.T) {
	takeover, path, _ := coexistFixture(t)
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	stripped := removeProjectionBlock(string(readFileBytes(t, path)))
	if err := os.WriteFile(path, []byte(stripped), 0o600); err != nil {
		t.Fatal(err)
	}
	takeover.Heal()
	if name, ok := codexWouldStart(t, path); !ok || name != GatewayProviderName {
		t.Fatalf("heal left Codex on %q (table %v)", name, ok)
	}
	if status := takeover.Status(); status.State != TakeoverStateActive {
		t.Fatalf("status %s (%s)", status.State, status.Detail)
	}
}

// A copy of Mewla's table without the markers (another tool wrote it back) is
// replaced, never duplicated into invalid TOML.
func TestTakeoverEnableReplacesAnUnmarkedCopyOfItsTable(t *testing.T) {
	takeover, path, _ := coexistFixture(t)
	withCopy := coexistUserConfig + "\n[model_providers.mewla-gateway]\nname = \"mewla-gateway\"\nbase_url = \"http://127.0.0.1:3435/v1\"\nwire_api = \"responses\"\n"
	if err := os.WriteFile(path, []byte(withCopy), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	got := string(readFileBytes(t, path))
	if strings.Count(got, "[model_providers.mewla-gateway]") != 1 || strings.Contains(got, "3435") {
		t.Fatalf("want exactly Mewla's own table:\n%s", got)
	}
	if err := validateConfigTOML([]byte(got)); err != nil {
		t.Fatal(err)
	}
	if _, err := takeover.Disable(); err != nil {
		t.Fatal(err)
	}
	if got := string(readFileBytes(t, path)); strings.Contains(got, "mewla-gateway") {
		t.Fatalf("disable left Mewla's name behind:\n%s", got)
	}
}

// A model_provider inside a profile is that profile's, not Codex's choice.
func TestTakeoverIgnoresAProfilesModelProvider(t *testing.T) {
	takeover, path, _ := coexistFixture(t)
	withProfile := coexistUserConfig + "\n[profiles.fast]\nmodel_provider = \"freeride-xcode\"\nmodel = \"gpt-6-luna\"\n"
	if err := os.WriteFile(path, []byte(withProfile), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatalf("a profile's model_provider blocked the takeover: %v", err)
	}
	if !strings.Contains(string(readFileBytes(t, path)), "[profiles.fast]\nmodel_provider = \"freeride-xcode\"") {
		t.Fatal("the profile was changed")
	}
	if _, err := takeover.Disable(); err != nil {
		t.Fatal(err)
	}
	if got := string(readFileBytes(t, path)); got != withProfile {
		t.Fatalf("disable did not give the exact config back:\n%s", got)
	}
}

// The user's provider lost its table while Mewla was on (another tool, or
// the user, removed it): disable does not write back a name Codex would
// refuse; Codex goes to its own login instead.
func TestTakeoverDisableDropsAProviderWhoseTableIsGone(t *testing.T) {
	takeover, path, _ := coexistFixture(t)
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	raw := string(readFileBytes(t, path))
	start := strings.Index(raw, "[model_providers.freeride-xcode]")
	end := strings.Index(raw, "[projects.")
	if err := os.WriteFile(path, []byte(raw[:start]+raw[end:]), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := takeover.Disable(); err != nil {
		t.Fatal(err)
	}
	if name, ok := codexWouldStart(t, path); !ok || name != "openai" {
		t.Fatalf("Codex points at %q (table %v), want its own login", name, ok)
	}
}

// Connect twice while another tool's table is there: one change, then none.
func TestTakeoverConnectIsIdempotentBesideAnotherToolsTable(t *testing.T) {
	takeover, path, magpie := coexistFixture(t)
	magpie.connect()
	magpie.disconnect()
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	first := readFileBytes(t, path)
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	if second := readFileBytes(t, path); string(second) != string(first) {
		t.Fatalf("second connect changed the config:\n%s", second)
	}
	state, _ := takeover.LoadState()
	if state.OriginalProviderValue != "freeride-xcode" {
		t.Fatalf("second connect saved %q as the user's provider", state.OriginalProviderValue)
	}
}
