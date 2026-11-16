"""The App Store Connect webhook receiver's tests (scripts/asc-webhook/receiver.test.mjs), under Node."""

import importlib.util
import pathlib
import shutil
import subprocess
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
TESTS = ROOT / "scripts/asc-webhook/receiver.test.mjs"
SPEC = importlib.util.spec_from_file_location("asc_webhook_register", ROOT / "scripts/asc-webhook/register.py")
register = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(register)
APP = "6821246662"
URL = "https://mewla-asc-webhook.example.workers.dev/app-store-connect"


class FakeClient:
    def __init__(self, hooks=()):
        self.hooks = list(hooks)
        self.calls = []

    def api_request(self, method, path, body=None):
        self.calls.append((method, path, body))
        if method == "GET" and path.startswith(f"/apps/{APP}/webhooks"):
            return {"data": self.hooks}
        if method == "POST" and path == "/webhooks":
            return {"data": {"type": "webhooks", "id": "w-new"}}
        if method == "PATCH":
            return {"data": {"type": "webhooks", "id": path.split("/")[2]}}
        raise AssertionError((method, path))

    def writes(self):
        return [(m, p, b) for m, p, b in self.calls if m != "GET"]


def hook(url=URL, events=None, enabled=True, name=register.NAME):
    return {"type": "webhooks", "id": "w1", "attributes": {
        "name": name, "url": url, "enabled": enabled, "eventTypes": events or list(register.EVENT_TYPES)}}


class RegisterTests(unittest.TestCase):
    def test_creates_the_app_webhook_for_the_two_build_events(self):
        client = FakeClient([hook(name="Something else")])
        result = register.register(client, APP, URL, "s" * 40)
        self.assertEqual(result, {"action": "create", "id": "w-new", "dry_run": False})
        [(method, path, body)] = client.writes()
        self.assertEqual((method, path), ("POST", "/webhooks"))
        self.assertEqual(body["data"]["attributes"], {
            "enabled": True, "name": register.NAME, "secret": "s" * 40, "url": URL,
            "eventTypes": ["BUILD_UPLOAD_STATE_UPDATED", "BUILD_BETA_DETAIL_EXTERNAL_BUILD_STATE_UPDATED"]})
        self.assertEqual(body["data"]["relationships"]["app"]["data"], {"type": "apps", "id": APP})

    def test_updates_a_changed_webhook_in_place_and_re_sends_the_secret_of_an_unchanged_one(self):
        client = FakeClient([hook(url="https://old.example/x", enabled=False)])
        self.assertEqual(register.register(client, APP, URL, "s" * 40)["action"], "update")
        client = FakeClient([hook()])
        self.assertEqual(register.register(client, APP, URL, "t" * 40)["action"], "keep (secret re-sent)")
        [(method, path, body)] = client.writes()
        self.assertEqual((method, path, body["data"]["attributes"]["secret"]), ("PATCH", "/webhooks/w1", "t" * 40))

    def test_a_dry_run_only_reads(self):
        client = FakeClient([])
        self.assertEqual(register.register(client, APP, URL, "", dry_run=True)["action"], "create")
        self.assertEqual(client.writes(), [])

    def test_two_webhooks_with_the_name_are_an_error(self):
        with self.assertRaises(RuntimeError):
            register.register(FakeClient([hook(), hook()]), APP, URL, "s" * 40)


class AscWebhookWorkerTests(unittest.TestCase):
    def test_worker(self):
        node = shutil.which("node")
        self.assertIsNotNone(node, "node is required for scripts/asc-webhook (GitHub's runners have it)")
        result = subprocess.run([node, "--test", str(TESTS)], cwd=ROOT, capture_output=True, text=True, timeout=120)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
