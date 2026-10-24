package connections

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/modelprofiles"
)

// Opt-in: MEWLA_HL_LIVE=1 go test -run TestHyperliquidLive -v ./connections
// Reads real mainnet data with no connection configured. The only write is a signed order from a
// throwaway key that Hyperliquid has not approved, which the exchange refuses
// while naming the address it recovered from the signature. Nothing is traded.
func TestHyperliquidLive(t *testing.T) {
	if os.Getenv("MEWLA_HL_LIVE") != "1" {
		t.Skip("set MEWLA_HL_LIVE=1 to talk to Hyperliquid")
	}
	m, err := newManager(filepath.Join(t.TempDir(), "catalog.json"), modelprofiles.NewMemoryCredentialStore(), newHTTPClient())
	if err != nil {
		t.Fatal(err)
	}
	const hlp = "0xdfc24b077bc1425ad1dea75bcb6f8158e10df303" // Hyperliquidity Provider (HLP), a public vault
	// Reads need no connection: the keyless mainnet reader names each address.
	if err := m.ensureHyperliquidReader(); err != nil {
		t.Fatal(err)
	}
	for _, network := range []string{"mainnet"} {
		id := ""
		for _, r := range m.records {
			id = r.Account.ID
		}
		for _, call := range []struct{ tool, args string }{
			{"get_me", `{}`},
			{"list_markets", `{"search":"BTC","limit":5}`},
			{"get_mids", `{"coins":["BTC","ETH"]}`},
			{"get_book", `{"coin":"BTC","depth":2,"timezone":"Asia/Shanghai"}`},
			{"get_candles", `{"coin":"BTC","interval":"1h","limit":3,"timezone":"Asia/Shanghai"}`},
			{"get_funding_history", `{"coin":"BTC","limit":3,"timezone":"Asia/Shanghai"}`},
			{"get_account", `{"address":"` + hlp + `"}`},
			{"get_open_orders", `{"address":"` + hlp + `"}`},
			{"get_fills", `{"address":"` + hlp + `","limit":2}`},
			{"get_portfolio", `{"address":"` + hlp + `","timeframe":"week"}`},
			{"get_vaults", `{"address":"` + hlp + `"}`},
		} {
			res, err := m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: call.tool, Arguments: json.RawMessage(call.args)})
			if err != nil {
				t.Errorf("%s %s: %v", network, call.tool, err)
				continue
			}
			text := string(res.Result)
			if len(text) > 420 {
				text = text[:420] + "…"
			}
			t.Logf("%s %s (%d bytes): %s", network, call.tool, len(res.Result), text)
			if !strings.Contains(string(res.Result), `"network":"`+network+`"`) {
				t.Errorf("%s %s does not name its network", network, call.tool)
			}
		}
	}

	// Signed-action proof on testnet with a fresh throwaway key.
	key, _ := hlParseKey("0x" + strings.Repeat("ab", 32))
	rec := &record{Account: Account{ID: "x", Integration: "hyperliquid", Network: "testnet", Address: hlp, Endpoint: hlTestnetAPI, Enabled: true}}
	mk, err := m.hlResolve(context.Background(), rec, "BTC")
	if err != nil {
		t.Fatal(err)
	}
	px, _ := hlCheckPrice("10000", mk)
	out, err := m.hlSend(context.Background(), rec, key, hlPlaceAction(hlOrder{asset: mk.asset, buy: true, price: px, size: "0.01", tif: "Gtc"}))
	if err != nil {
		t.Fatal(err)
	}
	reply := string(mustJSON(out["exchange_response"]))
	t.Logf("testnet refusal: %s; signer %s", reply, hlAddress(key))
	if !strings.Contains(strings.ToLower(reply), hlAddress(key)) {
		t.Errorf("the exchange did not recover the signer address we derived")
	}
}
