#!/usr/bin/env python3
"""Report where the newest Mewla Preview builds stand in Beta App Review, and open the public link.

For each of the newest builds: its version and number, processing state, Beta App Review state,
TestFlight's external state, and whether it is in the external group. Once a build has passed
review, the external group's public TestFlight link is turned on (when it is off), so anyone with
the link can install. The result goes to stdout, to a Markdown summary and to a JSON file.
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

# A build Beta App Review let into external testing (buildBetaDetail.externalBuildState), or whose
# review submission says so (betaAppReviewSubmission.betaReviewState).
APPROVED_EXTERNAL = frozenset({"BETA_APPROVED", "IN_BETA_TESTING"})
APPROVED_REVIEW = frozenset({"APPROVED"})
PUBLIC_LINK = re.compile(r"^https://testflight\.apple\.com/join/[A-Za-z0-9]+$")
CLICK_PATH = (
    "App Store Connect › Apps › Mewla Preview › TestFlight › the external group "
    "› Public Link › Enable Public Link"
)


def beta_group(client: Any, app_id: str, name: str) -> dict[str, Any]:
    groups = client.api_request(
        "GET",
        f"/apps/{app_id}/betaGroups?"
        + urllib.parse.urlencode(
            {
                "limit": "200",
                "fields[betaGroups]": "name,isInternalGroup,publicLinkEnabled,publicLink",
            }
        ),
    ).get("data", [])
    matches = [g for g in groups if g.get("attributes", {}).get("name") == name]
    if len(matches) != 1:
        raise RuntimeError(f"expected exactly one beta group named {name!r}; found {len(matches)}")
    return matches[0]


def newest_builds(client: Any, app_id: str, group_id: str, count: int) -> list[dict[str, Any]]:
    query = urllib.parse.urlencode(
        {
            "filter[app]": app_id,
            "sort": "-uploadedDate",
            "limit": str(count),
            "include": "preReleaseVersion,buildBetaDetail",
            "fields[builds]": "version,uploadedDate,expired,processingState,preReleaseVersion,buildBetaDetail",
            "fields[preReleaseVersions]": "version",
            "fields[buildBetaDetails]": "externalBuildState",
        }
    )
    document = client.api_request("GET", f"/builds?{query}")
    included = {(i.get("type"), i.get("id")): i for i in document.get("included", [])}

    def related(build: dict[str, Any], name: str) -> dict[str, Any]:
        ref = (build.get("relationships", {}).get(name, {}) or {}).get("data")
        return included.get(((ref or {}).get("type"), (ref or {}).get("id")), {})

    in_group = group_build_ids(client, app_id, group_id)
    rows = []
    for build in document.get("data", []):
        attributes = build.get("attributes", {})
        external = related(build, "buildBetaDetail").get("attributes", {}).get("externalBuildState")
        if external is None:
            detail = client.api_request("GET", f"/builds/{build['id']}/buildBetaDetail").get("data") or {}
            external = detail.get("attributes", {}).get("externalBuildState")
        rows.append(
            {
                "id": build["id"],
                "version": related(build, "preReleaseVersion").get("attributes", {}).get("version"),
                "build_number": attributes.get("version"),
                "uploaded": attributes.get("uploadedDate"),
                "processing": attributes.get("processingState"),
                "expired": attributes.get("expired"),
                "review": review_state(client, build["id"]),
                "external": external,
                "in_group": build["id"] in in_group,
            }
        )
    return rows


def group_build_ids(client: Any, app_id: str, group_id: str) -> set[str]:
    # /betaGroups/{id}/builds takes no filter on the app, so list the app's builds filtered to the group.
    query = urllib.parse.urlencode(
        {"filter[app]": app_id, "filter[betaGroups]": group_id, "limit": "200", "fields[builds]": "version"}
    )
    return {b["id"] for b in client.api_request("GET", f"/builds?{query}").get("data", [])}


def review_state(client: Any, build_id: str) -> str | None:
    query = urllib.parse.urlencode({"filter[build]": build_id, "limit": "2"})
    reviews = client.api_request("GET", f"/betaAppReviewSubmissions?{query}").get("data", [])
    if not reviews:
        return None
    return reviews[0].get("attributes", {}).get("betaReviewState")


def approved(row: dict[str, Any]) -> bool:
    return row.get("external") in APPROVED_EXTERNAL or row.get("review") in APPROVED_REVIEW


def ensure_public_link(client: Any, group: dict[str, Any], enable: bool) -> dict[str, Any]:
    """The group's public link, turned on when asked and off; what to click when the API refuses."""
    attributes = group.get("attributes", {})
    link = attributes.get("publicLink")
    result: dict[str, Any] = {
        "enabled": bool(attributes.get("publicLinkEnabled")),
        "url": link if isinstance(link, str) and PUBLIC_LINK.match(link) else None,
        "changed": False,
        "error": None,
    }
    if result["enabled"] or not enable:
        return result
    try:
        updated = client.api_request(
            "PATCH",
            f"/betaGroups/{group['id']}",
            {"data": {"type": "betaGroups", "id": group["id"], "attributes": {"publicLinkEnabled": True}}},
        ).get("data") or {}
    except asc_upload.AppStoreConnectHTTPError as exc:
        result["error"] = f"App Store Connect refused to enable the public link (HTTP {exc.status}); click: {CLICK_PATH}"
        return result
    attributes = updated.get("attributes", {})
    link = attributes.get("publicLink")
    result.update(
        enabled=bool(attributes.get("publicLinkEnabled")),
        url=link if isinstance(link, str) and PUBLIC_LINK.match(link) else None,
        changed=True,
    )
    return result


def report(group_name: str, rows: list[dict[str, Any]], link: dict[str, Any]) -> dict[str, Any]:
    newest_approved = next((r for r in rows if approved(r)), None)
    return {
        "group": group_name,
        "builds": rows,
        "newest_approved": newest_approved and {
            "version": newest_approved["version"],
            "build_number": newest_approved["build_number"],
            "in_group": newest_approved["in_group"],
        },
        "public_link": link,
    }


def markdown(result: dict[str, Any]) -> str:
    lines = [
        f"## TestFlight review: {result['group']}",
        "",
        "| Build | Uploaded | Processing | Beta App Review | External testing | In group |",
        "| --- | --- | --- | --- | --- | --- |",
    ]
    for row in result["builds"]:
        lines.append(
            f"| {row['version']} ({row['build_number']}) | {(row['uploaded'] or '')[:16]} | {row['processing']} "
            f"| {row['review'] or 'not submitted'} | {row['external'] or '—'} | {'yes' if row['in_group'] else 'no'} |"
        )
    newest = result["newest_approved"]
    lines.append("")
    lines.append(
        f"**Newest approved build:** {newest['version']} ({newest['build_number']})"
        + ("" if newest["in_group"] else f" — not in {result['group']}")
        if newest
        else "**Newest approved build:** none yet"
    )
    link = result["public_link"]
    if link["url"] and link["enabled"]:
        lines.append(f"**Public link:** {link['url']}" + (" (turned on by this run)" if link["changed"] else ""))
    elif link["error"]:
        lines.append(f"**Public link:** off. {link['error']}")
    else:
        lines.append("**Public link:** off" + ("" if newest else " (turned on once a build passes review)"))
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--app-id", required=True)
    parser.add_argument("--key-id", required=True)
    parser.add_argument("--key-path", required=True, type=pathlib.Path)
    parser.add_argument("--beta-group-name", required=True)
    parser.add_argument("--builds", type=int, default=5)
    parser.add_argument("--enable-public-link", action="store_true")
    parser.add_argument("--summary", type=pathlib.Path)
    parser.add_argument("--output", type=pathlib.Path)
    args = parser.parse_args()
    if not re.match(r"^[1-9]\d*$", args.app_id):
        raise SystemExit("error: --app-id must be numeric")
    client = asc_upload.AppStoreConnectClient(args.key_id, args.key_path)
    group = beta_group(client, args.app_id, args.beta_group_name)
    if group.get("attributes", {}).get("isInternalGroup"):
        raise RuntimeError(f"{args.beta_group_name!r} is an internal group; a public link needs an external one")
    rows = newest_builds(client, args.app_id, group["id"], args.builds)
    enable = args.enable_public_link and any(approved(r) and r["in_group"] for r in rows)
    result = report(args.beta_group_name, rows, ensure_public_link(client, group, enable))
    text = markdown(result)
    print(text)
    if args.summary:
        with args.summary.open("a", encoding="utf-8") as summary:
            summary.write(text)
    if args.output:
        args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (KeyError, RuntimeError, TimeoutError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        sys.exit(1)
