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


if __name__ == "__main__":
    unittest.main()
