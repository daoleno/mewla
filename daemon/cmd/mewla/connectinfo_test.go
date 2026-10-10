package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/link"
)

func TestNormalizeEndpoint(t *testing.T) {
	value, err := normalizeEndpoint("https://mewla.example.com")
	if err != nil {
		t.Fatalf("normalizeEndpoint returned error: %v", err)
	}
	if value != "wss://mewla.example.com/ws" {
		t.Fatalf("unexpected normalized URL: %s", value)
	}
}

func TestNormalizeEndpointRejectsMissingScheme(t *testing.T) {
	if _, err := normalizeEndpoint("mewla.example.com"); err == nil {
		t.Fatal("expected error for missing scheme")
	}
}

func TestBuildConnectLinkIncludesDaemonIdentity(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	pairing := auth.PairingToken{
		Value:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ExpiresAt: time.Date(2026, 4, 5, 8, 0, 0, 0, time.UTC),
	}

	rawLink := buildConnectLinkWithPublicKey(
		"wss://mewla.example.com/ws",
		manager.PublicKeyHex(),
		pairing,
	)
	parsed, err := url.Parse(rawLink)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if parsed.Scheme != "mewla" {
		t.Fatalf("unexpected scheme: %s", parsed.Scheme)
	}
	payloadValue := parsed.Query().Get(connectParamPayload)
	if payloadValue == "" {
		t.Fatal("expected compact payload query param")
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadValue)
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	if len(payload) != 1+2+len("wss://mewla.example.com/ws")+connectPublicKeyBytes+connectTokenBytes {
		t.Fatalf("unexpected payload size: %d", len(payload))
	}
	if payload[0] != connectPayloadVersion {
		t.Fatalf("unexpected payload version: %d", payload[0])
	}

	urlLength := int(binary.BigEndian.Uint16(payload[1:3]))
	offset := 3
	gotURL := string(payload[offset : offset+urlLength])
	offset += urlLength
	gotPublicKey := hex.EncodeToString(payload[offset : offset+connectPublicKeyBytes])
	offset += connectPublicKeyBytes
	gotToken := hex.EncodeToString(payload[offset : offset+connectTokenBytes])

	if gotURL != "wss://mewla.example.com/ws" {
		t.Fatalf("unexpected url: %s", gotURL)
	}
	if gotPublicKey != manager.PublicKeyHex() {
		t.Fatalf("unexpected daemon public key: %s", gotPublicKey)
	}
	if gotToken != pairing.Value {
		t.Fatalf("unexpected enrollment token: %s", gotToken)
	}
}

func TestBuildConnectionOffersUsesEndpoint(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	pairing := auth.PairingToken{
		Value:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ExpiresAt: time.Date(2026, 4, 5, 8, 0, 0, 0, time.UTC),
	}

	offers, err := buildConnectionOffersWithPublicKey(
		"https://mewla.example.com/gateway",
		manager.PublicKeyHex(),
		pairing,
	)
	if err != nil {
		t.Fatalf("buildConnectionOffers returned error: %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("expected one offer, got %d", len(offers))
	}
	if offers[0].URL != "wss://mewla.example.com/gateway" {
		t.Fatalf("unexpected offer URL: %s", offers[0].URL)
	}

	parsed, err := url.Parse(offers[0].ConnectLink)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got := parsed.Query().Get(connectParamPayload); got == "" {
		t.Fatal("expected compact payload query param")
	}
}

func testEntries(urls ...string) []addressbook.Entry {
	entries := make([]addressbook.Entry, 0, len(urls))
	seen := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	for index, raw := range urls {
		entries = append(entries, addressbook.Entry{URL: raw, Source: addressbook.SourceDiscovered, LastSeenAt: seen.Add(time.Duration(index) * time.Minute)})
	}
	return entries
}

func TestPrintStartupInfoForLoopbackWithoutPhoneAddress(t *testing.T) {
	var output bytes.Buffer
	printStartupInfo(&output, "127.0.0.1:9876", "/tmp/mewla-state", false, testEntries("http://127.0.0.1:9876"))

	rendered := output.String()
	for _, want := range []string{
		"listening on 127.0.0.1:9876 (this computer only)",
		"mewla pair -state-dir /tmp/mewla-state, then open the browser link",
		"restart with mewla --lan, or add an HTTPS address (mewla address add)",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("startup info missing %q: %q", want, rendered)
		}
	}
	for _, unwanted := range []string{"Daemon ID", "Auth:", "your-mewla-host", "No Wi-Fi/LAN"} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("startup info contains %q: %q", unwanted, rendered)
		}
	}
}

func TestPrintStartupInfoForLoopbackBehindHTTPS(t *testing.T) {
	var output bytes.Buffer
	printStartupInfo(&output, "127.0.0.1:9876", "", false, testEntries("http://127.0.0.1:9876", "https://mewla.example.com"))
	rendered := output.String()
	if !strings.Contains(rendered, "HTTPS       https://mewla.example.com") ||
		!strings.Contains(rendered, "Pair        mewla pair\n") ||
		strings.Contains(rendered, "restart with mewla --lan") {
		t.Fatalf("tunnel startup = %q", rendered)
	}
}

func TestPrintStartupInfoWithLink(t *testing.T) {
	var output bytes.Buffer
	printStartupInfo(&output, "127.0.0.1:9876", "/tmp/mewla-state", true, testEntries("http://127.0.0.1:9876"))
	rendered := output.String()
	for _, expected := range []string{
		"Mewla Link  connecting outbound",
		"Pair        mewla pair -state-dir /tmp/mewla-state",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("Link startup missing %q: %q", expected, rendered)
		}
	}
}

func TestPrintStartupInfoForLANListsAddressesAndOnePairCommand(t *testing.T) {
	var output bytes.Buffer
	printStartupInfo(&output, "0.0.0.0:9876", "", false, testEntries(
		"http://127.0.0.1:9876",
		"http://192.168.1.42:9876",
		"http://100.101.102.103:9876",
		"https://zen.example.com",
	))

	rendered := output.String()
	for _, want := range []string{
		"listening on all networks, port 9876",
		"Wi-Fi/LAN   http://192.168.1.42:9876",
		"Tailscale   http://100.101.102.103:9876",
		"HTTPS       https://zen.example.com",
		"Pair        mewla pair\n",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("startup info missing %q: %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "127.0.0.1") {
		t.Fatalf("startup offered loopback to phones: %q", rendered)
	}
	if strings.Count(rendered, "mewla pair") != 1 {
		t.Fatalf("startup should offer one pairing command: %q", rendered)
	}
}

func TestStartupWithoutPrivateAddressIsActionable(t *testing.T) {
	var output bytes.Buffer
	printStartupInfo(&output, "[::]:9876", "", false, nil)
	if !strings.Contains(output.String(), "No Wi-Fi/LAN or Tailscale address found") ||
		!strings.Contains(output.String(), "add an HTTPS address") {
		t.Fatalf("invalid no-address startup: %q", output.String())
	}
}

func TestStartupAddressesMatchTheListenSocket(t *testing.T) {
	detected := []privateNetworkAddress{
		{label: "Same Wi-Fi/LAN", ip: net.ParseIP("192.168.1.42").To4()},
		{label: "Tailscale", ip: net.ParseIP("100.101.102.103").To4()},
	}
	cases := map[string][]string{
		"127.0.0.1:9876":    {"http://127.0.0.1:9876"},
		"0.0.0.0:9988":      {"http://127.0.0.1:9988", "http://192.168.1.42:9988", "http://100.101.102.103:9988"},
		"192.168.1.42:9876": {"http://192.168.1.42:9876"},
	}
	for listen, want := range cases {
		if got := startupAddresses(listen, detected); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("startupAddresses(%s) = %v, want %v", listen, got, want)
		}
	}
}

func TestPairingNeverPutsLoopbackInThePhoneQR(t *testing.T) {
	publicKey := strings.Repeat("ab", 32)
	token := auth.PairingToken{Value: strings.Repeat("cd", 32), ExpiresAt: time.Now().Add(time.Minute)}
	links, err := buildPairingLinks(testEntries(
		"http://127.0.0.1:9876",
		"http://192.168.1.42:9876",
		"http://100.101.102.103:9876",
	), "", publicKey, token)
	if err != nil {
		t.Fatal(err)
	}
	if links.phoneAddress != "http://100.101.102.103:9876" {
		t.Fatalf("phone address = %q, want Tailscale over LAN", links.phoneAddress)
	}
	if !strings.HasPrefix(links.localBrowser, "http://127.0.0.1:9876/#pair=") || len(links.httpsBrowser) != 0 {
		t.Fatalf("browser links = %q %v", links.localBrowser, links.httpsBrowser)
	}

	entries := testEntries("http://127.0.0.1:9876", "https://old.example.com", "http://192.168.1.42:9876", "https://new.example.com")
	links, err = buildPairingLinks(entries, "", publicKey, token)
	if err != nil {
		t.Fatal(err)
	}
	if links.phoneAddress != "https://new.example.com" || len(links.httpsBrowser) != 2 {
		t.Fatalf("phone address = %q browsers = %v", links.phoneAddress, links.httpsBrowser)
	}

	links, err = buildPairingLinks(testEntries("http://127.0.0.1:9876"), "", publicKey, token)
	if err != nil {
		t.Fatal(err)
	}
	if links.phoneLink != "" || links.localBrowser == "" {
		t.Fatalf("loopback-only links = %#v", links)
	}
	var output bytes.Buffer
	printPairing(&output, "Pair a new phone or browser.", links)
	if !strings.Contains(output.String(), "Phones can't reach this computer yet") || strings.Contains(output.String(), "█") {
		t.Fatalf("loopback-only pairing = %q", output.String())
	}
}

func TestPairingHonoursAnExplicitAddress(t *testing.T) {
	links, err := buildPairingLinks(testEntries("http://192.168.1.42:9876"), "https://tunnel.example.com", strings.Repeat("ab", 32), auth.PairingToken{
		Value:     strings.Repeat("cd", 32),
		ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if links.phoneAddress != "https://tunnel.example.com" || !strings.HasPrefix(links.phoneLink, "mewla://settings?p=") {
		t.Fatalf("links = %#v", links)
	}
}

func TestBuildWebPairingURLCarriesTheConnectLinkInTheFragment(t *testing.T) {
	webURL, err := buildWebPairingURL("http://127.0.0.1:9876", strings.Repeat("ab", 32), auth.PairingToken{
		Value:     strings.Repeat("cd", 32),
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
	if link := fragment.Get("pair"); !strings.HasPrefix(link, "mewla://settings?p=") {
		t.Fatalf("pair link = %q", link)
	}
}

func TestPrintPairing(t *testing.T) {
	var output bytes.Buffer
	printPairing(&output, "Pair a new phone or browser.", pairingLinks{
		expiresAt:    time.Now().Add(time.Minute),
		phoneAddress: "https://mewla.example.com",
		phoneLink:    "mewla://settings?p=compact-payload",
		localBrowser: "http://127.0.0.1:9876/#pair=x",
		httpsBrowser: []string{"https://mewla.example.com/#pair=x"},
	})

	rendered := output.String()
	for _, want := range []string{
		"Phone — in Mewla, Settings → Pair a computer, scan:",
		"or paste:  mewla://settings?p=compact-payload",
		"Address:   https://mewla.example.com",
		"Browser on this computer:  http://127.0.0.1:9876/#pair=x",
		"Browser anywhere:          https://mewla.example.com/#pair=x",
		"mewla devices revoke -id DEVICE_ID",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("pairing output missing %q: %q", want, rendered)
		}
	}
}

func TestPairConfigUsesOnePositionalEndpoint(t *testing.T) {
	cfg, err := parsePairConfig([]string{
		"-state-dir", "/tmp/mewla-state",
		"https://mewla.example.com",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parsePairConfig returned error: %v", err)
	}
	if cfg.endpoint != "https://mewla.example.com" {
		t.Fatalf("endpoint = %q", cfg.endpoint)
	}
	if cfg.stateDir != "/tmp/mewla-state" {
		t.Fatalf("stateDir = %q", cfg.stateDir)
	}
}

func TestPairConfigAllowsNoEndpointOnlyForConfiguredLinkPath(t *testing.T) {
	cfg, err := parsePairConfig([]string{
		"-state-dir", "/tmp/mewla-state",
		"-link-config", "/tmp/mewla-link.json",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parsePairConfig returned error: %v", err)
	}
	if cfg.endpoint != "" || cfg.linkConfigPath != "/tmp/mewla-link.json" {
		t.Fatalf("unexpected no-endpoint config: %#v", cfg)
	}
}

func TestPairCommandWithoutAnyAddressFailsHonestly(t *testing.T) {
	stateDir := t.TempDir()
	var output bytes.Buffer
	err := runPairCommand([]string{"-state-dir", stateDir}, &output)
	if err == nil || !strings.Contains(err.Error(), "this computer has no address yet") {
		t.Fatalf("unexpected no-address pair error: %v", err)
	}
	if strings.Contains(output.String(), "mewla://") {
		t.Fatalf("no-address command printed an unusable pairing link: %q", output.String())
	}
}

func TestUnknownCommandIsNotStartedAsTheDaemon(t *testing.T) {
	for _, removed := range []string{"web", "frobnicate"} {
		err := run([]string{removed}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "unknown command") || !strings.Contains(err.Error(), "mewla --help") {
			t.Fatalf("run(%s) = %v", removed, err)
		}
	}
}

func TestRemovedWebOriginFlagIsRejected(t *testing.T) {
	if _, err := parseDaemonConfig([]string{"-web-origin", "https://mewla.example.com"}, io.Discard); err == nil {
		t.Fatal("daemon accepted removed -web-origin flag")
	}
}

func TestOptionalLinkConfigIsInertUntilExplicitlyConfigured(t *testing.T) {
	stateDir := t.TempDir()
	config, path, enabled, err := loadOptionalLinkConfig(stateDir, "")
	if err != nil {
		t.Fatalf("absent default Link config returned error: %v", err)
	}
	if enabled || path != filepath.Join(stateDir, link.DefaultConfigFilename) {
		t.Fatalf(
			"absent default Link config = enabled %t path %q config %#v",
			enabled,
			path,
			config,
		)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "link-identity.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default config check created Link identity state: %v", err)
	}

	explicitPath := filepath.Join(stateDir, "operator-link.json")
	if _, _, _, err := loadOptionalLinkConfig(stateDir, explicitPath); err == nil {
		t.Fatal("explicit missing Link config was silently ignored")
	}
}

func TestRemovedAdvertiseURLFlagsAreRejected(t *testing.T) {
	if _, err := parseDaemonConfig([]string{"-advertise-url", "https://mewla.example.com"}, io.Discard); err == nil {
		t.Fatal("daemon accepted removed -advertise-url flag")
	}
	if _, err := parsePairConfig([]string{"-url", "https://mewla.example.com"}, io.Discard); err == nil {
		t.Fatal("pair accepted removed -url flag")
	}
}

func TestDaemonConfigLANModeAndAddrConflict(t *testing.T) {
	cfg, err := parseDaemonConfig([]string{"--lan"}, io.Discard)
	if err != nil {
		t.Fatalf("parseDaemonConfig(--lan): %v", err)
	}
	if cfg.addr != "0.0.0.0:9876" || !cfg.lan {
		t.Fatalf("LAN config = %#v", cfg)
	}

	defaultCfg, err := parseDaemonConfig(nil, io.Discard)
	if err != nil {
		t.Fatalf("parseDaemonConfig(default): %v", err)
	}
	if defaultCfg.addr != "127.0.0.1:9876" || defaultCfg.lan {
		t.Fatalf("default config = %#v", defaultCfg)
	}

	if _, err := parseDaemonConfig([]string{"--lan", "-addr", "192.168.1.42:9876"}, io.Discard); err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("expected explicit --lan/-addr conflict, got %v", err)
	}
	if _, err := parseDaemonConfig([]string{"-addr", "192.168.1.42:9876", "--lan"}, io.Discard); err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("expected order-independent --lan/-addr conflict, got %v", err)
	}
}

func TestStartupPairingAddressesNeverReturnsWildcard(t *testing.T) {
	detected := []privateNetworkAddress{{label: "Same Wi-Fi/LAN", ip: net.ParseIP("10.0.0.7")}}
	got := startupPairingAddresses("0.0.0.0", detected)
	if len(got) != 1 || !got[0].ip.Equal(net.ParseIP("10.0.0.7")) {
		t.Fatalf("startupPairingAddresses = %#v", got)
	}
	if got := startupPairingAddresses("192.168.2.9", nil); len(got) != 1 || got[0].ip.String() != "192.168.2.9" {
		t.Fatalf("specific private address = %#v", got)
	}
}

func TestPrivateNetworkAddressDetectionSkipsContainerInterfaces(t *testing.T) {
	for _, name := range []string{"docker0", "br-deadbeef", "veth123", "virbr0", "cni0", "podman0", "kube-bridge"} {
		if !shouldSkipPrivateNetworkInterface(name) {
			t.Fatalf("expected %q to be skipped", name)
		}
	}
	for _, name := range []string{"en0", "eth0", "wlan0", "tailscale0"} {
		if shouldSkipPrivateNetworkInterface(name) {
			t.Fatalf("expected %q to remain eligible", name)
		}
	}
}

func TestTopLevelHelpIncludesWorkerAndBrainCommands(t *testing.T) {
	var output bytes.Buffer
	_, err := parseDaemonConfig([]string{"--help"}, &output)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseDaemonConfig error = %v, want ErrHelp", err)
	}
	rendered := output.String()
	for _, want := range []string{
		"worker       List, spawn, inspect, message, progress, and close Mewla Workers",
		"brain        Inspect Brain workspace and host executor configuration",
		"pair      Show a one-time QR code and links to pair a new phone or browser",
		"address   List, add or remove the addresses phones and browsers use",
		"-addr host:port       listen on host:port",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("top-level help missing %q:\n%s", want, rendered)
		}
	}
}

func TestWorkerAndBrainHelpAreDiscoverable(t *testing.T) {
	var workerOutput bytes.Buffer
	if err := runWorkerCommand([]string{"--help"}, &workerOutput); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runWorkerCommand error = %v, want ErrHelp", err)
	}
	workerHelp := workerOutput.String()
	for _, want := range []string{
		"Usage: mewla worker <list|spawn|send|capture|status|receipt|progress|release|close> [flags]",
		"mewla worker spawn -name \"Review docs\" -executor codex -model gpt-6-astra -reasoning high",
		"mewla worker capture -id",
		"mewla worker status -id",
		"mewla worker progress --status running",
		"mewla worker close -id",
	} {
		if !strings.Contains(workerHelp, want) {
			t.Fatalf("agent help missing %q:\n%s", want, workerHelp)
		}
	}

	var progressOutput bytes.Buffer
	if err := runWorkerCommand([]string{"progress", "--help"}, &progressOutput); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runWorkerCommand progress help error = %v, want ErrHelp", err)
	}
	progressHelp := progressOutput.String()
	for _, want := range []string{
		"Usage: mewla worker progress --status running --phase working --attention none",
		"-id",
		"-lease",
		"-status",
		"-task-class",
		"-event-kind",
		"-details-json",
	} {
		if !strings.Contains(progressHelp, want) {
			t.Fatalf("progress help missing %q:\n%s", want, progressHelp)
		}
	}

	var brainOutput bytes.Buffer
	if err := runBrainCommand([]string{"--help"}, &brainOutput); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runBrainCommand error = %v, want ErrHelp", err)
	}
	brainHelp := brainOutput.String()
	for _, want := range []string{
		"Usage: mewla brain <workspace|context|playbooks|gc|work|objective|executors|use> [flags]",
		"mewla brain objective set",
		"Reconcile product-owned Brain workspace blocks while preserving user content",
		"mewla brain workspace --json",
		"mewla brain context --json",
		"mewla brain playbooks --json",
		"mewla brain gc --json",
		"mewla brain executors --json",
	} {
		if !strings.Contains(brainHelp, want) {
			t.Fatalf("brain help missing %q:\n%s", want, brainHelp)
		}
	}
}

type captureCLIControlHandler struct {
	requests chan control.Request
}

func (h *captureCLIControlHandler) HandleControlRequest(req control.Request) control.Response {
	h.requests <- req
	response := control.Response{OK: true}
	if req.Type == "device_revoke" {
		durable := true
		response.PersistenceOutcome = control.PersistenceApplied
		response.PersistenceDurable = &durable
	}
	return response
}

func TestWorkerProgressCommandUsesWorkerIDFallback(t *testing.T) {
	req := runProgressCLIAndCaptureRequest(t,
		"mewla-worker-env:@1",
		[]string{
			"--turn-id", "turn:cli-current",
			"--status", "running",
			"--phase", "working",
			"--attention", "none",
			"--summary", "Reading files",
			"--task-class", "exploration",
			"--event-kind", "progress",
			"--details-json", `{"files":3}`,
			"--lease", "300",
			"--json=false",
		},
	)

	if req.Type != "worker_progress" || req.WorkerID != "mewla-worker-env:@1" || req.TurnID != "turn:cli-current" {
		t.Fatalf("request identity = %#v", req)
	}
	if req.Status != "running" || req.Phase != "working" || req.Attention != "none" {
		t.Fatalf("request progress = %#v", req)
	}
	if req.Summary != "Reading files" || req.LeaseSeconds != 300 {
		t.Fatalf("request progress metadata = %#v", req)
	}
	if req.TaskClass != "exploration" || req.EventKind != "progress" || req.DetailsJSON != `{"files":3}` {
		t.Fatalf("semantic request metadata = %#v", req)
	}
}

func TestWorkerProgressCommandExplicitIDOverridesEnv(t *testing.T) {
	req := runProgressCLIAndCaptureRequest(t,
		"mewla-worker-env:@1",
		[]string{
			"-id", "mewla-worker-explicit:@2",
			"--turn-id", "turn:explicit",
			"--status", "done",
			"--phase", "reporting",
			"--attention", "done",
			"--summary", "Finished",
			"--json=false",
		},
	)

	if req.WorkerID != "mewla-worker-explicit:@2" || req.TurnID != "turn:explicit" {
		t.Fatalf("request identity = %#v", req)
	}
	if req.Status != "done" || req.Phase != "reporting" || req.Attention != "done" {
		t.Fatalf("request progress = %#v", req)
	}
}

func TestWorkerProgressCommandUsesStateDirFallback(t *testing.T) {
	stateDir := shortControlStateDir(t)
	handler, done, cancel := startCLIControlServer(t, stateDir)
	defer cancel()

	t.Setenv("MEWLA_WORKER_ID", "mewla-worker-env:@1")
	t.Setenv("MEWLA_STATE_DIR", stateDir)
	var stderr bytes.Buffer
	if err := runWorkerProgress([]string{
		"--status", "running",
		"--phase", "working",
		"--attention", "none",
		"--summary", "Reading files",
		"--json=false",
	}, &stderr); err != nil {
		t.Fatalf("runWorkerProgress returned error: %v stderr=%s", err, stderr.String())
	}

	select {
	case req := <-handler.requests:
		if req.WorkerID != "mewla-worker-env:@1" || req.Status != "running" {
			t.Fatalf("request = %#v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for control request")
	}

	cancel()
	waitForCLIControlServerShutdown(t, done)
}

func TestBrainResolveCLI(t *testing.T) {
	stateDir := shortControlStateDir(t)
	handler, done, cancel := startCLIControlServer(t, stateDir)
	defer cancel()
	var stderr bytes.Buffer
	if err := runBrainCommand([]string{
		"work", "resolve", "--state-dir", stateDir, "--json=false",
		"--work-id", "work-1", "--handling-id", "handling-1",
		"--provider-turn-id", "provider-turn-1", "--revision", "7",
		"--disposition", "continue",
	}, &stderr); err != nil {
		t.Fatalf("runBrainCommand returned error: %v stderr=%s", err, stderr.String())
	}

	select {
	case req := <-handler.requests:
		got := req.BrainWorkDisposition
		if req.Type != "brain_work_resolve" || got == nil || got.WorkID != "work-1" ||
			got.HandlingID != "handling-1" || got.ProviderTurnID != "provider-turn-1" ||
			got.ExpectedWorkRevision != 7 || got.Disposition != brain.WorkDispositionContinue {
			t.Fatalf("resolve request = %#v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Brain resolve control request")
	}

	cancel()
	waitForCLIControlServerShutdown(t, done)
}

func TestBrainResolveCLIDueRetry(t *testing.T) {
	stateDir := shortControlStateDir(t)
	handler, done, cancel := startCLIControlServer(t, stateDir)
	defer cancel()
	const due = "2026-08-23T03:04:05Z"
	var stderr bytes.Buffer
	if err := runBrainCommand([]string{
		"work", "resolve", "--state-dir", stateDir, "--json=false",
		"--work-id", "work-due", "--handling-id", "handling-due",
		"--provider-turn-id", "provider-turn-due", "--revision", "9",
		"--disposition", "wait", "--wake-kind", "due_retry",
		"--wake-ref", "external-run:49dc23f4", "--next-attempt-at", due,
	}, &stderr); err != nil {
		t.Fatalf("runBrainCommand returned error: %v stderr=%s", err, stderr.String())
	}

	select {
	case req := <-handler.requests:
		got := req.BrainWorkDisposition
		wantDue, _ := time.Parse(time.RFC3339, due)
		if req.Type != "brain_work_resolve" || got == nil || got.Wake == nil ||
			got.Wake.Kind != brain.WorkWakeDueRetry || got.Wake.Ref != "external-run:49dc23f4" ||
			got.Wake.NextAttemptAt == nil || !got.Wake.NextAttemptAt.Equal(wantDue) {
			t.Fatalf("due retry request = %#v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for due retry resolve request")
	}

	cancel()
	waitForCLIControlServerShutdown(t, done)
}

func TestBrainWorkCloseCLI(t *testing.T) {
	stateDir := shortControlStateDir(t)
	handler, done, cancel := startCLIControlServer(t, stateDir)
	defer cancel()
	var stderr bytes.Buffer
	if err := runBrainCommand([]string{
		"work", "close", "--state-dir", stateDir, "--json=false",
		"--id", "work-1", "--revision", "19", "--status", "cancelled",
		"--actor", "brain", "--reason", "verified obsolete execution stage",
	}, &stderr); err != nil {
		t.Fatalf("runBrainCommand returned error: %v stderr=%s", err, stderr.String())
	}

	select {
	case req := <-handler.requests:
		if req.Type != "brain_work_close" || req.WorkID != "work-1" || req.Revision != 19 ||
			req.Status != "cancelled" || req.Actor != "brain" ||
			req.Reason != "verified obsolete execution stage" {
			t.Fatalf("close request = %#v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Brain close control request")
	}

	cancel()
	waitForCLIControlServerShutdown(t, done)
}

func TestRevokeDeviceUsesRunningDaemonControlOwner(t *testing.T) {
	stateDir := shortControlStateDir(t)
	handler, done, cancel := startCLIControlServer(t, stateDir)
	defer cancel()

	if _, err := revokeDevice(stateDir, "phone-one"); err != nil {
		t.Fatalf("revokeDevice returned error: %v", err)
	}
	select {
	case request := <-handler.requests:
		if request.Type != "device_revoke" || request.ID != "phone-one" {
			t.Fatalf("revoke request=%#v", request)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for device revoke request")
	}

	cancel()
	waitForCLIControlServerShutdown(t, done)
}

func TestWorkerProgressCommandRequiresIDOrEnv(t *testing.T) {
	var stderr bytes.Buffer
	t.Setenv("MEWLA_WORKER_ID", "")
	err := runWorkerProgress([]string{
		"--status", "running",
		"--phase", "working",
		"--attention", "none",
		"--json=false",
	}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "Worker id is required") {
		t.Fatalf("runWorkerProgress error = %v", err)
	}
}

func runProgressCLIAndCaptureRequest(t *testing.T, envWorkerID string, args []string) control.Request {
	t.Helper()
	stateDir := shortControlStateDir(t)
	handler, done, cancel := startCLIControlServer(t, stateDir)
	defer cancel()

	t.Setenv("MEWLA_WORKER_ID", envWorkerID)
	commandArgs := append([]string{"--state-dir", stateDir}, args...)
	var stderr bytes.Buffer
	if err := runWorkerProgress(commandArgs, &stderr); err != nil {
		t.Fatalf("runWorkerProgress returned error: %v stderr=%s", err, stderr.String())
	}

	var req control.Request
	select {
	case req = <-handler.requests:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for control request")
	}

	cancel()
	waitForCLIControlServerShutdown(t, done)
	return req
}

func startCLIControlServer(t *testing.T, stateDir string) (*captureCLIControlHandler, chan error, context.CancelFunc) {
	t.Helper()
	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatalf("DefaultSocketPath returned error: %v", err)
	}
	handler := &captureCLIControlHandler{requests: make(chan control.Request, 1)}
	server := &control.Server{Path: socketPath, Handler: handler}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx)
	}()
	waitForCLISocketPath(t, socketPath)
	return handler, done, cancel
}

func waitForCLIControlServerShutdown(t *testing.T, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server exited with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for control server shutdown")
	}
}

func waitForCLISocketPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for control socket at %s", path)
}

func TestFirstDeviceStartupPrintsQRAndLink(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	book, err := addressbook.New(manager.StorageDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = book.Add("http://127.0.0.1:9876", addressbook.SourceDiscovered)
	_, _ = book.Add("https://mewla.example", addressbook.SourceManual)
	var output bytes.Buffer
	if err := printFirstDevicePairing(&output, manager, book); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "First device: pair a phone or browser.") ||
		!strings.Contains(text, "Address:   https://mewla.example") ||
		!strings.Contains(text, "https://mewla.example/#pair=") ||
		!strings.Contains(text, "█") {
		t.Fatalf("fresh startup must print the phone QR and browser links: %q", text)
	}
}

func TestBrainObjectiveCLISendsControlRequests(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		wantType string
		wantText string
	}{
		{args: []string{"set", "Ship", "atlas-notes v1.4"}, wantType: "brain_objective_set", wantText: "Ship atlas-notes v1.4"},
		{args: []string{"clear"}, wantType: "brain_objective_clear"},
		{args: nil, wantType: "brain_objective"},
	} {
		stateDir := shortControlStateDir(t)
		handler, done, cancel := startCLIControlServer(t, stateDir)
		args := append([]string(nil), tc.args...)
		if len(args) > 0 {
			args = append([]string{args[0], "--state-dir", stateDir, "--json=false"}, args[1:]...)
		} else {
			args = []string{"--state-dir", stateDir, "--json=false"}
		}
		var stderr bytes.Buffer
		if err := runBrainObjective(args, &stderr); err != nil {
			t.Fatalf("runBrainObjective(%v) error = %v stderr=%s", tc.args, err, stderr.String())
		}
		var req control.Request
		select {
		case req = <-handler.requests:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for control request")
		}
		cancel()
		waitForCLIControlServerShutdown(t, done)
		if req.Type != tc.wantType || req.Text != tc.wantText {
			t.Fatalf("%v sent %q %q, want %q %q", tc.args, req.Type, req.Text, tc.wantType, tc.wantText)
		}
	}
	var stderr bytes.Buffer
	if err := runBrainObjective([]string{"set"}, &stderr); err == nil {
		t.Fatal("objective set without a title was accepted")
	}
}

func TestTailscaleOnlyListenIsOfferedToPhones(t *testing.T) {
	book, err := addressbook.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	detected := []privateNetworkAddress{
		{label: "Same Wi-Fi/LAN", ip: net.ParseIP("192.168.1.42").To4()},
		{label: "Tailscale", ip: net.ParseIP("100.92.174.90").To4()},
	}
	for _, listen := range []string{"100.92.174.90:9876", "0.0.0.0:9876"} {
		if err := book.ReplaceDiscovered(startupAddresses(listen, detected)); err != nil {
			t.Fatal(err)
		}
		entries, err := book.List()
		if err != nil {
			t.Fatal(err)
		}
		ranked := rankedPhoneAddresses(entries)
		if len(ranked) == 0 || ranked[0].URL != "http://100.92.174.90:9876" {
			t.Fatalf("listen %s: phone addresses = %v", listen, ranked)
		}
	}
}
