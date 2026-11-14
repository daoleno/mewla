"""client/scripts/ui-test-shard.py: CI's UI shards run every test file `nsh test` would, each once."""

import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("ui_test_shard", ROOT / "client/scripts/ui-test-shard.py")
shard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shard)


class UITestShardTests(unittest.TestCase):
    def test_every_file_is_in_exactly_one_shard(self):
        files = shard.test_files()
        self.assertGreater(len(files), 0)
        weights = shard.load_weights()
        for count in (1, 2, 4, 7):
            buckets = shard.shards(files, count, weights)
            self.assertEqual(len(buckets), count)
            together = [name for bucket in buckets for name in bucket]
            self.assertEqual(sorted(together), files, f"{count} shards")

    def test_files_are_the_ones_nsh_test_runs(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            for path, text in {
                "a/One.test.tsx": 'import { test } from "nsh/test"\n',
                "b/two.test.ts": "import { expect } from 'nsh/test'\n",
                "b/Plain.test.ts": 'import { test } from "bun:test"\n',
                "c/Helper.tsx": 'import { test } from "nsh/test"\n',
                "node_modules/x/X.test.tsx": 'import { test } from "nsh/test"\n',
            }.items():
                (root / path).parent.mkdir(parents=True, exist_ok=True)
                (root / path).write_text(text)
            self.assertEqual(shard.test_files(root), ["a/One.test.tsx", "b/two.test.ts"])

    def test_a_new_file_still_gets_a_shard(self):
        buckets = shard.shards(["new/New.test.tsx", "a.test.tsx"], 2, {"a.test.tsx": 100})
        self.assertEqual(buckets, [["a.test.tsx"], ["new/New.test.tsx"]])

    def test_shards_are_balanced_by_measured_seconds(self):
        weights = shard.load_weights()
        buckets = shard.shards(shard.test_files(), 4, weights)
        totals = [sum(weights.get(f, shard.DEFAULT_SECONDS) for f in b) for b in buckets]
        self.assertLess(max(totals), sum(totals) / 4 + max(weights.values(), default=0))


if __name__ == "__main__":
    unittest.main()
