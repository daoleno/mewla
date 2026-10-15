#!/usr/bin/env python3
"""Verify a signed Google Play App Bundle against the Play channel contract.

Checks, all on the built AAB (never on the prebuild tree):
  - bundletool validate
  - merged base manifest: package / versionName / versionCode from
    app/app.base.json, targetSdk 36, no REQUEST_INSTALL_PACKAGES,
    SYSTEM_ALERT_WINDOW or RECORD_AUDIO, no self-update FileProvider
  - embedded Expo config: extra.distribution.android == "play-store", so the
    GitHub update check never runs (services/useAppUpdate.ts)
  - arm64-v8a native libraries only; every .so has 16 KB ELF LOAD alignment
  - every split APK bundletool generates passes `zipalign -c -P 16`
  - the universal APK carries the Ghostty notice and no forbidden imports
  - upload certificate SHA-256 is the public release identity, so Play App
    Signing with the existing key keeps GitHub and Play installs compatible

Requires: java, keytool, a pinned bundletool jar, Android build-tools >= 35
(zipalign -P) and readelf (for the Ghostty import check).

Usage:
  ./scripts/verify-aab-play.py --bundletool bundletool-all.jar app.aab
"""

from __future__ import annotations

import argparse
import json
import os
import re
import struct
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
EXPECTED_CERT = "C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD"
TARGET_SDK = 36
PAGE_ALIGN = 16 * 1024
ALLOWED_ABIS = {"arm64-v8a"}
FORBIDDEN_PERMISSIONS = (
    "android.permission.REQUEST_INSTALL_PACKAGES",
    "android.permission.SYSTEM_ALERT_WINDOW",
    "android.permission.RECORD_AUDIO",
)
UPDATE_PROVIDER = "expo.modules.appupdate.AppUpdateFileProvider"
ANDROID_NS = "{http://schemas.android.com/apk/res/android}"
LIB_RE = re.compile(r"^[^/]+/lib/([^/]+)/[^/]+\.so$")


def android_attr(element: ET.Element, name: str) -> str | None:
    return element.get(f"{ANDROID_NS}{name}")


def check_manifest(xml_text: str, package: str, version_name: str, version_code: int) -> list[str]:
    """Return contract violations in the merged base manifest."""
    errors: list[str] = []
    root = ET.fromstring(xml_text)
    if root.get("package") != package:
        errors.append(f"package: got {root.get('package')!r} want {package!r}")
    if android_attr(root, "versionName") != version_name:
        errors.append(f"versionName: got {android_attr(root, 'versionName')!r} want {version_name!r}")
    if android_attr(root, "versionCode") != str(version_code):
        errors.append(f"versionCode: got {android_attr(root, 'versionCode')!r} want {version_code}")

    sdk = root.find("uses-sdk")
    target = android_attr(sdk, "targetSdkVersion") if sdk is not None else None
    if target != str(TARGET_SDK):
        errors.append(f"targetSdkVersion: got {target!r} want {TARGET_SDK}")

    permissions = {
        android_attr(item, "name")
        for tag in ("uses-permission", "uses-permission-sdk-23")
        for item in root.findall(tag)
    }
    for permission in FORBIDDEN_PERMISSIONS:
        if permission in permissions:
            errors.append(f"forbidden permission in Play manifest: {permission}")

    for provider in root.iter("provider"):
        name = android_attr(provider, "name") or ""
        authorities = android_attr(provider, "authorities") or ""
        if name == UPDATE_PROVIDER or any(a.endswith(".appupdate") for a in authorities.split(";")):
            errors.append(f"self-update FileProvider in Play manifest: {name} ({authorities})")
    return errors


def manifest_summary(xml_text: str) -> dict[str, object]:
    root = ET.fromstring(xml_text)
    sdk = root.find("uses-sdk")
    return {
        "package": root.get("package"),
        "versionName": android_attr(root, "versionName"),
        "versionCode": android_attr(root, "versionCode"),
        "minSdk": android_attr(sdk, "minSdkVersion") if sdk is not None else None,
        "targetSdk": android_attr(sdk, "targetSdkVersion") if sdk is not None else None,
        "permissions": sorted(
            android_attr(item, "name") or ""
            for tag in ("uses-permission", "uses-permission-sdk-23")
            for item in root.findall(tag)
        ),
    }


def elf_load_alignments(data: bytes) -> list[int]:
    """Return p_align of every PT_LOAD segment in a 32- or 64-bit ELF."""
    if data[:4] != b"\x7fELF":
        raise ValueError("not an ELF file")
    is64 = data[4] == 2
    endian = "<" if data[5] == 1 else ">"
    if is64:
        phoff, = struct.unpack_from(endian + "Q", data, 0x20)
        phentsize, phnum = struct.unpack_from(endian + "HH", data, 0x36)
    else:
        phoff, = struct.unpack_from(endian + "I", data, 0x1C)
        phentsize, phnum = struct.unpack_from(endian + "HH", data, 0x2A)
    alignments = []
    for index in range(phnum):
        offset = phoff + index * phentsize
        p_type, = struct.unpack_from(endian + "I", data, offset)
        if p_type != 1:  # PT_LOAD
            continue
        if is64:
            p_align, = struct.unpack_from(endian + "Q", data, offset + 0x30)
        else:
            p_align, = struct.unpack_from(endian + "I", data, offset + 0x1C)
        alignments.append(p_align)
    if not alignments:
        raise ValueError("ELF has no PT_LOAD segments")
    return alignments


def check_native_libraries(bundle: zipfile.ZipFile) -> tuple[list[str], list[str]]:
    """Return (errors, report lines) for ABI set and 16 KB ELF alignment."""
    errors: list[str] = []
    report: list[str] = []
    abis: set[str] = set()
    for name in sorted(bundle.namelist()):
        match = LIB_RE.match(name)
        if not match:
            continue
        abis.add(match.group(1))
        try:
            alignments = elf_load_alignments(bundle.read(name))
        except (ValueError, struct.error) as exc:
            errors.append(f"{name}: {exc}")
            continue
        low = min(alignments)
        report.append(f"{name} LOAD align min=0x{low:x}")
        if low < PAGE_ALIGN or low % PAGE_ALIGN:
            errors.append(f"{name}: LOAD alignment 0x{low:x} is not 16 KB aligned")
    if not abis & ALLOWED_ABIS:
        errors.append(f"missing arm64-v8a native libraries (found {sorted(abis)})")
    if abis - ALLOWED_ABIS:
        errors.append(f"unexpected ABIs (Play build is arm64-only): {sorted(abis - ALLOWED_ABIS)}")
    return errors, report


def check_distribution(bundle: zipfile.ZipFile) -> list[str]:
    try:
        config = json.loads(bundle.read("base/assets/app.config"))
    except KeyError:
        return ["embedded Expo config base/assets/app.config is missing"]
    channel = config.get("extra", {}).get("distribution", {}).get("android")
    if channel != "play-store":
        return [f"embedded extra.distribution.android: got {channel!r} want 'play-store'"]
    return []


def normalize_fingerprint(value: str) -> str:
    hexonly = re.sub(r"[^0-9A-Fa-f]", "", value).upper()
    return ":".join(hexonly[i : i + 2] for i in range(0, len(hexonly), 2)) if len(hexonly) == 64 else ""


def check_certificate(aab: Path) -> list[str]:
    out = subprocess.run(
        ["keytool", "-printcert", "-jarfile", str(aab)], capture_output=True, text=True
    ).stdout
    fingerprints = [normalize_fingerprint(m) for m in re.findall(r"SHA256:\s*([0-9A-Fa-f:]+)", out)]
    fingerprints = [fp for fp in fingerprints if fp]
    if not fingerprints:
        return ["AAB is not signed (keytool found no SHA-256 certificate)"]
    if fingerprints != [EXPECTED_CERT]:
        return [f"upload certificate SHA-256 mismatch: want {EXPECTED_CERT}, got {fingerprints}"]
    return []


def find_build_tool(name: str) -> str:
    sdk = os.environ.get("ANDROID_HOME") or os.environ.get("ANDROID_SDK_ROOT") or ""
    candidates = sorted(Path(sdk, "build-tools").glob(f"*/{name}"), key=lambda p: [
        int(part) if part.isdigit() else 0 for part in p.parent.name.split(".")
    ]) if sdk else []
    if candidates:
        return str(candidates[-1])
    raise SystemExit(f"error: {name} not found under $ANDROID_HOME/build-tools")


def run(cmd: list[str], **kwargs) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, check=True, **kwargs)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("aab", type=Path)
    parser.add_argument("--bundletool", type=Path, required=True, help="pinned bundletool-all jar")
    args = parser.parse_args()
    aab = args.aab.resolve()
    if not aab.is_file():
        raise SystemExit(f"error: AAB not found: {aab}")

    base = json.loads((ROOT / "app/app.base.json").read_text(encoding="utf-8"))["expo"]
    package = base["android"]["package"]
    version_name = base["version"]
    version_code = int(base["android"]["versionCode"])
    bundletool = ["java", "-jar", str(args.bundletool)]
    zipalign = find_build_tool("zipalign")
    errors: list[str] = []

    run(bundletool + ["validate", f"--bundle={aab}"], stdout=subprocess.DEVNULL)
    print("ok: bundletool validate")

    manifest = run(bundletool + ["dump", "manifest", f"--bundle={aab}"], capture_output=True, text=True).stdout
    errors += check_manifest(manifest, package, version_name, version_code)
    print("manifest:", json.dumps(manifest_summary(manifest), indent=2))

    with zipfile.ZipFile(aab) as bundle:
        errors += check_distribution(bundle)
        lib_errors, lib_report = check_native_libraries(bundle)
        errors += lib_errors
        print("\n".join(lib_report))
    errors += check_certificate(aab)

    with tempfile.TemporaryDirectory(prefix="aab-play.") as tmp:
        tmpdir = Path(tmp)
        # Split APKs as Play would serve them: uncompressed .so must sit on 16 KB.
        splits = tmpdir / "splits.apks"
        run(bundletool + ["build-apks", f"--bundle={aab}", f"--output={splits}"], stdout=subprocess.DEVNULL)
        with zipfile.ZipFile(splits) as archive:
            archive.extractall(tmpdir / "splits")
        split_apks = sorted((tmpdir / "splits").rglob("*.apk"))
        if not split_apks:
            errors.append("bundletool generated no split APKs")
        for apk in split_apks:
            result = subprocess.run([zipalign, "-c", "-P", "16", "-v", "4", str(apk)], capture_output=True, text=True)
            name = apk.relative_to(tmpdir / "splits")
            if result.returncode != 0:
                errors.append(f"zipalign -c -P 16 failed for {name}:\n{result.stdout[-2000:]}{result.stderr[-2000:]}")
            else:
                print(f"ok: zipalign -c -P 16 {name}")

        universal = tmpdir / "universal.apks"
        run(bundletool + ["build-apks", f"--bundle={aab}", f"--output={universal}", "--mode=universal"], stdout=subprocess.DEVNULL)
        with zipfile.ZipFile(universal) as archive:
            archive.extract("universal.apk", tmpdir)
        apk = tmpdir / "universal.apk"
        for cmd in (
            [str(ROOT / "scripts/verify-apk-notice.sh"), str(apk)],
            [str(ROOT / "scripts/verify-android-native-symbols.py"), "--lock",
             str(ROOT / "app/modules/terminal-vt/native.lock.json"), "--apk", str(apk)],
        ):
            if subprocess.run(cmd).returncode != 0:
                errors.append(f"universal APK check failed: {Path(cmd[0]).name}")

    if errors:
        print("FAIL: Play AAB verification", file=sys.stderr)
        for error in errors:
            print(f"  - {error}", file=sys.stderr)
        return 1
    print(f"ok: AAB package={package} versionName={version_name} versionCode={version_code} targetSdk={TARGET_SDK}")
    print("ok: no REQUEST_INSTALL_PACKAGES / SYSTEM_ALERT_WINDOW / RECORD_AUDIO, no update FileProvider")
    print("ok: embedded distribution play-store (no GitHub update check)")
    print("ok: arm64-v8a only; every .so 16 KB LOAD-aligned; split APKs pass zipalign -P 16")
    print("ok: upload certificate SHA-256 matches public identity")
    return 0


if __name__ == "__main__":
    sys.exit(main())
