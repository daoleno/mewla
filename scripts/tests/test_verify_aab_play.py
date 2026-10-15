import importlib.util
import io
import json
import pathlib
import struct
import unittest
import zipfile


SCRIPT = pathlib.Path(__file__).parents[1] / "verify-aab-play.py"
SPEC = importlib.util.spec_from_file_location("verify_aab_play", SCRIPT)
verify_aab = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(verify_aab)


def manifest(permissions=(), providers=(), target="36", version_code="54"):
    items = "".join(f'<uses-permission android:name="{p}"/>' for p in permissions)
    provs = "".join(
        f'<provider android:name="{name}" android:authorities="{auth}"/>' for name, auth in providers
    )
    return (
        '<manifest xmlns:android="http://schemas.android.com/apk/res/android" '
        f'package="com.daoleno.mewla" android:versionName="0.2.14" android:versionCode="{version_code}">'
        f'<uses-sdk android:minSdkVersion="24" android:targetSdkVersion="{target}"/>{items}'
        f"<application>{provs}</application></manifest>"
    )


def elf64(alignments):
    """Minimal little-endian ELF64 with one PT_LOAD per alignment."""
    phoff, phentsize = 64, 56
    header = bytearray(64)
    header[:6] = b"\x7fELF\x02\x01"
    struct.pack_into("<Q", header, 0x20, phoff)
    struct.pack_into("<HH", header, 0x36, phentsize, len(alignments))
    table = b"".join(
        struct.pack("<IIQQQQQQ", 1, 5, 0, 0, 0, 0, 0, align) for align in alignments
    )
    return bytes(header) + table


def bundle(entries):
    data = io.BytesIO()
    with zipfile.ZipFile(data, "w") as archive:
        for name, content in entries.items():
            archive.writestr(name, content)
    data.seek(0)
    return zipfile.ZipFile(data)


class PlayManifestTests(unittest.TestCase):
    def check(self, xml):
        return verify_aab.check_manifest(xml, "com.daoleno.mewla", "0.2.14", 54)

    def test_clean_play_manifest_passes(self):
        self.assertEqual(self.check(manifest(["android.permission.CAMERA"])), [])

    def test_rejects_self_update_and_overlay_permissions(self):
        errors = self.check(manifest([
            "android.permission.REQUEST_INSTALL_PACKAGES",
            "android.permission.SYSTEM_ALERT_WINDOW",
        ]))
        self.assertEqual(len(errors), 2)

    def test_rejects_update_provider_by_name_or_authority(self):
        self.assertTrue(self.check(manifest(providers=[(verify_aab.UPDATE_PROVIDER, "x")])))
        self.assertTrue(self.check(manifest(providers=[("Other", "com.daoleno.mewla.appupdate")])))
        self.assertEqual(self.check(manifest(providers=[("Other", "com.daoleno.mewla.files")])), [])

    def test_rejects_wrong_target_sdk_and_identity(self):
        self.assertTrue(self.check(manifest(target="35")))
        self.assertTrue(self.check(manifest(version_code="53")))


class NativeLibraryTests(unittest.TestCase):
    def test_reads_load_alignments(self):
        self.assertEqual(verify_aab.elf_load_alignments(elf64([0x4000, 0x10000])), [0x4000, 0x10000])

    def test_accepts_16k_arm64_libraries(self):
        errors, report = verify_aab.check_native_libraries(
            bundle({"base/lib/arm64-v8a/liba.so": elf64([0x4000])})
        )
        self.assertEqual(errors, [])
        self.assertEqual(len(report), 1)

    def test_rejects_4k_alignment_and_other_abis(self):
        errors, _ = verify_aab.check_native_libraries(bundle({
            "base/lib/arm64-v8a/liba.so": elf64([0x1000]),
            "base/lib/x86_64/liba.so": elf64([0x4000]),
        }))
        self.assertEqual(len(errors), 2)


class DistributionTests(unittest.TestCase):
    def test_requires_play_store_channel(self):
        config = lambda channel: json.dumps({"extra": {"distribution": {"android": channel}}})
        self.assertEqual(verify_aab.check_distribution(bundle({"base/assets/app.config": config("play-store")})), [])
        self.assertTrue(verify_aab.check_distribution(bundle({"base/assets/app.config": config("github-apk")})))
        self.assertTrue(verify_aab.check_distribution(bundle({})))


if __name__ == "__main__":
    unittest.main()
