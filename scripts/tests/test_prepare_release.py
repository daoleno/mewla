import importlib.util
import json
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "prepare-release.py"
SPEC = importlib.util.spec_from_file_location("prepare_release", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
prepare_release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(prepare_release)
release_notes = prepare_release.release_notes

VERSION_SOURCE = "client/ui/nsh.json"
IOS_PROJECT = "client/ios/project.yml"
release_identity = prepare_release.release_identity
CURRENT_VERSION = release_identity.read_version(ROOT)
CURRENT_TAG = f"v{CURRENT_VERSION}"
CURRENT_CODE = release_identity.build_code(CURRENT_VERSION)
NEXT_VERSION = prepare_release.next_release_version(CURRENT_VERSION)
NEXT_CODE = release_identity.build_code(NEXT_VERSION)
NEXT_TAG = f"v{NEXT_VERSION}"
CURRENT_CORE = tuple(int(part) for part in CURRENT_VERSION.split("-", 1)[0].split("."))
STABLE_TARGET = f"{CURRENT_CORE[0]}.{CURRENT_CORE[1]}.{CURRENT_CORE[2] + 1}"
FIXTURE_PATHS = (
    VERSION_SOURCE,
    IOS_PROJECT,
    "daemon/cmd/mewla/version.go",
    "scripts/verify-release-identity.sh",
    "docs/install-daemon.md",
    "client/core/update/version.ts",
)
# The release notes are written at release time; a release only needs its version (and What's New).
USER_NOTES = ""


def git(root: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", *args],
        cwd=root,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return result.stdout.rstrip("\n")


class NextBetaVersionTests(unittest.TestCase):
    def test_increments_only_beta_ordinal(self):
        self.assertEqual(
            prepare_release.next_beta_version("0.1.0-beta.22"),
            "0.1.0-beta.23",
        )
        self.assertEqual(
            prepare_release.next_beta_version("12.34.56-beta.99"),
            "12.34.56-beta.100",
        )

    def test_rejects_noncanonical_versions(self):
        for value in (
            "v0.1.0-beta.8",
            "0.1.0",
            "0.1.0-beta.0",
            "0.1.0-beta.08",
            "01.1.0-beta.8",
        ):
            with self.subTest(value=value):
                with self.assertRaises(prepare_release.PrepareError):
                    prepare_release.next_beta_version(value)

    def test_default_release_increment_handles_stable_versions(self):
        self.assertEqual(prepare_release.next_release_version("0.1.2"), "0.1.3")
        self.assertEqual(
            prepare_release.next_release_version("0.1.2-beta.9"),
            "0.1.2-beta.10",
        )


class ExplicitReleaseVersionTests(unittest.TestCase):
    def test_accepts_newer_stable_and_beta_targets(self):
        self.assertEqual(
            prepare_release.validate_target_version("0.1.0-beta.22", "0.1.0"),
            "0.1.0",
        )
        self.assertEqual(
            prepare_release.validate_target_version("0.1.0-beta.22", "0.1.2"),
            "0.1.2",
        )
        self.assertEqual(
            prepare_release.validate_target_version("0.1.2", "0.1.3-beta.1"),
            "0.1.3-beta.1",
        )

    def test_rejects_malformed_or_non_increasing_targets(self):
        for target in (
            "v0.1.2",
            "0.1",
            "0.1.2-rc.1",
            "0.1.0-beta.0",
            "0.1.0-beta.22",
            "0.0.9",
        ):
            with self.subTest(target=target):
                with self.assertRaises(prepare_release.PrepareError):
                    prepare_release.validate_target_version(
                        "0.1.0-beta.22", target
                    )


class PrepareReleaseIntegrationTests(unittest.TestCase):
    def setUp(self):
        scratch = os.environ.get("MEWLA_BUILD_TMPDIR") or os.environ.get("TMPDIR")
        scratch_path = Path(scratch) if scratch else None
        if scratch_path is not None:
            scratch_path.mkdir(parents=True, exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(
            prefix="mewla-prepare-release-", dir=scratch_path
        )
        self.addCleanup(self.temp.cleanup)
        self.fixture = Path(self.temp.name)

    def create_repo(
        self,
        *,
        with_commit: bool = True,
        root: Path | None = None,
        user_notes: dict[str, str] | None = None,
        whats_new: list[str] | None = None,
    ) -> Path:
        root = root or self.fixture
        for relative in FIXTURE_PATHS:
            source = ROOT / relative
            target = root / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)

        git(root, "init", "-b", "main")
        git(root, "config", "user.name", "Mewla Test")
        git(root, "config", "user.email", "mewla-test@example.invalid")
        git(root, "add", "--all")
        git(root, "commit", "-m", f"Release {CURRENT_TAG}")
        git(
            root,
            "tag",
            "-a",
            CURRENT_TAG,
            "-m",
            f"Mewla {CURRENT_TAG}",
        )
        if with_commit:
            (root / "feature.txt").write_text(
                "reviewed release change\n", encoding="utf-8"
            )
            git(root, "add", "feature.txt")
            git(root, "commit", "-m", "Add reviewed release change")
        if user_notes is None:
            user_notes = {NEXT_TAG: USER_NOTES}
        if whats_new is None:
            whats_new = [tag.removeprefix("v") for tag in user_notes]
        if whats_new:
            # What's New for each release (release-notes/), with the bundles the app and daemon ship.
            notes = root / release_notes.SOURCE_DIR
            notes.mkdir(parents=True, exist_ok=True)
            for version in whats_new:
                (notes / f"{version}.json").write_text(json.dumps({
                    "version": version,
                    "date": "2026-11-16",
                    "highlights": [],
                    "fixes": ["Pets are easier to find."],
                }), encoding="utf-8")
            self.assertEqual(release_notes.main(["--repo", str(root)]), 0)
        if whats_new:
            git(root, "add", "--all")
            git(root, "commit", "-m", "Write What's New")
        return root

    def run_script(
        self, root: Path, version: str | None = None
    ) -> subprocess.CompletedProcess[str]:
        command = [str(SCRIPT), "--repo", str(root)]
        if version is not None:
            command.extend(("--version", version))
        return subprocess.run(
            command,
            cwd=ROOT,
            check=False,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )

    def test_prepares_an_explicit_stable_release(self):
        root = self.create_repo(user_notes={f"v{STABLE_TARGET}": USER_NOTES})

        result = self.run_script(root, STABLE_TARGET)
        self.assertEqual(result.returncode, 0, result.stderr)
        output = json.loads(result.stdout)
        self.assertEqual(output["current_version"], CURRENT_VERSION)
        self.assertEqual(output["next_version"], STABLE_TARGET)
        self.assertEqual(output["next_tag"], f"v{STABLE_TARGET}")

        manifest = json.loads((root / VERSION_SOURCE).read_text(encoding="utf-8"))
        self.assertEqual(manifest["version"], STABLE_TARGET)
        project = (root / IOS_PROJECT).read_text(encoding="utf-8")
        self.assertIn(f'MARKETING_VERSION: "{STABLE_TARGET}"\n', project)
        self.assertIn(
            f'CURRENT_PROJECT_VERSION: "{release_identity.build_code(STABLE_TARGET)}"\n', project
        )

    def test_a_beta_keeps_the_marketing_version_and_orders_below_its_release(self):
        beta = f"{STABLE_TARGET}-beta.1"
        root = self.create_repo(user_notes={f"v{beta}": USER_NOTES})

        result = self.run_script(root, beta)
        self.assertEqual(result.returncode, 0, result.stderr)
        output = json.loads(result.stdout)
        code = release_identity.build_code(beta)
        self.assertEqual(output["android_version_code"], code)
        self.assertEqual(output["ios_build_number"], code)
        self.assertGreater(code, CURRENT_CODE)
        self.assertLess(code, release_identity.build_code(STABLE_TARGET))
        project = (root / IOS_PROJECT).read_text(encoding="utf-8")
        self.assertIn(f'MARKETING_VERSION: "{STABLE_TARGET}"\n', project)
        self.assertIn(f'CURRENT_PROJECT_VERSION: "{code}"\n', project)
        # The app reports the release itself (Info.plist MewlaVersion), beta suffix included.
        self.assertIn(f'MEWLA_APP_VERSION: "{beta}"\n', project)

    def test_a_patch_keeps_the_testflight_train_and_a_minor_bump_opens_the_next(self):
        train = release_identity.read_testflight_version(ROOT)
        root = self.create_repo(user_notes={f"v{STABLE_TARGET}": USER_NOTES})
        result = self.run_script(root, STABLE_TARGET)
        self.assertEqual(result.returncode, 0, result.stderr)
        project = (root / IOS_PROJECT).read_text(encoding="utf-8")
        self.assertIn(f'MEWLA_TESTFLIGHT_VERSION: "{train}"\n', project)

        minor = f"{CURRENT_CORE[0]}.{CURRENT_CORE[1] + 1}.0-beta.1"
        root = self.create_repo(root=self.fixture / "minor", user_notes={f"v{minor}": USER_NOTES})
        result = self.run_script(root, minor)
        self.assertEqual(result.returncode, 0, result.stderr)
        project = (root / IOS_PROJECT).read_text(encoding="utf-8")
        # The minor's first iOS release names its train, above every train before it.
        self.assertIn(f'MEWLA_TESTFLIGHT_VERSION: "{CURRENT_CORE[0]}.{CURRENT_CORE[1] + 1}.0"\n', project)
        self.assertEqual(project.count("MEWLA_TESTFLIGHT_VERSION:"), 1)

    def test_updates_exactly_the_identity_files(self):
        root = self.create_repo()

        result = self.run_script(root)
        self.assertEqual(result.returncode, 0, result.stderr)
        output = json.loads(result.stdout)
        self.assertEqual(output["current_version"], CURRENT_VERSION)
        self.assertEqual(output["next_version"], NEXT_VERSION)
        self.assertEqual(output["next_tag"], NEXT_TAG)
        self.assertEqual(output["android_version_code"], NEXT_CODE)
        self.assertEqual(output["ios_build_number"], NEXT_CODE)
        self.assertGreater(NEXT_CODE, CURRENT_CODE)
        self.assertEqual(output["commit_count"], 2)

        expected_paths = sorted(
            (
                VERSION_SOURCE,
                IOS_PROJECT,
                "daemon/cmd/mewla/version.go",
                "docs/install-daemon.md",
            )
        )
        self.assertEqual(output["changed_paths"], expected_paths)
        self.assertEqual(
            sorted(git(root, "status", "--short").splitlines()),
            sorted(
                [
                    f" M {VERSION_SOURCE}",
                    f" M {IOS_PROJECT}",
                    " M daemon/cmd/mewla/version.go",
                    " M docs/install-daemon.md",
                ]
            ),
        )

        manifest = json.loads((root / VERSION_SOURCE).read_text(encoding="utf-8"))
        self.assertEqual(manifest["version"], NEXT_VERSION)
        project = (root / IOS_PROJECT).read_text(encoding="utf-8")
        self.assertIn(
            f'MARKETING_VERSION: "{release_identity.marketing_version(NEXT_VERSION)}"\n', project
        )
        self.assertIn(f'CURRENT_PROJECT_VERSION: "{NEXT_CODE}"\n', project)
        self.assertIn(f'MEWLA_APP_VERSION: "{NEXT_VERSION}"\n', project)
        self.assertIn(
            f'var Version = "{NEXT_VERSION}"',
            (root / "daemon/cmd/mewla/version.go").read_text(encoding="utf-8"),
        )

        # The verifier derives the identity from the files above, so a
        # release never rewrites it.
        verifier = (root / "scripts/verify-release-identity.sh").read_text(
            encoding="utf-8"
        )
        self.assertNotIn(CURRENT_VERSION, verifier)
        self.assertNotIn(NEXT_VERSION, verifier)
        self.assertIn(
            f"MEWLA_VERSION={NEXT_TAG}",
            (root / "docs/install-daemon.md").read_text(encoding="utf-8"),
        )

    def test_refuses_a_version_without_its_whats_new(self):
        # Notes for earlier releases only: this one has none of its own.
        root = self.create_repo(root=self.fixture / "none", whats_new=[CURRENT_VERSION])
        original_base = (root / VERSION_SOURCE).read_bytes()
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(
            result.stderr.strip(),
            f"error: Mewla {NEXT_VERSION} has no What's New: write release-notes/{NEXT_VERSION}.json as "
            'release-notes/README.md says (for a release of fixes only, empty "highlights" and its "fixes"), '
            "then run python3 scripts/release-notes.py.",
        )
        self.assertEqual((root / VERSION_SOURCE).read_bytes(), original_base)
        self.assertEqual(git(root, "status", "--porcelain"), "")

        # No notes at all: the same refusal.
        root = self.create_repo(root=self.fixture / "empty", whats_new=[])
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"Mewla {NEXT_VERSION} has no What's New", result.stderr)

        # With its file (fixes only), the same tree passes this step.
        root = self.create_repo(root=self.fixture / "fixes-only", whats_new=[CURRENT_VERSION, NEXT_VERSION])
        result = self.run_script(root)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_refuses_a_release_whose_whats_new_is_a_wall_of_text(self):
        # Owner 11-22: 0.4.19's notes were nine long fixes and no highlight. A release from the gift rule on cannot be
        # prepared that way: too many fixes, each too long (scripts/lib/release_notes.py).
        root = self.create_repo(root=self.fixture / "wall", whats_new=[CURRENT_VERSION, NEXT_VERSION])
        notes = root / release_notes.SOURCE_DIR / f"{NEXT_VERSION}.json"
        notes.write_text(json.dumps({
            "version": NEXT_VERSION,
            "date": "2026-11-22",
            "highlights": [],
            "fixes": [f"Fix {i}: something changed and this line explains it at length, as a changelog would." for i in range(9)],
        }), encoding="utf-8")
        self.assertEqual(release_notes.main(["--repo", str(root)]), 1)
        git(root, "commit", "-am", "A wall of text")
        original_base = (root / VERSION_SOURCE).read_bytes()
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"release-notes/{NEXT_VERSION}.json: 9 fixes", result.stderr)
        self.assertEqual((root / VERSION_SOURCE).read_bytes(), original_base)

    def test_whats_new_copies_must_be_current(self):
        root = self.create_repo(root=self.fixture / "stale")
        (root / "client/ui/whatsnew/notes.json").write_text("{}\n", encoding="utf-8")
        git(root, "commit", "-am", "Stale bundle")
        original_base = (root / VERSION_SOURCE).read_bytes()
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not current: client/ui/whatsnew/notes.json", result.stderr)
        self.assertEqual((root / VERSION_SOURCE).read_bytes(), original_base)

    def test_rejects_a_minimum_computer_version_newer_than_the_release(self):
        root = self.create_repo()
        target = root / "client/core/update/version.ts"
        text = target.read_text(encoding="utf-8")
        target.write_text(
            re.sub(r'MIN_COMPUTER_VERSION = "[^"]+"', 'MIN_COMPUTER_VERSION = "99.0.0"', text),
            encoding="utf-8",
        )
        git(root, "add", "--all")
        git(root, "commit", "-m", "Raise the minimum computer version")
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("MIN_COMPUTER_VERSION 99.0.0 is newer than the release", result.stderr)

    def test_fails_closed_when_there_are_no_commits(self):
        result = self.run_script(
            self.create_repo(with_commit=False, user_notes={})
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"no commits exist after {CURRENT_TAG}", result.stderr)

    def test_fails_closed_on_dirty_state_before_writing(self):
        root = self.create_repo()
        (root / "unexpected.txt").write_text("dirty\n", encoding="utf-8")
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unexpected dirty state", result.stderr)
        self.assertFalse((root / f"docs/releases/{NEXT_TAG}.md").exists())

    def test_fails_closed_on_malformed_or_mismatched_identity(self):
        root = self.create_repo()
        version_go = root / "daemon/cmd/mewla/version.go"
        version_go.write_text(
            version_go.read_text(encoding="utf-8").replace(
                CURRENT_VERSION, "9.9.9-beta.999"
            ),
            encoding="utf-8",
        )
        git(root, "add", "daemon/cmd/mewla/version.go")
        git(root, "commit", "-m", "Introduce mismatched identity")
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("daemon/cmd/mewla/version.go", result.stderr)
        self.assertFalse((root / f"docs/releases/{NEXT_TAG}.md").exists())

    def test_fails_closed_on_missing_identity_source(self):
        for relative in (IOS_PROJECT, "client/core/update/version.ts"):
            with self.subTest(relative=relative):
                root = self.create_repo(root=self.fixture / Path(relative).name)
                (root / relative).unlink()
                git(root, "add", relative)
                git(root, "commit", "-m", "Remove an identity source")
                result = self.run_script(root)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f"missing required file: {relative}", result.stderr)
                self.assertFalse((root / f"docs/releases/{NEXT_TAG}.md").exists())

    def test_fails_closed_when_the_ios_project_disagrees_with_the_version(self):
        root = self.create_repo()
        project = root / IOS_PROJECT
        project.write_text(
            project.read_text(encoding="utf-8").replace(
                f'CURRENT_PROJECT_VERSION: "{CURRENT_CODE}"', 'CURRENT_PROJECT_VERSION: "1"'
            ),
            encoding="utf-8",
        )
        git(root, "add", IOS_PROJECT)
        git(root, "commit", "-m", "Introduce a stale build number")
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(IOS_PROJECT, result.stderr)
        self.assertFalse((root / f"docs/releases/{NEXT_TAG}.md").exists())

    def test_fails_closed_when_the_next_tag_exists(self):
        root = self.create_repo()
        git(root, "tag", "-a", NEXT_TAG, "-m", f"Existing {NEXT_TAG}")
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("already exist", result.stderr)


class ReleaseWorkflowContractTests(unittest.TestCase):
    def test_release_workflow_is_explicit_manual_atomic_and_build_gated(self):
        workflow = (ROOT / ".github/workflows/release-next-beta.yml").read_text(
            encoding="utf-8"
        )
        dispatch = workflow.split("workflow_dispatch:", 1)[1].split(
            "concurrency:", 1
        )[0]
        self.assertIn("inputs:", dispatch)
        self.assertIn("version:", dispatch)
        self.assertIn("required: true", dispatch)
        permissions = workflow.split("permissions:", 1)[1].split("jobs:", 1)[0]
        self.assertEqual(
            {
                line.strip()
                for line in permissions.splitlines()
                if line.strip()
            },
            {"actions: write", "contents: write"},
        )
        for marker in (
            "fetch-depth: 0",
            'PYTHONDONTWRITEBYTECODE: "1"',
            'START_SHA="$(git rev-parse refs/remotes/origin/main)"',
            './scripts/prepare-release.py --version "$TARGET_VERSION"',
            "./scripts/verify-release-identity.sh",
            'git config user.name "github-actions[bot]"',
            'git add --pathspec-from-file="$RUNNER_TEMP/mewla-release-paths"',
            'git commit -m "Prepare $NEXT_TAG"',
            'git tag -a "$NEXT_TAG" HEAD',
            'git rev-parse refs/remotes/origin/main)" == "$START_SHA"',
            "git push --atomic origin",
            '"HEAD:refs/heads/main"',
            '"refs/tags/$NEXT_TAG"',
            "gh workflow run release-artifacts.yml",
            '--ref "$NEXT_TAG"',
            '-f "ref=$NEXT_TAG"',
            '-f "publish=true"',
            "actions: write",
            "contents: write",
        ):
            self.assertIn(marker, workflow)
        # The iPhone build is dispatched by the published release, not at tag time.
        self.assertNotIn("ios-release.yml", workflow)
        self.assertNotIn("gh release", workflow)
        self.assertNotIn("gh pr", workflow)
        self.assertNotIn("pull-requests:", workflow)
        self.assertNotRegex(workflow, r"\n\s+push:")
        self.assertNotRegex(workflow, r"\n\s+pull_request:")
        self.assertNotIn("release-please", workflow)
        self.assertNotIn("PAT", workflow)
        self.assertNotIn("secrets.", workflow)


if __name__ == "__main__":
    unittest.main()
