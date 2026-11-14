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

    def test_package_is_the_shipped_identity(self):
        manifest = json.loads((ROOT / release_identity.SOURCE).read_text(encoding="utf-8"))
        self.assertEqual(manifest["id"], release_identity.PACKAGE)
        self.assertEqual(release_identity.PACKAGE, "com.daoleno.mewla")


if __name__ == "__main__":
    unittest.main()
