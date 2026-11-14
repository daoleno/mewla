import importlib.util
import pathlib
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).parents[1] / "testflight-review-state.py"
SPEC = importlib.util.spec_from_file_location("tf_review", SCRIPT)
tf = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(tf)

LINK = "https://testflight.apple.com/join/AbC123xy"


def build(identifier, number, marketing, external):
    return (
        {
            "type": "builds",
            "id": identifier,
            "attributes": {"version": number, "uploadedDate": "2026-11-14T03:27:00Z", "processingState": "VALID", "expired": False},
            "relationships": {
                "preReleaseVersion": {"data": {"type": "preReleaseVersions", "id": f"p{identifier}"}},
                "buildBetaDetail": {"data": {"type": "buildBetaDetails", "id": f"d{identifier}"}} if external else {"data": None},
            },
        },
        [{"type": "preReleaseVersions", "id": f"p{identifier}", "attributes": {"version": marketing}}]
        + ([{"type": "buildBetaDetails", "id": f"d{identifier}", "attributes": {"externalBuildState": external}}] if external else []),
    )


class FakeClient:
    def __init__(self, builds, reviews, in_group, group=None, lookups=None, refuse_patch=False):
        self.builds = builds
        self.reviews = reviews
        self.in_group = in_group
        self.group = group or {"name": "Mewla Preview", "isInternalGroup": False, "publicLinkEnabled": False, "publicLink": None}
        self.lookups = lookups or {}
        self.refuse_patch = refuse_patch
        self.patches = []

    def api_request(self, method, path, body=None):
        if method == "PATCH":
            self.patches.append((path, body))
            if self.refuse_patch:
                raise tf.asc_upload.AppStoreConnectHTTPError(403, "FORBIDDEN")
            return {"data": {"id": "g1", "attributes": {"publicLinkEnabled": True, "publicLink": LINK}}}
        if "/betaGroups?" in path:
            return {"data": [{"id": "g1", "attributes": self.group}, {"id": "g2", "attributes": {"name": "Team"}}]}
        if path.startswith("/betaAppReviewSubmissions?"):
            build_id = path.split("filter%5Bbuild%5D=")[1].split("&")[0]
            state = self.reviews.get(build_id)
            return {"data": [{"attributes": {"betaReviewState": state}}] if state else []}
        if path.endswith("/buildBetaDetail"):
            return {"data": {"attributes": {"externalBuildState": self.lookups.get(path.split("/")[2])}}}
        if "filter%5BbetaGroups%5D" in path:
            return {"data": [{"id": i} for i in self.in_group]}
        return {"data": [b for b, _ in self.builds], "included": [i for _, items in self.builds for i in items]}


def run(client, enable=True):
    group = tf.beta_group(client, "6821246662", "Mewla Preview")
    rows = tf.newest_builds(client, "6821246662", group["id"], 5)
    wanted = enable and any(tf.approved(r) and r["in_group"] for r in rows)
    result = tf.report("Mewla Preview", rows, tf.ensure_public_link(client, group, wanted))
    return result, tf.markdown(result)


class ReviewStateTests(unittest.TestCase):
    def test_waiting_build_is_reported_and_nothing_is_changed(self):
        client = FakeClient([build("b40200", "40200", "0.4.1", None)], {"b40200": "WAITING_FOR_REVIEW"}, ["b40200"],
                            lookups={"b40200": "WAITING_FOR_BETA_REVIEW"})
        result, text = run(client)
        self.assertEqual(client.patches, [])
        self.assertIsNone(result["newest_approved"])
        self.assertIn("| 0.4.1 (40200) | 2026-11-14T03:27 | VALID | WAITING_FOR_REVIEW | WAITING_FOR_BETA_REVIEW | yes |", text)
        self.assertIn("**Newest approved build:** none yet", text)
        self.assertIn("**Public link:** off (turned on once a build passes review)", text)

    def test_an_approved_build_in_the_group_turns_the_public_link_on(self):
        client = FakeClient([build("b40200", "40200", "0.4.1", "BETA_APPROVED")], {"b40200": "APPROVED"}, ["b40200"])
        result, text = run(client)
        self.assertEqual(client.patches, [("/betaGroups/g1", {"data": {"type": "betaGroups", "id": "g1", "attributes": {"publicLinkEnabled": True}}})])
        self.assertEqual(result["public_link"]["url"], LINK)
        self.assertIn(f"**Public link:** {LINK} (turned on by this run)", text)
        self.assertIn("**Newest approved build:** 0.4.1 (40200)", text)

    def test_a_link_already_on_is_left_alone_and_printed(self):
        group = {"name": "Mewla Preview", "isInternalGroup": False, "publicLinkEnabled": True, "publicLink": LINK}
        client = FakeClient([build("b40200", "40200", "0.4.1", "IN_BETA_TESTING")], {}, ["b40200"], group=group)
        _, text = run(client)
        self.assertEqual(client.patches, [])
        self.assertIn(f"**Public link:** {LINK}\n", text)

    def test_a_refused_change_says_what_to_click(self):
        client = FakeClient([build("b40200", "40200", "0.4.1", "BETA_APPROVED")], {}, ["b40200"], refuse_patch=True)
        result, text = run(client)
        self.assertFalse(result["public_link"]["enabled"])
        self.assertIn("HTTP 403", text)
        self.assertIn("Enable Public Link", text)

    def test_an_approved_build_outside_the_group_changes_nothing(self):
        client = FakeClient([build("b40200", "40200", "0.4.1", "BETA_APPROVED")], {}, [])
        _, text = run(client)
        self.assertEqual(client.patches, [])
        self.assertIn("— not in Mewla Preview", text)

    def test_the_summary_is_appended(self):
        client = FakeClient([build("b1", "40200", "0.4.1", "BETA_APPROVED")], {}, ["b1"])
        _, text = run(client, enable=False)
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "summary.md"
            path.write_text("before\n")
            with path.open("a") as summary:
                summary.write(text)
            self.assertTrue(path.read_text().startswith("before\n## TestFlight review: Mewla Preview"))


if __name__ == "__main__":
    unittest.main()
