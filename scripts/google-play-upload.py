#!/usr/bin/env python3
"""Upload a verified AAB to a Google Play testing track (Play Developer API v3).

One edit: insert -> upload bundle -> set the track release -> commit. The edit
is deleted on any failure, so a half-finished upload never lingers in Play
Console. --validate-only asks Play to validate the edit and then deletes it.
--dry-run checks the inputs and prints the plan without any network access.

The service account JSON key is read from a file (CI writes it mode 0600 from
a secret); its private key is never printed. The JWT is signed with openssl.

Usage:
  ./scripts/google-play-upload.py --aab app.aab --package com.daoleno.mewla \\
    --version-code 54 --track internal --service-account key.json [--dry-run]
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import pathlib
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from typing import Any


API_ROOT = "https://androidpublisher.googleapis.com/androidpublisher/v3/applications"
UPLOAD_ROOT = "https://androidpublisher.googleapis.com/upload/androidpublisher/v3/applications"
SCOPE = "https://www.googleapis.com/auth/androidpublisher"
TRACKS = ("internal", "alpha", "beta", "production")
STATUSES = ("draft", "completed")


class PlayHTTPError(RuntimeError):
    def __init__(self, status: int, detail: str) -> None:
        self.status = status
        super().__init__(f"Google Play returned HTTP {status}: {detail}")


def base64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


def sha256_file(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_service_account(path: pathlib.Path) -> dict[str, str]:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise SystemExit(f"error: service account key is not readable JSON: {exc.__class__.__name__}")
    if data.get("type") != "service_account":
        raise SystemExit("error: key file is not a service_account key")
    for field in ("client_email", "private_key", "token_uri"):
        if not isinstance(data.get(field), str) or not data[field]:
            raise SystemExit(f"error: service account key lacks {field}")
    return data


def signed_jwt(account: dict[str, str], now: int) -> str:
    header = {"alg": "RS256", "typ": "JWT"}
    claims = {
        "iss": account["client_email"],
        "scope": SCOPE,
        "aud": account["token_uri"],
        "iat": now,
        "exp": now + 3600,
    }
    signing_input = f"{base64url(json.dumps(header).encode())}.{base64url(json.dumps(claims).encode())}"
    with tempfile.NamedTemporaryFile("w", suffix=".pem", delete=True) as key:
        pathlib.Path(key.name).chmod(0o600)
        key.write(account["private_key"])
        key.flush()
        signature = subprocess.run(
            ["openssl", "dgst", "-sha256", "-sign", key.name],
            input=signing_input.encode(),
            capture_output=True,
            check=True,
        ).stdout
    return f"{signing_input}.{base64url(signature)}"


def request(
    method: str,
    url: str,
    token: str | None = None,
    body: bytes | None = None,
    content_type: str = "application/json",
    timeout: int = 120,
) -> dict[str, Any]:
    headers = {"Accept": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    if body is not None:
        headers["Content-Type"] = content_type
    req = urllib.request.Request(url, data=body, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            raw = response.read()
    except urllib.error.HTTPError as exc:
        raise PlayHTTPError(exc.code, exc.read().decode("utf-8", "replace")[:2000]) from None
    return json.loads(raw) if raw else {}


def access_token(account: dict[str, str]) -> str:
    form = urllib.parse.urlencode({
        "grant_type": "urn:ietf:params:oauth:grant-type:jwt-bearer",
        "assertion": signed_jwt(account, int(time.time())),
    }).encode()
    result = request("POST", account["token_uri"], body=form, content_type="application/x-www-form-urlencoded")
    token = result.get("access_token")
    if not isinstance(token, str) or not token:
        raise SystemExit("error: token endpoint returned no access_token")
    return token


def track_body(track: str, version_code: int, status: str, release_name: str) -> dict[str, Any]:
    return {
        "track": track,
        "releases": [{"name": release_name, "versionCodes": [str(version_code)], "status": status}],
    }


def check_inputs(args: argparse.Namespace) -> None:
    if not args.aab.is_file() or not zipfile.is_zipfile(args.aab):
        raise SystemExit(f"error: AAB is not a zip file: {args.aab}")
    with zipfile.ZipFile(args.aab) as bundle:
        if "base/manifest/AndroidManifest.xml" not in bundle.namelist():
            raise SystemExit("error: not an Android App Bundle (no base/manifest/AndroidManifest.xml)")
    if args.version_code < 1:
        raise SystemExit("error: --version-code must be positive")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--aab", type=pathlib.Path, required=True)
    parser.add_argument("--package", required=True)
    parser.add_argument("--version-code", type=int, required=True)
    parser.add_argument("--track", choices=TRACKS, required=True)
    parser.add_argument("--release-status", choices=STATUSES, default="draft",
                        help="draft until the app's first release is rolled out in Play Console")
    parser.add_argument("--release-name", default="")
    parser.add_argument("--service-account", type=pathlib.Path)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--dry-run", action="store_true", help="check inputs and print the plan; no network")
    mode.add_argument("--validate-only", action="store_true", help="upload, let Play validate, then discard the edit")
    args = parser.parse_args(argv)
    if args.track == "production":
        raise SystemExit("error: this workflow uploads to testing tracks only")

    check_inputs(args)
    digest = sha256_file(args.aab)
    release_name = args.release_name or str(args.version_code)
    body = track_body(args.track, args.version_code, args.release_status, release_name)
    if args.service_account:
        load_service_account(args.service_account)
    elif not args.dry_run:
        raise SystemExit("error: --service-account is required unless --dry-run")

    app = f"{API_ROOT}/{urllib.parse.quote(args.package)}"
    plan = [
        f"POST   {app}/edits",
        f"POST   {UPLOAD_ROOT}/{args.package}/edits/<edit>/bundles?uploadType=media  ({args.aab.name}, sha256 {digest})",
        f"PUT    {app}/edits/<edit>/tracks/{args.track}  {json.dumps(body)}",
        f"POST   {app}/edits/<edit>:{'validate' if args.validate_only else 'commit'}",
    ]
    if args.validate_only:
        plan.append(f"DELETE {app}/edits/<edit>")
    if args.dry_run:
        print("dry run: no request sent. Plan:")
        print("\n".join(f"  {step}" for step in plan))
        return 0

    account = load_service_account(args.service_account)
    token = access_token(account)
    edit = request("POST", f"{app}/edits", token, body=b"{}")["id"]
    edit_url = f"{app}/edits/{urllib.parse.quote(edit)}"
    try:
        uploaded = request(
            "POST",
            f"{UPLOAD_ROOT}/{urllib.parse.quote(args.package)}/edits/{urllib.parse.quote(edit)}/bundles?uploadType=media",
            token,
            body=args.aab.read_bytes(),
            content_type="application/octet-stream",
            timeout=1800,
        )
        if int(uploaded.get("versionCode", -1)) != args.version_code:
            raise SystemExit(f"error: Play read versionCode {uploaded.get('versionCode')}, expected {args.version_code}")
        if uploaded.get("sha256") and uploaded["sha256"].lower() != digest:
            raise SystemExit("error: Play reported a different bundle sha256")
        print(f"uploaded bundle versionCode={args.version_code} sha256={digest}")
        request("PUT", f"{edit_url}/tracks/{args.track}", token, body=json.dumps(body).encode())
        print(f"track {args.track}: {args.release_status} release {release_name}")
        if args.validate_only:
            request("POST", f"{edit_url}:validate", token, body=b"{}")
            print("ok: Play validated the edit; discarding it (--validate-only)")
            request("DELETE", edit_url, token)
            return 0
        request("POST", f"{edit_url}:commit", token, body=b"{}")
        print(f"ok: committed to {args.track}")
        return 0
    except BaseException:
        try:
            request("DELETE", edit_url, token)
        except Exception:
            pass
        raise


if __name__ == "__main__":
    try:
        sys.exit(main())
    except PlayHTTPError as exc:
        print(f"error: {exc}", file=sys.stderr)
        sys.exit(1)
