import base64
import contextlib
import importlib.util
import io
import json
import pathlib
import subprocess
import tempfile
import unittest
import zipfile


SCRIPT = pathlib.Path(__file__).parents[1] / "google-play-upload.py"
SPEC = importlib.util.spec_from_file_location("google_play_upload", SCRIPT)
play = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(play)


def b64url_decode(value: str) -> bytes:
    return base64.urlsafe_b64decode(value + "=" * (-len(value) % 4))


class GooglePlayUploadTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = pathlib.Path(self.tmp.name)
        self.aab = self.dir / "app.aab"
        with zipfile.ZipFile(self.aab, "w") as bundle:
            bundle.writestr("base/manifest/AndroidManifest.xml", b"\x00")

    def tearDown(self):
        self.tmp.cleanup()

    def args(self, *extra):
        return ["--aab", str(self.aab), "--package", "com.daoleno.mewla", "--version-code", "54", *extra]

    def test_dry_run_prints_plan_without_credentials(self):
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertEqual(play.main(self.args("--track", "internal", "--dry-run")), 0)
        text = out.getvalue()
        self.assertIn("no request sent", text)
        self.assertIn("tracks/internal", text)
        self.assertIn('"status": "draft"', text)
        self.assertIn(":commit", text)

    def test_requires_credentials_outside_dry_run(self):
        with self.assertRaises(SystemExit):
            play.main(self.args("--track", "alpha"))

    def test_refuses_production_and_non_bundles(self):
        with self.assertRaises(SystemExit):
            play.main(self.args("--track", "production", "--dry-run"))
        self.aab.write_bytes(b"not a zip")
        with self.assertRaises(SystemExit):
            play.main(self.args("--track", "internal", "--dry-run"))

    def test_track_body_names_one_release(self):
        self.assertEqual(
            play.track_body("alpha", 54, "completed", "0.2.14"),
            {"track": "alpha", "releases": [{"name": "0.2.14", "versionCodes": ["54"], "status": "completed"}]},
        )

    def test_jwt_is_rs256_signed_for_the_android_publisher_scope(self):
        key = self.dir / "key.pem"
        pub = self.dir / "pub.pem"
        subprocess.run(["openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048",
                        "-out", str(key)], check=True, capture_output=True)
        subprocess.run(["openssl", "pkey", "-in", str(key), "-pubout", "-out", str(pub)], check=True)
        account = {"client_email": "ci@example.iam.gserviceaccount.com",
                   "private_key": key.read_text(), "token_uri": "https://oauth2.googleapis.com/token"}
        token = play.signed_jwt(account, 1_700_000_000)
        header, claims, signature = token.split(".")
        self.assertEqual(json.loads(b64url_decode(header))["alg"], "RS256")
        payload = json.loads(b64url_decode(claims))
        self.assertEqual(payload["scope"], play.SCOPE)
        self.assertEqual(payload["exp"] - payload["iat"], 3600)
        sig = self.dir / "sig"
        sig.write_bytes(b64url_decode(signature))
        verified = subprocess.run(["openssl", "dgst", "-sha256", "-verify", str(pub), "-signature", str(sig)],
                                  input=f"{header}.{claims}".encode(), capture_output=True)
        self.assertEqual(verified.returncode, 0, verified.stderr)

    def test_service_account_must_be_complete(self):
        path = self.dir / "sa.json"
        path.write_text(json.dumps({"type": "service_account", "client_email": "x"}))
        with self.assertRaises(SystemExit):
            play.load_service_account(path)


if __name__ == "__main__":
    unittest.main()
