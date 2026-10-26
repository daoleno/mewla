package connections

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// The SDK's published test key. It holds nothing.
const hlTestKey = "0x0123456789012345678901234567890123456789012345678901234567890123"

type hlVectors struct {
	Address string `json:"address"`
	Cases   map[string]struct {
		Nonce       uint64      `json:"nonce"`
		MsgpackHash string      `json:"msgpack_hash"`
		Mainnet     hlSignature `json:"mainnet"`
		Testnet     hlSignature `json:"testnet"`
	} `json:"cases"`
	Wires map[string]string `json:"wires"`
}

func loadHLVectors(t *testing.T) hlVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/hyperliquid_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v hlVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// The vectors were produced by the official Python SDK's own sign_l1_action
// (testdata/README.md). The first three cases are the SDK's regression tests.
func TestHyperliquidSigningMatchesOfficialSDK(t *testing.T) {
	v := loadHLVectors(t)
	key, err := hlParseKey(hlTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if hlAddress(key) != v.Address {
		t.Fatalf("address %s, SDK %s", hlAddress(key), v.Address)
	}
	cloid := "0xabcdefabcdefabcdefabcdefabcdef12"
	actions := map[string]hlMap{
		"order_eth_gtc":           hlPlaceAction(hlOrder{asset: 1, buy: true, price: "100", size: "100", tif: "Gtc"}),
		"order_eth_cloid":         hlPlaceAction(hlOrder{asset: 1, buy: true, price: "100", size: "100", tif: "Gtc", cloid: "0x00000000000000000000000000000001"}),
		"order_eth_ioc":           hlPlaceAction(hlOrder{asset: 4, buy: true, price: "1670.1", size: "0.0147", tif: "Ioc"}),
		"order_btc_far_bid":       hlPlaceAction(hlOrder{asset: 0, buy: true, price: "50000", size: "0.001", tif: "Gtc"}),
		"order_spot_alo":          hlPlaceAction(hlOrder{asset: 10107, price: "0.0001234", size: "12345", reduceOnly: true, tif: "Alo"}),
		"order_builder_dex_asset": hlPlaceAction(hlOrder{asset: 110000, buy: true, price: "123456", size: "1", tif: "Gtc", cloid: cloid}),
		"order_asset_200":         hlPlaceAction(hlOrder{asset: 200, price: "0.5", size: "7.25", tif: "Gtc"}),
		"cancel":                  hlCancelAction(0, 91490942),
		"cancel_by_cloid":         hlCancelCloidAction(3, "0x1234567890abcdef1234567890abcdef"),
		"modify":                  hlModifyAction(uint64(91490942), hlOrder{asset: 0, buy: true, price: "49000.5", size: "0.002", tif: "Gtc"}),
		"leverage_cross":          hlLeverageAction(1, true, 5),
		"leverage_isolated":       hlLeverageAction(0, false, 20),
		"schedule_cancel":         hlScheduleCancelAction(1792761800000),
		"schedule_cancel_unset":   hlScheduleCancelAction(0),
	}
	if len(actions) != len(v.Cases) {
		t.Fatalf("%d actions, %d vectors", len(actions), len(v.Cases))
	}
	for name, want := range v.Cases {
		action, ok := actions[name]
		if !ok {
			t.Fatalf("no action for vector %s", name)
		}
		hash, err := hlActionHash(action, want.Nonce)
		if err != nil {
			t.Fatal(name, err)
		}
		if "0x"+hex.EncodeToString(hash) != want.MsgpackHash {
			t.Errorf("%s: action hash %x, SDK %s", name, hash, want.MsgpackHash)
		}
		for network, expected := range map[string]hlSignature{"mainnet": want.Mainnet, "testnet": want.Testnet} {
			got, err := hlSignL1(key, action, want.Nonce, network == "mainnet")
			if err != nil || got != expected {
				t.Errorf("%s %s: signature %+v (%v), SDK %+v", name, network, got, err, expected)
			}
		}
	}
}

func TestHyperliquidWireDecimalMatchesFloatToWire(t *testing.T) {
	for in, want := range loadHLVectors(t).Wires {
		got, err := hlWireDecimal(in)
		if err != nil || got != want {
			t.Errorf("%s: %q (%v), SDK %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "1e3", "-1x", "0.000000001", "1/2", "+5"} {
		if got, err := hlWireDecimal(bad); err == nil {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
	if got, _ := hlWireDecimal("-0"); got != "0" {
		t.Errorf("-0 -> %q", got)
	}
}

func TestHyperliquidPriceAndSizeRules(t *testing.T) {
	btc := hlMarket{name: "BTC", szDecimals: 5}
	spot := hlMarket{name: "PURR/USDC", szDecimals: 0, spot: true}
	for _, c := range []struct {
		mk    hlMarket
		price string
		ok    bool
	}{
		{btc, "50000", true},    // whole numbers are always valid
		{btc, "123456", true},   // even beyond 5 significant figures
		{btc, "81752.5", false}, // 6 significant figures
		{btc, "1234.5", true},   // 5 significant figures, 1 decimal allowed (6-5)
		{btc, "0.1", true},
		{btc, "0.12", false},      // 2 decimals > 6-5
		{spot, "0.0001234", true}, // 7 decimals <= 8
		{spot, "0.000012345", false},
		{spot, "0.123456", false}, // 6 significant figures
		{btc, "0", false},
	} {
		got, err := hlCheckPrice(c.price, c.mk)
		if (err == nil) != c.ok {
			t.Errorf("%s %s: %q, %v", c.mk.name, c.price, got, err)
		}
		if err != nil && strings.Contains(err.Error(), "is not valid for") && !strings.Contains(err.Error(), "Nothing was sent") {
			t.Errorf("rejection should say nothing was sent: %v", err)
		}
	}
	if _, err := hlCheckSize("0.00123", btc); err != nil {
		t.Error(err)
	}
	if _, err := hlCheckSize("0.0000001", btc); err == nil {
		t.Error("size with too many decimals accepted")
	}
	if _, err := hlCheckSize("1.5", spot); err == nil {
		t.Error("fractional size accepted for a whole-unit spot token")
	}
}

type hlFake struct {
	t        *testing.T
	signer   string // the API wallet the exchange has approved
	posts    []string
	exchange string // body for /exchange
	approved bool
	network  string // the host that approved the signer: "api.hyperliquid.xyz" unless set
	role     string // userRole of an unapproved signer: "missing" unless set
}

func (f *hlFake) transport(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	if r.Header.Get("Authorization") != "" {
		f.t.Errorf("Hyperliquid request carried an Authorization header")
	}
	f.posts = append(f.posts, r.URL.Host+r.URL.Path+" "+string(body))
	if r.URL.Path == "/exchange" {
		return reply(200, f.exchange), nil
	}
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	switch req["type"] {
	case "userRole":
		host := f.network
		if host == "" {
			host = "api.hyperliquid.xyz"
		}
		if f.approved && req["user"] == f.signer && r.URL.Host == host {
			return reply(200, `{"role":"agent","data":{"user":"`+hlMaster+`"}}`), nil
		}
		role := f.role
		if role == "" {
			role = "missing"
		}
		return reply(200, `{"role":"`+role+`"}`), nil
	case "extraAgents":
		if f.approved {
			return reply(200, `[{"name":"mewla","address":"`+f.signer+`","validUntil":4102444800000}]`), nil
		}
		return reply(200, `[]`), nil
	case "meta":
		return reply(200, `{"universe":[{"name":"BTC","szDecimals":5,"maxLeverage":40},{"name":"ETH","szDecimals":4,"maxLeverage":25}]}`), nil
	case "spotMeta":
		return reply(200, `{"tokens":[{"name":"USDC","szDecimals":8},{"name":"PURR","szDecimals":0}],"universe":[{"name":"PURR/USDC","index":0,"tokens":[1,0]}]}`), nil
	case "l2Book":
		return reply(200, `{"coin":"BTC","time":1792761713319,"levels":[[{"px":"81752.0","sz":"1","n":3},{"px":"81751.0","sz":"2","n":1}],[{"px":"81753.0","sz":"1","n":2}]]}`), nil
	case "frontendOpenOrders":
		return reply(200, `[{"coin":"BTC","oid":7,"timestamp":1792761713319}]`), nil
	}
	return reply(200, `{}`), nil
}

const hlMaster = "0x00000000000000000000000000000000000000aa"

func hlConnect(t *testing.T, m *Manager, key string) (*Account, error) {
	t.Helper()
	r, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "hyperliquid", Credential: key}})
	if err != nil {
		return nil, err
	}
	return r.Account, nil
}

func hlInvoke(t *testing.T, m *Manager, id, tool, args string) (map[string]any, error) {
	t.Helper()
	r, err := m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: tool, Arguments: json.RawMessage(args)})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(r.Result, &out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

func hlReader(t *testing.T, m *Manager) *Account {
	t.Helper()
	if err := m.ensureHyperliquidReader(); err != nil {
		t.Fatal(err)
	}
	for _, r := range m.records {
		if r.Account.Integration == "hyperliquid" && r.Account.Address == "" && r.Account.Status != "disconnected" {
			a := m.projection(r)
			return &a
		}
	}
	t.Fatal("no reader")
	return nil
}

func TestHyperliquidReadsNeedNoSetup(t *testing.T) {
	f := &hlFake{t: t}
	m := testManager(t, f.transport)
	reader := hlReader(t, m)
	if len(f.posts) != 0 {
		t.Fatalf("seeding made requests: %v", f.posts)
	}
	if reader.Name != "Hyperliquid" || reader.Network != "mainnet" || reader.Address != "" || reader.Status != "connected" || !reader.Enabled || reader.Identity != "Read only" {
		t.Fatalf("%+v", reader)
	}
	for _, tool := range reader.Tools {
		if tool.Group != "read" || !tool.Allowed {
			t.Fatalf("%s group=%s allowed=%v", tool.Name, tool.Group, tool.Allowed)
		}
	}
	// Seeding again adds nothing.
	hlReader(t, m)
	if len(m.records) != 1 {
		t.Fatalf("%d accounts", len(m.records))
	}
	// Market reads need no address; an account read names its own.
	f.posts = nil
	out, err := hlInvoke(t, m, reader.ID, "get_book", `{"coin":"BTC"}`)
	if err != nil || out["network"] != "mainnet" || !strings.HasPrefix(f.posts[0], "api.hyperliquid.xyz/info") {
		t.Fatalf("%v %s %v", err, mustJSON(out), f.posts)
	}
	if _, err := hlInvoke(t, m, reader.ID, "get_account", `{}`); err == nil || !strings.Contains(err.Error(), "`address`") || !strings.HasPrefix(err.Error(), "Hyperliquid Mainnet: ") {
		t.Fatalf("a read without an address: %v", err)
	}
	f.posts = nil
	out, err = hlInvoke(t, m, reader.ID, "get_open_orders", `{"address":"`+hlMaster+`"}`)
	if err != nil || out["address"] != hlMaster || !strings.Contains(f.posts[0], hlMaster) {
		t.Fatalf("%v %s %v", err, mustJSON(out), f.posts)
	}
	if _, err := hlInvoke(t, m, reader.ID, "get_me", `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err := hlInvoke(t, m, reader.ID, "place_order", `{"coin":"BTC","side":"buy","size":"0.001","price":"50000"}`); err == nil {
		t.Fatal("the reader placed an order")
	}
	// Reading is not something to sign out of: the reader refuses to disconnect.
	if _, err := m.Handle(context.Background(), Request{Action: "disconnect", ID: reader.ID}); err == nil {
		t.Fatal("the reader disconnected")
	}
	if r := m.records[reader.ID]; r.Account.Status != "connected" || !r.Account.Enabled {
		t.Fatalf("%+v", r.Account)
	}
}

// A reader that an earlier version let the person disconnect has nothing but a
// key form on its page. The daemon brings it back in place, on.
func TestHyperliquidDisconnectedReaderComesBackOn(t *testing.T) {
	f := &hlFake{t: t}
	m := testManager(t, f.transport)
	id := "11111111-2222-4333-8444-555555555555"
	m.records[id] = &record{Account: Account{ID: id, Integration: "hyperliquid", Name: "Hyperliquid", Identity: "Mainnet · read only", Network: "mainnet", Enabled: false, Status: "disconnected", History: []Event{}}, Grants: map[string]string{}}
	if err := m.ensureHyperliquidReader(); err != nil || len(m.records) != 1 {
		t.Fatalf("%v %d accounts", err, len(m.records))
	}
	reader := hlReader(t, m)
	if reader.ID != id || !reader.Enabled || reader.Status != "connected" || reader.Identity != "Read only" || len(reader.Tools) == 0 {
		t.Fatalf("%+v", reader)
	}
	for _, tool := range reader.Tools {
		if tool.Group != "read" || !tool.Allowed {
			t.Fatalf("%s group=%s allowed=%v", tool.Name, tool.Group, tool.Allowed)
		}
	}
	if _, err := hlInvoke(t, m, id, "get_book", `{"coin":"BTC"}`); err != nil {
		t.Fatal(err)
	}
	// It is kept across restarts of the manager's defaults, and a switched-off reader stays off.
	mustHandle(t, m, Request{Action: "disable", ID: id})
	if err := m.ensureHyperliquidReader(); err != nil || m.records[id].Account.Enabled {
		t.Fatalf("a switched-off reader was turned on: %v", err)
	}
}

// The trading key is added and removed without touching reading: the same
// account, its switch and its read permissions stay as they were.
func TestHyperliquidKeyIsIndependentOfReading(t *testing.T) {
	key, _ := hlParseKey(hlTestKey)
	f := &hlFake{t: t, signer: hlAddress(key), approved: true}
	m := testManager(t, f.transport)
	reader := hlReader(t, m)
	grants := map[string]string{}
	for name, g := range m.records[reader.ID].Grants {
		grants[name] = g
	}
	acct, err := hlConnect(t, m, hlTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if acct.ID != reader.ID || !acct.Enabled {
		t.Fatalf("%+v", acct)
	}
	// Removing the key changes nothing about reading, switched off or on.
	mustHandle(t, m, Request{Action: "disable", ID: acct.ID})
	removed := mustHandle(t, m, Request{Action: "remove_key", ID: acct.ID}).Account
	if removed.ID != reader.ID || removed.Enabled || removed.Status != "connected" || len(m.records) != 1 {
		t.Fatalf("%+v (%d accounts)", removed, len(m.records))
	}
	for name, g := range grants {
		if m.records[reader.ID].Grants[name] != g {
			t.Fatalf("read grant %s changed", name)
		}
	}
	// With the key gone the account cannot be disconnected either; reading stays.
	if _, err := m.Handle(context.Background(), Request{Action: "disconnect", ID: acct.ID}); err == nil {
		t.Fatal("the mainnet account disconnected")
	}
}

func TestHyperliquidReaderIsSeededOnStartAndTradingKeyUpgradesIt(t *testing.T) {
	key, _ := hlParseKey(hlTestKey)
	f := &hlFake{t: t, signer: hlAddress(key), approved: true}
	m := testManager(t, f.transport)
	reader := hlReader(t, m)
	acct, err := hlConnect(t, m, hlTestKey)
	if err != nil {
		t.Fatal(err)
	}
	// The key becomes the reader's trading side; Brain never sees two readers.
	if acct.ID != reader.ID || len(m.records) != 1 || acct.Network != "mainnet" || acct.Address != hlMaster || acct.Signer != hlAddress(key) {
		t.Fatalf("%+v (%d accounts)", acct, len(m.records))
	}
	if acct.Name != "Hyperliquid" || !strings.Contains(acct.Identity, "trading key saved") {
		t.Fatalf("%+v", acct)
	}
	// With an address of its own, reads default to it and may still name another.
	f.posts = nil
	out, err := hlInvoke(t, m, acct.ID, "get_open_orders", `{}`)
	if err != nil || out["address"] != hlMaster {
		t.Fatalf("%v %s", err, mustJSON(out))
	}
	// The key leaves by Remove key, which keeps the account reading; Disconnect is refused.
	if _, err := m.Handle(context.Background(), Request{Action: "disconnect", ID: acct.ID}); err == nil {
		t.Fatal("the trading account disconnected")
	}
	removed := mustHandle(t, m, Request{Action: "remove_key", ID: acct.ID}).Account
	if removed.ID != reader.ID || !removed.Enabled || removed.Signer != "" || len(m.records) != 1 {
		t.Fatalf("%+v", removed)
	}
	if _, ok, _ := m.vault.Get("integration:" + acct.ID); ok {
		t.Fatal("the key survived remove_key")
	}
}

func TestHyperliquidDaemonStartSeedsTheReader(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil || m.EnsureDefaults() != nil {
		t.Fatal(err)
	}
	if len(m.records) != 1 {
		t.Fatalf("%d accounts", len(m.records))
	}
	again, err := New(dir)
	if err != nil || again.EnsureDefaults() != nil || len(again.records) != 1 {
		t.Fatalf("restart: %v %d", err, len(again.records))
	}
}

func TestHyperliquidTheKeyNamesTheNetworkAndEveryResultNamesIt(t *testing.T) {
	key, _ := hlParseKey(hlTestKey)
	f := &hlFake{t: t, signer: hlAddress(key), approved: true, network: "api.hyperliquid-testnet.xyz"}
	m := testManager(t, f.transport)
	reader := hlReader(t, m)
	// A key only testnet approved is a testnet account beside the mainnet reader.
	test, err := hlConnect(t, m, hlTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if test.ID == reader.ID || test.Network != "testnet" || test.Name != "Hyperliquid Testnet" || !strings.HasPrefix(test.Identity, "0x0000…00aa") || len(m.records) != 2 {
		t.Fatalf("%+v", test)
	}
	for _, c := range []struct {
		id, network, host string
	}{{reader.ID, "mainnet", "api.hyperliquid.xyz"}, {test.ID, "testnet", "api.hyperliquid-testnet.xyz"}} {
		for _, call := range []struct{ tool, args string }{
			{"get_me", `{}`}, {"list_markets", `{}`}, {"get_book", `{"coin":"BTC"}`}, {"get_open_orders", `{"address":"` + hlMaster + `"}`},
		} {
			f.posts = nil
			out, err := hlInvoke(t, m, c.id, call.tool, call.args)
			if err != nil {
				t.Fatalf("%s %s: %v", c.network, call.tool, err)
			}
			if out["network"] != c.network {
				t.Errorf("%s %s result names network %v", c.network, call.tool, out["network"])
			}
			for _, p := range f.posts {
				if !strings.HasPrefix(p, c.host+"/") {
					t.Errorf("%s %s went to %s", c.network, call.tool, p)
				}
			}
		}
	}
	// The same key again updates the account.
	again, err := hlConnect(t, m, hlTestKey)
	if err != nil || again.ID != test.ID || len(m.records) != 2 {
		t.Fatalf("reconnect: %v %v", err, len(m.records))
	}
	// Errors name the network too.
	if _, err := hlInvoke(t, m, test.ID, "get_book", `{"coin":"BTC","timezone":"Nowhere/Land"}`); err == nil || !strings.HasPrefix(err.Error(), "Hyperliquid Testnet: ") {
		t.Fatalf("error without a network: %v", err)
	}
	// Time zone: timestamps are stamped in the asked zone.
	out, err := hlInvoke(t, m, test.ID, "get_open_orders", `{"timezone":"Asia/Shanghai"}`)
	if err != nil || !strings.Contains(string(mustJSON(out)), `"timestamp_at":"2026-10-23T`) {
		t.Fatalf("%v %s", err, mustJSON(out))
	}
	if !strings.Contains(string(mustJSON(out)), "+08:00") {
		t.Fatalf("not in the asked time zone: %s", mustJSON(out))
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestHyperliquidTradingKey(t *testing.T) {
	key, _ := hlParseKey(hlTestKey)
	signer := hlAddress(key)
	f := &hlFake{t: t, signer: signer, network: "api.hyperliquid-testnet.xyz", role: "user"}
	m := testManager(t, f.transport)

	// A wallet's own key is refused, before anything is stored.
	if _, err := hlConnect(t, m, hlTestKey); err == nil || !strings.Contains(err.Error(), "API wallet") || strings.Contains(err.Error(), hlTestKey[2:]) {
		t.Fatalf("main key accepted: %v", err)
	}
	// Vaults and sub-accounts are refused in a few words.
	f.role = "vault"
	if _, err := hlConnect(t, m, hlTestKey); err == nil || !strings.Contains(err.Error(), "vault") {
		t.Fatalf("vault key: %v", err)
	}
	// A key nobody approved is refused, naming the page that makes one.
	f.role = "missing"
	_, err := hlConnect(t, m, hlTestKey)
	if err == nil || !strings.Contains(err.Error(), "app.hyperliquid.xyz/API") || strings.Contains(err.Error(), hlTestKey[2:]) {
		t.Fatalf("unapproved key: %v", err)
	}
	// Not a key at all.
	if _, err := hlConnect(t, m, "nope"); err == nil {
		t.Fatal("a non-key was accepted")
	}
	if len(m.records) != 0 {
		t.Fatal("a refused connect left an account")
	}
	f.approved = true
	acct, err := hlConnect(t, m, hlTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Signer != signer || !strings.Contains(acct.Identity, "trading key saved") {
		t.Fatalf("%+v", acct)
	}
	// Reads were allowed; changes stay off until allowed.
	for _, tool := range acct.Tools {
		if tool.Allowed != (tool.Group == "read") {
			t.Fatalf("%s allowed=%v", tool.Name, tool.Allowed)
		}
	}
	order := `{"coin":"BTC","side":"buy","size":"0.001","price":"50000"}`
	if _, err := hlInvoke(t, m, acct.ID, "place_order", order); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("changes were off: %v", err)
	}
	mustHandle(t, m, Request{Action: "permissions", ID: acct.ID, Group: "write", Allowed: true})

	f.exchange = `{"status":"ok","response":{"type":"order","data":{"statuses":[{"resting":{"oid":4242}}]}}}`
	f.posts = nil
	out, err := hlInvoke(t, m, acct.ID, "place_order", order)
	if err != nil {
		t.Fatal(err)
	}
	if out["network"] != "testnet" || out["ok"] != true || out["signer"] != signer || out["account_address"] != hlMaster {
		t.Fatalf("%s", mustJSON(out))
	}
	// The echo keeps the signed field order, so it is byte for byte what was sent.
	echoed, _ := m.Handle(context.Background(), Request{Action: "invoke", ID: acct.ID, Tool: "place_order", Arguments: json.RawMessage(order)})
	if !strings.Contains(string(echoed.Result), `"action_sent":{"type":"order","orders":[{"a":0,"b":true,"p":"50000","s":"0.001","r":false,"t":{"limit":{"tif":"Gtc"}}}],"grouping":"na"}`) {
		t.Fatalf("action_sent: %s", echoed.Result)
	}
	if !strings.Contains(string(mustJSON(out["exchange_response"])), `"oid":4242`) {
		t.Fatalf("exchange reply not returned verbatim: %s", mustJSON(out))
	}
	// The posted body recovers to the API wallet on testnet, and to nobody
	// else when read as mainnet: the signature carries the network.
	var posted struct {
		Action    json.RawMessage `json:"action"`
		Nonce     uint64          `json:"nonce"`
		Signature hlSignature     `json:"signature"`
	}
	var body string
	for _, p := range f.posts {
		if strings.Contains(p, "/exchange ") {
			body = strings.SplitN(p, " ", 2)[1]
		}
	}
	if body == "" {
		t.Fatalf("no exchange post: %v", f.posts)
	}
	if err := json.Unmarshal([]byte(body), &posted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "vaultAddress") || strings.Contains(body, hlTestKey[2:]) {
		t.Fatalf("body: %s", body)
	}
	recovers := func(mainnet bool) string {
		hash, _ := hlActionHash(hlPlaceAction(hlOrder{asset: 0, buy: true, price: "50000", size: "0.001", tif: "Gtc"}), posted.Nonce)
		r, _ := hex.DecodeString(strings.Repeat("0", 64-len(posted.Signature.R[2:])) + posted.Signature.R[2:])
		s, _ := hex.DecodeString(strings.Repeat("0", 64-len(posted.Signature.S[2:])) + posted.Signature.S[2:])
		compact := append([]byte{byte(posted.Signature.V)}, append(r, s...)...)
		pub, _, err := ecdsa.RecoverCompact(compact, hlL1Digest(hash, mainnet))
		if err != nil {
			return ""
		}
		return hlAddressOfPub(pub.SerializeUncompressed())
	}
	if recovers(false) != signer || recovers(true) == signer {
		t.Fatalf("signature is not bound to testnet: %s / %s", recovers(false), recovers(true))
	}

	// An exchange refusal reaches the caller verbatim, and nothing retries.
	f.exchange = `{"status":"err","response":"Order must have minimum value of $10."}`
	f.posts = nil
	out, err = hlInvoke(t, m, acct.ID, "place_order", order)
	if err != nil || out["ok"] != false || !strings.Contains(string(mustJSON(out)), "minimum value of $10") {
		t.Fatalf("%v %s", err, mustJSON(out))
	}
	exchangePosts := 0
	for _, p := range f.posts {
		if strings.Contains(p, "/exchange ") {
			exchangePosts++
		}
	}
	if exchangePosts != 1 {
		t.Fatalf("%d exchange posts for one call", exchangePosts)
	}
	// A price that would need rounding is refused before anything is signed.
	f.posts = nil
	if _, err := hlInvoke(t, m, acct.ID, "place_order", `{"coin":"BTC","side":"buy","size":"0.001","price":"50000.123"}`); err == nil || !strings.Contains(err.Error(), "Nothing was sent") {
		t.Fatalf("%v", err)
	}
	for _, p := range f.posts {
		if strings.Contains(p, "/exchange ") {
			t.Fatal("an invalid order reached the exchange")
		}
	}
	// Unknown market, builder-deployed market.
	for _, coin := range []string{"NOPE", "xyz:AAPL"} {
		if _, err := hlInvoke(t, m, acct.ID, "place_order", `{"coin":"`+coin+`","side":"buy","size":"1","price":"1"}`); err == nil {
			t.Fatalf("%s accepted", coin)
		}
	}
	// Spot is addressed as 10000 + index.
	f.exchange = `{"status":"ok","response":{"type":"order","data":{"statuses":[{"resting":{"oid":1}}]}}}`
	out, err = hlInvoke(t, m, acct.ID, "place_order", `{"coin":"PURR/USDC","side":"buy","size":"5","price":"0.0001234","tif":"Alo"}`)
	if err != nil || !strings.Contains(string(mustJSON(out["action_sent"])), `"a":10000`) {
		t.Fatalf("%v %s", err, mustJSON(out))
	}
	// The key never shows in results, history or the account.
	raw, _ := json.Marshal(m.records)
	if strings.Contains(string(raw), hlTestKey[2:]) {
		t.Fatal("the catalog holds the key")
	}

	// Removing the key leaves a read-only account.
	removed := mustHandle(t, m, Request{Action: "remove_key", ID: acct.ID}).Account
	if removed.Signer != "" || strings.Contains(removed.Identity, "trading key saved") {
		t.Fatalf("%+v", removed)
	}
	for _, tool := range removed.Tools {
		if tool.Group != "read" {
			t.Fatalf("%s survived key removal", tool.Name)
		}
	}
	if _, ok, _ := m.vault.Get("integration:" + acct.ID); ok {
		t.Fatal("key still in the vault")
	}
}

func TestHyperliquidKeyIsPerNetwork(t *testing.T) {
	key, _ := hlParseKey(hlTestKey)
	f := &hlFake{t: t, signer: hlAddress(key), approved: true, network: "api.hyperliquid-testnet.xyz"}
	m := testManager(t, f.transport)
	reader := hlReader(t, m)
	testnet, err := hlConnect(t, m, hlTestKey)
	if err != nil {
		t.Fatal(err)
	}
	// The mainnet reader has no key: the testnet key was not copied to it.
	if testnet.ID == reader.ID {
		t.Fatal("a testnet key upgraded the mainnet reader")
	}
	if v, ok, _ := m.vault.Get("integration:" + reader.ID); ok || v != "" {
		t.Fatal("the testnet key was copied to the mainnet account")
	}
	// Asking for mainnet only does not find a testnet key.
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "hyperliquid", Network: "mainnet", Credential: hlTestKey}}); err == nil {
		t.Fatal("a testnet key was accepted for mainnet")
	}
}

func TestHyperliquidCatalogAndToolsAreReviewed(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range hyperliquidTools() {
		if seen[tool.Name] {
			t.Fatalf("duplicate %s", tool.Name)
		}
		seen[tool.Name] = true
		for _, banned := range []string{"withdraw", "transfer", "send", "vault", "deposit"} {
			if !tool.Read && strings.Contains(strings.ToLower(tool.Name), banned) {
				t.Fatalf("%s moves funds", tool.Name)
			}
		}
	}
	for _, name := range []string{"place_order", "modify_order", "cancel_order", "set_leverage", "schedule_cancel"} {
		for _, tool := range hyperliquidTools() {
			if tool.Name == name && tool.Read {
				t.Fatalf("%s is marked read", name)
			}
		}
	}
}
