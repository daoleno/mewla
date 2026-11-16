import importlib.util
import pathlib
import unittest

SCRIPT = pathlib.Path(__file__).parents[1] / "testflight-review-queue.py"
SPEC = importlib.util.spec_from_file_location("tf_queue", SCRIPT)
tf = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(tf)

APP = "6821246662"
GROUP = "Mewla Preview"


def build(identifier, number, marketing, external, expired=False, processing="VALID"):
    """A /v1/builds data item and its included preReleaseVersion and buildBetaDetail, as Apple returns them."""
    return (
        {
            "type": "builds",
            "id": identifier,
            "attributes": {"version": number, "expired": expired, "processingState": processing},
            "relationships": {
                "preReleaseVersion": {"data": {"type": "preReleaseVersions", "id": f"p{identifier}"}},
                "buildBetaDetail": {"data": {"type": "buildBetaDetails", "id": f"d{identifier}"}} if external else {"data": None},
            },
        },
        [{"type": "preReleaseVersions", "id": f"p{identifier}", "attributes": {"version": marketing}}]
        + ([{"type": "buildBetaDetails", "id": f"d{identifier}", "attributes": {"externalBuildState": external}}] if external else []),
    )


# The Preview app on 2026-11-16 03:11 UTC (ios-testflight-review run 43796677347).
NOW = [
    build("b40899", "40899", "0.4.8", "WAITING_FOR_BETA_REVIEW"),
    build("b40799", "40799", "0.4.7", "EXPIRED", expired=True),
    build("b40699", "40699", "0.4.6", "EXPIRED", expired=True),
    build("b40599", "40599", "0.4.5", "IN_BETA_TESTING"),
    build("b40499", "40499", "0.4.4", "EXPIRED", expired=True),
]
NOW_REVIEWS = {"b40899": "WAITING_FOR_REVIEW", "b40599": "APPROVED"}


class FakeClient:
    def __init__(self, builds, reviews, in_group=None, lookups=None, refuse=(), race=False):
        self.builds = builds
        self.reviews = dict(reviews)
        self.in_group = set(in_group if in_group is not None else (b["id"] for b, _ in builds))
        self.lookups = lookups or {}
        self.refuse = set(refuse)
        self.race = race
        self.calls = []

    def api_request(self, method, path, body=None):
        self.calls.append((method, path, body))
        if method == "PATCH":
            build_id = path.split("/")[2]
            if build_id in self.refuse:
                raise tf.asc_upload.AppStoreConnectHTTPError(409, '{"errors":[{"code":"STATE_ERROR"}]}')
            return {"data": {"type": "builds", "id": build_id, "attributes": {"expired": True}}}
        if method == "POST":
            assert path == "/betaAppReviewSubmissions"
            build_id = body["data"]["relationships"]["build"]["data"]["id"]
            if self.race:
                self.reviews[build_id] = "WAITING_FOR_REVIEW"
            if build_id in self.refuse or self.race:
                raise tf.asc_upload.AppStoreConnectHTTPError(409, '{"errors":[{"code":"ENTITY_ERROR"}]}')
            return {"data": {"type": "betaAppReviewSubmissions", "id": f"r{build_id}"}}
        if path.startswith(f"/apps/{APP}/betaGroups?"):
            return {"data": [{"type": "betaGroups", "id": "g1", "attributes": {"name": GROUP, "isInternalGroup": False}},
                             {"type": "betaGroups", "id": "g0", "attributes": {"name": "Team", "isInternalGroup": True}}]}
        if path.startswith("/betaAppReviewSubmissions?"):
            build_id = path.split("filter%5Bbuild%5D=")[1].split("&")[0]
            state = self.reviews.get(build_id)
            return {"data": [{"type": "betaAppReviewSubmissions", "id": f"r{build_id}",
                              "attributes": {"betaReviewState": state}}] if state else []}
        if path.endswith("/buildBetaDetail"):
            return {"data": {"type": "buildBetaDetails", "attributes": {"externalBuildState": self.lookups.get(path.split("/")[2])}}}
        assert path.startswith("/builds?") and "filter%5Bapp%5D=6821246662" in path
        if "filter%5BbetaGroups%5D=g1" in path:
            return {"data": [{"type": "builds", "id": i} for i in self.in_group]}
        return {"data": [b for b, _ in self.builds], "included": [i for _, items in self.builds for i in items]}

    def writes(self):
        return [(m, p) for m, p, _ in self.calls if m != "GET"]


def numbers(rows):
    return [r["build_number"] for r in rows]


class ReviewQueueTests(unittest.TestCase):
    def test_today_keeps_the_build_in_review_and_does_nothing_else(self):
        client = FakeClient(NOW, NOW_REVIEWS)
        result = tf.run(client, APP, GROUP)
        self.assertEqual(client.writes(), [])
        self.assertEqual(numbers(result["in_review"]), ["40899"])
        self.assertEqual((result["submit"], result["waiting"], result["expire"]), (None, None, []))
        self.assertEqual(result["approved"]["build_number"], "40599")
        self.assertFalse(tf.failed(result))

    def test_a_new_upload_waits_while_another_build_is_in_review(self):
        builds = [build("b40999", "40999", "0.4.9", "READY_FOR_BETA_SUBMISSION")] + NOW
        client = FakeClient(builds, NOW_REVIEWS)
        result = tf.run(client, APP, GROUP)
        self.assertEqual(client.writes(), [])
        self.assertEqual(result["waiting"]["build_number"], "40999")
        self.assertIn("- Waiting: 0.4.9 (40999), submitted once the review above ends", tf.markdown(result))

    def test_only_the_newest_waiting_upload_survives_and_the_build_in_review_is_never_expired(self):
        builds = [
            build("b41099", "41099", "0.4.10", "READY_FOR_BETA_SUBMISSION"),
            build("b40999", "40999", "0.4.9", "READY_FOR_BETA_SUBMISSION"),
        ] + NOW
        client = FakeClient(builds, NOW_REVIEWS)
        result = tf.run(client, APP, GROUP)
        self.assertEqual(client.writes(), [("PATCH", "/builds/b40999")])
        self.assertEqual(result["waiting"]["build_number"], "41099")
        self.assertEqual([(r["build_number"], r["done"]) for r in result["expire"]], [("40999", True)])

    def test_once_the_review_is_approved_the_newest_waiting_build_is_submitted(self):
        builds = [
            build("b41099", "41099", "0.4.10", "READY_FOR_BETA_SUBMISSION"),
            build("b40999", "40999", "0.4.9", "READY_FOR_BETA_SUBMISSION"),
            build("b40899", "40899", "0.4.8", "IN_BETA_TESTING"),
            build("b40599", "40599", "0.4.5", "IN_BETA_TESTING"),
        ]
        client = FakeClient(builds, {"b40899": "APPROVED", "b40599": "APPROVED"})
        result = tf.run(client, APP, GROUP)
        self.assertEqual(client.writes(), [("POST", "/betaAppReviewSubmissions"), ("PATCH", "/builds/b40999")])
        posted = [b for m, _, b in client.calls if m == "POST"][0]
        self.assertEqual(posted["data"]["relationships"]["build"]["data"], {"type": "builds", "id": "b41099"})
        self.assertEqual((result["submit"]["build_number"], result["submit"]["done"]), ("41099", True))
        # The approved 0.4.5 below the newest approved one is left alone.
        self.assertNotIn(("PATCH", "/builds/b40599"), client.writes())

    def test_a_rejected_review_lets_the_newer_build_through_and_alone_does_nothing(self):
        builds = [
            build("b40999", "40999", "0.4.9", "READY_FOR_BETA_SUBMISSION"),
            build("b40899", "40899", "0.4.8", "BETA_REJECTED"),
            build("b40599", "40599", "0.4.5", "IN_BETA_TESTING"),
        ]
        reviews = {"b40899": "REJECTED", "b40599": "APPROVED"}
        client = FakeClient(builds, reviews)
        self.assertEqual(tf.run(client, APP, GROUP)["submit"]["build_number"], "40999")
        self.assertEqual(client.writes(), [("POST", "/betaAppReviewSubmissions")])
        client = FakeClient(builds[1:], reviews)
        result = tf.run(client, APP, GROUP)
        self.assertEqual((client.writes(), result["submit"], result["expire"]), ([], None, []))
        # A build older than the rejected one is not resubmitted either.
        client = FakeClient([builds[1], build("b40700", "40700", "0.4.7", "READY_FOR_BETA_SUBMISSION"), builds[2]], reviews)
        self.assertIsNone(tf.run(client, APP, GROUP)["submit"])
        self.assertEqual(client.writes(), [])

    def test_a_train_build_waits_behind_the_review_and_is_named_by_its_build_number(self):
        # A train that sorts below live builds (a bare 0.4 under 0.4.8 and 0.4.5) is reported; the pinned
        # trains (client/ios/project.yml MEWLA_TESTFLIGHT_VERSION) never are, see the next test.
        builds = [build("b40999", "40999", "0.4", "READY_FOR_BETA_SUBMISSION")] + NOW
        client = FakeClient(builds, NOW_REVIEWS)
        result = tf.run(client, APP, GROUP)
        self.assertEqual(client.writes(), [])
        self.assertEqual((result["waiting"]["build_number"], result["waiting"]["release"]), ("40999", "0.4.9"))
        text = tf.markdown(result)
        self.assertIn("- Waiting: 0.4.9 (40999, TestFlight 0.4), submitted once the review above ends", text)
        # 0.4.8 and 0.4.5 have a higher marketing version than the train, so TestFlight lists them above it.
        self.assertEqual(numbers(result["above_train"]), ["40899", "40599"])
        self.assertIn("- Listed above the newest build in TestFlight (higher marketing version): "
                      "0.4.8 (40899), 0.4.5 (40599)", text)

    def test_the_pinned_0_4_9_train_waits_behind_0_4_8_and_is_listed_newest(self):
        builds = [build("b40999", "40999", "0.4.9", "READY_FOR_BETA_SUBMISSION")] + NOW
        result = tf.run(FakeClient(builds, NOW_REVIEWS), APP, GROUP)
        self.assertEqual(result["waiting"]["build_number"], "40999")
        self.assertEqual(result["above_train"], [])
        later = [build("b41099", "41099", "0.4.9", "READY_FOR_BETA_SUBMISSION"),
                 build("b40999", "40999", "0.4.9", "IN_BETA_TESTING"),
                 build("b40899", "40899", "0.4.8", "IN_BETA_TESTING"),
                 build("b40599", "40599", "0.4.5", "IN_BETA_TESTING")]
        approved = {"b40999": "APPROVED", "b40899": "APPROVED", "b40599": "APPROVED"}
        result = tf.run(FakeClient(later, approved), APP, GROUP)
        self.assertEqual((result["submit"]["release"], result["above_train"]), ("0.4.10", []))
        self.assertIn("0.4.10 (41099, TestFlight 0.4.9)", tf.markdown(result))

    def test_the_next_build_of_a_train_is_submitted_once_its_first_is_approved(self):
        builds = [
            build("b41099", "41099", "0.4.9", "READY_FOR_BETA_SUBMISSION"),
            build("b40999", "40999", "0.4.9", "IN_BETA_TESTING"),
        ]
        client = FakeClient(builds, {"b40999": "APPROVED"})
        result = tf.run(client, APP, GROUP)
        self.assertEqual(client.writes(), [("POST", "/betaAppReviewSubmissions")])
        self.assertEqual(result["submit"]["release"], "0.4.10")
        self.assertEqual(result["above_train"], [])

    def test_a_new_train_is_not_outranked(self):
        builds = [build("b50099", "50099", "0.5", "READY_FOR_BETA_SUBMISSION")] + NOW
        result = tf.run(FakeClient(builds, {"b40599": "APPROVED", "b40899": "APPROVED"}), APP, GROUP)
        self.assertEqual(result["above_train"], [])

    def test_a_build_not_ready_or_outside_the_group_is_not_submitted(self):
        approved = build("b40599", "40599", "0.4.5", "IN_BETA_TESTING")
        for row, in_group in (
            (build("b40999", "40999", "0.4.9", "MISSING_EXPORT_COMPLIANCE"), None),
            (build("b40999", "40999", "0.4.9", "PROCESSING", processing="PROCESSING"), None),
            (build("b40999", "40999", "0.4.9", "READY_FOR_BETA_SUBMISSION"), ["b40599"]),
        ):
            client = FakeClient([row, approved], {"b40599": "APPROVED"}, in_group=in_group)
            result = tf.run(client, APP, GROUP)
            self.assertEqual((client.writes(), result["submit"]), ([], None))

    def test_nothing_at_or_below_the_newest_approved_build_is_expired(self):
        builds = [
            build("b40999", "40999", "0.4.9", "READY_FOR_BETA_SUBMISSION"),
            build("b40599", "40599", "0.4.5", "IN_BETA_TESTING"),
            build("b40400", "40400", "0.4.4", "READY_FOR_BETA_SUBMISSION"),
            build("b40300", "40300", "0.4.3", "BETA_APPROVED"),
        ]
        client = FakeClient(builds, {"b40599": "APPROVED", "b40300": "APPROVED"})
        result = tf.run(client, APP, GROUP)
        self.assertEqual(client.writes(), [("POST", "/betaAppReviewSubmissions")])
        self.assertEqual(result["expire"], [])

    def test_a_missing_external_state_is_looked_up(self):
        builds = [build("b40999", "40999", "0.4.9", None)] + NOW
        client = FakeClient(builds, NOW_REVIEWS, lookups={"b40999": "READY_FOR_BETA_SUBMISSION"})
        self.assertEqual(tf.run(client, APP, GROUP)["waiting"]["external"], "READY_FOR_BETA_SUBMISSION")
        self.assertIn(("GET", "/builds/b40999/buildBetaDetail", None), client.calls)

    def test_a_dry_run_only_reads(self):
        builds = [
            build("b41099", "41099", "0.4.10", "READY_FOR_BETA_SUBMISSION"),
            build("b40999", "40999", "0.4.9", "READY_FOR_BETA_SUBMISSION"),
            build("b40599", "40599", "0.4.5", "IN_BETA_TESTING"),
        ]
        client = FakeClient(builds, {"b40599": "APPROVED"})
        result = tf.run(client, APP, GROUP, dry_run=True)
        self.assertEqual({m for m, _, _ in client.calls}, {"GET"})
        self.assertEqual((result["submit"]["build_number"], numbers(result["expire"])), ("41099", ["40999"]))
        text = tf.markdown(result)
        self.assertIn("(dry run)", text)
        self.assertIn("0.4.10 (41099), would submit", text)
        self.assertIn("0.4.9 (40999) would expire", text)
        self.assertFalse(tf.failed(result))
        with self.assertRaises(RuntimeError):
            tf.ReadOnly(client).api_request("PATCH", "/builds/b1", {})

    def test_a_refusal_fails_the_run_and_a_lost_race_does_not(self):
        builds = [
            build("b41099", "41099", "0.4.10", "READY_FOR_BETA_SUBMISSION"),
            build("b40999", "40999", "0.4.9", "READY_FOR_BETA_SUBMISSION"),
            build("b40599", "40599", "0.4.5", "IN_BETA_TESTING"),
        ]
        client = FakeClient(builds, {"b40599": "APPROVED"}, refuse={"b40999"})
        result = tf.run(client, APP, GROUP)
        self.assertTrue(result["submit"]["done"])
        self.assertEqual((result["expire"][0]["status"], result["expire"][0]["done"]), (409, False))
        self.assertTrue(tf.failed(result))
        self.assertIn("refused (HTTP 409)", tf.markdown(result))
        client = FakeClient(builds, {"b40599": "APPROVED"}, refuse={"b41099"})
        self.assertTrue(tf.failed(tf.run(client, APP, GROUP)))
        # The release and the hourly job submitted it at once: the other one's submission counts.
        client = FakeClient(builds[:1] + builds[2:], {"b40599": "APPROVED"}, race=True)
        result = tf.run(client, APP, GROUP)
        self.assertEqual((result["submit"]["status"], result["submit"]["done"]), (409, True))
        self.assertFalse(tf.failed(result))


if __name__ == "__main__":
    unittest.main()
