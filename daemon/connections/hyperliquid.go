package connections

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/google/uuid"
)

// Hyperliquid reads markets and accounts through the public info endpoint and
// trades through the exchange endpoint with an API wallet. Sources:
// https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api and the
// official Python SDK (hyperliquid-dex/hyperliquid-python-sdk).
//
// An account is one address on one network. The network is chosen when the
// account is connected and shown wherever the account is; the other network is
// another account. An API wallet is approved per network (approveAgent names
// "Mainnet" or "Testnet") and signatures carry the network (phantom agent
// source "a" or "b"), so a key is stored with the account it was approved for
// and never reused for the other network.
const (
	hlMainnetAPI = "https://api.hyperliquid.xyz"
	hlTestnetAPI = "https://api.hyperliquid-testnet.xyz"

	hlMaxRows = 200
)

func hlEndpoint(network string) string {
	if network == "testnet" {
		return hlTestnetAPI
	}
	return hlMainnetAPI
}

func hlNetworkLabel(network string) string {
	if network == "testnet" {
		return "Testnet"
	}
	return "Mainnet"
}

func hlShort(address string) string {
	if len(address) < 12 {
		return address
	}
	return address[:6] + "…" + address[len(address)-4:]
}

// hlAccountName names an account by network and address, so the same address
// on both networks is two clearly different accounts.
func hlAccountName(network, address string) string {
	return "Hyperliquid " + hlNetworkLabel(network) + " · " + hlShort(address)
}

func hlIdentity(network, address string, trading bool) string {
	identity := hlNetworkLabel(network) + " · " + hlShort(address)
	if trading {
		identity += " · trading key saved"
	} else {
		identity += " · read only"
	}
	return identity
}

func hlTool(name, desc string, read bool, props map[string]any, required ...string) Tool {
	schema, _ := json.Marshal(object(props, required...))
	return Tool{Name: name, Description: desc, InputSchema: schema, Read: read}
}

func hlInt(min, max int) map[string]any {
	return map[string]any{"type": "integer", "minimum": min, "maximum": max}
}
func hlEnum(values ...string) map[string]any { return map[string]any{"type": "string", "enum": values} }
func hlText(desc string) map[string]any {
	m := str()
	m["description"] = desc
	return m
}

var hlWhen = hlText("RFC 3339 time, for example 2026-10-23T09:00:00+08:00")
var hlZone = hlText("IANA time zone for the times in the result, for example Asia/Shanghai; this computer's time zone if omitted")
var hlUser = hlText("0x address to read instead of this account's own; account data on Hyperliquid is public")

func hlOrderProps() map[string]any {
	return map[string]any{
		"coin":            hlText("Perp name such as BTC, or spot pair such as PURR/USDC or @107"),
		"side":            hlEnum("buy", "sell"),
		"size":            hlText("Size in base-coin units as a decimal string, with at most the market's size decimals"),
		"price":           hlText("Limit price in USDC as a decimal string: at most 5 significant figures, and no more than 6 (perp) or 8 (spot) minus the size decimals after the point"),
		"tif":             hlEnum("Gtc", "Alo", "Ioc"),
		"reduce_only":     map[string]any{"type": "boolean"},
		"client_order_id": hlText("Optional 0x followed by 32 hex digits"),
	}
}

func hyperliquidTools() []Tool {
	address := map[string]any{"address": hlUser, "timezone": hlZone}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{"address": hlUser, "timezone": hlZone}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	modify := hlOrderProps()
	modify["oid"] = map[string]any{"type": "integer", "minimum": 1, "description": "Order id to replace"}
	modify["order_to_replace"] = hlText("Client order id (0x + 32 hex digits) of the order to replace, instead of oid")
	return []Tool{
		hlTool("get_me", "Show this Hyperliquid account: network (mainnet or testnet), address, and whether a trading key is saved. Makes no request.", true, map[string]any{}),
		hlTool("list_markets", "List perpetual and spot markets with their asset index, size decimals and maximum leverage (one page). Spot names look like PURR/USDC or @107.", true, map[string]any{
			"kind": hlEnum("all", "perp", "spot"), "search": hlText("Part of a market name"), "limit": hlInt(1, hlMaxRows)}),
		hlTool("get_mids", "Read mid prices in USDC for the named coins, or the first 100 coins by name.", true, map[string]any{
			"coins": map[string]any{"type": "array", "minItems": 1, "maxItems": 50, "items": str()}}),
		hlTool("get_book", "Read the level 2 order book of a coin: bids and asks, price in USDC and size in coin units.", true, map[string]any{
			"coin": str(), "depth": hlInt(1, 20), "timezone": hlZone}, "coin"),
		hlTool("get_candles", "Read OHLCV candles of a coin, oldest first. Prices in USDC, volume in coin units. Defaults to the latest candles.", true, map[string]any{
			"coin": str(), "interval": hlEnum("1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "8h", "12h", "1d", "3d", "1w", "1M"),
			"start": hlWhen, "end": hlWhen, "limit": hlInt(1, hlMaxRows), "timezone": hlZone}, "coin", "interval"),
		hlTool("get_funding_history", "Read a perp's hourly funding rate history, oldest first (rates are per hour, as fractions). Defaults to the last 7 days.", true, map[string]any{
			"coin": str(), "start": hlWhen, "end": hlWhen, "limit": hlInt(1, hlMaxRows), "timezone": hlZone}, "coin"),
		hlTool("get_account", "Read an account's perp positions and margin summary (USDC) and its spot balances.", true, address),
		hlTool("get_open_orders", "List an account's open orders with ids (one page).", true, with(nil)),
		hlTool("get_fills", "List an account's recent fills, newest first (one page).", true, with(map[string]any{"start": hlWhen, "end": hlWhen, "limit": hlInt(1, 100)})),
		hlTool("get_funding_payments", "List funding payments an account paid or received, oldest first. Defaults to the last 7 days.", true, with(map[string]any{"start": hlWhen, "end": hlWhen, "limit": hlInt(1, hlMaxRows)})),
		hlTool("get_portfolio", "Read an account's account-value and PnL history in USDC for one timeframe (the latest 48 points).", true, with(map[string]any{
			"timeframe": hlEnum("day", "week", "month", "allTime", "perpDay", "perpWeek", "perpMonth", "perpAllTime")})),
		hlTool("get_vaults", "List the vaults an account holds a deposit in, with equity in USDC.", true, with(nil)),
		hlTool("get_order_status", "Read one order's status by order id or client order id, to confirm what happened to it.", true, with(map[string]any{
			"oid": map[string]any{"type": "integer", "minimum": 1}, "client_order_id": hlText("0x followed by 32 hex digits")})),
		hlTool("place_order", "Place one limit order. The result states the network and exactly what was signed and sent, and Hyperliquid's own reply with the order id. Price and size are never rounded for you; a value that is not valid is refused. Never retried. Only on the user's explicit instruction.", false, hlOrderProps(), "coin", "side", "size", "price"),
		hlTool("modify_order", "Replace an open order (by oid, or by the client order id in order_to_replace) with a new one. Everything in the new order is given in full. The result states the network and what was sent. Never retried. Only on the user's explicit instruction.", false, modify, "coin", "side", "size", "price"),
		hlTool("cancel_order", "Cancel one open order by order id, or by client order id. The result states the network and Hyperliquid's reply. Never retried.", false, map[string]any{
			"coin": str(), "oid": map[string]any{"type": "integer", "minimum": 1}, "client_order_id": hlText("0x followed by 32 hex digits")}, "coin"),
		hlTool("set_leverage", "Set the leverage of a perp, cross or isolated. Spot has no leverage.", false, map[string]any{
			"coin": str(), "leverage": hlInt(1, 100), "mode": hlEnum("cross", "isolated")}, "coin", "leverage"),
		hlTool("schedule_cancel", "Arm a dead man's switch that cancels every open order after a delay, or clear it. Hyperliquid allows 10 triggers a day.", false, map[string]any{
			"after_seconds": hlInt(5, 86400), "clear": map[string]any{"type": "boolean"}}),
	}
}

// ---- wire actions -------------------------------------------------------

func (m hlMap) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range m {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.K)
		v, err := json.Marshal(kv.V)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type hlOrder struct {
	asset      uint64
	buy        bool
	price      string
	size       string
	reduceOnly bool
	tif        string
	cloid      string
}

// order keys follow the SDK's order_request_to_order_wire: a b p s r t [c].
func (o hlOrder) wire() hlMap {
	w := hlMap{{"a", o.asset}, {"b", o.buy}, {"p", o.price}, {"s", o.size}, {"r", o.reduceOnly},
		{"t", hlMap{{"limit", hlMap{{"tif", o.tif}}}}}}
	if o.cloid != "" {
		w = append(w, hlKV{"c", o.cloid})
	}
	return w
}

func hlPlaceAction(o hlOrder) hlMap {
	return hlMap{{"type", "order"}, {"orders", []any{o.wire()}}, {"grouping", "na"}}
}
func hlModifyAction(oid any, o hlOrder) hlMap {
	return hlMap{{"type", "batchModify"}, {"modifies", []any{hlMap{{"oid", oid}, {"order", o.wire()}}}}}
}
func hlCancelAction(asset, oid uint64) hlMap {
	return hlMap{{"type", "cancel"}, {"cancels", []any{hlMap{{"a", asset}, {"o", oid}}}}}
}
func hlCancelCloidAction(asset uint64, cloid string) hlMap {
	return hlMap{{"type", "cancelByCloid"}, {"cancels", []any{hlMap{{"asset", asset}, {"cloid", cloid}}}}}
}
func hlLeverageAction(asset uint64, cross bool, leverage uint64) hlMap {
	return hlMap{{"type", "updateLeverage"}, {"asset", asset}, {"isCross", cross}, {"leverage", leverage}}
}
func hlScheduleCancelAction(at uint64) hlMap {
	if at == 0 {
		return hlMap{{"type", "scheduleCancel"}}
	}
	return hlMap{{"type", "scheduleCancel"}, {"time", at}}
}

// Nonces are the time in milliseconds, raised when needed to stay above the
// signer's last one (Hyperliquid keeps the 100 highest per signer).
var hlNonces = struct {
	sync.Mutex
	last map[string]uint64
}{last: map[string]uint64{}}

func hlNextNonce(signer string) uint64 {
	hlNonces.Lock()
	defer hlNonces.Unlock()
	n := uint64(time.Now().UnixMilli())
	if last := hlNonces.last[signer]; n <= last {
		n = last + 1
	}
	hlNonces.last[signer] = n
	return n
}

// ---- HTTP ---------------------------------------------------------------

type hlHTTPError struct {
	status int
	body   string
}

func (e hlHTTPError) Error() string {
	return fmt.Sprintf("Hyperliquid returned HTTP %d: %s", e.status, e.body)
}

// hlPost sends one request. Hyperliquid needs no bearer token, so none is
// attached: the API wallet key signs locally and never leaves the daemon.
func (m *Manager) hlPost(ctx context.Context, r *record, path string, body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	u, err := trustedEndpointURL(r.Account.Endpoint, nil)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(u.String(), "/")+path, bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mewla-Integrations")
	resp, err := m.clientFor(r).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, safeError(ctx.Err())
		}
		return nil, errNetwork
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResultBytes+1))
	if err != nil {
		return nil, errors.New("Hyperliquid's response could not be read")
	}
	if len(data) > MaxResultBytes {
		return nil, errors.New("Hyperliquid's response exceeds 1 MiB; ask for less")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text := strings.TrimSpace(string(data))
		if len(text) > 2000 {
			text = text[:2000]
		}
		return nil, hlHTTPError{resp.StatusCode, text}
	}
	return data, nil
}

func (m *Manager) hlInfo(ctx context.Context, r *record, req map[string]any) (json.RawMessage, error) {
	data, err := m.hlPost(ctx, r, "/info", req)
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, errors.New("Hyperliquid returned invalid JSON")
	}
	return data, nil
}

// ---- discovery ----------------------------------------------------------

func (m *Manager) discoverHyperliquid(ctx context.Context, r *record, secret string) ([]Tool, error) {
	a := &r.Account
	if a.Network != "mainnet" && a.Network != "testnet" {
		return nil, errors.New("choose Mainnet or Testnet")
	}
	if !hlValidAddress(a.Address) {
		return nil, errors.New("enter the account address: 0x followed by 40 hex digits")
	}
	a.Endpoint = hlEndpoint(a.Network)
	if _, err := m.hlInfo(ctx, r, map[string]any{"type": "clearinghouseState", "user": a.Address}); err != nil {
		return nil, errors.New("could not reach Hyperliquid " + hlNetworkLabel(a.Network) + ": " + err.Error())
	}
	a.Signer = ""
	if secret != "" {
		key, err := hlParseKey(secret)
		if err != nil {
			return nil, err
		}
		signer := hlAddress(key)
		if hlSameAddress(signer, a.Address) {
			return nil, errors.New("this is the key of the account itself. Paste an API wallet key instead: create one at app.hyperliquid.xyz/API, which can trade but cannot withdraw")
		}
		raw, err := m.hlInfo(ctx, r, map[string]any{"type": "extraAgents", "user": a.Address})
		if err != nil {
			return nil, err
		}
		var agents []struct {
			Address    string `json:"address"`
			ValidUntil int64  `json:"validUntil"`
		}
		if json.Unmarshal(raw, &agents) != nil {
			return nil, errors.New("Hyperliquid did not list the account's API wallets")
		}
		approved := false
		for _, agent := range agents {
			if hlSameAddress(agent.Address, signer) && (agent.ValidUntil == 0 || agent.ValidUntil > time.Now().UnixMilli()) {
				approved = true
			}
		}
		if !approved {
			site := "app.hyperliquid.xyz/API"
			if a.Network == "testnet" {
				site = "app.hyperliquid-testnet.xyz/API"
			}
			return nil, fmt.Errorf("this key (%s) is not an approved API wallet for %s on %s. API wallets are approved per network: approve it at %s", hlShort(signer), hlShort(a.Address), hlNetworkLabel(a.Network), site)
		}
		a.Signer = signer
	}
	a.Identity = hlIdentity(a.Network, a.Address, a.Signer != "")
	var tools []Tool
	for _, t := range hyperliquidTools() {
		if t.Read || secret != "" {
			tools = append(tools, t)
		}
	}
	return tools, nil
}

// ---- invoke -------------------------------------------------------------

func (m *Manager) invokeHyperliquid(ctx context.Context, r *record, secret string, t Tool, args map[string]any) ([]byte, error) {
	var known *Tool
	for _, c := range hyperliquidTools() {
		if c.Name == t.Name {
			v := c
			known = &v
		}
	}
	if known == nil || fingerprint(*known) != fingerprint(t) {
		return nil, errors.New("tool definition changed; refresh this account, then confirm the update in Extensions")
	}
	if r.Account.Network != "mainnet" && r.Account.Network != "testnet" {
		return nil, errors.New("this account has no network; connect it again")
	}
	// Every outcome names the network, errors included: a failed order on
	// mainnet must never read like a testnet one.
	label := "Hyperliquid " + hlNetworkLabel(r.Account.Network)
	fail := func(err error) ([]byte, error) { return nil, fmt.Errorf("%s: %w", label, err) }
	zone, err := hlLocation(args)
	if err != nil {
		return fail(err)
	}
	var out map[string]any
	if known.Read {
		out, err = m.hlRead(ctx, r, t.Name, args, zone)
	} else {
		if secret == "" || r.Account.Signer == "" {
			return fail(errors.New("this account has no trading key; connect it again with an API wallet key to place orders"))
		}
		key, kerr := hlParseKey(secret)
		if kerr != nil {
			return fail(errors.New("the saved trading key is unreadable; connect this account again"))
		}
		out, err = m.hlWrite(ctx, r, key, t.Name, args)
	}
	if err != nil {
		return fail(err)
	}
	out["network"] = r.Account.Network
	return json.Marshal(out)
}

func hlLocation(args map[string]any) (*time.Location, error) {
	name, _ := args["timezone"].(string)
	if name == "" {
		// The daemon runs on the user's computer, so its zone is theirs.
		return time.Local, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("unknown time zone %q; use an IANA name such as Asia/Shanghai", name)
	}
	return loc, nil
}

func hlStr(args map[string]any, key string) string { s, _ := args[key].(string); return s }
func hlNum(args map[string]any, key string, def int) int {
	if f, ok := args[key].(float64); ok {
		return int(f)
	}
	return def
}

func hlTime(args map[string]any, key string, def time.Time) (time.Time, error) {
	s := hlStr(args, key)
	if s == "" {
		return def, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC 3339 time such as 2026-10-23T09:00:00+08:00", key)
	}
	return t, nil
}

// hlAddressArg is the address a read concerns: the one named, else the account's.
func hlAddressArg(r *record, args map[string]any) (string, error) {
	if a := hlStr(args, "address"); a != "" {
		if !hlValidAddress(a) {
			return "", errors.New("address must be 0x followed by 40 hex digits")
		}
		return strings.ToLower(a), nil
	}
	return r.Account.Address, nil
}

var hlTimeKeys = map[string]bool{"time": true, "timestamp": true, "t": true, "T": true, "lockedUntilTimestamp": true, "validUntil": true, "statusTimestamp": true}

// hlStamp adds a readable `<key>_at` beside each millisecond time in a result,
// in the caller's time zone. The original number stays.
func hlStamp(v any, loc *time.Location) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if n, ok := e.(json.Number); ok && hlTimeKeys[k] {
				if ms, err := n.Int64(); err == nil && ms > 1e12 && ms < 4e12 {
					x[k+"_at"] = time.UnixMilli(ms).In(loc).Format(time.RFC3339)
				}
				continue
			}
			x[k] = hlStamp(e, loc)
		}
	case []any:
		for i, e := range x {
			x[i] = hlStamp(e, loc)
		}
	}
	return v
}

func hlDecode(raw json.RawMessage, loc *time.Location) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, errors.New("Hyperliquid returned unreadable data")
	}
	return hlStamp(v, loc), nil
}

func hlTail(v any, n int) (any, bool) {
	if list, ok := v.([]any); ok && len(list) > n {
		return list[len(list)-n:], true
	}
	return v, false
}
func hlHead(v any, n int) (any, bool) {
	if list, ok := v.([]any); ok && len(list) > n {
		return list[:n], true
	}
	return v, false
}

var hlIntervals = map[string]time.Duration{"1m": time.Minute, "3m": 3 * time.Minute, "5m": 5 * time.Minute, "15m": 15 * time.Minute, "30m": 30 * time.Minute,
	"1h": time.Hour, "2h": 2 * time.Hour, "4h": 4 * time.Hour, "8h": 8 * time.Hour, "12h": 12 * time.Hour, "1d": 24 * time.Hour, "3d": 72 * time.Hour, "1w": 168 * time.Hour, "1M": 30 * 24 * time.Hour}

func (m *Manager) hlRead(ctx context.Context, r *record, name string, args map[string]any, loc *time.Location) (map[string]any, error) {
	out := map[string]any{}
	info := func(req map[string]any) (any, error) {
		raw, err := m.hlInfo(ctx, r, req)
		if err != nil {
			return nil, err
		}
		return hlDecode(raw, loc)
	}
	user := ""
	if strings.HasPrefix(name, "get_") && name != "get_me" && name != "get_mids" && name != "get_book" && name != "get_candles" && name != "get_funding_history" {
		var err error
		if user, err = hlAddressArg(r, args); err != nil {
			return nil, err
		}
		out["address"] = user
	}
	now := time.Now()
	switch name {
	case "get_me":
		out["address"] = r.Account.Address
		out["trading_enabled"] = r.Account.Signer != ""
		if r.Account.Signer != "" {
			out["api_wallet"] = r.Account.Signer
		}
		out["api"] = r.Account.Endpoint
	case "list_markets":
		kind, search, limit := hlStr(args, "kind"), strings.ToLower(hlStr(args, "search")), hlNum(args, "limit", 50)
		markets, err := m.hlMarkets(ctx, r)
		if err != nil {
			return nil, err
		}
		rows := []any{}
		for _, mk := range markets {
			if (kind == "perp" && mk.spot) || (kind == "spot" && !mk.spot) || !strings.Contains(strings.ToLower(mk.name), search) {
				continue
			}
			row := map[string]any{"name": mk.name, "asset": mk.asset, "kind": map[bool]string{false: "perp", true: "spot"}[mk.spot], "size_decimals": mk.szDecimals}
			if !mk.spot {
				row["max_leverage"] = mk.maxLeverage
			}
			rows = append(rows, row)
		}
		out["total"] = len(rows)
		page, truncated := hlHead(rows, limit)
		out["markets"], out["truncated"] = page, truncated
	case "get_mids":
		v, err := info(map[string]any{"type": "allMids"})
		if err != nil {
			return nil, err
		}
		all, _ := v.(map[string]any)
		mids := map[string]any{}
		if coins, ok := args["coins"].([]any); ok {
			for _, c := range coins {
				s, _ := c.(string)
				if p, found := all[s]; found {
					mids[s] = p
				} else {
					mids[s] = nil
				}
			}
		} else {
			keys := make([]string, 0, len(all))
			for k := range all {
				if !strings.HasPrefix(k, "#") {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			if len(keys) > 100 {
				out["truncated"] = true
				keys = keys[:100]
			}
			for _, k := range keys {
				mids[k] = all[k]
			}
		}
		out["mids_usdc"] = mids
	case "get_book":
		coin, err := hlBookCoin(args)
		if err != nil {
			return nil, err
		}
		v, err := info(map[string]any{"type": "l2Book", "coin": coin})
		if err != nil {
			return nil, err
		}
		book, _ := v.(map[string]any)
		depth := hlNum(args, "depth", 10)
		if levels, ok := book["levels"].([]any); ok {
			for i, side := range levels {
				levels[i], _ = hlHead(side, depth)
			}
		}
		out["coin"], out["book"] = coin, book
	case "get_candles":
		coin, err := hlBookCoin(args)
		if err != nil {
			return nil, err
		}
		step := hlIntervals[hlStr(args, "interval")]
		limit := hlNum(args, "limit", 100)
		end, err := hlTime(args, "end", now)
		if err != nil {
			return nil, err
		}
		start, err := hlTime(args, "start", end.Add(-step*time.Duration(limit)))
		if err != nil {
			return nil, err
		}
		v, err := info(map[string]any{"type": "candleSnapshot", "req": map[string]any{"coin": coin, "interval": hlStr(args, "interval"), "startTime": start.UnixMilli(), "endTime": end.UnixMilli()}})
		if err != nil {
			return nil, err
		}
		page, truncated := hlTail(v, limit)
		out["coin"], out["interval"], out["candles"], out["truncated"] = coin, hlStr(args, "interval"), page, truncated
	case "get_funding_history", "get_funding_payments":
		end, err := hlTime(args, "end", now)
		if err != nil {
			return nil, err
		}
		start, err := hlTime(args, "start", end.Add(-7*24*time.Hour))
		if err != nil {
			return nil, err
		}
		req := map[string]any{"type": "fundingHistory", "startTime": start.UnixMilli(), "endTime": end.UnixMilli()}
		key := "history"
		if name == "get_funding_history" {
			if req["coin"], err = hlBookCoin(args); err != nil {
				return nil, err
			}
			out["coin"] = req["coin"]
		} else {
			req["type"], req["user"], key = "userFunding", user, "payments"
		}
		v, err := info(req)
		if err != nil {
			return nil, err
		}
		page, truncated := hlTail(v, hlNum(args, "limit", 50))
		out[key], out["truncated"] = page, truncated
	case "get_account":
		perp, err := info(map[string]any{"type": "clearinghouseState", "user": user})
		if err != nil {
			return nil, err
		}
		spot, err := info(map[string]any{"type": "spotClearinghouseState", "user": user})
		if err != nil {
			return nil, err
		}
		out["perp"], out["spot"] = perp, spot
	case "get_open_orders":
		v, err := info(map[string]any{"type": "frontendOpenOrders", "user": user})
		if err != nil {
			return nil, err
		}
		page, truncated := hlHead(v, hlMaxRows)
		out["orders"], out["truncated"] = page, truncated
	case "get_fills":
		req := map[string]any{"type": "userFills", "user": user}
		if hlStr(args, "start") != "" || hlStr(args, "end") != "" {
			start, err := hlTime(args, "start", time.UnixMilli(0))
			if err != nil {
				return nil, err
			}
			end, err := hlTime(args, "end", now)
			if err != nil {
				return nil, err
			}
			req = map[string]any{"type": "userFillsByTime", "user": user, "startTime": start.UnixMilli(), "endTime": end.UnixMilli()}
		}
		v, err := info(req)
		if err != nil {
			return nil, err
		}
		// Hyperliquid lists fills oldest first; show the newest.
		if list, ok := v.([]any); ok {
			for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
				list[i], list[j] = list[j], list[i]
			}
		}
		page, truncated := hlHead(v, hlNum(args, "limit", 20))
		out["fills"], out["truncated"] = page, truncated
	case "get_portfolio":
		v, err := info(map[string]any{"type": "portfolio", "user": user})
		if err != nil {
			return nil, err
		}
		frame := hlStr(args, "timeframe")
		if frame == "" {
			frame = "month"
		}
		out["timeframe"] = frame
		if pairs, ok := v.([]any); ok {
			for _, p := range pairs {
				if pair, ok := p.([]any); ok && len(pair) == 2 && pair[0] == frame {
					series, _ := pair[1].(map[string]any)
					for _, k := range []string{"accountValueHistory", "pnlHistory"} {
						if h, ok := series[k].([]any); ok {
							for i, point := range h {
								// [ms, value]: stamp the time beside it.
								if pt, ok := point.([]any); ok && len(pt) == 2 {
									if n, ok := pt[0].(json.Number); ok {
										ms, _ := n.Int64()
										h[i] = map[string]any{"time": ms, "time_at": time.UnixMilli(ms).In(loc).Format(time.RFC3339), "value_usdc": pt[1]}
									}
								}
							}
							series[k], _ = hlTail(h, 48)
						}
					}
					out["volume_usdc"] = series["vlm"]
					out["account_value_history"], out["pnl_history"] = series["accountValueHistory"], series["pnlHistory"]
				}
			}
		}
	case "get_vaults":
		v, err := info(map[string]any{"type": "userVaultEquities", "user": user})
		if err != nil {
			return nil, err
		}
		out["vaults"] = v
	case "get_order_status":
		req := map[string]any{"type": "orderStatus", "user": user}
		switch {
		case args["oid"] != nil:
			req["oid"] = args["oid"]
		case hlStr(args, "client_order_id") != "":
			if !hlValidCloid(hlStr(args, "client_order_id")) {
				return nil, errors.New("client_order_id must be 0x followed by 32 hex digits")
			}
			req["oid"] = strings.ToLower(hlStr(args, "client_order_id"))
		default:
			return nil, errors.New("give oid or client_order_id")
		}
		v, err := info(req)
		if err != nil {
			return nil, err
		}
		out["order"] = v
	default:
		return nil, errors.New("tool not found")
	}
	return out, nil
}

func hlBookCoin(args map[string]any) (string, error) {
	coin := strings.TrimSpace(hlStr(args, "coin"))
	if coin == "" || strings.Contains(coin, ":") {
		return "", errors.New("name a perp such as BTC, or a spot pair such as @107; builder-deployed markets are not supported")
	}
	return coin, nil
}

func hlValidCloid(s string) bool {
	if len(s) != 34 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
}

// ---- markets and rounding -------------------------------------------------

type hlMarket struct {
	name        string
	asset       uint64
	szDecimals  int
	maxLeverage int
	spot        bool
}

// hlMarkets lists perps (asset = index in meta.universe) and spot pairs
// (asset = 10000 + index in spotMeta.universe), as the asset-id doc defines.
func (m *Manager) hlMarkets(ctx context.Context, r *record) ([]hlMarket, error) {
	rawMeta, err := m.hlInfo(ctx, r, map[string]any{"type": "meta"})
	if err != nil {
		return nil, err
	}
	var meta struct {
		Universe []struct {
			Name        string `json:"name"`
			SzDecimals  int    `json:"szDecimals"`
			MaxLeverage int    `json:"maxLeverage"`
			Delisted    bool   `json:"isDelisted"`
		} `json:"universe"`
	}
	if json.Unmarshal(rawMeta, &meta) != nil {
		return nil, errors.New("Hyperliquid returned unreadable market data")
	}
	rawSpot, err := m.hlInfo(ctx, r, map[string]any{"type": "spotMeta"})
	if err != nil {
		return nil, err
	}
	var spot struct {
		Tokens []struct {
			Name       string `json:"name"`
			SzDecimals int    `json:"szDecimals"`
		} `json:"tokens"`
		Universe []struct {
			Name   string `json:"name"`
			Index  int    `json:"index"`
			Tokens [2]int `json:"tokens"`
		} `json:"universe"`
	}
	if json.Unmarshal(rawSpot, &spot) != nil {
		return nil, errors.New("Hyperliquid returned unreadable market data")
	}
	var out []hlMarket
	for i, u := range meta.Universe {
		if !u.Delisted {
			out = append(out, hlMarket{name: u.Name, asset: uint64(i), szDecimals: u.SzDecimals, maxLeverage: u.MaxLeverage})
		}
	}
	for _, u := range spot.Universe {
		mk := hlMarket{name: u.Name, asset: uint64(10000 + u.Index), spot: true}
		if b := u.Tokens[0]; b >= 0 && b < len(spot.Tokens) {
			mk.szDecimals = spot.Tokens[b].SzDecimals
		}
		out = append(out, mk)
		// "@107" is also known by its pair name, HYPE/USDC.
		if q := u.Tokens[1]; strings.HasPrefix(u.Name, "@") && u.Tokens[0] < len(spot.Tokens) && q < len(spot.Tokens) {
			alias := mk
			alias.name = spot.Tokens[u.Tokens[0]].Name + "/" + spot.Tokens[q].Name
			out = append(out, alias)
		}
	}
	return out, nil
}

func (m *Manager) hlResolve(ctx context.Context, r *record, coin string) (hlMarket, error) {
	coin = strings.TrimSpace(coin)
	if strings.Contains(coin, ":") {
		return hlMarket{}, errors.New("builder-deployed (HIP-3) markets are not supported; use a main perp or a spot pair")
	}
	markets, err := m.hlMarkets(ctx, r)
	if err != nil {
		return hlMarket{}, err
	}
	for _, mk := range markets {
		if mk.name == coin {
			return mk, nil
		}
	}
	var found []hlMarket
	for _, mk := range markets {
		if strings.EqualFold(mk.name, coin) {
			found = append(found, mk)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	return hlMarket{}, fmt.Errorf("%q is not a market on Hyperliquid %s; list_markets shows the names", coin, hlNetworkLabel(r.Account.Network))
}

// hlCheckPrice applies the tick rule: at most 5 significant figures and at
// most (6 for perps, 8 for spot) minus szDecimals decimals; a whole number is
// always valid. It refuses a price instead of rounding it.
func hlCheckPrice(price string, mk hlMarket) (string, error) {
	wire, err := hlWireDecimal(price)
	if err != nil {
		return "", errors.New("price: " + err.Error())
	}
	rat, _ := new(big.Rat).SetString(wire)
	if rat.Sign() <= 0 {
		return "", errors.New("price must be above zero")
	}
	if !strings.Contains(wire, ".") {
		return wire, nil
	}
	limit := 6
	if mk.spot {
		limit = 8
	}
	limit -= mk.szDecimals
	whole, frac, _ := strings.Cut(wire, ".")
	digits := strings.TrimLeft(whole+frac, "0")
	if len(digits) > 5 || len(frac) > limit {
		f, _ := rat.Float64()
		suggestion := strconv.FormatFloat(f, 'g', 5, 64)
		if x, err := strconv.ParseFloat(suggestion, 64); err == nil {
			suggestion = strconv.FormatFloat(x, 'f', -1, 64)
			if i := strings.Index(suggestion, "."); i >= 0 && len(suggestion)-i-1 > limit {
				suggestion = strconv.FormatFloat(x, 'f', limit, 64)
				suggestion = strings.TrimRight(strings.TrimRight(suggestion, "0"), ".")
			}
		}
		return "", fmt.Errorf("price %s is not valid for %s: at most 5 significant figures and %d decimals (for example %s). Nothing was sent", wire, mk.name, limit, suggestion)
	}
	return wire, nil
}

func hlCheckSize(size string, mk hlMarket) (string, error) {
	wire, err := hlWireDecimal(size)
	if err != nil {
		return "", errors.New("size: " + err.Error())
	}
	rat, _ := new(big.Rat).SetString(wire)
	if rat.Sign() <= 0 {
		return "", errors.New("size must be above zero")
	}
	if _, frac, _ := strings.Cut(wire, "."); len(frac) > mk.szDecimals {
		return "", fmt.Errorf("size %s has too many decimals for %s: it allows %d. Nothing was sent", wire, mk.name, mk.szDecimals)
	}
	return wire, nil
}

// ---- writes ---------------------------------------------------------------

func (m *Manager) hlOrderFrom(ctx context.Context, r *record, args map[string]any) (hlOrder, hlMarket, error) {
	mk, err := m.hlResolve(ctx, r, hlStr(args, "coin"))
	if err != nil {
		return hlOrder{}, mk, err
	}
	px, err := hlCheckPrice(hlStr(args, "price"), mk)
	if err != nil {
		return hlOrder{}, mk, err
	}
	sz, err := hlCheckSize(hlStr(args, "size"), mk)
	if err != nil {
		return hlOrder{}, mk, err
	}
	tif := hlStr(args, "tif")
	if tif == "" {
		tif = "Gtc"
	}
	o := hlOrder{asset: mk.asset, buy: hlStr(args, "side") == "buy", price: px, size: sz, tif: tif}
	o.reduceOnly, _ = args["reduce_only"].(bool)
	if c := hlStr(args, "client_order_id"); c != "" {
		if !hlValidCloid(c) {
			return hlOrder{}, mk, errors.New("client_order_id must be 0x followed by 32 hex digits")
		}
		o.cloid = strings.ToLower(c)
	}
	return o, mk, nil
}

func (m *Manager) hlWrite(ctx context.Context, r *record, key *secp256k1.PrivateKey, name string, args map[string]any) (map[string]any, error) {
	var action hlMap
	switch name {
	case "place_order":
		o, _, err := m.hlOrderFrom(ctx, r, args)
		if err != nil {
			return nil, err
		}
		action = hlPlaceAction(o)
	case "modify_order":
		o, _, err := m.hlOrderFrom(ctx, r, args)
		if err != nil {
			return nil, err
		}
		var which any
		switch {
		case args["oid"] != nil:
			which = uint64(hlNum(args, "oid", 0))
		case hlStr(args, "order_to_replace") != "" && hlValidCloid(hlStr(args, "order_to_replace")):
			which = strings.ToLower(hlStr(args, "order_to_replace"))
		default:
			return nil, errors.New("give oid, or order_to_replace as 0x followed by 32 hex digits")
		}
		action = hlModifyAction(which, o)
	case "cancel_order":
		mk, err := m.hlResolve(ctx, r, hlStr(args, "coin"))
		if err != nil {
			return nil, err
		}
		switch {
		case args["oid"] != nil:
			action = hlCancelAction(mk.asset, uint64(hlNum(args, "oid", 0)))
		case hlValidCloid(hlStr(args, "client_order_id")):
			action = hlCancelCloidAction(mk.asset, strings.ToLower(hlStr(args, "client_order_id")))
		default:
			return nil, errors.New("give oid, or client_order_id as 0x followed by 32 hex digits")
		}
	case "set_leverage":
		mk, err := m.hlResolve(ctx, r, hlStr(args, "coin"))
		if err != nil {
			return nil, err
		}
		if mk.spot {
			return nil, errors.New("spot markets have no leverage")
		}
		if lev := hlNum(args, "leverage", 0); lev > mk.maxLeverage {
			return nil, fmt.Errorf("%s allows at most %dx leverage", mk.name, mk.maxLeverage)
		}
		action = hlLeverageAction(mk.asset, hlStr(args, "mode") != "isolated", uint64(hlNum(args, "leverage", 0)))
	case "schedule_cancel":
		clear, _ := args["clear"].(bool)
		after := hlNum(args, "after_seconds", 0)
		if clear == (after > 0) {
			return nil, errors.New("give after_seconds to arm the switch, or clear to remove it")
		}
		action = hlScheduleCancelAction(0)
		if after > 0 {
			action = hlScheduleCancelAction(uint64(time.Now().Add(time.Duration(after) * time.Second).UnixMilli()))
		}
	default:
		return nil, errors.New("tool not found")
	}
	return m.hlSend(ctx, r, key, action)
}

// hlSend signs and posts one action, once. What is signed is the same ordered
// map that is sent and echoed back, so the result shows exactly what was placed.
func (m *Manager) hlSend(ctx context.Context, r *record, key *secp256k1.PrivateKey, action hlMap) (map[string]any, error) {
	mainnet := r.Account.Network == "mainnet"
	nonce := hlNextNonce(hlAddress(key))
	sig, err := hlSignL1(key, action, nonce, mainnet)
	if err != nil {
		return nil, err
	}
	body := hlMap{{"action", action}, {"nonce", nonce}, {"signature", sig}}
	raw, err := m.hlPost(ctx, r, "/exchange", body)
	out := map[string]any{"account_address": r.Account.Address, "signer": hlAddress(key), "nonce": nonce, "action_sent": action}
	var httpErr hlHTTPError
	switch {
	case errors.As(err, &httpErr):
		out["ok"], out["http_status"], out["exchange_response"] = false, httpErr.status, httpErr.body
		return out, nil
	case err != nil:
		return nil, fmt.Errorf("%w; the action may or may not have been applied: check get_open_orders or get_order_status before trying again", err)
	}
	var reply struct {
		Status   string `json:"status"`
		Response struct {
			Data struct {
				Statuses []json.RawMessage `json:"statuses"`
			} `json:"data"`
		} `json:"response"`
	}
	ok := json.Unmarshal(raw, &reply) == nil && reply.Status == "ok"
	for _, s := range reply.Response.Data.Statuses {
		if bytes.Contains(s, []byte(`"error"`)) {
			ok = false
		}
	}
	out["ok"] = ok
	if json.Valid(raw) {
		out["exchange_response"] = json.RawMessage(raw)
	} else {
		out["exchange_response"] = strings.TrimSpace(string(raw))
	}
	return out, nil
}

// ---- connect ----------------------------------------------------------------

// addHyperliquid connects an address on a network, with an optional API wallet
// key. Connecting an address and network that is already connected updates
// that account, so adding a key later needs no second account. Without a key
// the account is read-only, and an existing key is kept.
func (m *Manager) addHyperliquid(ctx context.Context, in *Input) (Response, error) {
	network := strings.ToLower(strings.TrimSpace(in.Network))
	address := strings.ToLower(strings.TrimSpace(in.Address))
	key := strings.TrimSpace(in.Credential)
	if network != "mainnet" && network != "testnet" {
		return Response{}, errors.New("choose Mainnet or Testnet")
	}
	if !hlValidAddress(address) {
		return Response{}, errors.New("enter the account address: 0x followed by 40 hex digits")
	}
	if len(key) > 200 || strings.ContainsAny(key, "\r\n") {
		return Response{}, errors.New("invalid API wallet key")
	}
	var existing *record
	for _, r := range m.records {
		if r.Account.Integration == "hyperliquid" && r.Account.Network == network && r.Account.Address == address && r.Account.Status != "disconnected" {
			existing = r
		}
	}
	if existing == nil && len(m.records) >= 50 {
		return Response{}, errors.New("maximum of 50 accounts reached")
	}
	r := existing
	if r == nil {
		r = &record{Account: Account{ID: uuid.NewString(), Integration: "hyperliquid", Name: hlAccountName(network, address), Network: network, Address: address, Endpoint: hlEndpoint(network), Enabled: true, History: []Event{}}, Grants: map[string]string{}}
	}
	ref := "integration:" + r.Account.ID
	secret := key
	if secret == "" && existing != nil {
		if saved, ok, err := m.vault.Get(ref); err == nil && ok {
			secret = saved
		}
	}
	candidate := *r
	candidate.Account = cloneAccount(r.Account)
	if err := m.discover(ctx, &candidate, secret); err != nil {
		return Response{}, errors.New(redactText(err.Error(), secret))
	}
	if key != "" {
		if err := m.vault.Set(ref, key); err != nil {
			return Response{}, errors.New("credential could not be stored")
		}
	}
	before := map[string]bool{}
	for _, t := range r.Account.Tools {
		before[t.Name] = true
	}
	r.Account = candidate.Account
	r.Account.Enabled = true
	if r.Grants == nil {
		r.Grants = map[string]string{}
	}
	offered := map[string]bool{}
	for _, t := range r.Account.Tools {
		offered[t.Name] = true
		if t.Read && !before[t.Name] {
			r.Grants[t.Name] = fingerprint(t)
		}
	}
	for name := range r.Grants {
		if !offered[name] {
			delete(r.Grants, name)
		}
	}
	m.records[r.Account.ID] = r
	if err := m.save(); err != nil {
		if existing == nil {
			delete(m.records, r.Account.ID)
			_ = m.vault.Delete(ref)
		}
		return Response{}, err
	}
	a := m.projection(r)
	return Response{Account: &a}, nil
}

func redactText(text, secret string) string {
	if secret == "" {
		return text
	}
	return string(redactValues([]byte(text), []string{secret}))
}

// removeHyperliquidKey deletes the saved API wallet key and leaves the
// account read-only; its trading tools and their permissions go with it.
func (m *Manager) removeHyperliquidKey(r *record) error {
	if r.Account.Integration != "hyperliquid" {
		return errors.New("only a Hyperliquid account has a trading key")
	}
	if err := m.vault.Delete("integration:" + r.Account.ID); err != nil {
		return errors.New("the trading key could not be removed; try again")
	}
	r.Account.Signer = ""
	r.Account.Identity = hlIdentity(r.Account.Network, r.Account.Address, false)
	var tools []Tool
	for _, t := range r.Account.Tools {
		// Tool.Read is not stored; the reviewed definition decides.
		if toolGroup(r, t) == "read" {
			tools = append(tools, t)
		} else {
			delete(r.Grants, t.Name)
			delete(r.Updates, t.Name)
		}
	}
	r.Account.Tools = tools
	return nil
}
