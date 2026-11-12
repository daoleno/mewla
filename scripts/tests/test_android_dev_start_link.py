"""The dev start link (mewla-dev://start?server=…, DevStartActivity) is in the debug build only.

A release variant merges src/main, its flavor's and src/release, never src/debug: so the link must be
declared in src/debug/AndroidManifest.xml and nowhere else, and its class must live under src/debug.
"""

from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]
SRC = ROOT / "client/android/app/src"


class DevStartLinkIsDebugOnly(unittest.TestCase):
    def test_only_the_debug_manifest_declares_it(self):
        manifests = sorted(SRC.glob("*/AndroidManifest.xml"))
        self.assertIn(SRC / "debug/AndroidManifest.xml", manifests)
        for manifest in manifests:
            text = manifest.read_text()
            declares = "DevStartActivity" in text or "mewla-dev" in text
            with self.subTest(manifest=str(manifest.relative_to(ROOT))):
                self.assertEqual(declares, manifest.parent.name == "debug")

    def test_its_class_is_compiled_only_into_debug(self):
        sources = [path for path in SRC.rglob("*.kt") if "class DevStartActivity" in path.read_text()]
        self.assertTrue(sources)
        for path in sources:
            self.assertEqual(path.relative_to(SRC).parts[0], "debug", str(path))

    def test_the_app_takes_a_dev_server_only_through_the_confirmed_link(self):
        # One way in: no extra that names a server (any app can start the exported MainActivity), only the
        # token DevStartActivity gets for the server the person confirmed, and only in a debug build.
        main = (SRC / "main/java/com/daoleno/mewla/MainActivity.kt").read_text()
        self.assertIn("if (BuildConfig.DEBUG) DevLaunch.serverFor(", main)
        for path in SRC.rglob("*.kt"):
            self.assertNotIn('"devServer"', path.read_text(), str(path))
        start = (SRC / "debug/java/com/daoleno/mewla/DevStartActivity.kt").read_text()
        self.assertLess(start.index(".setPositiveButton("), start.index("DevLaunch.confirm("))


if __name__ == "__main__":
    unittest.main()
