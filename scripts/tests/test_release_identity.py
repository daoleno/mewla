import importlib.util
import json
import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("release_identity", ROOT / "scripts/lib/release_identity.py")
release_identity = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release_identity)

# What shipped from the Expo app (app/app.base.json, app/ios-build.json at 0.3.1).
EXPO_VERSION_CODE = 142
EXPO_IOS_BUILD = 140


class BuildCodeTests(unittest.TestCase):
    def test_the_rule(self):
        self.assertEqual(release_identity.build_code("0.3.1"), 30199)
        self.assertEqual(release_identity.build_code("0.3.2"), 30299)
        self.assertEqual(release_identity.build_code("0.3.2-beta.1"), 30201)
        self.assertEqual(release_identity.build_code("1.2.3"), 1020399)
        self.assertEqual(release_identity.marketing_version("0.3.2-beta.4"), "0.3.2")

    def test_grows_with_semver_precedence(self):
        ordered = ["0.3.1", "0.3.2-beta.1", "0.3.2-beta.2", "0.3.2", "0.3.3-beta.1", "0.4.0-beta.1", "0.4.0", "1.0.0"]
        codes = [release_identity.build_code(v) for v in ordered]
        self.assertEqual(codes, sorted(codes))
        self.assertEqual(len(set(codes)), len(codes))

    def test_starts_above_every_expo_build(self):
        code = release_identity.build_code(release_identity.read_version(ROOT))
        self.assertGreater(code, EXPO_VERSION_CODE)
        self.assertGreater(code, EXPO_IOS_BUILD)

    def test_rejects_versions_without_a_code(self):
        for version in ("0.3", "v0.3.1", "0.3.1-rc.1", "0.100.0", "0.3.100", "0.3.1-beta.99", "0.3.1-beta.0", 31):
            with self.subTest(version=version):
                with self.assertRaises(release_identity.IdentityError):
                    release_identity.build_code(version)


class TestFlightTrainTests(unittest.TestCase):
    def test_the_pinned_train_carries_every_release_of_its_minor(self):
        for version in ("0.4.9", "0.4.10", "0.4.10-beta.2", "0.4.8", "0.4.99"):
            with self.subTest(version=version):
                self.assertEqual(release_identity.check_testflight_version(version, "0.4.9"), "0.4.9")
                self.assertEqual(release_identity.identity(version, "0.4.9")["testflight_version"], "0.4.9")

    def test_a_train_is_one_minor_and_a_real_x_y_z(self):
        for version, train in (("0.5.0", "0.4.9"), ("1.4.9", "0.4.9"), ("0.4.9", "0.4"), ("0.4.9", "0.4.9-beta.1"), ("0.4.9", None)):
            with self.subTest(version=version, train=train):
                with self.assertRaises(release_identity.IdentityError):
                    release_identity.check_testflight_version(version, train)

    def test_the_next_train_opens_on_a_minor_bump_with_that_release(self):
        self.assertEqual(release_identity.next_testflight_version("0.4.9", "0.4.10"), "0.4.9")
        self.assertEqual(release_identity.next_testflight_version("0.4.9", "0.5.0-beta.1"), "0.5.0")
        self.assertEqual(release_identity.next_testflight_version("0.5.0", "0.5.3"), "0.5.0")
        self.assertEqual(release_identity.next_testflight_version("0.5.0", "1.0.0"), "1.0.0")

    def test_trains_sort_above_every_earlier_one(self):
        # TestFlight lists the highest marketing version as the newest: 0.4.9 is above the live
        # 0.4.5 and 0.4.8 (the version-per-release builds), and the next minor's train above it.
        order = lambda v: tuple(int(p) for p in v.split("."))
        self.assertGreater(order(release_identity.read_testflight_version(ROOT)), order("0.4.8"))
        self.assertGreater(order(release_identity.next_testflight_version("0.4.9", "0.5.0-beta.1")), order("0.4.9"))

    def test_a_build_number_names_its_release(self):
        for version in ("0.4.5", "0.4.8", "0.4.9", "0.4.10", "0.4.10-beta.3", "0.5.0-beta.1", "1.2.3", "0.3.2-beta.98"):
            with self.subTest(version=version):
                self.assertEqual(release_identity.version_of_code(release_identity.build_code(version)), version)
        self.assertEqual(release_identity.version_of_code(41099), "0.4.10")

    def test_numbers_the_rule_never_makes_name_nothing(self):
        # Expo's builds (140 at 0.3.1), suffix 0, non-integers.
        for code in (140, 9_999, 40900, "40999", None, True):
            with self.subTest(code=code):
                self.assertIsNone(release_identity.version_of_code(code))

    def test_builds_of_a_train_grow_with_the_release(self):
        ordered = ["0.4.9", "0.4.10-beta.1", "0.4.10", "0.4.11"]
        codes = [release_identity.build_code(v) for v in ordered]
        self.assertEqual(codes, sorted(codes))
        self.assertEqual(len(set(codes)), len(codes))
        self.assertEqual({release_identity.check_testflight_version(v, "0.4.9") for v in ordered}, {"0.4.9"})

    def test_the_cli_reads_the_train_only_when_asked(self):
        import subprocess, sys
        script = str(ROOT / "scripts/lib/release_identity.py")
        out = subprocess.run([sys.executable, script, "testflight_version"], capture_output=True, text=True)
        self.assertEqual(out.stdout.strip(), release_identity.read_testflight_version(ROOT))
        # Another minor's build code still works; only its train would not.
        out = subprocess.run([sys.executable, script, "--version", "0.5.0", "version_code"], capture_output=True, text=True)
        self.assertEqual(out.stdout.strip(), "50099")
        out = subprocess.run([sys.executable, script, "--version", "0.5.0", "testflight_version"], capture_output=True, text=True)
        self.assertNotEqual(out.returncode, 0)


class SourcesAgreeTests(unittest.TestCase):
    def test_gradle_computes_the_same_rule(self):
        gradle = (ROOT / "client/android/app/build.gradle.kts").read_text(encoding="utf-8")
        self.assertIn("major * 1_000_000 + minor * 10_000 + patch * 100 + (beta ?: 99)", gradle)
        self.assertIn("(beta ?: 0) > 98", gradle)
        self.assertIn('uiDir.resolve("nsh.json")', gradle)

    def test_ios_project_carries_the_derived_values(self):
        version = release_identity.read_version(ROOT)
        project = (ROOT / "client/ios/project.yml").read_text(encoding="utf-8")
        self.assertRegex(project, rf'\n\s+MARKETING_VERSION: "{re.escape(release_identity.marketing_version(version))}"\n')
        self.assertRegex(project, rf'\n\s+CURRENT_PROJECT_VERSION: "{release_identity.build_code(version)}"\n')
        self.assertRegex(project, rf'\n\s+MEWLA_APP_VERSION: "{re.escape(version)}"\n')
        self.assertIn("MewlaVersion: $(MEWLA_APP_VERSION)", project)
        release_identity.check_testflight_version(version, release_identity.read_testflight_version(ROOT))

    def test_package_is_the_shipped_identity(self):
        manifest = json.loads((ROOT / release_identity.SOURCE).read_text(encoding="utf-8"))
        self.assertEqual(manifest["id"], release_identity.PACKAGE)
        self.assertEqual(release_identity.PACKAGE, "ai.mewla.app")

    def test_every_identifier_derives_from_the_one_app_id(self):
        ids = release_identity
        self.assertEqual(ids.DEV_PACKAGE, "ai.mewla.app.dev")
        self.assertEqual(ids.IOS_LIVE_BUNDLE_ID, "ai.mewla.app.live")
        self.assertEqual(ids.APP_GROUP, "group.ai.mewla.app")
        self.assertEqual(ids.DAEMON_LABEL, "ai.mewla.daemon")
        self.assertEqual(ids.NATIVE_HOST, "ai.mewla.browser")
        # What the builds before the move are called, which a release and an update still have to name.
        self.assertEqual(ids.LEGACY_PACKAGE, "com.daoleno.mewla")
        self.assertEqual(ids.LEGACY_DAEMON_LABEL, "com.daoleno.mewla.daemon")
        self.assertEqual(ids.LEGACY_NATIVE_HOST, "com.mewla.browser")
        values = ids.identity("0.4.19")
        for field in ("package", "legacy_package", "dev_package", "daemon_label", "native_host", "app_group", "ios_bundle_id", "ios_live_bundle_id"):
            self.assertIn(field, values)
        self.assertEqual(values["ios_bundle_id"], ids.PACKAGE)

    def test_no_file_names_the_old_personal_prefix_except_where_it_must(self):
        """The old ids live only in the legacy constants, the source namespaces and the history (docs/identifiers.md)."""
        import subprocess

        tracked = subprocess.run(["git", "ls-files"], cwd=ROOT, capture_output=True, text=True, check=True).stdout.splitlines()
        old_ids = re.compile(r"com\.daoleno\.(?:mewla|zen)\b(?!\.(?:[A-Z]|[a-z]+\.[A-Z]))")
        # Kotlin source packages, class names and the JNI symbols that follow them are not identifiers.
        namespace = re.compile(r"com\.daoleno\.mewla\.[a-z_]+(?:\.[A-Za-z_]+)*")
        allowed_files = {
            "scripts/lib/release_identity.py",        # the legacy constants
            "scripts/tests/test_release_identity.py",  # this test
            "scripts/verify-release-identity.sh",       # checks the legacy constants
            "scripts/stage-release.sh",                 # says what `package` is for
            "daemon/macapp/macapp.go",                  # LegacyBundleID
            "daemon/cmd/mewla/boot_launchd.go",         # the old label and the move
            "daemon/cmd/mewla/boot_launchd_test.go",
            "daemon/agentuse/mewlaext.go",              # LegacyNativeHostName (com.mewla.browser)
            "client/macos/src/agent.rs",                # the old label it takes over
            "client/android/app/build.gradle.kts",      # the id the handover looks for, the namespace
            "client/android/appupdate/src/main/AndroidManifest.xml",
            "client/android/appupdate/src/main/java/com/daoleno/mewla/appupdate/Handover.kt",
            "client/android/app/google-services.json",  # Firebase's entries, until the owner adds the new ids
            "client/core/update/appUpdate.test.ts",      # a release made for the old app
            "client/core/update/controller.test.ts",
            "client/core/crash.test.ts",                  # crash reports of the old app
            "client/ui/crash/CrashReports.test.tsx",
            "daemon/brain/claude_queued_paste_echo_test.go",
            "client/android/app/src/test/java/com/daoleno/mewla/share/ShareRulesTest.kt",
            "client/scripts/release-upgrade.sh",         # the old app of a move
            "docs/ios-ci-release.md",                    # the old team's Preview record, until the new team ships
            "docs/identifiers.md",
        }
        offenders = []
        for path in tracked:
            if path.startswith(("docs/releases/", "release-notes/")) or path.endswith(("CHANGELOG.md", "bun.lock", "licenses.json", ".png", ".svg", ".webp", ".ttf", ".otf")):
                continue
            if path in allowed_files or path.endswith((".kt", ".java", ".cpp", ".pro", "build.gradle.kts")) and "/android/" in path:
                continue
            try:
                text = (ROOT / path).read_text(encoding="utf-8")
            except (UnicodeDecodeError, FileNotFoundError, IsADirectoryError):
                continue
            for number, line in enumerate(text.splitlines(), 1):
                if old_ids.search(namespace.sub("", line)):
                    offenders.append(f"{path}:{number}: {line.strip()[:100]}")
        self.assertEqual(offenders, [], "the old personal prefix is back; see docs/identifiers.md")


if __name__ == "__main__":
    unittest.main()
