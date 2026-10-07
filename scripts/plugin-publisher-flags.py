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
}


def publisher_flags(document, required):
    if not isinstance(document, dict) or set(document) != {"schema_version", "github", "slack"}:
        raise ValueError("publisher manifest must contain only schema_version, github and slack")
    if document["schema_version"] != 1:
        raise ValueError("unsupported publisher manifest version")
    flags = []
    for service, symbol in SYMBOLS.items():
        config = document[service]
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
        if service == "slack" and config[identity] is not None and (
            not isinstance(config[identity], str) or not re.fullmatch(r"T[A-Z0-9]+", config[identity])
        ):
            raise ValueError("Slack publisher must identify an actual team workspace")
        if client_id and not config[identity]:
            raise ValueError(f"{service}: publisher identity is required with client_id")
        if service in required and client_id is None:
            raise ValueError(f"{service}: Zen publisher registration is not configured; cannot publish")
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
