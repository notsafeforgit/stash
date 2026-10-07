from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.dedupe import Journal, private_directory
from stash_ingest.dedupe_host import FORMAT, configuration, execute, main, publish_stamp
from stash_ingest.encoding import InvalidData, encode


class HostDedupeTests(unittest.TestCase):
    def fixture(self, directory):
        root = Path(directory)
        state, _ = private_directory(root / "state")
        return {"format": FORMAT, "endpoint": "http://127.0.0.1:8009", "root_uuid": str(uuid.uuid4()),
                "root": str(root), "state_dir": str(state), "library_lock": str(root / "backup.lock"),
                "lock_roots": [str(root)], "api_key_file": str(root / "key"), "fclones": "/usr/bin/fclones",
                "stamp_file": str(root / "stamp")}

    def test_review_finishes_maintenance_without_claiming_removal_and_sets_cooldown(self):
        with tempfile.TemporaryDirectory() as directory:
            value = self.fixture(directory)
            report = {"finished": True, "pending": 0, "committed": 1, "review": 2, "all_removed": False}
            with patch("stash_ingest.dedupe_host.run", return_value=report) as call:
                self.assertEqual(report, execute(value))
                self.assertEqual("daily_cooldown", execute(value)["reason"])
                self.assertEqual(1, call.call_count)
                self.assertEqual(report, execute(value, before_backup=True))
                self.assertEqual(2, call.call_count)
                args = call.call_args.args[0]
                self.assertEqual(value["api_key_file"], args.key_file)
                self.assertEqual(value["lock_roots"], args.lock_root)
            stamp = Path(value["stamp_file"])
            self.assertGreater(int(stamp.read_text()), int(time.time()) - 5)
            self.assertEqual(0, stamp.stat().st_mode & 0o077)

    def test_pending_journal_bypasses_cooldown_and_failure_preserves_timestamp(self):
        with tempfile.TemporaryDirectory() as directory:
            value = self.fixture(directory)
            state = Path(value["state_dir"])
            Journal.create(state, {"root_uuid": value["root_uuid"]}, [])
            publish_stamp(value["stamp_file"], int(time.time()))
            original = Path(value["stamp_file"]).read_bytes()
            with patch("stash_ingest.dedupe_host.run", side_effect=Unavailable("network_unavailable")) as call:
                with self.assertRaises(Unavailable):
                    execute(value)
                self.assertEqual(1, call.call_count)
            self.assertTrue(Journal.has_active(state))
            self.assertEqual(original, Path(value["stamp_file"]).read_bytes())

    def test_pending_output_and_busy_lock_never_mark_completed(self):
        with tempfile.TemporaryDirectory() as directory:
            value = self.fixture(directory)
            with patch("stash_ingest.dedupe_host.run", return_value={"finished": False, "pending": 1}):
                with self.assertRaises(InvalidData):
                    execute(value)
            self.assertFalse(Path(value["stamp_file"]).exists())
            path = Path(directory) / "config.json"
            path.write_bytes(encode(value))
            with patch("stash_ingest.dedupe_host.run", side_effect=BlockingIOError), redirect_stdout(io.StringIO()) as out:
                self.assertEqual(0, main(["--config", str(path), "--before-backup"]))
            self.assertEqual({"skipped": True, "reason": "busy"}, json.loads(out.getvalue()))
            self.assertFalse(Path(value["stamp_file"]).exists())

    def test_configuration_refuses_unknown_fields_relative_paths_and_missing_barriers(self):
        with tempfile.TemporaryDirectory() as directory:
            value = self.fixture(directory)
            for change in ({"secret": "must-not-be-accepted"}, {"root": "relative"}, {"endpoint": "http://x/path"},
                           {"lock_roots": []}, {"lock_roots": [directory, directory]}, {"root_uuid": "invalid"}):
                with self.subTest(change=list(change)), self.assertRaises(InvalidData):
                    configuration({**value, **change})

    def test_timestamp_symlinks_and_invalid_contents_cannot_suppress_work(self):
        with tempfile.TemporaryDirectory() as directory:
            value = self.fixture(directory)
            victim = Path(directory) / "keep"
            victim.write_text("do not overwrite")
            stamp = Path(value["stamp_file"])
            stamp.symlink_to(victim)
            with self.assertRaises(InvalidData):
                publish_stamp(str(stamp), int(time.time()))
            self.assertEqual("do not overwrite", victim.read_text())
            stamp.unlink()
            stamp.write_text("nonsense")
            with patch("stash_ingest.dedupe_host.run") as call, self.assertRaises(InvalidData):
                execute(value)
            self.assertFalse(call.called)
