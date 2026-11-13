"""The Dev app's live start (DevBoot, its built-in server, the start link DevStartActivity) is in the debug build only.

A release variant merges src/main, its flavor's and src/release, never src/debug: so the link must be
declared in src/debug/AndroidManifest.xml and nowhere else, its classes must live under src/debug, the
release DevBoot does nothing, and only the debug build type is given a dev server.
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
        sources = [path for path in SRC.rglob("*.kt") if any(c in path.read_text() for c in ("class DevStartActivity", "object DevLaunch"))]
        self.assertEqual(len(sources), 2)
        for path in sources:
            self.assertEqual(path.relative_to(SRC).parts[0], "debug", str(path))

    def test_the_app_takes_a_dev_server_only_once_the_person_confirmed_it(self):
        # No extra that names a server (any app can start the exported MainActivity): the start link hands
        # MainActivity only the token DevStartActivity gets once the person tapped Load, and DevBoot connects
        # to a remembered or built-in server only after its own Load.
        for path in SRC.rglob("*.kt"):
            self.assertNotIn('"devServer"', path.read_text(), str(path))
        start = (SRC / "debug/java/com/daoleno/mewla/DevStartActivity.kt").read_text()
        self.assertLess(start.index("DevBoot.confirmDialog("), start.index("DevLaunch.confirm("))
        boot = (SRC / "debug/java/com/daoleno/mewla/DevBoot.kt").read_text()
        dialog = boot.index("confirmDialog(activity, server")
        self.assertLess(boot.index("if (server in confirmed(prefs))"), dialog)
        self.assertLess(dialog, boot.index("remember(prefs, server)\n      connectIfAnswering"))

    def test_a_release_build_has_no_trace_of_it(self):
        main = (SRC / "main/java/com/daoleno/mewla/MainActivity.kt").read_text()
        self.assertIn('if (!DevBoot.start(this, nsh, intent)) nsh.loadAssetBundle("nsh")', main)
        # Everything that knows a dev server is under src/debug.
        for path in SRC.rglob("*.kt"):
            if path.relative_to(SRC).parts[0] == "debug":
                continue
            text = path.read_text()
            for trace in ("connectDevServer", "DEV_SERVER", "DevLaunch", "mewla-dev", "ws://", "getSharedPreferences(\"mewla-dev"):
                with self.subTest(path=str(path.relative_to(ROOT)), trace=trace):
                    self.assertNotIn(trace, text)
        release = (SRC / "release/java/com/daoleno/mewla/DevBoot.kt").read_text()
        self.assertIn("Boolean = false", release)
        # Only the debug build type is given a dev server, and no address is in the source.
        gradle = (ROOT / "client/android/app/build.gradle.kts").read_text()
        field = gradle.index('buildConfigField("String", "DEV_SERVER"')
        self.assertEqual(gradle.count('"DEV_SERVER"'), 1)
        self.assertLess(gradle.index('getByName("debug")'), field)
        self.assertLess(field, gradle.index('getByName("release")'))
        for path in [*SRC.rglob("*.kt"), ROOT / "client/android/app/build.gradle.kts", ROOT / "client/scripts/dev-android.sh"]:
            self.assertNotRegex(path.read_text(), r"ws://\d", str(path))


if __name__ == "__main__":
    unittest.main()
