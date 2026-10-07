from contextlib import redirect_stderr, redirect_stdout
from copy import deepcopy
import io
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

from stash_ingest.encoding import InvalidData
from stash_ingest.n8n_migration import OLD_PARENT_CHECK, PARENT_ARGUMENT, PARENT_CHECK, convert
from stash_ingest.n8n_parent_alive import alive, main


class N8nParentTests(unittest.TestCase):
    def test_live_waiting_and_terminal_statuses_and_database_bytes_are_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "n8n.sqlite"
            with sqlite3.connect(path) as db:
                db.execute("CREATE TABLE execution_entity(id INTEGER PRIMARY KEY,status TEXT,stoppedAt TEXT)")
                db.executemany("INSERT INTO execution_entity VALUES(?,?,?)", [
                    (1, "new", None), (2, "running", None), (3, "waiting", None), (4, "success", None),
                    (5, "error", None), (6, "canceled", None), (7, "crashed", None), (8, "running", "2026-10-07")])
            before = path.read_bytes()
            for identity in range(1, 10):
                self.assertEqual(alive(str(identity), path), identity in (1, 2, 3))
            self.assertEqual(path.read_bytes(), before)
            output = io.StringIO()
            with redirect_stdout(output):
                self.assertEqual(main(["--database", str(path), "--execution", "3"]), 0)
            self.assertEqual(json.loads(output.getvalue()), {"parent_alive": True})

    def test_invalid_execution_never_opens_or_creates_a_database(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "missing.sqlite"
            for identity in (None, "", "invalid", "1 OR 1=1", "1:token", "1"*31):
                self.assertFalse(alive(identity, path))
            self.assertFalse(path.exists())

    def test_unavailable_database_is_an_error_not_a_dead_parent_result(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "missing.sqlite"
            output, errors = io.StringIO(), io.StringIO()
            with redirect_stdout(output), redirect_stderr(errors):
                self.assertEqual(main(["--execution", "1", "--database", str(path)]), 1)
            self.assertEqual(output.getvalue(), "")
            self.assertNotIn(str(path), errors.getvalue())
            self.assertFalse(path.exists())

    def test_converter_only_relocates_the_reviewed_command(self):
        original = {"id": "heartbeatFixture", "active": True, "settings": {"executionOrder": "v1"}, "connections": {},
                    "nodes": [{"id": "parent-check", "name": "Check parent", "type": "n8n-nodes-base.executeCommand",
                               "parameters": {"command": OLD_PARENT_CHECK+PARENT_ARGUMENT}, "position": [100, 200]},
                              {"id": "unrelated", "name": "Coordination", "type": "n8n-nodes-base.code",
                               "parameters": {"jsCode": "return $input.all();"}, "credentials": {"fixture": "retained"}}]}
        saved = deepcopy(original)
        result = convert(original)
        expected = deepcopy(original)
        expected["nodes"][0]["parameters"]["command"] = PARENT_CHECK+PARENT_ARGUMENT
        self.assertEqual(result, expected)
        self.assertEqual(original, saved)
        original["nodes"][0]["parameters"]["command"] += " --unreviewed"
        with self.assertRaises(InvalidData):
            convert(original)


if __name__ == "__main__":
    unittest.main()
