#!/usr/bin/env python3
"""Fetch, verify and unpack the libghostty-vt release pinned in native.lock.json.

Usage:
  ghostty_release.py LOCK prepare --cache DIR --dest DIR [--release-dir DIR]
  ghostty_release.py LOCK headers-digest INCLUDE_GHOSTTY_DIR

`prepare` downloads the signed source tarball and the companion dependency
tarball into --cache (or takes them from --release-dir for an offline build),
checks both against the sha256 values in the lock, checks the minisign
signatures when a verifier is available, unpacks them into the empty --dest
directory and prints one JSON object describing the result. The sha256 values
in the lock are the hard gate: they were taken from a minisign-verified release.
The signature check is defense in depth, and it is skipped (and reported as
such) on a host with neither `minisign` nor Python `cryptography`.
"""
import argparse
import hashlib
import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tarfile
import urllib.request
from pathlib import Path

HERE = Path(__file__).resolve().parent


def die(message: str) -> "None":
    sys.exit(f"error: {message}")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def headers_digest(include_dir: Path) -> str:
    """sha256 over `<sha256>  <relative path>\\n` for every header, sorted by the path string."""
    files = {
        path.relative_to(include_dir).as_posix(): path
        for path in include_dir.rglob("*")
        if path.is_file()
    }
    lines = [f"{sha256_file(files[name])}  {name}\n" for name in sorted(files)]
    if not lines:
        die(f"no headers under {include_dir}")
    return hashlib.sha256("".join(lines).encode()).hexdigest()


def load_minisign():
    spec = importlib.util.spec_from_file_location("minisign_verify", HERE / "minisign_verify.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def download(url: str, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    part = dest.with_name(dest.name + ".part")
    last = None
    for attempt in range(3):
        try:
            # The release host's CDN rejects urllib's default User-Agent with a 403.
            request = urllib.request.Request(url, headers={"User-Agent": "curl/8 mewla-build"})
            with urllib.request.urlopen(request, timeout=60) as response, open(part, "wb") as out:
                shutil.copyfileobj(response, out)
            os.replace(part, dest)
            return
        except OSError as exc:  # URLError and socket errors both derive from OSError
            last = exc
    die(f"could not download {url}: {last}")


def fetch_artifact(release: dict, key: str, cache: Path, release_dir: Path | None) -> Path:
    spec = release[key]
    name = spec["file"]
    path = cache / name
    sig = cache / (name + ".minisig")
    if release_dir is not None:
        for candidate in (name, name + ".minisig"):
            source = release_dir / candidate
            if not source.is_file():
                die(f"{source} is missing from --release-dir")
            shutil.copyfile(source, cache / candidate)
    else:
        if not path.is_file() or sha256_file(path) != spec["sha256"]:
            print(f"Downloading {release['base_url']}/{name}", file=sys.stderr)
            download(f"{release['base_url']}/{name}", path)
        if not sig.is_file():
            download(f"{release['base_url']}/{name}.minisig", sig)
    actual = sha256_file(path)
    if actual != spec["sha256"]:
        path.unlink()
        die(f"{name} sha256 {actual} != lock {spec['sha256']}")
    return path


def check_signature(release: dict, artifact: Path) -> bool:
    sig = artifact.with_name(artifact.name + ".minisig")
    key = release["minisign_public_key"]
    if shutil.which("minisign"):
        result = subprocess.run(
            ["minisign", "-V", "-P", key, "-m", str(artifact), "-x", str(sig)],
            capture_output=True,
            text=True,
        )
        if result.returncode != 0:
            die(f"minisign rejected {artifact.name}: {result.stderr.strip()}")
        return True
    module = load_minisign()
    if module.available():
        try:
            module.verify(key, str(artifact), str(sig))
        except module.SignatureError as exc:
            die(f"minisign rejected {artifact.name}: {exc}")
        return True
    return False


def extract(archive: Path, dest: Path, root_name: str) -> Path:
    dest.mkdir(parents=True, exist_ok=False)
    base = dest.resolve()
    with tarfile.open(archive, "r:gz") as tar:
        members = tar.getmembers()
        for member in members:
            target = (base / member.name).resolve()
            if not target.is_relative_to(base):
                die(f"{archive.name}: member escapes the extraction directory: {member.name}")
            if member.issym() or member.islnk():
                link = (target.parent / member.linkname).resolve()
                if not link.is_relative_to(base):
                    die(f"{archive.name}: link escapes the extraction directory: {member.name}")
            if not (member.isfile() or member.isdir() or member.issym() or member.islnk()):
                die(f"{archive.name}: unsupported member type: {member.name}")
        try:
            tar.extractall(base, members=members, filter="data")
        except TypeError:  # Python without extraction filters; members were checked above
            tar.extractall(base, members=members)
    root = base / root_name
    if not root.is_dir():
        die(f"{archive.name} does not unpack to {root_name}/")
    return root


def prepare(lock: dict, args: argparse.Namespace) -> dict:
    ghostty = lock["ghostty"]
    release = ghostty["release"]
    cache = Path(args.cache)
    cache.mkdir(parents=True, exist_ok=True)
    release_dir = Path(args.release_dir) if args.release_dir else None

    source_tar = fetch_artifact(release, "source", cache, release_dir)
    deps_tar = fetch_artifact(release, "deps", cache, release_dir)
    verified = [check_signature(release, source_tar), check_signature(release, deps_tar)]
    if not all(verified):
        print(
            "note: minisign signatures not checked (no minisign binary or Python "
            "'cryptography'); the sha256 pins in the lock were taken from a verified release",
            file=sys.stderr,
        )

    dest = Path(args.dest)
    source_dir = extract(source_tar, dest / "source", release["source"]["archive_root"])
    deps_dir = extract(deps_tar, dest / "deps", release["deps"]["archive_root"])

    version = (source_dir / "LIB_VERSION").read_text().strip()
    if version != ghostty["version"]:
        die(f"tarball LIB_VERSION {version!r} != lock version {ghostty['version']!r}")
    digest = headers_digest(source_dir / "include" / "ghostty")
    if digest != ghostty["headers_sha256"]:
        die(f"headers digest {digest} != lock {ghostty['headers_sha256']}")
    license_sha = sha256_file(source_dir / "LICENSE")
    if license_sha != ghostty["license_sha256"]:
        die(f"LICENSE sha256 {license_sha} != lock {ghostty['license_sha256']}")

    return {
        "source_dir": str(source_dir),
        "deps_dir": str(deps_dir),
        "version": version,
        "source_sha256": release["source"]["sha256"],
        "deps_sha256": release["deps"]["sha256"],
        "signature_verified": all(verified),
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("lock")
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("prepare")
    p.add_argument("--cache", required=True)
    p.add_argument("--dest", required=True)
    p.add_argument("--release-dir")
    d = sub.add_parser("headers-digest")
    d.add_argument("include_dir")
    args = parser.parse_args()

    if args.command == "headers-digest":
        print(headers_digest(Path(args.include_dir)))
        return
    lock = json.loads(Path(args.lock).read_text(encoding="utf-8"))
    print(json.dumps(prepare(lock, args)))


if __name__ == "__main__":
    main()
