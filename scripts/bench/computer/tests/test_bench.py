import json, os, subprocess, sys, tempfile, unittest
here = os.path.dirname(os.path.abspath(__file__))
bench = os.path.join(os.path.dirname(here), "bench.py")

class T(unittest.TestCase):
    def test_pct(self):
        sys.path.insert(0, os.path.dirname(here))
        import bench as b
        self.assertEqual(b.pct([1, 2, 3, 4, 5, 6, 7, 8, 9, 10], 50), 5)
        self.assertEqual(b.pct([1, 2, 3, 4, 5, 6, 7, 8, 9, 10], 95), 10)
        self.assertIsNone(b.pct([], 50))

    def test_mewla_against_fake(self):
        import bench as b
        out = b.mewla_run(5, ["Finder", "TextEdit", "Calculator"],
                          [sys.executable, os.path.join(here, "fake_mcp.py")], tempfile.mkdtemp())
        p = out["primitives"]
        self.assertEqual(p["apps"]["n"], 4)  # the first call is the cold one
        self.assertEqual(p["app_state/Finder"]["rows_p50"], 2)
        self.assertEqual(p["app_state/Finder"]["truncated"], 5)
        self.assertIn("press->answer/Calculator", p)
        self.assertIn("screenshot/screen", p)

    def test_report(self):
        d = tempfile.mkdtemp()
        json.dump({"primitives": {"apps": {"p50_ms": 1, "p95_ms": 2, "bytes_p50": 3, "rows_p50": None,
                                           "truncated": 0}}}, open(os.path.join(d, "prims-x.json"), "w"))
        r = subprocess.run([sys.executable, bench, "report", d], capture_output=True, text=True)
        self.assertIn("1 / 2 (3, -)", r.stdout)

if __name__ == "__main__":
    sys.path.insert(0, os.path.dirname(here))
    unittest.main()
