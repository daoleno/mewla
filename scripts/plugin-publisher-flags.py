#!/usr/bin/env python3
"""Validate tracked public publisher identities and emit reproducible Go flags.

No environment secrets or per-daemon OAuth configuration are used. A null ID
means registration is still pending; publishing can require a specific service.
"""

import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SYMBOLS = {
    "github": "GitHubPublicClientID",
    "slack": "SlackPublicClientID",
    "google": "GoogleExchangeOrigin",
}
# Google has no public client ID to ship: its sign-in runs on Mewla's own
# service, which holds the client secret. The daemon only needs that origin.
GOOGLE_ORIGIN = "https://connect.mewla.ai"


def publisher_flags(document, required):
    if not isinstance(document, dict) or set(document) != {"schema_version", "github", "slack", "google"}:
        raise ValueError("publisher manifest must contain only schema_version, github, slack and google")
    if document["schema_version"] != 1:
        raise ValueError("unsupported publisher manifest version")
    flags = []
    for service, symbol in SYMBOLS.items():
        config = document[service]
        if service == "google":
            if not isinstance(config, dict) or set(config) != {"exchange_origin"}:
                raise ValueError("google: only exchange_origin is allowed")
            origin = config["exchange_origin"]
            if origin is not None and origin != GOOGLE_ORIGIN:
                raise ValueError(f"google: the exchange origin must be {GOOGLE_ORIGIN}")
            if service in required and origin is None:
                raise ValueError("google: Mewla's Google sign-in is not configured; cannot publish")
            if origin:
                flags.extend(["-X", f"github.com/daoleno/mewla/daemon/connections.{symbol}={origin}"])
            continue
        identity = "owner" if service == "github" else "workspace_id"
        if not isinstance(config, dict) or set(config) != {identity, "client_id"}:
            raise ValueError(f"{service}: only public publisher identity and client_id are allowed")
        client_id = config["client_id"]
        if client_id is not None and (
            not isinstance(client_id, str) or not re.fullmatch(r"[A-Za-z0-9._-]{1,128}", client_id)
        ):
            raise ValueError(f"{service}: invalid public client ID")
        if service == "github" and config[identity] != "daoleno":
            raise ValueError("GitHub publisher must be the approved owner daoleno")
        if service == "slack" and config[identity] is not None and config[identity] != "T0CA7628ECC":
            raise ValueError("Slack publisher must be the approved Mewla workspace T0CA7628ECC")
        if client_id and not config[identity]:
            raise ValueError(f"{service}: publisher identity is required with client_id")
        if service in required and client_id is None:
            raise ValueError(f"{service}: Mewla publisher registration is not configured; cannot publish")
        if client_id:
            flags.extend(["-X", f"github.com/daoleno/mewla/daemon/connections.{symbol}={client_id}"])
    return " ".join(flags)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=ROOT / "release/plugin-publishers.json")
    parser.add_argument("--require", action="append", choices=SYMBOLS, default=[])
    args = parser.parse_args()
    try:
        print(publisher_flags(json.loads(args.config.read_text()), args.require))
    except (OSError, ValueError) as error:
        parser.exit(1, f"publisher configuration: {error}\n")


if __name__ == "__main__":
    main()
