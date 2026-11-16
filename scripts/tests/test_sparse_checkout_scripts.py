"""A workflow that checks out only some scripts (sparse-checkout) must list everything they load.

v0.4.9's iPhone release uploaded its build and then failed: ios-release.yml checked out
scripts/testflight-review-queue.py without scripts/lib/release_identity.py, which it imports. Each test
here copies exactly the paths one sparse-checkout lists into an empty tree and runs every listed script
there (`--help` loads all its imports, then exits), so a missing file fails here, not on the runner. It
also checks that every `<path>/scripts/...` a workflow runs from such a checkout is in its list.
"""

from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
WORKFLOWS = ROOT / ".github" / "workflows"


def sparse_checkouts(text: str) -> list[tuple[str, list[str]]]:
    """Each actions/checkout `with:` block that sets sparse-checkout: (its path, the listed paths)."""
    found = []
    for match in re.finditer(r"^(\s*)sparse-checkout:[ \t]*(.*)$", text, flags=re.MULTILINE):
        indent, inline = match.group(1), match.group(2).strip()
        if inline and inline not in ("|", "|-", ">", ">-"):
            paths = [inline]
        else:
            paths = []
            for line in text[match.end() + 1:].splitlines():
                if not line.strip():
                    continue
                if len(line) - len(line.lstrip()) <= len(indent):
                    break
                paths.append(line.strip())
        # The `with:` block it belongs to: from its `with:` line to the first line indented less than it.
        start = text.rfind("with:", 0, match.start())
        block = []
        for line in text[start:].splitlines()[1:]:
            if line.strip() and len(line) - len(line.lstrip()) < len(indent):
                break
            block.append(line)
        path = re.search(rf"^{indent}path:[ \t]*(\S+)", "\n".join(block), flags=re.MULTILINE)
        found.append((path.group(1) if path else "", paths))
    return found


class SparseCheckoutScriptTests(unittest.TestCase):
    def test_every_sparse_checked_script_runs_from_its_checkout_alone(self):
        checked = 0
        for workflow in sorted(WORKFLOWS.glob("*.yml")):
            for path, listed in sparse_checkouts(workflow.read_text()):
                with self.subTest(workflow=workflow.name, path=path or "."):
                    self.assertTrue(listed, "empty sparse-checkout list")
                    with tempfile.TemporaryDirectory() as directory:
                        tree = Path(directory)
                        for item in listed:
                            source = ROOT / item
                            self.assertTrue(source.is_file(), f"{item} is listed but is not a file")
                            (tree / item).parent.mkdir(parents=True, exist_ok=True)
                            shutil.copy2(source, tree / item)
                        for item in listed:
                            if not item.endswith(".py"):
                                continue
                            run = subprocess.run(
                                [sys.executable, "-I", str(tree / item), "--help"],
                                cwd=tree, capture_output=True, text=True, timeout=60,
                            )
                            self.assertEqual(
                                run.returncode, 0,
                                f"{item} does not run from {workflow.name}'s sparse checkout of {listed}:\n{run.stderr}",
                            )
                            checked += 1
        self.assertGreaterEqual(checked, 3)

    def test_every_script_a_workflow_runs_from_a_sparse_checkout_is_listed(self):
        for workflow in sorted(WORKFLOWS.glob("*.yml")):
            text = workflow.read_text()
            for path, listed in sparse_checkouts(text):
                if not path:
                    continue
                for used in sorted(set(re.findall(rf"{re.escape(path)}/([A-Za-z0-9_./-]+)", text))):
                    with self.subTest(workflow=workflow.name, used=used):
                        self.assertIn(used, listed, f"{workflow.name} runs {path}/{used}, which its sparse-checkout leaves out")

    def test_the_ios_release_checkout_is_found(self):
        checkouts = dict(sparse_checkouts((WORKFLOWS / "ios-release.yml").read_text()))
        self.assertIn("scripts/testflight-review-queue.py", checkouts[".release-automation"])


if __name__ == "__main__":
    unittest.main()
