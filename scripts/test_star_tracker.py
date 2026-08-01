import datetime as dt
import io
import unittest

import star_tracker


class MemoryCsvPath:
    def __init__(self) -> None:
        self.parent = self
        self.text = ""
        self.did_write = False

    def mkdir(self, parents: bool = False, exist_ok: bool = False) -> None:
        return None

    def exists(self) -> bool:
        return self.did_write

    def open(self, mode: str = "r", newline: str | None = None):
        if "w" in mode:
            return MemoryCsvWrite(self)
        return io.StringIO(self.text, newline=newline)


class MemoryCsvWrite:
    def __init__(self, path: MemoryCsvPath) -> None:
        self.path = path
        self.buffer = io.StringIO(newline="")

    def __enter__(self) -> io.StringIO:
        return self.buffer

    def __exit__(self, exc_type, exc, tb) -> None:
        if exc_type is None:
            self.path.text = self.buffer.getvalue()
            self.path.did_write = True
        self.buffer.close()


class StarTrackerLineEndingTest(unittest.TestCase):
    def assert_lf_only(self, path: MemoryCsvPath) -> None:
        self.assertIn("\n", path.text)
        self.assertNotIn("\r\n", path.text)

    def test_append_update_and_backfill_write_lf_only(self) -> None:
        append_path = MemoryCsvPath()
        action, changed = star_tracker.append_today(append_path, "owner/repo", 7)
        self.assertEqual((action, changed), ("created", True))
        self.assert_lf_only(append_path)

        action, changed = star_tracker.append_today(append_path, "owner/repo", 8)
        self.assertEqual((action, changed), ("updated", True))
        self.assert_lf_only(append_path)

        backfill_path = MemoryCsvPath()
        rows = star_tracker.write_backfill(backfill_path, "owner/repo", [dt.date.today()])
        self.assertEqual(rows, 1)
        self.assert_lf_only(backfill_path)


if __name__ == "__main__":
    unittest.main()
