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
LOCK = ROOT / "native/terminal-vt/native.lock.json"


def load(name):
    spec = importlib.util.spec_from_file_location(name, LIB / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


minisign = load("minisign_verify")
release = load("ghostty_release")

# A real upstream signature: the SHA256SUMS of libghostty-vt 1.0.0 and the minisign
# signature Ghostty published for it, checked against Ghostty's release key.
UPSTREAM_KEY = "RWQlAjJC23149WL2sEpT/l0QKy7hMIFhYdQOFy0Z7z7PbneUgvlsnYcV"
SUMS = (
    "4a6ef758be721923e1969e7b81b443dcc92d8dd3fb5a39600c03a8817a753676  libghostty-vt-1.0.0.tar.gz\n"
    "4d5ea5e448e4c7ea8fb55ccbaa8668f2a97d44f438cee831e2b7ecd162d85c29  libghostty-vt-1.0.0-deps.tar.gz\n"
    "2c32b7409b2b6c2ef6cfd606526de357762ad458bdc27134ad074e86506a8bc2  ghostty-vt-1.0.0.xcframework.zip\n"
    "7b428fee6d0677bc81279458f8f9a3335454834123a9ff47efb3fd177f6da5a8  ghostty-vt-1.0.0.wasm\n"
    "dad8e14ce8da57db3864f66ae36caf66a17c4e2691ad0a0110e05c746de047a4  ghostty-vt-1.0.0-small.wasm\n"
    "bc07fc750e5f7c3e0aea4c93b17c8f42f243aea15a962ac431c454fdfc78088c  libghostty-vt-1.0.0-docs.zip\n"
)
SUMS_SIG = (
    "untrusted comment: signature from minisign secret key\n"
    "RUQlAjJC23149UBB58VZlVqXptxuhLKr3VWMqcmUZvA60oVXjuF6WnmBTyz0XmoVEtrNdNLqi5o5fNetvO5/m+x2dAegXZ32Vw4=\n"
    "trusted comment: timestamp:1794007216\tfile:SHA256SUMS\thashed\n"
    "JEx2GEejbXT8doLtmJaU0HZJ7cncMbmH/XlQLKh5clzBdA1BbhohpSrFMa/d/N6O7zknupPaq0z2vClLKPJ7Bg==\n"
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
