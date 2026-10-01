from contextlib import redirect_stderr, redirect_stdout
from copy import deepcopy
import io
import json
from pathlib import Path
import tempfile
import unittest
import uuid

from stash_ingest.encoding import InvalidData
from stash_ingest.n8n_migration import OLD_RUNNER, convert, edge, identity_expression, main, old_inspection


def workflow(mode="reddit-new"):
    def node(name, kind, parameters):
        return {"id": str(uuid.uuid4()), "name": name, "type": "n8n-nodes-base." + kind, "parameters": parameters,
                "position": [0, 0], "typeVersion": 1}
    return {"id": "fixtureWorkflow", "active": True, "versionId": str(uuid.uuid4()), "settings": {"executionOrder": "v1"},
            "nodes": [node("Download", "executeCommand", {"command": OLD_RUNNER + " --mode " + mode
                         + " --identity='" + identity_expression(mode) + "'", "executeOnce": False}),
                      node("Inspect", "executeCommand", {"command": old_inspection(), "executeOnce": False}),
                      node("Parse", "set", {"assignments": {"assignments": [{"id": str(uuid.uuid4()), "name": field,
                          "type": kind, "value": "={{ JSON.parse($json.stdout)." + field + " }}"}
                          for field, kind in (("command_failed", "boolean"), ("exit_code", "number"),
                                               ("stderr_tail", "string"), ("stdout_tail", "string"))]}, "options": {}}),
                      {**node("Error", "if", {"retained": "error conditions"}), "credentials": {"fixture": {"id": "existing-reference"}}},
                      node("Success", "set", {"retained": "success contract"})],
            "connections": {"Download": {"main": [[edge("Inspect")]]}, "Inspect": {"main": [[edge("Parse")]]},
                            "Parse": {"main": [[edge("Error")]]}, "Error": {"main": [[], [edge("Success")]]}}}


class N8nMigrationTests(unittest.TestCase):
    def test_original_ids_credentials_results_and_metadata_survive_while_pending_work_waits(self):
        for mode in ("reddit-new", "reddit-top", "twitter"):
            original = workflow(mode)
            before = deepcopy(original)
            changed = convert(original)
            self.assertEqual(original, before)
            self.assertEqual(changed["nodes"][3:5], original["nodes"][3:5])
            for key in ("id", "versionId", "settings", "active"):
                self.assertEqual(changed[key], original[key])
            for old, new in zip(original["nodes"], changed["nodes"]):
                self.assertEqual((old["id"], old["name"]), (new["id"], new["name"]))
            connections = changed["connections"]
            self.assertEqual(connections["Error"], original["connections"]["Error"])
            self.assertEqual(connections["Parse"], {"main": [[edge("If native backfill pending")]]})
            self.assertEqual(connections["If native backfill pending"], {"main": [[edge("Wait for native backfill")], [edge("Error")]]})
            self.assertEqual(connections["Wait for native backfill"], {"main": [[edge("Inspect")]]})
            self.assertEqual(changed["nodes"][-1]["parameters"], {"resume": "timeInterval", "amount": 90, "unit": "seconds"})
            self.assertIn("$execution.id", changed["nodes"][0]["parameters"]["command"])
            self.assertIn("$itemIndex", changed["nodes"][0]["parameters"]["command"])
            self.assertIn("$json.token ?? JSON.parse", changed["nodes"][1]["parameters"]["command"])

    def test_changed_command_graph_or_result_semantics_require_review(self):
        for mutate in (lambda w: w["nodes"][0]["parameters"].update(command=OLD_RUNNER + " --unreviewed"),
                       lambda w: w["nodes"][0]["parameters"].update(executeOnce=True),
                       lambda w: w["connections"]["Parse"]["main"][0].append(edge("Success")),
                       lambda w: w["nodes"][2]["parameters"]["assignments"]["assignments"][0].update(value="=false")):
            value = workflow()
            mutate(value)
            with self.assertRaises(InvalidData):
                convert(value)

    def test_staging_is_atomic_private_and_never_overwrites_existing_exports(self):
        with tempfile.TemporaryDirectory() as directory:
            source, output = Path(directory) / "input.json", Path(directory) / "staged.json"
            original = json.dumps([workflow()]).encode()
            source.write_bytes(original)
            args = ["--input", str(source), "--output", str(output)]
            with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
                self.assertEqual(main(args), 0)
                before = output.read_bytes()
                self.assertEqual(main(args), 1)
            self.assertEqual(output.read_bytes(), before)
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)
            self.assertEqual(source.read_bytes(), original)
            self.assertEqual(len(json.loads(before)[0]["nodes"]), 7)


if __name__ == "__main__":
    unittest.main()
