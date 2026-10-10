"""Execute the actual CI validation scripts with disposable signing placeholders."""

import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class IOSWorkflowValidationTests(unittest.TestCase):
    def validate(self, *, postprocess=False, **overrides):
        filename = "ios-testflight-postprocess.yml" if postprocess else "ios-release.yml"
        step = (
            "Validate input and Individual API key secrets"
            if postprocess else "Validate explicit inputs and required signing secrets"
        )
        workflow = (ROOT / ".github/workflows" / filename).read_text()
        script = workflow.split(f"- name: {step}\n", 1)[1].split("        run: |\n", 1)[1]
        script = re.split(r"\n      - ", script, maxsplit=1)[0]
        script = re.sub(r"^          ", "", script, flags=re.MULTILINE)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "env"
            environment = {
                "PATH": os.environ["PATH"],
                "GITHUB_ENV": str(output),
                "MEWLA_IOS_APP_VARIANT": "preview",
                "MEWLA_IOS_DESTINATION": "testflight",
                "MEWLA_IOS_VERSION": "0.2.3",
                "MEWLA_IOS_BUILD_NUMBER": "41",
                "MEWLA_APPLE_CERTIFICATE_BASE64": "test-certificate",
                "MEWLA_APPLE_CERTIFICATE_PASSWORD": "test-password",
                "MEWLA_APPLE_PROVISIONING_PROFILE_BASE64": "test-profile",
                "MEWLA_APPLE_TEAM_ID": "HD84J3DJ2B",
                "MEWLA_ASC_KEY_ID": "test-key-id",
                "MEWLA_ASC_API_KEY_BASE64": "test-key",
                "MEWLA_ASC_APP_ID": "123",
                "PREVIEW_ASC_APP_ID": "123",
                "PRODUCTION_ASC_APP_ID": "456",
                **overrides,
            }
            result = subprocess.run(
                ["bash", "-c", script], env=environment, capture_output=True, text=True,
            )
            return result, output.read_text() if output.exists() else ""

    def test_selects_only_requested_identity(self):
        for variant, app_id in [("preview", "123"), ("production", "456")]:
            with self.subTest(variant=variant):
                result, exported = self.validate(MEWLA_IOS_APP_VARIANT=variant)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(exported, f"MEWLA_ASC_APP_ID={app_id}\n")

    def test_missing_preview_id_never_falls_back_to_production(self):
        result, exported = self.validate(PREVIEW_ASC_APP_ID="")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("MEWLA_ASC_PREVIEW_APP_ID", result.stderr)
        self.assertEqual(exported, "")

    def test_missing_production_id_never_falls_back_to_preview(self):
        result, exported = self.validate(MEWLA_IOS_APP_VARIANT="production", PRODUCTION_ASC_APP_ID="")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ZEN_ASC_APP_ID", result.stderr)
        self.assertEqual(exported, "")

    def test_rejects_malformed_ids_before_export(self):
        for value in ["0", "abc", "123\nOTHER=value"]:
            with self.subTest(value=value):
                result, exported = self.validate(PREVIEW_ASC_APP_ID=value)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(exported, "")

    def test_artifact_only_needs_no_app_id(self):
        result, exported = self.validate(MEWLA_IOS_DESTINATION="artifact-only", PREVIEW_ASC_APP_ID="")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(exported, "")

    def test_postprocess_requires_valid_identity_and_version(self):
        result, _ = self.validate(postprocess=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        result, _ = self.validate(postprocess=True, MEWLA_ASC_APP_ID="")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("MEWLA_ASC_PREVIEW_APP_ID", result.stderr)
        result, _ = self.validate(postprocess=True, MEWLA_IOS_VERSION="$(exit 0)")
        self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
