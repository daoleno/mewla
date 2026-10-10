import importlib.util
import json
import os
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

ROOT_BASE = json.loads((ROOT / "app/app.base.json").read_text(encoding="utf-8"))
CURRENT_VERSION = ROOT_BASE["expo"]["version"]
CURRENT_TAG = f"v{CURRENT_VERSION}"
CURRENT_VERSION_CODE = ROOT_BASE["expo"]["android"]["versionCode"]
CURRENT_IOS_BUILD = json.loads(
    (ROOT / "app/ios-build.json").read_text(encoding="utf-8")
)["buildNumber"]
NEXT_VERSION = prepare_release.next_release_version(CURRENT_VERSION)
NEXT_TAG = f"v{NEXT_VERSION}"
CURRENT_CORE = tuple(int(part) for part in CURRENT_VERSION.split("-", 1)[0].split("."))
STABLE_TARGET = f"{CURRENT_CORE[0]}.{CURRENT_CORE[1]}.{CURRENT_CORE[2] + 1}"
INDEXED_NOTE_PATHS = tuple(
    f"docs/releases/{tag}.md"
    for tag in prepare_release.CHANGELOG_ENTRY_RE.findall(
        (ROOT / "CHANGELOG.md").read_text(encoding="utf-8")
    )
)

FIXTURE_PATHS = (
    "CHANGELOG.md",
    "app/app.base.json",
    "app/ios-build.json",
    "daemon/cmd/mewla/version.go",
    "scripts/verify-release-identity.sh",
    "docs/install-daemon.md",
    "docs/ios-ci-release.md",
) + INDEXED_NOTE_PATHS
CERTIFICATE = prepare_release.extract_certificate(
    (ROOT / "scripts/verify-release-identity.sh").read_text(encoding="utf-8")
)
USER_NOTES = """Pets are easier to find.

## New

- The cat waits on the Work that needs you.
  Tap it to open that Work.

## Fixed

- Pets no longer shrink after a hop.
"""


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


class ChangelogValidationTests(unittest.TestCase):
    def test_accepts_older_semver_core_history(self):
        scratch = os.environ.get("MEWLA_BUILD_TMPDIR") or os.environ.get("TMPDIR")
        with tempfile.TemporaryDirectory(
            prefix="mewla-changelog-", dir=Path(scratch) if scratch else None
        ) as temporary:
            root = Path(temporary)
            notes = root / "docs/releases"
            notes.mkdir(parents=True)
            for tag in ("v0.1.1-beta.1", "v0.1.0-beta.8"):
                (notes / f"{tag}.md").write_text(
                    f"# Mewla {tag}\n", encoding="utf-8"
                )
            changelog = (
                prepare_release.CHANGELOG_PREFIX
                + "- [v0.1.1-beta.1](docs/releases/v0.1.1-beta.1.md)\n"
                + "- [v0.1.0-beta.8](docs/releases/v0.1.0-beta.8.md)\n"
            )
            prepare_release.validate_changelog(
                root,
                changelog,
                "0.1.1-beta.1",
                "v0.1.1-beta.2",
            )


class UserNotesTests(unittest.TestCase):
    def test_splits_summary_and_sections_in_canonical_order(self):
        summary, sections = prepare_release.parse_user_notes(
            "Plain summary.\n\n## Fixed\n\n- A fix.\n", "notes.md"
        )
        self.assertEqual(summary, "Plain summary.")
        self.assertEqual(sections, {"Fixed": "- A fix."})

        summary, sections = prepare_release.parse_user_notes(
            "Summary.\n\n## Fixed\n\n- A fix.\n\n## New\n\n- A change.\n",
            "notes.md",
        )
        self.assertEqual(list(sections), ["New", "Fixed"])

        summary, sections = prepare_release.parse_user_notes(USER_NOTES, "notes.md")
        self.assertEqual(summary, "Pets are easier to find.")
        self.assertEqual(
            sections["New"],
            "- The cat waits on the Work that needs you.\n  Tap it to open that Work.",
        )

    def test_rejects_missing_empty_or_malformed_user_notes(self):
        cases = {
            "empty": ("", "missing the one-sentence summary"),
            "no-summary": ("## New\n\n- A change.\n", "missing the one-sentence summary"),
            "no-sections": ("Only a summary.\n", "missing ## New or ## Fixed"),
            "empty-section": (
                "Summary.\n\n## New\n\n## Fixed\n\n- A fix.\n",
                "## New has no bullets",
            ),
            "other-heading": (
                "Summary.\n\n## Highlights\n\n- A change.\n",
                "unexpected heading '## Highlights'",
            ),
            "commit-style-heading": (
                "Summary.\n\n## What changed\n\n- Add x (`abc1234`)\n",
                "unexpected heading '## What changed'",
            ),
            "duplicate": (
                "Summary.\n\n## New\n\n- A.\n\n## New\n\n- B.\n",
                "duplicate ## New",
            ),
            "prose-in-section": (
                "Summary.\n\n## New\n\nA paragraph.\n",
                "may contain only '- ' bullets",
            ),
            "bullet-in-summary": (
                "- A change.\n\n## New\n\n- A.\n",
                "put bullets under ## New or ## Fixed",
            ),
            "two-paragraph-summary": (
                "One.\n\nTwo.\n\n## New\n\n- A.\n",
                "one short paragraph",
            ),
        }
        for name, (text, error) in cases.items():
            with self.subTest(name=name):
                with self.assertRaises(prepare_release.PrepareError) as raised:
                    prepare_release.parse_user_notes(text, "docs/releases/reviewed/v9.md")
                message = str(raised.exception)
                self.assertIn("docs/releases/reviewed/v9.md", message)
                self.assertIn(error, message)
                self.assertIn("## New", message)

    def test_reads_the_certificate_from_the_release_verifier(self):
        self.assertRegex(CERTIFICATE, r"^[0-9A-F]{2}(:[0-9A-F]{2}){31}$")
        for source in ("", 'EXPECTED_CERT_FP="AB:CD"\n'):
            with self.subTest(source=source):
                with self.assertRaises(prepare_release.PrepareError):
                    prepare_release.extract_certificate(source)


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
        if user_notes:
            reviewed = root / "docs/releases/reviewed"
            reviewed.mkdir(parents=True, exist_ok=True)
            for tag, text in user_notes.items():
                (reviewed / f"{tag}.md").write_text(text, encoding="utf-8")
            git(root, "add", "docs/releases/reviewed")
            git(root, "commit", "-m", "Write user-facing release notes")
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

        base = json.loads((root / "app/app.base.json").read_text(encoding="utf-8"))
        self.assertEqual(base["expo"]["version"], STABLE_TARGET)
        notes = (root / f"docs/releases/v{STABLE_TARGET}.md").read_text(
            encoding="utf-8"
        )
        self.assertTrue(notes.startswith(f"# Mewla {STABLE_TARGET}\n\n"))
        self.assertIn(f"mewla-android-arm64-v{STABLE_TARGET}.apk", notes)
        self.assertIn(f"/compare/{CURRENT_TAG}...v{STABLE_TARGET})", notes)
        changelog = (root / "CHANGELOG.md").read_text(encoding="utf-8")
        self.assertLess(
            changelog.index(f"v{STABLE_TARGET}"), changelog.index(CURRENT_TAG)
        )
        ios_docs = (root / "docs/ios-ci-release.md").read_text(encoding="utf-8")
        self.assertIn(f"marketing version `{STABLE_TARGET}`", ios_docs)

    def test_updates_exact_identity_files_and_writes_user_notes(self):
        root = self.create_repo()

        result = self.run_script(root)
        self.assertEqual(result.returncode, 0, result.stderr)
        output = json.loads(result.stdout)
        self.assertEqual(output["current_version"], CURRENT_VERSION)
        self.assertEqual(output["next_version"], NEXT_VERSION)
        self.assertEqual(output["next_tag"], NEXT_TAG)
        self.assertEqual(output["android_version_code"], CURRENT_VERSION_CODE + 1)
        self.assertEqual(output["ios_build_number"], CURRENT_IOS_BUILD + 1)
        self.assertEqual(output["commit_count"], 2)

        next_notes_path = f"docs/releases/{NEXT_TAG}.md"
        expected_paths = sorted(
            (
                "CHANGELOG.md",
                "app/app.base.json",
                "app/ios-build.json",
                "daemon/cmd/mewla/version.go",
                "docs/install-daemon.md",
                "docs/ios-ci-release.md",
                next_notes_path,
                "scripts/verify-release-identity.sh",
            )
        )
        self.assertEqual(output["changed_paths"], expected_paths)
        self.assertEqual(
            sorted(git(root, "status", "--short").splitlines()),
            sorted(
                [
                    " M CHANGELOG.md",
                    " M app/app.base.json",
                    " M app/ios-build.json",
                    " M daemon/cmd/mewla/version.go",
                    " M docs/install-daemon.md",
                    " M docs/ios-ci-release.md",
                    " M scripts/verify-release-identity.sh",
                    f"?? {next_notes_path}",
                ]
            ),
        )

        base = json.loads((root / "app/app.base.json").read_text(encoding="utf-8"))
        self.assertEqual(base["expo"]["version"], NEXT_VERSION)
        self.assertEqual(
            base["expo"]["android"]["versionCode"], CURRENT_VERSION_CODE + 1
        )
        self.assertEqual(
            json.loads((root / "app/ios-build.json").read_text(encoding="utf-8"))[
                "buildNumber"
            ],
            CURRENT_IOS_BUILD + 1,
        )
        self.assertIn(
            f'var Version = "{NEXT_VERSION}"',
            (root / "daemon/cmd/mewla/version.go").read_text(encoding="utf-8"),
        )

        verifier = (root / "scripts/verify-release-identity.sh").read_text(
            encoding="utf-8"
        )
        self.assertNotIn(CURRENT_VERSION, verifier)
        self.assertEqual(verifier.count(NEXT_VERSION), 3)
        self.assertIn(
            f'EXPECTED_VERSION_CODE="{CURRENT_VERSION_CODE + 1}"', verifier
        )
        self.assertIn(
            f'EXPECTED_IOS_BUILD_NUMBER="{CURRENT_IOS_BUILD + 1}"', verifier
        )
        self.assertIn(
            f"MEWLA_VERSION={NEXT_TAG}",
            (root / "docs/install-daemon.md").read_text(encoding="utf-8"),
        )
        ios_docs = (root / "docs/ios-ci-release.md").read_text(encoding="utf-8")
        self.assertIn(f"`{NEXT_VERSION}`", ios_docs)
        self.assertIn(
            f"baseline is build `{CURRENT_IOS_BUILD + 1}` in `app/ios-build.json`",
            ios_docs,
        )

        changelog = (root / "CHANGELOG.md").read_text(encoding="utf-8")
        self.assertLess(changelog.index(NEXT_TAG), changelog.index(CURRENT_TAG))
        self.assertEqual(changelog.count(f"docs/releases/{NEXT_TAG}.md"), 1)
        self.assertEqual(changelog.count(f"docs/releases/{CURRENT_TAG}.md"), 1)

        notes = (root / next_notes_path).read_text(encoding="utf-8")
        self.assertEqual(
            notes.split("## Get it", 1)[0],
            f"# Mewla {NEXT_VERSION}\n\n" + USER_NOTES + "\n",
        )
        for marker in (
            "- **Already installed?** Run `mewla update`",
            f"`curl -fsSL {prepare_release.INSTALL_SCRIPT_URL} | sh`",
            "- **Mac (Apple Silicon):** `mewla-darwin-arm64.tar.gz`",
            "- **Linux x64:** `mewla-linux-amd64.tar.gz`",
            "- **Linux ARM:** `mewla-linux-arm64.tar.gz`",
            f"- **Android:** `mewla-android-arm64-{NEXT_TAG}.apk`",
            "unknown sources",
            f"https://github.com/daoleno/mewla/compare/{CURRENT_TAG}...{NEXT_TAG}",
            '<details markdown="1">\n<summary>Verify downloads</summary>\n\n',
            "SHA256SUMS",
            f"```text\n{CERTIFICATE}\n```\n\n</details>\n",
        ):
            self.assertIn(marker, notes)
        for dropped in (
            "Add reviewed release change",
            "Write user-facing release notes",
            "Source tag",
            "versionCode",
            "com.daoleno.mewla",
            "TestFlight",
            "Obtainium",
            "As of",
        ):
            self.assertNotIn(dropped, notes)

    def test_uses_only_the_target_versions_user_notes(self):
        root = self.create_repo(
            user_notes={
                NEXT_TAG: USER_NOTES,
                "v99.0.0": "Unrelated future notes.\n\n## New\n\n- Later.\n",
            }
        )

        result = self.run_script(root)
        self.assertEqual(result.returncode, 0, result.stderr)
        notes = (root / f"docs/releases/{NEXT_TAG}.md").read_text(encoding="utf-8")
        self.assertIn("- Pets no longer shrink after a hop.", notes)
        self.assertNotIn("Unrelated future notes", notes)
        self.assertNotIn(
            f"docs/releases/reviewed/{NEXT_TAG}.md",
            json.loads(result.stdout)["changed_paths"],
        )

    def test_fails_closed_without_user_facing_notes(self):
        cases = {
            "missing": ({}, f"missing docs/releases/reviewed/{NEXT_TAG}.md"),
            "empty": ({NEXT_TAG: "\n"}, "missing the one-sentence summary"),
            "no-bullets": (
                {NEXT_TAG: "Summary.\n\n## New\n"},
                "## New has no bullets",
            ),
        }
        for name, (user_notes, error) in cases.items():
            with self.subTest(name=name):
                root = self.create_repo(
                    root=self.fixture / name, user_notes=user_notes
                )
                original_base = (root / "app/app.base.json").read_bytes()

                result = self.run_script(root)

                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)
                self.assertIn("## Fixed", result.stderr)
                self.assertNotIn("Add reviewed release change", result.stderr)
                self.assertEqual(
                    (root / "app/app.base.json").read_bytes(), original_base
                )
                self.assertFalse(
                    (root / f"docs/releases/{NEXT_TAG}.md").exists()
                )

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
        root = self.create_repo()
        (root / "app/ios-build.json").unlink()
        git(root, "add", "app/ios-build.json")
        git(root, "commit", "-m", "Remove iOS identity source")
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing required file: app/ios-build.json", result.stderr)
        self.assertFalse((root / f"docs/releases/{NEXT_TAG}.md").exists())

    def test_fails_closed_when_next_output_or_tag_exists(self):
        for existing in ("notes", "tag"):
            with self.subTest(existing=existing):
                root = self.create_repo(root=self.fixture / existing)
                if existing == "notes":
                    notes = root / f"docs/releases/{NEXT_TAG}.md"
                    notes.write_text("existing\n", encoding="utf-8")
                    git(root, "add", str(notes.relative_to(root)))
                    git(root, "commit", "-m", "Reserve beta 9 notes")
                else:
                    git(
                        root,
                        "tag",
                        "-a",
                        NEXT_TAG,
                        "-m",
                        f"Existing {NEXT_TAG}",
                    )
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
        self.assertLess(
            workflow.index("Test release preparation and workflow contracts"),
            workflow.index("Prepare deterministic release identity and notes"),
        )
        release_job_header = workflow.split("steps:", 1)[0]
        self.assertNotIn("runner.temp", release_job_header)
        self.assertEqual(
            workflow.count("MEWLA_BUILD_TMPDIR: ${{ runner.temp }}/mewla-build"),
            1,
        )
        test_step = workflow.split(
            "- name: Test release preparation and workflow contracts",
            1,
        )[1].split("- name: Prepare deterministic release identity and notes", 1)[0]
        self.assertLess(
            test_step.index('mkdir -p "$MEWLA_BUILD_TMPDIR"'),
            test_step.index("python3 -m unittest discover"),
        )
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
            "go test ./cmd/mewla",
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
            "gh workflow run ios-release.yml",
            '-f "build_number=$IOS_BUILD_NUMBER"',
            '-f "app_identity=preview"',
            '-f "destination=testflight"',
            "steps.release.outputs.ios_build_number",
            "actions: write",
            "contents: write",
        ):
            self.assertIn(marker, workflow)
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
