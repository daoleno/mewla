#!/usr/bin/env python3
"""Point App Store Connect's webhook for Mewla Preview at the receiver (receiver.mjs), and prove it.

Creates the app's webhook named "Mewla TestFlight pipeline" (POST /v1/webhooks), or updates it in place
(PATCH /v1/webhooks/{id}): the receiver URL, the shared secret (ASC_WEBHOOK_SECRET in the environment,
never an argument) and the two build events the pipeline moves on. --ping then sends a test event
(POST /v1/webhookPings) and prints the webhook's newest deliveries with their HTTP status, which shows
the whole path App Store Connect -> receiver -> signature check. --dry-run only reads.

  ASC_WEBHOOK_SECRET=... scripts/asc-webhook/register.py --app-id 6821246662 --key-id ... \\
    --key-path AuthKey.p8 --url https://mewla-asc-webhook.<account>.workers.dev/app-store-connect --ping
"""

from __future__ import annotations

import argparse
import importlib.util
import os
import pathlib
import re
import sys
import time
import urllib.parse
from typing import Any

_SPEC = importlib.util.spec_from_file_location(
    "asc_upload", pathlib.Path(__file__).resolve().parents[1] / "app-store-connect-upload.py"
)
asc_upload = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(asc_upload)

NAME = "Mewla TestFlight pipeline"
EVENT_TYPES = ["BUILD_UPLOAD_STATE_UPDATED", "BUILD_BETA_DETAIL_EXTERNAL_BUILD_STATE_UPDATED"]
URL = re.compile(r"^https://[A-Za-z0-9.-]+(/[A-Za-z0-9._~/-]*)?$")


def find(client: Any, app_id: str) -> dict[str, Any] | None:
    hooks = client.api_request("GET", f"/apps/{app_id}/webhooks?limit=200").get("data", [])
    named = [h for h in hooks if h.get("attributes", {}).get("name") == NAME]
    if len(named) > 1:
        raise RuntimeError(f"App Store Connect has {len(named)} webhooks named {NAME!r}; delete the extra ones")
    return named[0] if named else None


def plan(existing: dict[str, Any] | None, url: str) -> str:
    """create, update or keep: the secret is write-only, so an existing webhook is always re-sent it."""
    if existing is None:
        return "create"
    attributes = existing.get("attributes", {})
    same = (attributes.get("url") == url and attributes.get("enabled") is True
            and sorted(attributes.get("eventTypes") or []) == sorted(EVENT_TYPES))
    return "keep" if same else "update"


def register(client: Any, app_id: str, url: str, secret: str, dry_run: bool = False) -> dict[str, Any]:
    existing = find(client, app_id)
    action = plan(existing, url)
    if dry_run:
        return {"action": action, "id": existing and existing.get("id"), "dry_run": True}
    attributes = {"enabled": True, "eventTypes": EVENT_TYPES, "name": NAME, "secret": secret, "url": url}
    if existing is None:
        body = {"data": {"type": "webhooks", "attributes": attributes,
                         "relationships": {"app": {"data": {"type": "apps", "id": app_id}}}}}
        created = client.api_request("POST", "/webhooks", body).get("data") or {}
        return {"action": "create", "id": created.get("id"), "dry_run": False}
    # "keep" still sends the secret: App Store Connect never returns it, so this is the only way to be sure.
    body = {"data": {"type": "webhooks", "id": existing["id"], "attributes": attributes}}
    client.api_request("PATCH", f"/webhooks/{existing['id']}", body)
    return {"action": "update" if action == "update" else "keep (secret re-sent)", "id": existing["id"], "dry_run": False}


def ping(client: Any, webhook_id: str) -> None:
    body = {"data": {"type": "webhookPings",
                     "relationships": {"webhook": {"data": {"type": "webhooks", "id": webhook_id}}}}}
    client.api_request("POST", "/webhookPings", body)


def deliveries(client: Any, webhook_id: str, limit: int = 5, days: int = 7) -> list[dict[str, Any]]:
    # App Store Connect requires exactly one date filter on this list.
    since = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - days * 86400))
    query = urllib.parse.urlencode(
        {"limit": str(limit), "include": "event", "filter[createdDateGreaterThanOrEqualTo]": since}
    )
    document = client.api_request("GET", f"/webhooks/{webhook_id}/deliveries?{query}")
    events = {i.get("id"): i.get("attributes", {}) for i in document.get("included", [])}
    rows = []
    for item in document.get("data", []):
        attributes = item.get("attributes", {})
        ref = ((item.get("relationships", {}).get("event") or {}).get("data") or {}).get("id")
        rows.append({
            "created": attributes.get("createdDate"),
            "state": attributes.get("deliveryState"),
            "http": (attributes.get("response") or {}).get("httpStatusCode"),
            "body": ((attributes.get("response") or {}).get("body") or "").strip()[:120],
            "event": events.get(ref, {}).get("eventType"),
            "ping": events.get(ref, {}).get("ping"),
        })
    return rows


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--app-id", required=True)
    parser.add_argument("--key-id", required=True)
    parser.add_argument("--key-path", required=True, type=pathlib.Path)
    parser.add_argument("--url", required=True)
    parser.add_argument("--ping", action="store_true", help="send a test event and print the deliveries")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    if not re.match(r"^[1-9]\d*$", args.app_id):
        raise SystemExit("error: --app-id must be numeric")
    if not URL.match(args.url):
        raise SystemExit("error: --url must be an https URL")
    secret = os.environ.get("ASC_WEBHOOK_SECRET", "")
    if not args.dry_run and len(secret) < 32:
        raise SystemExit("error: set ASC_WEBHOOK_SECRET (32+ characters) in the environment")
    client = asc_upload.AppStoreConnectClient(args.key_id, args.key_path)
    result = register(client, args.app_id, args.url, secret, args.dry_run)
    print(f"webhook {NAME!r}: {result['action']}{' (dry run)' if args.dry_run else ''} id={result['id']}")
    if args.ping and not args.dry_run and result["id"]:
        ping(client, result["id"])
        time.sleep(10)
    if result["id"]:
        for row in deliveries(client, result["id"]):
            print(f"delivery {row['created']} {row['event']}{' (ping)' if row['ping'] else ''}: "
                  f"{row['state']} HTTP {row['http']} {row['body']}")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (KeyError, RuntimeError, ValueError, asc_upload.AppStoreConnectHTTPError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        sys.exit(1)
