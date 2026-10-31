#!/usr/bin/env python3
"""Unit checks for publisher/publish.py (stdlib only)."""
import io
import sys
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import MagicMock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import publish


class FilterMissingTests(unittest.TestCase):
    def test_splits_existing_and_missing(self):
        r2 = MagicMock()
        # Even sha ends with even hex digit -> treat as existing when last char is 0-7
        def req(method, key):
            self.assertEqual(method, "HEAD")
            sha = key.rsplit("/", 1)[-1]
            return 200 if sha.startswith("aa") else 404

        r2.req.side_effect = req
        shas = [f"{c}{c}" + "0" * 62 for c in "abcdef"]  # aa.., bb.., ...
        buf = io.StringIO()
        with redirect_stdout(buf):
            todo = publish.filter_missing(r2, "rom2/client/", shas, workers=4, progress_every=2)
        out = buf.getvalue()
        self.assertIn("checking ", out)
        self.assertIn("check done:", out)
        self.assertEqual(todo, [s for s in sorted(shas) if not s.startswith("aa")])
        self.assertEqual(r2.req.call_count, len(shas))

    def test_all_exist(self):
        r2 = MagicMock()
        r2.req.return_value = 200
        shas = ["1" * 64, "2" * 64, "3" * 64]
        buf = io.StringIO()
        with redirect_stdout(buf):
            todo = publish.filter_missing(r2, "p/", shas, workers=2, progress_every=500)
        self.assertEqual(todo, [])
        self.assertIn("3 already on R2, 0 to upload", buf.getvalue())


class SkipTests(unittest.TestCase):
    def test_char_cache_skipped(self):
        self.assertTrue("311777688826776.a2c".endswith(publish.SKIP_EXTS))
        self.assertFalse("hotkey.1.a2key".endswith(publish.SKIP_EXTS))
        self.assertFalse("famehall.dat".endswith(publish.SKIP_EXTS))


class PackTests(unittest.TestCase):
    def test_build_packs(self):
        import tempfile, zipfile, os, hashlib
        with tempfile.TemporaryDirectory() as d:
            stage, out = os.path.join(d, "s"), os.path.join(d, "o")
            os.makedirs(os.path.join(stage, "data")); os.makedirs(out)
            files = []
            for i in range(5):
                p = f"data/f{i}.bin"; body = bytes([i]) * 40
                open(os.path.join(stage, p), "wb").write(body)
                files.append({"path": p, "size": 40, "sha256": hashlib.sha256(body).hexdigest()})
            open(os.path.join(stage, "big"), "wb").write(b"x" * 200)
            files.append({"path": "big", "size": 200, "sha256": "0" * 64})
            packs, new = publish.build_packs(stage, files, out, target=100)
            self.assertEqual([p["files"] for p in packs], [["data/f0.bin", "data/f1.bin"], ["data/f2.bin", "data/f3.bin"]])
            for p in packs:
                path = os.path.join(out, p["sha256"])
                self.assertEqual(publish.sha256_file(path), p["sha256"])
                with zipfile.ZipFile(path) as z:
                    self.assertEqual(z.namelist(), p["files"])
            again, new2 = publish.build_packs(stage, files, out, target=100)
            self.assertEqual(again, packs)  # deterministic
            self.assertEqual(new2, 0)


if __name__ == "__main__":
    unittest.main()
