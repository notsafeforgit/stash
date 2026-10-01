from contextlib import closing, redirect_stdout
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.backfill_calls import BackfillCalls
from stash_ingest.client import Client, Unavailable
from stash_ingest.cli import main as producer_main
from stash_ingest.n8n_runner import execution_uuid, main
from stash_ingest.outbox import Outbox
from stash_ingest.source_calls import SourceCalls
from helpers import PRODUCER
from test_configuration import profile_fixture
from test_backfill_calls import decision, status


class N8nRunnerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.path, self.value = profile_fixture(self.directory)
        self.value["source_category"] = "reddit"
        self.value["gallery"]["skip"] = True
        self.path.write_text(json.dumps(self.value))
        self.database = self.directory / "producer.sqlite"
        self.base = ["--outbox", str(self.database), "--endpoint", "http://fixture.invalid", "--producer", PRODUCER]
        self.node, self.call = str(uuid.uuid4()), str(uuid.uuid4())

    def invoke(self, args):
        output = io.StringIO()
        with redirect_stdout(output):
            code = main([*self.base, *args])
        self.assertNotIn("private-first-value", output.getvalue())
        return code, json.loads(output.getvalue())

    def test_execution_identity_is_stable_and_replay_does_not_require_the_profile(self):
        args = ["--mode", "reddit-new", "--identity", "Example", "--workflow", "workflow123", "--execution", "100",
                "--node", self.node, "--item", "0", "--profile", str(self.path)]
        with patch.object(Client, "capabilities", side_effect=AssertionError("offline recording reached Stash")):
            code, first = self.invoke(args)
            self.assertEqual(code, 0)
            self.path.unlink()
            self.assertEqual(self.invoke(args), (code, first))
        expected = execution_uuid(PRODUCER, "reddit-new", "Example", "workflow123", "100", self.node, 0)
        self.assertEqual(first["token"], uuid.UUID(expected).hex)
        self.assertNotEqual(expected, execution_uuid(PRODUCER, "reddit-new", "Example", "workflow123", "101", self.node, 0))
        with closing(Outbox(self.database, self.base[3], PRODUCER)) as box:
            self.assertEqual(BackfillCalls(box).summary()["calls"], 1)
            self.assertEqual(SourceCalls(box).summary()["calls"], 0)

    def test_pending_inspection_does_not_become_success_and_known_acceptance_replays_offline(self):
        args = ["--mode", "reddit-new", "--identity", "Example", "--call", self.call, "--profile", str(self.path)]
        _, first = self.invoke(args)
        inspect = ["--inspect", first["token"], "--strict"]
        with patch.object(Client, "capabilities", side_effect=Unavailable("network_unavailable")):
            code, pending = self.invoke(inspect)
        self.assertEqual(code, 2)
        self.assertTrue(pending["backfill_pending"])
        with closing(Outbox(self.database, self.base[3], PRODUCER)) as box:
            spec = BackfillCalls(box)._definition(BackfillCalls(box)._find(self.call))
            box.db.execute("UPDATE backfill_calls SET available_at=0 WHERE uuid=?", (self.call,))
        accepted = status(spec, [decision("reddit-new")])
        with patch.object(Client, "capabilities", return_value={"source_backfill_protocol": 1}), \
                patch.object(Client, "_request", return_value=accepted):
            code, complete = self.invoke(inspect)
        self.assertEqual(code, 0)
        self.assertFalse(complete["backfill_pending"])
        self.assertTrue(complete["backfill_cached"])
        self.path.unlink()
        with patch.object(Client, "capabilities", side_effect=AssertionError("replayed permanent receipt used network")):
            self.assertEqual(self.invoke(inspect), (code, complete))

    def test_invalid_identity_unknown_tokens_and_non_history_profiles_cannot_enqueue(self):
        common = ["--mode", "reddit-new", "--call", self.call, "--profile", str(self.path)]
        for identity in ("me", "bad'account", "u/Example"):
            self.assertEqual(self.invoke([*common, "--identity", identity])[0], 1)
        self.value["gallery"].pop("skip")
        self.path.write_text(json.dumps(self.value))
        self.assertEqual(self.invoke([*common, "--identity", "Example"])[0], 1)
        with patch.object(Client, "capabilities", side_effect=AssertionError("unknown token reached Stash")):
            self.assertEqual(self.invoke(["--inspect", uuid.uuid4().hex])[0], 1)
        with closing(Outbox(self.database, self.base[3], PRODUCER)) as box:
            self.assertEqual(BackfillCalls(box).summary()["calls"], 0)

    def test_dispatch_remains_pending_when_only_a_backfill_history_check_is_outstanding(self):
        self.invoke(["--mode", "reddit-new", "--identity", "Example", "--call", self.call, "--profile", str(self.path)])
        output = io.StringIO()
        with patch.object(Client, "capabilities", side_effect=Unavailable("network_unavailable")), \
                patch("stash_ingest.dispatch.Dispatcher.once", return_value={"state": "idle"}), redirect_stdout(output):
            code = producer_main([*self.base, "dispatch", "--profile", str(self.path)])
        result = json.loads(output.getvalue())
        self.assertEqual(code, 2)
        self.assertEqual(result["source_calls"]["calls"], 0)
        self.assertEqual(result["backfill_calls"]["counts"]["pending"], 1)


if __name__ == "__main__":
    unittest.main()
