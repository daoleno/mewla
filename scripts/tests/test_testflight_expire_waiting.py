import importlib.util
import pathlib
import unittest

SCRIPT = pathlib.Path(__file__).parents[1] / "testflight-expire-waiting.py"
SPEC = importlib.util.spec_from_file_location("tf_expire", SCRIPT)
tf = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(tf)

APP = "6821246662"


def build(identifier, number, marketing, external, expired=False):
    """A /v1/builds data item and its included preReleaseVersion and buildBetaDetail, as Apple returns them."""
    return (
        {
            "type": "builds",
            "id": identifier,
            "attributes": {"version": number, "expired": expired, "processingState": "VALID"},
            "relationships": {
                "preReleaseVersion": {"data": {"type": "preReleaseVersions", "id": f"p{identifier}"}},
                "buildBetaDetail": {"data": {"type": "buildBetaDetails", "id": f"d{identifier}"}} if external else {"data": None},
            },
        },
        [{"type": "preReleaseVersions", "id": f"p{identifier}", "attributes": {"version": marketing}}]
        + ([{"type": "buildBetaDetails", "id": f"d{identifier}", "attributes": {"externalBuildState": external}}] if external else []),
    )


# The Preview app on 2026-11-15: 0.3.1 (140) live on the public link, four builds waiting.
SHELF = [
    build("b40599", "40599", "0.4.5", "WAITING_FOR_BETA_REVIEW"),
    build("b40499", "40499", "0.4.4", "WAITING_FOR_BETA_REVIEW"),
    build("b40400", "40400", "0.4.4", "READY_FOR_BETA_SUBMISSION"),
    build("b40299", "40299", "0.4.2", "IN_BETA_REVIEW"),
    build("b40200", "40200", "0.4.1", None),
    build("b30299", "30299", "0.3.2", "EXPIRED", expired=True),
    build("b140", "140", "0.3.1", "IN_BETA_TESTING"),
    build("b139", "139", "0.3.1", "BETA_APPROVED"),
]
REVIEWS = {
    "b40599": "WAITING_FOR_REVIEW",
    "b40499": "WAITING_FOR_REVIEW",
    "b40299": "IN_REVIEW",
    "b40200": "WAITING_FOR_REVIEW",
    "b140": "APPROVED",
    "b139": "APPROVED",
}


class FakeClient:
    def __init__(self, builds=SHELF, reviews=REVIEWS, lookups=None, refuse=()):
        self.builds = builds
        self.reviews = reviews
        self.lookups = lookups or {"b40200": "WAITING_FOR_BETA_REVIEW"}
        self.refuse = set(refuse)
        self.patches = []

    def api_request(self, method, path, body=None):
        if method == "PATCH":
            self.patches.append((path, body))
            build_id = path.split("/")[2]
            if build_id in self.refuse:
                raise tf.asc_upload.AppStoreConnectHTTPError(
                    409, '{"errors":[{"status":"409","code":"STATE_ERROR","detail":"in review"}]}'
                )
            return {"data": {"type": "builds", "id": build_id, "attributes": {"expired": True}}}
        if path.startswith("/betaAppReviewSubmissions?"):
            build_id = path.split("filter%5Bbuild%5D=")[1].split("&")[0]
            state = self.reviews.get(build_id)
            return {"data": [{"type": "betaAppReviewSubmissions", "id": f"r{build_id}",
                              "attributes": {"betaReviewState": state}}] if state else []}
        if path.endswith("/buildBetaDetail"):
            return {"data": {"type": "buildBetaDetails", "attributes": {"externalBuildState": self.lookups.get(path.split("/")[2])}}}
        assert path.startswith("/builds?") and "filter%5Bapp%5D=6821246662" in path
        return {"data": [b for b, _ in self.builds], "included": [i for _, items in self.builds for i in items]}


def patched(client):
    return [path for path, _ in client.patches]


class ExpireWaitingTests(unittest.TestCase):
    def test_only_older_builds_still_waiting_are_expired(self):
        client = FakeClient()
        result = tf.run(client, APP, 40599)
        self.assertEqual(patched(client), ["/builds/b40499", "/builds/b40299", "/builds/b40200"])
        self.assertEqual(client.patches[0][1], {"data": {"type": "builds", "id": "b40499", "attributes": {"expired": True}}})
        self.assertEqual([(r["build_number"], r["status"], r["expired"]) for r in result["builds"]],
                         [("40499", 200, True), ("40299", 200, True), ("40200", 200, True)])
        text = tf.markdown(result)
        self.assertIn("| 0.4.2 (40299) | IN_BETA_REVIEW | IN_REVIEW | expired (HTTP 200) |", text)
        # Its external state came from /builds/{id}/buildBetaDetail, since it was not included.
        self.assertIn("| 0.4.1 (40200) | WAITING_FOR_BETA_REVIEW | WAITING_FOR_REVIEW | expired (HTTP 200) |", text)

    def test_the_new_build_newer_builds_approved_and_unsubmitted_ones_are_untouched(self):
        client = FakeClient()
        tf.run(client, APP, 40599)
        for kept in ("b40599", "b40400", "b30299", "b140", "b139"):
            self.assertNotIn(f"/builds/{kept}", patched(client))
        client = FakeClient()
        tf.run(client, APP, 40299)
        self.assertEqual(patched(client), ["/builds/b40200"])

    def test_an_approved_review_wins_over_a_waiting_external_state(self):
        builds = [build("b40599", "40599", "0.4.5", None), build("b40499", "40499", "0.4.4", "WAITING_FOR_BETA_REVIEW")]
        client = FakeClient(builds, {"b40499": "APPROVED"}, lookups={})
        self.assertEqual(tf.run(client, APP, 40599)["builds"], [])
        self.assertEqual(client.patches, [])
        self.assertIn("None waiting below it.", tf.markdown(tf.run(client, APP, 40599)))

    def test_a_refusal_is_reported_with_apples_status_and_error(self):
        client = FakeClient(refuse={"b40299"})
        result = tf.run(client, APP, 40599)
        refused = [r for r in result["builds"] if not r["expired"]]
        self.assertEqual([(r["build_number"], r["status"]) for r in refused], [("40299", 409)])
        self.assertIn("STATE_ERROR", refused[0]["error"])
        self.assertIn("refused (HTTP 409)", tf.markdown(result))

    def test_a_dry_run_changes_nothing(self):
        client = FakeClient()
        result = tf.run(client, APP, 40599, dry_run=True)
        self.assertEqual(client.patches, [])
        self.assertEqual([r["build_number"] for r in result["builds"]], ["40499", "40299", "40200"])
        self.assertIn("would expire", tf.markdown(result))


if __name__ == "__main__":
    unittest.main()
