#!/usr/bin/env bash
# Verify canonical release identity sources and optional stage layout.
#
# Usage:
#   ./scripts/verify-release-identity.sh
#   ./scripts/verify-release-identity.sh --tag vX.Y.Z
#   ./scripts/verify-release-identity.sh --stage dist-download/vX.Y.Z
#
# The version is client/ui/nsh.json "version"; the Android versionCode and the
# iOS build number follow from it (scripts/lib/release_identity.py). Every
# other identity source (the client's Gradle and Xcode projects, the daemon),
# the tag, the release notes and the stage must agree with them.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

STAGE=""
RELEASE_TAG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --stage) STAGE="${2:?}"; shift 2 ;;
    --tag) RELEASE_TAG="${2:?}"; shift 2 ;;
    -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
    *) echo "error: unknown arg $1" >&2; exit 2 ;;
  esac
done

EXPECTED_PACKAGE="com.daoleno.mewla"
IFS=$'\t' read -r EXPECTED_VERSION EXPECTED_VERSION_CODE EXPECTED_IOS_BUILD_NUMBER < <(
  python3 - "$ROOT" <<'PY'
import sys
from pathlib import Path

root = Path(sys.argv[1])
sys.path.insert(0, str(root / "scripts" / "lib"))
import release_identity

try:
    tracked = release_identity.identity(release_identity.read_version(root))
except release_identity.IdentityError as exc:
    sys.exit(f"error: {exc}")
print(f"{tracked['version']}\t{tracked['version_code']}\t{tracked['ios_build_number']}")
PY
)
[[ -n "${EXPECTED_IOS_BUILD_NUMBER:-}" ]] || {
  echo "error: could not read the tracked release identity" >&2
  exit 1
}
EXPECTED_CERT_FP="C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD"

if [[ -n "$RELEASE_TAG" ]]; then
  [[ "$RELEASE_TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.[1-9][0-9]*)?$ ]] || {
    echo "error: release tag must exactly match vX.Y.Z or vX.Y.Z-beta.N" >&2
    exit 1
  }
  [[ "$RELEASE_TAG" == "v${EXPECTED_VERSION}" ]] || {
    echo "error: release tag $RELEASE_TAG does not match tracked version v${EXPECTED_VERSION}" >&2
    exit 1
  }
  [[ "$(git cat-file -t "refs/tags/$RELEASE_TAG" 2>/dev/null || true)" == "tag" ]] || {
    echo "error: $RELEASE_TAG must be an annotated tag" >&2
    exit 1
  }
  TAG_COMMIT="$(git rev-parse "refs/tags/${RELEASE_TAG}^{commit}")"
  HEAD_COMMIT="$(git rev-parse HEAD)"
  [[ "$TAG_COMMIT" == "$HEAD_COMMIT" ]] || {
    echo "error: checked-out release tag does not resolve to HEAD" >&2
    exit 1
  }
  git show-ref --verify --quiet refs/remotes/origin/main || {
    echo "error: origin/main must be fetched before release tag validation" >&2
    exit 1
  }
  git merge-base --is-ancestor "$HEAD_COMMIT" refs/remotes/origin/main || {
    echo "error: release tag commit is not on origin/main" >&2
    exit 1
  }
  echo "ok: annotated release tag $RELEASE_TAG resolves to HEAD on origin/main"
fi

python3 - "$ROOT" "$EXPECTED_VERSION" "$EXPECTED_PACKAGE" "$EXPECTED_VERSION_CODE" "$EXPECTED_IOS_BUILD_NUMBER" "$EXPECTED_CERT_FP" "$STAGE" <<'PY'
import hashlib
import json
import os
import re
import sys
from pathlib import Path

root = Path(sys.argv[1])
exp_version = sys.argv[2]
exp_package = sys.argv[3]
exp_vc = int(sys.argv[4])
exp_ios_build = int(sys.argv[5])
exp_cert = sys.argv[6]
stage = sys.argv[7] if len(sys.argv) > 7 else ""

errors = []

# Daemon archives and the executable each contains.
DAEMON_ARCHIVE_ENTRIES = {
    "mewla-linux-amd64.tar.gz": "mewla",
    "mewla-linux-arm64.tar.gz": "mewla",
    "mewla-darwin-arm64.tar.gz": "mewla",
    "mewla-windows-amd64.zip": "mewla.exe",
}
DAEMON_TARGETS = ["linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64"]
DAEMON_ARCHIVES = tuple(DAEMON_ARCHIVE_ENTRIES)


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()

# --- tracked identity: the NSH client (client/) ---
exp_marketing = exp_version.split("-", 1)[0]
nsh = json.loads((root / "client/ui/nsh.json").read_text(encoding="utf-8"))
if nsh.get("version") != exp_version:
    errors.append(f"client/ui/nsh.json version: got {nsh.get('version')!r} want {exp_version!r}")
if nsh.get("id") != exp_package:
    errors.append(f"client/ui/nsh.json id: got {nsh.get('id')!r} want {exp_package!r}")

# Android: the production identity is the shipped package; the version code is
# release_identity.py's rule, computed by Gradle from nsh.json.
gradle = (root / "client/android/app/build.gradle.kts").read_text(encoding="utf-8")
for needle, problem in (
    (f'applicationId = "{exp_package}"', f"applicationId must be {exp_package}"),
    (f'namespace = "{exp_package}"', f"namespace must be {exp_package}"),
    ('uiDir.resolve("nsh.json")', "must read the version from client/ui/nsh.json"),
    ("major * 1_000_000 + minor * 10_000 + patch * 100 + (beta ?: 99)", "versionCode must follow scripts/lib/release_identity.py"),
    ("versionCode = mewlaVersionCode", "versionCode must be the derived code"),
    ("versionName = version", "versionName must be the nsh.json version"),
    ('create("production") { dimension = "identity"; resValue', "production flavor must keep the package unsuffixed"),
    ('providers.gradleProperty("mewla.channel")', "must take the -Pmewla.channel=github|play channel"),
    ('if (channel == "github") {', "Phone use and the in-app update must be GitHub-channel only"),
):
    if needle not in gradle:
        errors.append(f"client/android/app/build.gradle.kts: {problem}")
for name in ("MEWLA_ANDROID_KEYSTORE", "MEWLA_ANDROID_KEYSTORE_PASSWORD", "MEWLA_ANDROID_KEY_ALIAS", "MEWLA_ANDROID_KEY_PASSWORD"):
    if f'"{name}"' not in gradle:
        errors.append(f"client/android/app/build.gradle.kts: release signing must read {name}")

# iOS: the shipped bundle id, and the marketing version / build number this release derives.
project = (root / "client/ios/project.yml").read_text(encoding="utf-8")
def ios_setting(name):
    found = re.findall(rf'(?m)^\s+{name}: "?([^"\n]*)"?\s*$', project)
    return found[0] if len(found) == 1 else None
for name, want in (
    ("MEWLA_APP_ID", exp_package),
    ("MARKETING_VERSION", exp_marketing),
    ("CURRENT_PROJECT_VERSION", str(exp_ios_build)),
    ("MEWLA_IOS_DISTRIBUTION", "none"),
):
    got = ios_setting(name)
    if got != want:
        errors.append(f"client/ios/project.yml {name}: got {got!r} want {want!r}")
for needle in ("PRODUCT_BUNDLE_IDENTIFIER: $(MEWLA_APP_ID)\n", "PRODUCT_BUNDLE_IDENTIFIER: $(MEWLA_APP_ID).live\n",
               "group.$(MEWLA_APP_ID)", "MewlaDistribution: $(MEWLA_IOS_DISTRIBUTION)",
               "PROVISIONING_PROFILE_SPECIFIER: $(MEWLA_APP_PROFILE)", "PROVISIONING_PROFILE_SPECIFIER: $(MEWLA_LIVE_PROFILE)"):
    if needle not in project:
        errors.append(f"client/ios/project.yml missing {needle.strip()!r}")

# The oldest computer the app accepts is never newer than the app (prepare-release.py enforces it too).
min_computer = re.search(
    r'export const MIN_COMPUTER_VERSION = "([^"]+)";',
    (root / "client/core/update/version.ts").read_text(encoding="utf-8"),
)
if not min_computer:
    errors.append("client/core/update/version.ts must declare MIN_COMPUTER_VERSION")
else:
    order = lambda v: tuple(int(x) for x in re.match(r"(\d+)\.(\d+)\.(\d+)", v).groups()) + (0 if "-" in v else 1,)
    if order(min_computer.group(1)) > order(exp_version):
        errors.append(f"MIN_COMPUTER_VERSION {min_computer.group(1)} is newer than the release {exp_version}")

version_go = (root / "daemon/cmd/mewla/version.go").read_text(encoding="utf-8")
m = re.search(r'var Version = "([^"]+)"', version_go)
if not m:
    errors.append("daemon/cmd/mewla/version.go: Version default not found")
elif m.group(1) != exp_version:
    errors.append(f"version.go default: got {m.group(1)!r} want {exp_version!r}")

crash_line = ""
for line in (root / "scripts/capture-android-crash.sh").read_text(encoding="utf-8").splitlines():
    if line.startswith("PACKAGE_NAME="):
        crash_line = line
        break
if exp_package not in crash_line:
    errors.append("capture-android-crash.sh default package does not match")

apk_script = (root / "scripts/android-release-apk.sh").read_text(encoding="utf-8")
for needle, problem in (
    (":app:assembleProductionRelease", "must build the client's production APK"),
    (":app:bundleProductionRelease", "must build the client's production AAB with --aab"),
    ('-Pmewla.channel="$CHANNEL" -Pmewla.abis=arm64-v8a', "must pick the channel and build arm64-v8a only"),
    ("versionName", "must assert the APK's versionName/versionCode identity"),
    ("versionCode", "must assert the APK's versionName/versionCode identity"),
):
    if needle not in apk_script:
        errors.append(f"android-release-apk.sh {problem}")
# Local default only; must not force a host path that overrides CI Temurin JAVA_HOME.
if 'JAVA_HOME="${JAVA_HOME:-' not in apk_script and "JAVA_HOME=\"${JAVA_HOME:-" not in apk_script:
    errors.append("android-release-apk.sh must default JAVA_HOME only when unset")
if "verify-android-native-symbols.py" not in apk_script:
    errors.append("android-release-apk.sh must verify packaged Android native symbols")

apk_verifier = (root / "scripts/verify-apk-release.sh").read_text(encoding="utf-8")
if "verify-android-native-symbols.py" not in apk_verifier:
    errors.append("verify-apk-release.sh must verify packaged Android native symbols")


for rel in (
    "scripts/lib/release_identity.py",
    "scripts/stage-release.sh",
    "scripts/build-daemon-linux.sh",
    "scripts/verify-release-identity.sh",
    "scripts/verify-apk-release.sh",
    "scripts/materialize-android-keystore.sh",
    "scripts/sign-release-manifest.sh",
    "release/mewla-update-public-key.pem",
    "docs/ci-release.md",
    ".github/workflows/release-artifacts.yml",
    ".github/actions/android-native/action.yml",
):
    if not (root / rel).is_file():
        errors.append(f"missing required release file: {rel}")

wf = (root / ".github/workflows/release-artifacts.yml").read_text(encoding="utf-8")
if "MEWLA_ANDROID_KEYSTORE_BASE64" not in wf:
    errors.append("release-artifacts.yml must reference MEWLA_ANDROID_KEYSTORE_BASE64")
if "workflow_dispatch" not in wf:
    errors.append("release-artifacts.yml must support workflow_dispatch")
if (
    'tags:' not in wf
    or '"v*.*.*"' not in wf
    or '"v*.*.*-beta.*"' not in wf
):
    errors.append("release-artifacts.yml must trigger from stable and beta tag pushes")
if "types: [published]" in wf or "github.event.release" in wf:
    errors.append("release-artifacts.yml must not depend on a release-published event")
if "type: boolean" not in wf or "needs.validate.outputs.publish == 'true'" not in wf:
    errors.append("manual release recovery must require the reviewed publish boolean")
if "--tag" not in wf or "git fetch --no-tags origin main" not in wf:
    errors.append("release-artifacts.yml must validate tag identity and origin/main ancestry")
if "gh release upload" not in wf:
    errors.append("release-artifacts.yml must upload assets via gh release upload")
if "materialize-android-keystore" not in wf:
    errors.append("release-artifacts.yml must materialize keystore via helper script")
if "-Dorg.gradle.jvmargs=-Xmx6g" not in wf:
    errors.append("release-artifacts.yml must provide enough Gradle heap for release dex merging")
if "uses: ./.github/actions/android-native" not in wf:
    errors.append("release-artifacts.yml must build Android through .github/actions/android-native")
android_native = root / ".github/actions/android-native/action.yml"
native_action = android_native.read_text(encoding="utf-8") if android_native.is_file() else ""
for required in (
    "android-ghostty-output-v2-arm64-",
    "app/modules/terminal-vt/android/src/main/cpp/ghostty",
    "./scripts/verify-libghostty.sh --release",
):
    if required not in native_action:
        errors.append(f"android-native action missing complete native cache contract: {required}")
if "MEWLA_UPDATE_SIGNING_KEY_BASE64" not in wf:
    errors.append("release-artifacts.yml must use the updater manifest signing secret")
for asset in (
    *DAEMON_ARCHIVES,
    "release-manifest.json",
    "release-manifest.json.sig",
):
    if asset not in wf:
        errors.append(f"release-artifacts.yml missing release asset: {asset}")
if "GH_REPO" not in wf or "github.repository" not in wf:
    errors.append("release-artifacts.yml publish path must set GH_REPO from github.repository")
for required in (
    # Desktop first: archives publish without waiting for Android, and the
    # APK joins the same signed asset set when it is ready.
    'needs: [validate, daemon]',
    'needs: [validate, daemon, android, desktop-stage]',
    'needs: [validate, desktop-publish, complete-stage]',
    'RELEASE_IS_PRERELEASE=true',
    'RELEASE_IS_PRERELEASE=false',
    '--prerelease="$RELEASE_IS_PRERELEASE"',
    '--latest="$RELEASE_IS_STABLE"',
):
    if required not in wf:
        errors.append(f"release-artifacts.yml missing gated release contract: {required}")

notes_path = root / f"docs/releases/v{exp_version}.md"
if not notes_path.is_file():
    errors.append(f"missing tracked release notes: {notes_path.relative_to(root)}")
else:
    notes = notes_path.read_text(encoding="utf-8")
    if not notes.startswith(f"# Mewla {exp_version}\n"):
        errors.append("release notes must start with the version heading")
    if exp_cert not in notes:
        errors.append("release notes missing official certificate SHA-256 fingerprint")
    if not re.search(r"^## (New|Fixed)\n\n- ", notes, re.MULTILINE):
        errors.append("release notes missing user-facing ## New or ## Fixed bullets")
    # Windows ships from first_windows on; earlier notes name only the other archives.
    first_windows = (0, 3, 0)
    tracked = tuple(int(x) for x in re.match(r"(\d+)\.(\d+)\.(\d+)", exp_version).groups())
    notes_archives = [a for a in DAEMON_ARCHIVES if tracked >= first_windows or not a.endswith(".zip")]
    for needle in (
        "## Get it",
        "mewla update",
        *notes_archives,
        f"mewla-android-arm64-v{exp_version}.apk",
        "unknown sources",
        "SHA256SUMS",
    ):
        if needle not in notes:
            errors.append(f"release notes missing required topic: {needle!r}")

CLIENT_GHOSTTY_NOTICE = "client/ui/more/settings/licenses/texts/Ghostty-MIT.txt"
for rel in ("LICENSE", "NOTICE", "TRADEMARKS.md", CLIENT_GHOSTTY_NOTICE):
    if not (root / rel).is_file():
        errors.append(f"missing repo legal/notice file: {rel}")
# The client packages its own copy; the libghostty build still copies app/'s next to the libraries.
expo_notice = root / "app/assets/notices/GHOSTTY-MIT.txt"
if expo_notice.is_file() and (root / CLIENT_GHOSTTY_NOTICE).is_file():
    if expo_notice.read_bytes() != (root / CLIENT_GHOSTTY_NOTICE).read_bytes():
        errors.append(f"{CLIENT_GHOSTTY_NOTICE} differs from app/assets/notices/GHOSTTY-MIT.txt")

public_key_pem = root / "release/mewla-update-public-key.pem"
updater_go = root / "daemon/selfupdate/selfupdate.go"
if public_key_pem.is_file() and updater_go.is_file():
    public_der_b64 = "".join(
        line.strip()
        for line in public_key_pem.read_text(encoding="utf-8").splitlines()
        if not line.startswith("-----")
    )
    if public_der_b64 not in updater_go.read_text(encoding="utf-8"):
        errors.append("daemon embedded update key does not match release public key")

# --- optional stage ---
if stage:
    stage_p = Path(stage)
    if ".." in Path(stage).parts:
        errors.append(f"stage path must not contain ..: {stage}")
    if not stage_p.is_dir():
        errors.append(f"stage dir missing: {stage}")
    else:
        required = [
            *DAEMON_ARCHIVES,
            "SHA256SUMS",
            "release-manifest.json",
            "release-manifest.json.sig",
        ]
        for rel in required:
            if not (stage_p / rel).is_file():
                errors.append(f"stage missing {rel}")

        sums_path = stage_p / "SHA256SUMS"
        if sums_path.is_file():
            sums_text = sums_path.read_text(encoding="utf-8")
            for name in DAEMON_ARCHIVES:
                if name not in sums_text:
                    errors.append(f"SHA256SUMS missing {name}")
            # Verify checksums match files
            for line in sums_text.splitlines():
                line = line.strip()
                if not line or line.startswith("#"):
                    continue
                parts = line.split()
                if len(parts) < 2:
                    errors.append(f"bad SHA256SUMS line: {line!r}")
                    continue
                digest, rel = parts[0], parts[-1]
                p = stage_p / rel
                if not p.is_file():
                    errors.append(f"SHA256SUMS entry missing file: {rel}")
                    continue
                got = sha256_file(p)
                if got != digest:
                    errors.append(f"SHA256 mismatch for {rel}: sums={digest[:12]}… file={got[:12]}…")

        ident_path = stage_p / "release-manifest.json"
        if ident_path.is_file():
            ident = json.loads(ident_path.read_text(encoding="utf-8"))
            if ident.get("schema_version") != 2:
                errors.append(f"stage identity schema_version: got {ident.get('schema_version')!r}")
            if ident.get("version") != exp_version:
                errors.append(f"stage identity version: got {ident.get('version')!r}")
            and_id = ident.get("android") or {}
            if and_id.get("package") != exp_package:
                errors.append(f"stage identity package: got {and_id.get('package')!r}")
            if int(and_id.get("version_code", -1)) != exp_vc:
                errors.append("stage identity version_code mismatch")
            if and_id.get("certificate_sha256_fingerprint") != exp_cert:
                errors.append("stage identity missing/wrong certificate fingerprint")
            roles = {a.get("path"): a.get("role") for a in ident.get("artifacts") or []}
            expected_roles = {name: "daemon_archive" for name in DAEMON_ARCHIVES}
            for path, role in expected_roles.items():
                if roles.get(path) != role:
                    errors.append(f"release-manifest.json role for {path}: got {roles.get(path)!r} want {role!r}")
            daemon = ident.get("daemon") or {}
            if daemon.get("targets") != DAEMON_TARGETS:
                errors.append(f"release manifest daemon targets: got {daemon.get('targets')!r}")
            # Every release daemon carries the session host's terminal engine.
            if daemon.get("cgo") != {target: True for target in DAEMON_TARGETS}:
                errors.append(f"release manifest daemon cgo: got {daemon.get('cgo')!r}")

            signature_path = stage_p / "release-manifest.json.sig"
            public_key = root / "release/mewla-update-public-key.pem"
            if signature_path.is_file() and public_key.is_file():
                import subprocess
                verified = subprocess.run(
                    [
                        "openssl", "pkeyutl", "-verify", "-rawin", "-pubin",
                        "-inkey", str(public_key), "-in", str(ident_path),
                        "-sigfile", str(signature_path),
                    ],
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                )
                if verified.returncode != 0:
                    errors.append("release manifest Ed25519 signature verification failed")

        import tarfile
        import zipfile
        for archive, entry in DAEMON_ARCHIVE_ENTRIES.items():
            archive_path = stage_p / archive
            if not archive_path.is_file():
                continue
            if archive.endswith(".zip"):
                with zipfile.ZipFile(archive_path) as zf:
                    names = sorted(zf.namelist())
            else:
                with tarfile.open(archive_path, "r:gz") as tf:
                    names = sorted(member.name.lstrip("./") for member in tf.getmembers() if member.isfile())
            expected = sorted(["LICENSE", "NOTICE", "TRADEMARKS.md", entry])
            if names != expected:
                errors.append(f"{archive} contents: got {names!r} want {expected!r}")


if errors:
    print("FAIL: release identity checks", file=sys.stderr)
    for e in errors:
        print(f"  - {e}", file=sys.stderr)
    raise SystemExit(1)

print(f"ok: identity version={exp_version} package={exp_package} versionCode={exp_vc}")
print(f"ok: certificate fingerprint present in tracked notes")
if stage:
    print(f"ok: stage {stage}")
PY
