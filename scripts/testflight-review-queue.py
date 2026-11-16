#!/usr/bin/env python3
"""Keep one Mewla Preview build in Beta App Review and submit the newest waiting one when it is done.

Beta App Review takes about a day, and a new submission starts its own clock. So a build already
waiting for review (or in review) is never expired: an upload made meanwhile waits, VALID and in the
external group but not submitted. Once no build is in review (the one there was approved or
rejected), the newest waiting build is submitted. Builds a newer one superseded before they were
ever submitted are expired.

Decided from the app's builds (plan(), no I/O), then applied: POST /v1/betaAppReviewSubmissions for
the submission, PATCH /v1/builds/{id} expired=true for the superseded ones. With --dry-run only GET
requests reach App Store Connect. ios-release.yml runs it after each Preview upload and
ios-testflight-review.yml every hour.
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
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent / "lib"))
import release_identity  # noqa: E402

# TestFlight's external state (buildBetaDetail.externalBuildState) and the review submission's state
# (betaAppReviewSubmission.betaReviewState).
IN_REVIEW_EXTERNAL = frozenset({"WAITING_FOR_BETA_REVIEW", "IN_BETA_REVIEW"})
IN_REVIEW = frozenset({"WAITING_FOR_REVIEW", "IN_REVIEW"})
APPROVED_EXTERNAL = frozenset({"BETA_APPROVED", "IN_BETA_TESTING"})
APPROVED_REVIEW = frozenset({"APPROVED"})
REJECTED_EXTERNAL = frozenset({"BETA_REJECTED"})
REJECTED_REVIEW = frozenset({"REJECTED"})
# Processed and never submitted. Only READY_FOR_BETA_SUBMISSION (export compliance answered) can be
# submitted; MISSING_EXPORT_COMPLIANCE is a release still being prepared.
READY = "READY_FOR_BETA_SUBMISSION"
UNSUBMITTED_EXTERNAL = frozenset({READY, "MISSING_EXPORT_COMPLIANCE"})


def number(row: dict[str, Any]) -> int | None:
    value = row.get("build_number")
    return int(value) if isinstance(value, str) and re.match(r"^[1-9]\d*$", value) else None


def marketing(row: dict[str, Any]) -> tuple[int, ...] | None:
    value = row.get("version")
    return tuple(int(p) for p in value.split(".")) if isinstance(value, str) and re.match(r"^\d+(\.\d+){0,2}$", value) else None


def in_review(row: dict[str, Any]) -> bool:
    return row.get("review") in IN_REVIEW or row.get("external") in IN_REVIEW_EXTERNAL


def approved(row: dict[str, Any]) -> bool:
    return row.get("review") in APPROVED_REVIEW or row.get("external") in APPROVED_EXTERNAL


def rejected(row: dict[str, Any]) -> bool:
    return row.get("review") in REJECTED_REVIEW or row.get("external") in REJECTED_EXTERNAL


def unsubmitted(row: dict[str, Any]) -> bool:
    return row.get("review") is None and row.get("external") in UNSUBMITTED_EXTERNAL


def submittable(row: dict[str, Any]) -> bool:
    return (row.get("review") is None and row.get("external") == READY
            and row.get("processing") == "VALID" and row.get("in_group") is True)


def plan(rows: list[dict[str, Any]]) -> dict[str, Any]:
    """What to do now: the builds in review, the build to submit or keep waiting, the ones to expire.

    Only the non-expired builds with a numeric build number count. Nothing at or below the newest
    approved build is ever touched, and neither is an approved, in-review or rejected build.
    """
    live = sorted((r for r in rows if r.get("expired") is False and number(r) is not None),
                  key=number, reverse=True)
    floor = max((number(r) for r in live if approved(r)), default=0)
    reviewing = [r for r in live if in_review(r)]
    submitted = max((number(r) for r in live if in_review(r) or approved(r) or rejected(r)), default=0)
    newest = next((r for r in live if number(r) > submitted and submittable(r)), None)
    submit = newest if newest is not None and not reviewing else None
    waiting = newest if newest is not None and reviewing else None
    ceiling = max([number(r) for r in reviewing] + ([number(newest)] if newest else []), default=0)
    expire = [r for r in live
              if floor < number(r) < ceiling and r is not newest and unsubmitted(r)]
    # TestFlight shows the highest marketing version as the newest. A Preview build's is its train
    # (0.4), so an older build still live under a higher one (0.4.8) is listed above the train.
    top = live[0] if live else None
    above = [r for r in live[1:] if r not in expire and marketing(r) and marketing(top)
             and marketing(r) > marketing(top)] if top else []
    return {
        "above_train": above,
        "approved": next((r for r in live if approved(r)), None),
        "in_review": reviewing,
        "submit": submit,
        "waiting": waiting,
        "expire": expire,
    }


def app_builds(client: Any, app_id: str, group_id: str) -> list[dict[str, Any]]:
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

    in_group = group_build_ids(client, app_id, group_id)
    rows = []
    for build in document.get("data", []):
        attributes = build.get("attributes", {})
        row = {
            "id": build["id"],
            "version": related(build, "preReleaseVersion").get("attributes", {}).get("version"),
            "build_number": attributes.get("version"),
            "release": None,
            "processing": attributes.get("processingState"),
            "expired": attributes.get("expired"),
            "external": related(build, "buildBetaDetail").get("attributes", {}).get("externalBuildState"),
            "review": None,
            "in_group": build["id"] in in_group,
        }
        if row["expired"] is False:
            if row["external"] is None:
                detail = client.api_request("GET", f"/builds/{row['id']}/buildBetaDetail").get("data") or {}
                row["external"] = detail.get("attributes", {}).get("externalBuildState")
            row["review"] = review_state(client, row["id"])
        if number(row) is not None:
            row["release"] = release_identity.version_of_code(number(row))
        rows.append(row)
    return rows


def beta_group_id(client: Any, app_id: str, name: str) -> str:
    query = urllib.parse.urlencode({"limit": "200", "fields[betaGroups]": "name,isInternalGroup"})
    groups = client.api_request("GET", f"/apps/{app_id}/betaGroups?{query}").get("data", [])
    matches = [g for g in groups if g.get("attributes", {}).get("name") == name]
    if len(matches) != 1:
        raise RuntimeError(f"expected exactly one beta group named {name!r}; found {len(matches)}")
    if matches[0].get("attributes", {}).get("isInternalGroup"):
        raise RuntimeError(f"{name!r} is an internal group; Beta App Review is for an external one")
    return matches[0]["id"]


def group_build_ids(client: Any, app_id: str, group_id: str) -> set[str]:
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


class ReadOnly:
    """A client that lets only GET requests through, for --dry-run."""

    def __init__(self, client: Any) -> None:
        self.client = client

    def api_request(self, method: str, path: str, body: dict[str, Any] | None = None) -> dict[str, Any]:
        if method != "GET":
            raise RuntimeError(f"dry run refused {method} {path}")
        return self.client.api_request(method, path, body)


def submit(client: Any, row: dict[str, Any]) -> dict[str, Any]:
    body = {"data": {"type": "betaAppReviewSubmissions",
                     "relationships": {"build": {"data": {"type": "builds", "id": row["id"]}}}}}
    try:
        client.api_request("POST", "/betaAppReviewSubmissions", body)
    except asc_upload.AppStoreConnectHTTPError as exc:
        # Another run (the release and the hourly job) may have submitted it a moment ago.
        state = review_state(client, row["id"])
        if state is None or exc.status != 409:
            return {"status": exc.status, "done": False, "error": exc.detail}
        return {"status": exc.status, "done": True, "error": None, "review": state}
    return {"status": 201, "done": True, "error": None}


def expire(client: Any, row: dict[str, Any]) -> dict[str, Any]:
    body = {"data": {"type": "builds", "id": row["id"], "attributes": {"expired": True}}}
    try:
        updated = client.api_request("PATCH", f"/builds/{row['id']}", body).get("data") or {}
    except asc_upload.AppStoreConnectHTTPError as exc:
        return {"status": exc.status, "done": False, "error": exc.detail}
    done = updated.get("attributes", {}).get("expired") is True
    return {"status": 200, "done": done, "error": None if done else "not expired"}


def brief(row: dict[str, Any] | None) -> dict[str, Any] | None:
    if row is None:
        return None
    return {k: row.get(k) for k in ("id", "version", "release", "build_number", "external", "review")}


def run(client: Any, app_id: str, group_name: str, dry_run: bool = False) -> dict[str, Any]:
    reader = ReadOnly(client) if dry_run else client
    decided = plan(app_builds(reader, app_id, beta_group_id(reader, app_id, group_name)))
    result: dict[str, Any] = {
        "dry_run": dry_run,
        "approved": brief(decided["approved"]),
        "in_review": [brief(r) for r in decided["in_review"]],
        "waiting": brief(decided["waiting"]),
        "submit": None,
        "expire": [],
        "above_train": [brief(r) for r in decided["above_train"]],
    }
    if decided["submit"] is not None:
        result["submit"] = {**brief(decided["submit"]),
                            **({"status": None, "done": False, "error": None} if dry_run
                               else submit(client, decided["submit"]))}
    for row in decided["expire"]:
        result["expire"].append({**brief(row),
                                 **({"status": None, "done": False, "error": None} if dry_run
                                    else expire(client, row))})
    return result


def failed(result: dict[str, Any]) -> bool:
    if result["dry_run"]:
        return False
    actions = ([result["submit"]] if result["submit"] else []) + result["expire"]
    return any(not a["done"] for a in actions)


def label(row: dict[str, Any]) -> str:
    """The release the build number names (40999 is 0.4.9), and the train when it differs."""
    release = row.get("release")
    if release and release != row.get("version"):
        return f"{release} ({row['build_number']}, TestFlight {row['version']})"
    return f"{row['version']} ({row['build_number']})"


def outcome(row: dict[str, Any], dry_run: bool, verb: str, done: str) -> str:
    if dry_run:
        return f"would {verb}"
    if row["done"]:
        return f"{done} (HTTP {row['status']})"
    return f"refused (HTTP {row['status']}): {row['error']}"


def markdown(result: dict[str, Any]) -> str:
    dry = result["dry_run"]
    lines = ["## Beta App Review queue" + (" (dry run)" if dry else ""), ""]
    approved_row = result["approved"]
    lines.append(f"- Newest approved: {label(approved_row) if approved_row else 'none'}")
    reviewing = result["in_review"]
    lines.append("- In review: " + (", ".join(f"{label(r)} {r['review'] or r['external']}" for r in reviewing)
                                    if reviewing else "none"))
    if result["waiting"]:
        lines.append(f"- Waiting: {label(result['waiting'])}, submitted once the review above ends")
    if result["submit"]:
        lines.append(f"- Submit: {label(result['submit'])}, {outcome(result['submit'], dry, 'submit', 'submitted')}")
    elif not result["waiting"]:
        lines.append("- Submit: nothing newer to submit")
    if result["expire"]:
        lines.append("- Expire (superseded, never submitted): "
                     + ", ".join(f"{label(r)} {outcome(r, dry, 'expire', 'expired')}" for r in result["expire"]))
    if result.get("above_train"):
        lines.append("- Listed above the newest build in TestFlight (higher marketing version): "
                     + ", ".join(label(r) for r in result["above_train"]))
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--app-id", required=True)
    parser.add_argument("--key-id", required=True)
    parser.add_argument("--key-path", required=True, type=pathlib.Path)
    parser.add_argument("--beta-group-name", required=True)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--summary", type=pathlib.Path)
    parser.add_argument("--output", type=pathlib.Path)
    args = parser.parse_args()
    if not re.match(r"^[1-9]\d*$", args.app_id):
        raise SystemExit("error: --app-id must be numeric")
    client = asc_upload.AppStoreConnectClient(args.key_id, args.key_path)
    result = run(client, args.app_id, args.beta_group_name, args.dry_run)
    text = markdown(result)
    print(text)
    if args.summary:
        with args.summary.open("a", encoding="utf-8") as summary:
            summary.write(text)
    if args.output:
        args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return 1 if failed(result) else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (KeyError, RuntimeError, TimeoutError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        sys.exit(1)
