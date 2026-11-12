"""Tests for the signed libghostty-vt release helpers."""
import hashlib
import importlib.util
import io
import json
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
LIB = ROOT / "scripts/lib"
LOCK = ROOT / "app/modules/terminal-vt/native.lock.json"


def load(name):
    spec = importlib.util.spec_from_file_location(name, LIB / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


minisign = load("minisign_verify")
release = load("ghostty_release")

# A real upstream signature: the SHA256SUMS of libghostty-vt 1.0.1 and the minisign
# signature Ghostty published for it, checked against Ghostty's release key.
UPSTREAM_KEY = "RWQlAjJC23149WL2sEpT/l0QKy7hMIFhYdQOFy0Z7z7PbneUgvlsnYcV"
SUMS = (
    "333dfce97641ef9b536bb9e08907ad86abaaada50e8f7417b70d9284adef37aa  libghostty-vt-1.0.1.tar.gz\n"
    "91a4e10960a52bf5bf4b15aa2a2cddaff8a89d6cd903d3055bda4fd17de3356d  libghostty-vt-1.0.1-deps.tar.gz\n"
    "c0f9631277f726154c8f521954d17cdc41aac4626fc0df8cf925a19ee58e829b  ghostty-vt-1.0.1.xcframework.zip\n"
    "2ee8434a58866aa3ce43cebbbf284d684f5f5b64c4f6040fa031cfe789e11646  ghostty-vt-1.0.1.wasm\n"
    "e226900d2126faab515668e5400b69fc78ba3d99aaf9525b325f79707b29bf01  ghostty-vt-1.0.1-small.wasm\n"
    "8e2eb9e1822e85d786b456c8beb86f49d09c318c91f0c2913f91e7ffa12cda29  ghostty-vt-1.0.1-x86_64-windows-gnu.zip\n"
    "cc61da5f968ea0b08639bdf019c6b7da46bad1d985774fd8a2de57af32862892  ghostty-vt-1.0.1-aarch64-windows-gnu.zip\n"
    "930cea515569eec23f167415e56db819cbbc3a3677b358ee5be5247fbb7eee23  libghostty-vt-1.0.1-docs.zip\n"
)
SUMS_SIG = (
    "untrusted comment: signature from minisign secret key\n"
    "RUQlAjJC23149Yn0WeSD2yQBWdZf6rWbBpBw9ku2OXpOBEETFAt4brhTE03zgJ3fJ4h5ja6+PAmVPFotQ3e3KhC5kSE4hlTengg=\n"
    "trusted comment: timestamp:1794432462\tfile:SHA256SUMS\thashed\n"
    "gO83ch6lAMyVxjDn4b3rt2tg+SFmns1miufnJdMae8m5p8PMJsXi+qu3QJ0zxc92yZ80VqvKpZIrsxC1AlcIAg==\n"
)


class LockTests(unittest.TestCase):
    def test_lock_matches_the_signed_release_sums(self):
        lock = json.loads(LOCK.read_text())["ghostty"]["release"]
        sums = dict(line.split("  ")[::-1] for line in SUMS.splitlines())
        for kind in ("source", "deps"):
            self.assertEqual(sums[lock[kind]["file"]], lock[kind]["sha256"])
        self.assertEqual(lock["minisign_public_key"], UPSTREAM_KEY)


@unittest.skipUnless(minisign.available(), "python 'cryptography' is not installed")
class MinisignTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self.tmp.name)
        (self.dir / "SHA256SUMS").write_text(SUMS)
        (self.dir / "SHA256SUMS.minisig").write_text(SUMS_SIG)

    def tearDown(self):
        self.tmp.cleanup()

    def test_accepts_the_upstream_signature(self):
        comment = minisign.verify(UPSTREAM_KEY, str(self.dir / "SHA256SUMS"), str(self.dir / "SHA256SUMS.minisig"))
        self.assertIn("file:SHA256SUMS", comment)

    def test_rejects_a_modified_file(self):
        (self.dir / "SHA256SUMS").write_text(SUMS + "\n")
        with self.assertRaises(minisign.SignatureError):
            minisign.verify(UPSTREAM_KEY, str(self.dir / "SHA256SUMS"), str(self.dir / "SHA256SUMS.minisig"))

    def test_rejects_another_key(self):
        other = "RWQlAjJC23149WL2sEpT/l0QKy7hMIFhYdQOFy0Z7z7PbneUgvlsnYcW"
        with self.assertRaises(Exception):
            minisign.verify(other, str(self.dir / "SHA256SUMS"), str(self.dir / "SHA256SUMS.minisig"))


class ReleaseTests(unittest.TestCase):
    def test_headers_digest_covers_names_and_contents(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "vt").mkdir()
            (root / "vt.h").write_text("a")
            (root / "vt" / "x.h").write_text("b")
            first = release.headers_digest(root)
            expected = hashlib.sha256(
                (
                    hashlib.sha256(b"a").hexdigest() + "  vt.h\n"
                    + hashlib.sha256(b"b").hexdigest() + "  vt/x.h\n"
                ).encode()
            ).hexdigest()
            self.assertEqual(first, expected)
            (root / "vt" / "x.h").write_text("c")
            self.assertNotEqual(release.headers_digest(root), first)

    def test_extract_rejects_members_that_escape(self):
        with tempfile.TemporaryDirectory() as tmp:
            archive = Path(tmp) / "bad.tar.gz"
            with tarfile.open(archive, "w:gz") as tar:
                data = b"x"
                info = tarfile.TarInfo("../escape")
                info.size = len(data)
                tar.addfile(info, io.BytesIO(data))
            with self.assertRaises(SystemExit):
                release.extract(archive, Path(tmp) / "out", "root")
            self.assertFalse((Path(tmp) / "escape").exists())

    def test_extract_requires_the_named_root(self):
        with tempfile.TemporaryDirectory() as tmp:
            archive = Path(tmp) / "ok.tar.gz"
            with tarfile.open(archive, "w:gz") as tar:
                info = tarfile.TarInfo("other/file")
                info.size = 1
                tar.addfile(info, io.BytesIO(b"x"))
            with self.assertRaises(SystemExit):
                release.extract(archive, Path(tmp) / "out", "root")

    def test_prepare_refuses_a_tarball_that_does_not_match_the_lock(self):
        lock = json.loads(LOCK.read_text())
        with tempfile.TemporaryDirectory() as tmp:
            offline = Path(tmp) / "offline"
            offline.mkdir()
            for kind in ("source", "deps"):
                name = lock["ghostty"]["release"][kind]["file"]
                (offline / name).write_bytes(b"not the release")
                (offline / (name + ".minisig")).write_text("")
            result = subprocess.run(
                [sys.executable, str(LIB / "ghostty_release.py"), str(LOCK), "prepare",
                 "--cache", str(Path(tmp) / "cache"), "--dest", str(Path(tmp) / "dest"),
                 "--release-dir", str(offline)],
                capture_output=True, text=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("sha256", result.stderr)


if __name__ == "__main__":
    unittest.main()
