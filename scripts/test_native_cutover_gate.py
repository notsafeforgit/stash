import copy
import json
from pathlib import Path
import tempfile
import unittest

import native_cutover_gate as gate


class PublicationGateTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.run = Path(self.temp.name) / "selected-run"
        (self.run / "archive").mkdir(parents=True)
        self.expected = {"run_id": self.run.name, "checkpoint_uuid": "selected-checkpoint",
                         "config_sha256": "a" * 64}
        self.write("identity.json", self.expected)
        inventory = b'{"fixture":"inventory"}\n'
        (self.run / "archive/artifacts.jsonl").write_bytes(inventory)
        manifest = {"uuid": "selected-archive", "inventory": {
            "size": len(inventory), "sha256": gate.digest(inventory)}}
        self.write("archive/manifest.json", manifest)
        manifest_body = (self.run / "archive/manifest.json").read_bytes()
        self.publication = {"checkpoint_uuid": self.expected["checkpoint_uuid"],
                            "archive_uuid": manifest["uuid"],
                            "manifest": {"key": "manifest", "bytes": len(manifest_body),
                                         "sha256": gate.digest(manifest_body)},
                            "inventory": {"key": "inventory", "bytes": len(inventory),
                                          "sha256": gate.digest(inventory)},
                            "verification": {"key": "verification", "bytes": 123, "sha256": "b" * 64}}
        self.write("publication.json", self.publication)
        self.master = {"run_id": self.run.name, "native_archive": self.publication}
        self.write("master.json", self.master)
        self.finished = {"master_sha256": gate.digest((self.run / "master.json").read_bytes()),
                         "publication": self.publication}
        self.write("finished.json", self.finished)
        self.write("released.json", self.finished)

    def write(self, name, value):
        (self.run / name).write_text(json.dumps(value, sort_keys=True) + "\n")

    def evidence(self):
        return gate.publication_evidence(self.run, self.expected)

    def controller(self):
        proof = self.evidence()
        return {**{key: proof[key] for key in ("run_id", "checkpoint_uuid", "master_manifest", "master_sha256")},
                "coordinated_backup_complete": True, "isolated_restore_complete": False,
                "status": "running", "stage": "restore", "child": {"pid": 123, "start_ticks": "456"}}

    def test_publication_is_separate_from_restore_success(self):
        proof = self.evidence()
        self.assertEqual(proof["gate"], "backup_publication")
        self.assertTrue(proof["restore_verification_required"])
        self.assertNotIn("isolated_restore_complete", proof)
        gate.check_controller_publication(self.controller(), proof, "restore", "complete")

    def test_packed_archive_without_finish_cannot_authorize_rollout(self):
        (self.run / "finished.json").unlink()
        with self.assertRaises(gate.GatePending):
            self.evidence()

    def test_wrong_identity_is_rejected(self):
        for key in self.expected:
            with self.subTest(key=key):
                with self.assertRaises(ValueError):
                    gate.publication_evidence(self.run, {**self.expected, key: "other"})

    def test_abandoned_or_unreleased_run_is_rejected(self):
        self.write("abandoned.json", {})
        with self.assertRaises(ValueError):
            self.evidence()
        (self.run / "abandoned.json").unlink()
        (self.run / "released.json").unlink()
        with self.assertRaises(FileNotFoundError):
            self.evidence()

    def test_changed_master_inventory_or_manifest_is_rejected(self):
        for name in ("master.json", "archive/artifacts.jsonl", "archive/manifest.json"):
            with self.subTest(name=name):
                path = self.run / name
                original = path.read_bytes()
                path.write_bytes(original + b" ")
                with self.assertRaises(ValueError):
                    self.evidence()
                path.write_bytes(original)

    def test_mismatched_completion_reference_is_rejected(self):
        wrong = copy.deepcopy(self.finished)
        wrong["publication"]["checkpoint_uuid"] = "other"
        self.write("finished.json", wrong)
        with self.assertRaises(ValueError):
            self.evidence()

    def test_pre_restore_hold_check_must_finish(self):
        state = self.controller()
        state["stage"] = "publish"
        with self.assertRaises(gate.GatePending):
            gate.check_controller_publication(state, self.evidence(), "restore", "complete")
        state.update(stage="restore", child=None)
        with self.assertRaises(gate.GatePending):
            gate.check_controller_publication(state, self.evidence(), "restore", "complete")

    def test_restore_failure_keeps_publication_valid_but_never_proves_restore(self):
        state = {**self.controller(), "status": "failed", "child": None, "child_exit_code": 1}
        gate.check_controller_publication(state, self.evidence(), "restore", "complete")
        with self.assertRaises(ValueError):
            gate.check_restore_finished(state, "restore", "complete")

    def test_restore_output_is_insufficient_while_child_or_controller_is_running(self):
        for child in (None, {"pid": 123}):
            with self.subTest(child=child):
                state = {**self.controller(), "child": child, "child_exit_code": 0}
                with self.assertRaises(gate.GatePending):
                    gate.check_restore_finished(state, "restore", "complete")

    def test_successful_restore_child_can_have_obsolete_controller_hold_failure(self):
        state = {**self.controller(), "status": "failed", "child": None, "child_exit_code": 0}
        gate.check_restore_finished(state, "restore", "complete")

    def test_restore_requires_native_audit_even_when_publication_allows_sqlite_only(self):
        proof = {"contents_verified": True, "verification_method": "isolated-restore",
                 "native_snapshot": {"database_verified": True}}
        gate.check_restore_audit(proof)
        historical = copy.deepcopy(proof)
        del historical["verification_method"]
        gate.check_restore_audit(historical)
        for change in ({"verification_method": "streamed-contents"}, {"verification_method": "captured-contents"},
                       {"contents_verified": False},
                       {"native_snapshot": None}, {"native_snapshot": {}},
                       {"native_snapshot": {"database_verified": False}},
                       {"native_snapshot": {"database_verified": 1}}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                gate.check_restore_audit(proof | change)
        with self.assertRaises(ValueError):
            gate.check_restore_audit({"contents_verified": True, "sqlite_snapshots": {}})

    def test_foreign_controller_publication_is_rejected(self):
        for key in ("run_id", "checkpoint_uuid", "master_manifest", "master_sha256"):
            with self.subTest(key=key):
                with self.assertRaises(ValueError):
                    gate.check_controller_publication({**self.controller(), key: "other"}, self.evidence(), "restore", "complete")


if __name__ == "__main__":
    unittest.main()
