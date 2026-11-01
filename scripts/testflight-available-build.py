#!/usr/bin/env python3
"""Report the newest Mewla build that TestFlight testers can install right now.

An uploaded, VALID build is not installable: the public group only receives it
after Beta App Review. The App Store Connect build beta detail says when it is
in beta testing for external testers, so only that state counts.
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import pathlib
import re
import sys
import urllib.parse
from typing import Any, Callable

_SPEC = importlib.util.spec_from_file_location(
    "asc_upload", pathlib.Path(__file__).with_name("app-store-connect-upload.py")
)
asc_upload = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(asc_upload)

# ExternalBetaState values seen from App Store Connect for a build that Beta App Review let
# into the public group: BETA_APPROVED (observed on every reviewed Mewla build) and
# IN_BETA_TESTING. Anything earlier (READY_FOR_BETA_SUBMISSION, WAITING_FOR_BETA_REVIEW,
# IN_BETA_REVIEW, BETA_REJECTED, ...) is not installable.
INSTALLABLE_STATES = frozenset({"BETA_APPROVED", "IN_BETA_TESTING"})
MARKETING_VERSION = re.compile(r"^\d+\.\d+\.\d+$")
BUILD_NUMBER = re.compile(r"^[1-9]\d*$")
PUBLIC_LINK = re.compile(r"^https://testflight\.apple\.com/join/[A-Za-z0-9]+$")


def select_installable_build(
    document: dict[str, Any], detail_lookup: Callable[[str], str | None] | None = None
) -> dict[str, Any] | None:
    """Pick the highest (marketing version, build number) testers can install.

    The list response links a build's beta detail only for some builds; for the others
    detail_lookup(build id) asks for it directly, newest first, stopping at the first hit.
    """

    included = {
        (item.get("type"), item.get("id")): item for item in document.get("included", [])
    }

    def related(build: dict[str, Any], name: str) -> dict[str, Any]:
        ref = (build.get("relationships", {}).get(name, {}) or {}).get("data")
        if not isinstance(ref, dict):
            return {}
        return included.get((ref.get("type"), ref.get("id")), {})

    candidates: list[tuple[tuple[tuple[int, ...], int], dict[str, Any], dict[str, Any]]] = []
    for build in document.get("data", []):
        attributes = build.get("attributes", {})
        if attributes.get("expired") is not False or attributes.get("processingState") != "VALID":
            continue
        marketing = related(build, "preReleaseVersion").get("attributes", {}).get("version")
        number = attributes.get("version")
        if not (
            isinstance(marketing, str)
            and MARKETING_VERSION.match(marketing)
            and isinstance(number, str)
            and BUILD_NUMBER.match(number)
        ):
            continue
        rank = (tuple(int(part) for part in marketing.split(".")), int(number))
        candidates.append((rank, build, {"marketing_version": marketing, "build_number": int(number)}))
    for _, build, result in sorted(candidates, key=lambda item: item[0], reverse=True):
        state = related(build, "buildBetaDetail").get("attributes", {}).get("externalBuildState")
        if state is None and detail_lookup is not None:
            state = detail_lookup(build["id"])
        if state in INSTALLABLE_STATES:
            return result
    return None


def print_build_states(document: dict[str, Any]) -> None:
    """Log each build's state (no secrets) so a wrong state name shows in the run log."""

    included = {(i.get("type"), i.get("id")): i for i in document.get("included", [])}
    for build in document.get("data", []):
        attributes = build.get("attributes", {})
        relationships = build.get("relationships", {})

        def attribute(name: str, key: str) -> Any:
            ref = (relationships.get(name, {}) or {}).get("data")
            item = included.get(((ref or {}).get("type"), (ref or {}).get("id")), {})
            return item.get("attributes", {}).get(key)

        print(
            f"build {attribute('preReleaseVersion', 'version')} ({attributes.get('version')}) "
            f"processing={attributes.get('processingState')} expired={attributes.get('expired')} "
            f"external={attribute('buildBetaDetail', 'externalBuildState')} "
            f"internal={attribute('buildBetaDetail', 'internalBuildState')} "
            f"detail_linked={bool((relationships.get('buildBetaDetail', {}) or {}).get('data'))}"
        )


def find_installable_build(
    client: Any, app_id: str, group_name: str
) -> dict[str, Any] | None:
    groups = client.api_request(
        "GET",
        f"/apps/{app_id}/betaGroups?"
        + urllib.parse.urlencode(
            {"limit": "200", "fields[betaGroups]": "name,publicLinkEnabled,publicLink"}
        ),
    ).get("data", [])
    matches = [g for g in groups if g.get("attributes", {}).get("name") == group_name]
    if len(matches) != 1:
        raise RuntimeError(f"expected exactly one beta group named {group_name!r}; found {len(matches)}")
    group = matches[0]
    # /betaGroups/{id}/builds takes no include, so list the app's builds filtered to the group.
    query = urllib.parse.urlencode(
        {
            "filter[app]": app_id,
            "filter[betaGroups]": group["id"],
            "limit": "200",
            "include": "preReleaseVersion,buildBetaDetail",
            "fields[builds]": "version,expired,processingState,preReleaseVersion,buildBetaDetail",
            "fields[preReleaseVersions]": "version",
            "fields[buildBetaDetails]": "externalBuildState,internalBuildState",
        }
    )
    document = client.api_request("GET", f"/builds?{query}")
    print_build_states(document)
    def external_state(build_id: str) -> str | None:
        detail = client.api_request("GET", f"/builds/{build_id}/buildBetaDetail").get("data") or {}
        state = detail.get("attributes", {}).get("externalBuildState")
        print(f"beta detail lookup for build {build_id}: external={state}")
        return state

    build = select_installable_build(document, external_state)
    if build is None:
        return None
    build["app_id"] = app_id
    link = group.get("attributes", {}).get("publicLink")
    if isinstance(link, str) and PUBLIC_LINK.match(link):
        build["public_link"] = link
    return build


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--app-id", required=True)
    parser.add_argument("--key-id", required=True)
    parser.add_argument("--key-path", required=True, type=pathlib.Path)
    parser.add_argument("--beta-group-name", required=True)
    parser.add_argument("--output", required=True, type=pathlib.Path)
    args = parser.parse_args()
    if not re.match(r"^[1-9]\d*$", args.app_id):
        raise SystemExit("error: --app-id must be numeric")
    client = asc_upload.AppStoreConnectClient(args.key_id, args.key_path)
    build = find_installable_build(client, args.app_id, args.beta_group_name)
    args.output.write_text(json.dumps(build or {}, sort_keys=True) + "\n", encoding="utf-8")
    print(f"Installable TestFlight build: {build['marketing_version']} ({build['build_number']})" if build else "No installable TestFlight build yet")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (KeyError, RuntimeError, TimeoutError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        sys.exit(1)
