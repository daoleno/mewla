#!/usr/bin/env python3
"""Expire the older Mewla Preview builds that are still waiting for Beta App Review.

Only the newest Preview build should wait for review: if an older one were approved first, the
public TestFlight link would hand out a build with bugs the newest fixes. Given a build number N,
this expires every non-expired build of the app numbered below N whose Beta App Review is still
pending (TestFlight's external state WAITING_FOR_BETA_REVIEW or IN_BETA_REVIEW, or a review
submission WAITING_FOR_REVIEW or IN_REVIEW), with PATCH /v1/builds/{id} expired=true. A build that
passed review or is in beta testing, build N itself and every newer build are never touched.
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import pathlib
import re
import sys
import urllib.parse
from typing import Any

_SPEC = importlib.util.spec_from_file_location(
    "asc_upload", pathlib.Path(__file__).with_name("app-store-connect-upload.py")
)
asc_upload = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(asc_upload)

WAITING_EXTERNAL = frozenset({"WAITING_FOR_BETA_REVIEW", "IN_BETA_REVIEW"})
WAITING_REVIEW = frozenset({"WAITING_FOR_REVIEW", "IN_REVIEW"})
APPROVED_EXTERNAL = frozenset({"BETA_APPROVED", "IN_BETA_TESTING"})
APPROVED_REVIEW = frozenset({"APPROVED"})


def app_builds(client: Any, app_id: str) -> list[dict[str, Any]]:
    query = urllib.parse.urlencode(
        {
            "filter[app]": app_id,
            "sort": "-uploadedDate",
            "limit": "200",
            "include": "preReleaseVersion,buildBetaDetail",
            "fields[builds]": "version,expired,processingState,preReleaseVersion,buildBetaDetail",
            "fields[preReleaseVersions]": "version",
            "fields[buildBetaDetails]": "externalBuildState",
        }
    )
    document = client.api_request("GET", f"/builds?{query}")
    included = {(i.get("type"), i.get("id")): i for i in document.get("included", [])}

    def related(build: dict[str, Any], name: str) -> dict[str, Any]:
        ref = (build.get("relationships", {}).get(name, {}) or {}).get("data")
        return included.get(((ref or {}).get("type"), (ref or {}).get("id")), {})

    rows = []
    for build in document.get("data", []):
        attributes = build.get("attributes", {})
        rows.append(
            {
                "id": build["id"],
                "version": related(build, "preReleaseVersion").get("attributes", {}).get("version"),
                "build_number": attributes.get("version"),
                "expired": attributes.get("expired"),
                "external": related(build, "buildBetaDetail").get("attributes", {}).get("externalBuildState"),
            }
        )
    return rows


def review_state(client: Any, build_id: str) -> str | None:
    query = urllib.parse.urlencode({"filter[build]": build_id, "limit": "2"})
    reviews = client.api_request("GET", f"/betaAppReviewSubmissions?{query}").get("data", [])
    if not reviews:
        return None
    return reviews[0].get("attributes", {}).get("betaReviewState")


def number(row: dict[str, Any]) -> int | None:
    value = row.get("build_number")
    return int(value) if isinstance(value, str) and re.match(r"^[1-9]\d*$", value) else None


def candidates(client: Any, rows: list[dict[str, Any]], keep: int) -> list[dict[str, Any]]:
    """The builds below `keep` still waiting for review, each with its review state."""
    chosen = []
    for row in rows:
        built = number(row)
        if built is None or built >= keep or row.get("expired") is not False:
            continue
        if row.get("external") is None:
            detail = client.api_request("GET", f"/builds/{row['id']}/buildBetaDetail").get("data") or {}
            row["external"] = detail.get("attributes", {}).get("externalBuildState")
        if row.get("external") in APPROVED_EXTERNAL:
            continue
        row["review"] = review_state(client, row["id"])
        if row["review"] in APPROVED_REVIEW:
            continue
        if row.get("external") in WAITING_EXTERNAL or row["review"] in WAITING_REVIEW:
            chosen.append(row)
    return chosen


def expire(client: Any, row: dict[str, Any]) -> dict[str, Any]:
    result = {"id": row["id"], "version": row["version"], "build_number": row["build_number"],
              "external": row.get("external"), "review": row.get("review")}
    try:
        updated = client.api_request(
            "PATCH",
            f"/builds/{row['id']}",
            {"data": {"type": "builds", "id": row["id"], "attributes": {"expired": True}}},
        ).get("data") or {}
    except asc_upload.AppStoreConnectHTTPError as exc:
        result.update(status=exc.status, expired=False, error=exc.detail)
        return result
    result.update(status=200, expired=updated.get("attributes", {}).get("expired") is True, error=None)
    return result


def run(client: Any, app_id: str, keep: int, dry_run: bool = False) -> dict[str, Any]:
    waiting = candidates(client, app_builds(client, app_id), keep)
    if dry_run:
        results = [{"id": r["id"], "version": r["version"], "build_number": r["build_number"],
                    "external": r.get("external"), "review": r.get("review"), "status": None,
                    "expired": False, "error": None} for r in waiting]
    else:
        results = [expire(client, r) for r in waiting]
    return {"keep": keep, "dry_run": dry_run, "builds": results}


def markdown(result: dict[str, Any]) -> str:
    title = "Older builds waiting for Beta App Review" + (" (dry run)" if result["dry_run"] else "")
    lines = [f"## {title}", "", f"Kept: build {result['keep']} and every newer build.", ""]
    if not result["builds"]:
        return "\n".join(lines + ["None waiting below it."]) + "\n"
    lines += ["| Build | External testing | Beta App Review | Result |", "| --- | --- | --- | --- |"]
    for row in result["builds"]:
        if result["dry_run"]:
            outcome = "would expire"
        elif row["expired"]:
            outcome = f"expired (HTTP {row['status']})"
        else:
            outcome = f"refused (HTTP {row['status']}): {row['error'] or 'not expired'}"
        lines.append(f"| {row['version']} ({row['build_number']}) | {row['external'] or '—'} "
                     f"| {row['review'] or 'not submitted'} | {outcome} |")
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--app-id", required=True)
    parser.add_argument("--key-id", required=True)
    parser.add_argument("--key-path", required=True, type=pathlib.Path)
    parser.add_argument("--keep-build-number", required=True,
                        help="the build that stays in review; builds numbered below it are expired")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--summary", type=pathlib.Path)
    parser.add_argument("--output", type=pathlib.Path)
    args = parser.parse_args()
    if not re.match(r"^[1-9]\d*$", args.app_id):
        raise SystemExit("error: --app-id must be numeric")
    if not re.match(r"^[1-9]\d*$", args.keep_build_number):
        raise SystemExit("error: --keep-build-number must be a positive integer")
    client = asc_upload.AppStoreConnectClient(args.key_id, args.key_path)
    result = run(client, args.app_id, int(args.keep_build_number), args.dry_run)
    text = markdown(result)
    print(text)
    if args.summary:
        with args.summary.open("a", encoding="utf-8") as summary:
            summary.write(text)
    if args.output:
        args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return 1 if any(not r["expired"] for r in result["builds"]) and not args.dry_run else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (KeyError, RuntimeError, TimeoutError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        sys.exit(1)
