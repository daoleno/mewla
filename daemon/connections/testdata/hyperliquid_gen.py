"""Generate L1 signing vectors with the official Hyperliquid Python SDK.

Run: hl-venv/bin/python -I gen.py > vectors.json
The key is the SDK's own published test key; it holds nothing.
"""
import json

import eth_account
from hyperliquid.utils.signing import (
    action_hash,
    construct_phantom_agent,
    float_to_wire,
    order_request_to_order_wire,
    order_wires_to_order_action,
    sign_l1_action,
)
from hyperliquid.utils.types import Cloid
from eth_utils import to_hex

KEY = "0x0123456789012345678901234567890123456789012345678901234567890123"
wallet = eth_account.Account.from_key(KEY)


def order(asset, buy, px, sz, reduce_only, tif, cloid=None):
    req = {
        "coin": "X",
        "is_buy": buy,
        "sz": sz,
        "limit_px": px,
        "reduce_only": reduce_only,
        "order_type": {"limit": {"tif": tif}},
        "cloid": Cloid.from_str(cloid) if cloid else None,
    }
    return order_request_to_order_wire(req, asset)


def place(wires):
    return order_wires_to_order_action(wires)


cases = {}


def add(name, action, nonce):
    out = {"nonce": nonce}
    out["msgpack_hash"] = to_hex(action_hash(action, None, nonce, None))
    for net, mainnet in (("mainnet", True), ("testnet", False)):
        sig = sign_l1_action(wallet, action, None, nonce, None, mainnet)
        out[net] = sig
    cases[name] = out


# The SDK's own regression vectors.
add("order_eth_gtc", place([order(1, True, 100, 100, False, "Gtc")]), 0)
add("order_eth_cloid", place([order(1, True, 100, 100, False, "Gtc", "0x00000000000000000000000000000001")]), 0)
add("order_eth_ioc", place([order(4, True, 1670.1, 0.0147, False, "Ioc")]), 1677777606040)
# Wider integers and fractions: uint8, uint16 and uint32 asset ids, small prices.
add("order_btc_far_bid", place([order(0, True, 50000, 0.001, False, "Gtc")]), 1792761713617)
add("order_spot_alo", place([order(10107, False, 0.0001234, 12345, True, "Alo")]), 1792761713618)
add("order_builder_dex_asset", place([order(110000, True, 123456, 1, False, "Gtc", "0xabcdefabcdefabcdefabcdefabcdef12")]), 1792761713619)
add("order_asset_200", place([order(200, False, 0.5, 7.25, False, "Gtc")]), 1792761713620)
add("cancel", {"type": "cancel", "cancels": [{"a": 0, "o": 91490942}]}, 1792761713621)
add("cancel_by_cloid", {"type": "cancelByCloid", "cancels": [{"asset": 3, "cloid": "0x1234567890abcdef1234567890abcdef"}]}, 1792761713622)
add(
    "modify",
    {
        "type": "batchModify",
        "modifies": [{"oid": 91490942, "order": order(0, True, 49000.5, 0.002, False, "Gtc")}],
    },
    1792761713623,
)
add("leverage_cross", {"type": "updateLeverage", "asset": 1, "isCross": True, "leverage": 5}, 1792761713624)
add("leverage_isolated", {"type": "updateLeverage", "asset": 0, "isCross": False, "leverage": 20}, 1792761713625)
add("schedule_cancel", {"type": "scheduleCancel", "time": 1792761800000}, 1792761713626)
add("schedule_cancel_unset", {"type": "scheduleCancel"}, 1792761713627)

wires = {
    s: float_to_wire(v)
    for s, v in {
        "1670.1": 1670.1,
        "0.0147": 0.0147,
        "100": 100.0,
        "0.0001234": 0.0001234,
        "123456": 123456.0,
        "50000.50": 50000.5,
        "0.00000001": 0.00000001,
    }.items()
}

print(json.dumps({"address": wallet.address.lower(), "cases": cases, "wires": wires}, indent=1))
