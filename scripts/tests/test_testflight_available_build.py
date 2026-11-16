import importlib.util
import pathlib
import unittest

SCRIPT = pathlib.Path(__file__).parents[1] / "testflight-available-build.py"
SPEC = importlib.util.spec_from_file_location("tf_available", SCRIPT)
tf = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(tf)


def build(identifier, number, marketing, state, expired=False, processing="VALID"):
    return (
        {
            "type": "builds",
            "id": f"b{identifier}",
            "attributes": {"version": number, "expired": expired, "processingState": processing},
            "relationships": {
                "preReleaseVersion": {"data": {"type": "preReleaseVersions", "id": f"p{identifier}"}},
                "buildBetaDetail": {"data": {"type": "buildBetaDetails", "id": f"d{identifier}"}},
            },
        },
        [
            {"type": "preReleaseVersions", "id": f"p{identifier}", "attributes": {"version": marketing}},
            {"type": "buildBetaDetails", "id": f"d{identifier}", "attributes": {"externalBuildState": state}},
        ],
    )


def document(*builds):
    return {"data": [b for b, _ in builds], "included": [i for _, items in builds for i in items]}


class FakeClient:
    def __init__(self, group_attributes, builds, lookups=None):
        self.group_attributes = group_attributes
        self.builds = builds
        self.lookups = lookups or {}
        self.paths = []

    def api_request(self, method, path, body=None):
        self.paths.append(path)
        if "/betaGroups?" in path:
            return {"data": [{"id": "g1", "attributes": self.group_attributes}]}
        if path.endswith("/buildBetaDetail"):
            return {"data": {"attributes": {"externalBuildState": self.lookups.get(path.split("/")[2])}}}
        return self.builds


class InstallableBuildTests(unittest.TestCase):
    def test_only_approved_or_in_testing_counts(self):
        doc = document(
            build(1, "40599", "0.4.5", "BETA_APPROVED"),
            build(2, "40699", "0.4.6", "IN_BETA_REVIEW"),
            build(3, "40799", "0.4.7", "WAITING_FOR_BETA_REVIEW"),
            build(4, "40899", "0.4.8", "BETA_REJECTED"),
        )
        self.assertEqual(
            tf.select_installable_build(doc), {"marketing_version": "0.4.5", "build_number": 40599}
        )

    def test_a_train_build_names_its_release_by_build_number(self):
        # The Preview's marketing version is the train (0.4.9); 40999 is 0.4.9 and 41099 is 0.4.10.
        doc = document(
            build(1, "40599", "0.4.5", "IN_BETA_TESTING"),
            build(2, "40999", "0.4.9", "IN_BETA_TESTING"),
            build(3, "41099", "0.4.9", "IN_BETA_TESTING"),
        )
        self.assertEqual(
            tf.select_installable_build(doc), {"marketing_version": "0.4.10", "build_number": 41099}
        )

    def test_the_build_number_orders_builds_not_the_marketing_version(self):
        # TestFlight still holds 0.4.8 (higher marketing version than the 0.4 train) at a lower build.
        doc = document(
            build(1, "40899", "0.4.8", "IN_BETA_TESTING"),
            build(2, "40999", "0.4", "IN_BETA_TESTING"),
        )
        self.assertEqual(tf.select_installable_build(doc)["build_number"], 40999)

    def test_a_beta_build_names_its_numeric_version(self):
        doc = document(build(1, "50001", "0.5", "IN_BETA_TESTING"))
        self.assertEqual(
            tf.select_installable_build(doc), {"marketing_version": "0.5.0", "build_number": 50001}
        )

    def test_skips_expired_unprocessed_malformed_and_expo_builds(self):
        doc = document(
            build(1, "40999", "0.4", "IN_BETA_TESTING", expired=True),
            build(2, "41099", "0.4", "IN_BETA_TESTING", processing="PROCESSING"),
            build(3, "40900", "0.4", "IN_BETA_TESTING"),
            build(4, "x", "0.4", "IN_BETA_TESTING"),
            build(5, "140", "0.3.1", "IN_BETA_TESTING"),
        )
        self.assertIsNone(tf.select_installable_build(doc))
        self.assertIsNone(tf.select_installable_build({}))

    def test_build_without_beta_detail_is_not_installable_unless_looked_up(self):
        item, included = build(1, "40999", "0.4", "IN_BETA_TESTING")
        item["relationships"].pop("buildBetaDetail")
        self.assertIsNone(tf.select_installable_build({"data": [item], "included": included}))

    def test_reports_app_id_and_valid_public_link(self):
        client = FakeClient(
            {"name": "Mewla Preview", "publicLink": "https://testflight.apple.com/join/nMQheDCE"},
            document(build(1, "40999", "0.4", "IN_BETA_TESTING")),
        )
        self.assertEqual(
            tf.find_installable_build(client, "6790486999", "Mewla Preview"),
            {
                "marketing_version": "0.4.9",
                "build_number": 40999,
                "app_id": "6790486999",
                "public_link": "https://testflight.apple.com/join/nMQheDCE",
            },
        )
        # /betaGroups/{id}/builds rejects include (real API, HTTP 400), so /builds is filtered instead.
        self.assertTrue(client.paths[-1].startswith("/builds?"))
        self.assertIn("filter%5BbetaGroups%5D=g1", client.paths[-1])
        self.assertIn("include=preReleaseVersion%2CbuildBetaDetail", client.paths[-1])

    def test_unlinked_beta_detail_is_looked_up_newest_first(self):
        item, included = build(2, "41099", "0.4", "IN_BETA_REVIEW")
        item["relationships"].pop("buildBetaDetail")
        old, old_included = build(1, "40999", "0.4", "BETA_APPROVED")
        client = FakeClient({"name": "Mewla Preview"}, document((item, included), (old, old_included)), {"b2": "BETA_APPROVED"})
        self.assertEqual(tf.find_installable_build(client, "1", "Mewla Preview")["build_number"], 41099)
        client = FakeClient({"name": "Mewla Preview"}, document((item, included), (old, old_included)), {"b2": "WAITING_FOR_BETA_REVIEW"})
        self.assertEqual(tf.find_installable_build(client, "1", "Mewla Preview")["build_number"], 40999)

    def test_ignores_unexpected_link_and_requires_the_group(self):
        client = FakeClient(
            {"name": "Mewla Preview", "publicLink": "https://example.com/x"},
            document(build(1, "40999", "0.4", "IN_BETA_TESTING")),
        )
        self.assertNotIn("public_link", tf.find_installable_build(client, "1", "Mewla Preview"))
        with self.assertRaises(RuntimeError):
            tf.find_installable_build(client, "1", "Other Group")


if __name__ == "__main__":
    unittest.main()
