from copy import deepcopy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.dedupe import exclusive_lock
from stash_ingest.encoding import InvalidData, encode
from stash_ingest.intake_client import includes, page, preview, scan_scope, status
from stash_ingest.intake_folder import FORMAT, configuration, run
from stash_ingest.intake_journal import Journal, database


def uid():
    return str(uuid.uuid4())


def digest(value):
    return hashlib.sha256(encode(value)).hexdigest()


def fixture(directory):
    return {"format": FORMAT, "endpoint": "http://127.0.0.1:8009", "root_uuid": uid(),
            "collections": [{"uuid": uid(), "path_prefix": "."}], "exclude": [],
            "state_dir": str(Path(directory) / "state"), "library_lock": str(Path(directory) / "backup.lock"),
            "api_key_file": str(Path(directory) / "key"), "max_pending": 2, "entries_per_run": 100,
            "settle_seconds": 60, "scan_interval_seconds": 3600}


class Server:
    def __init__(self, config):
        self.config, self.files, self.jobs, self.registered = config, {}, {}, {}
        self.applies, self.previews, self.pages = [], [], []
        self.lose_response = self.reject = self.malformed = self.immediate = False
        self.policy = 0
        self.scopes = {}

    def add(self, path, version="original", existing=False):
        self.files[path] = version
        if existing:
            self.registered[path] = uid()

    def preview(self, input, root):
        path = input["relative_path"]
        if path not in self.files:
            raise Unavailable("file_not_found", 404)
        self.previews.append(path)
        result = {**input, "root_uuid": root, "root_revision": 1, "collection_revision": 1,
                  "policy_revision": self.policy, "filename": path.split("/")[-1], "size": 16,
                  "modified_at": "2020-01-01T00:00:00Z", "file_signature": digest([path, self.files[path]])}
        if "scan_collection_uuid" in input:
            result["scan_collection_revision"] = 1
        if path in self.registered:
            result["existing_file_uuid"] = self.registered[path]
        result["signature"] = digest(result)
        return preview(result, input, root)

    def scan_scope(self, collection, root, folder):
        value = {"scan_collection_uuid": collection["uuid"], "scan_collection_revision": 1,
                 "scan_path_prefix": collection["path_prefix"], "root_uuid": root, "root_revision": 1,
                 "directory": folder, "policy_revision": self.policy}
        selection = self.scopes.get(folder, {"uuid": collection["uuid"], "path_prefix": collection["path_prefix"]})
        if isinstance(selection, str):
            value["blocked_reason"] = selection
        else:
            value.update(collection_uuid=selection["uuid"], collection_revision=1, path_prefix=selection["path_prefix"])
        return scan_scope(value, collection, root, folder)

    def directory(self, collection, root, folder, after="", signature=""):
        self.pages.append((folder, after))
        entries = {}
        prefix = "" if folder == "." else folder + "/"
        for filename in self.files:
            if not filename.startswith(prefix):
                continue
            tail = filename[len(prefix):]
            name = tail.split("/")[0]
            child = "/" in tail
            entries[("0/" if child else "1/") + name] = {
                "name": name, "relative_path": prefix + name,
                "kind": "directory" if child else "image" if name.endswith(".jpg") else "scene",
                "size": 0 if child else 16, "modified_at": "2020-01-01T00:00:00Z"}
        stamp = digest(entries)
        if signature and signature != stamp:
            raise Unavailable("directory_changed", 409)
        keys = [key for key in sorted(entries) if key > after]
        selected = keys[:50]
        value = {"collection_uuid": collection["uuid"], "collection_revision": 1, "root_uuid": root,
                 "root_revision": 1, "path_prefix": collection["path_prefix"], "directory": folder,
                 "signature": stamp, "entries": [entries[key] for key in selected]}
        if len(keys) > 50:
            value["next_after"] = selected[-1]
        return page(value, collection, root, folder, after, signature)

    def apply(self, request):
        if self.reject:
            raise Unavailable("intake_preview_changed", 409)
        self.applies.append(deepcopy(request))
        now = "2026-10-07T00:00:00Z"
        result = {**request, "job_uuid": uid(), "state": "queued", "revision": 1, "attempts": 0,
                  "max_attempts": 8, "created_at": now, "updated_at": now, "available_at": now,
                  "registration_committed": False, "media_ingested": False}
        self.jobs[request["request_uuid"]] = result
        if self.immediate:
            self.finish(request["request_uuid"])
        if self.lose_response:
            self.lose_response = False
            raise Unavailable("network_unavailable")
        if self.malformed:
            result = {**result, "request_uuid": uid()}
        return status(result, request)

    def receipt(self, request):
        result = self.jobs.get(request["request_uuid"])
        return status(deepcopy(result), request) if result else None

    def finish(self, request_id, state="succeeded"):
        result = self.jobs[request_id]
        result.update(state=state, revision=result["revision"] + 1)
        if state == "succeeded":
            file_id = self.registered.setdefault(result["relative_path"], uid())
            result.update(media_ingested=True, registration_committed=True,
                          publication={"file_uuid": file_id, "generation": 1, "content_uuid": uid(),
                                       "media_uuid": uid(), "media_kind": result["media_kind"]})


class FolderIntakeTests(unittest.TestCase):
    def test_dynamic_policy_and_new_source_need_no_host_config_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            original = deepcopy(config)
            server = Server(config)
            server.immediate = True
            run(config, client=server, now=2000000000)
            child = {"uuid": uid(), "path_prefix": "purchases"}
            server.scopes.update(purchases=child, scrape="source_folder")
            server.add("purchases/a.mp4")
            server.add("scrape/account/a.mp4")
            result = run(config, client=server, now=2000004000)
            self.assertEqual(1, result["submitted"])
            self.assertEqual({"source_folder": 1}, result["scope_exclusions"])
            self.assertEqual(child["uuid"], server.applies[0]["collection_uuid"])
            self.assertEqual(config["collections"][0]["uuid"], server.applies[0]["scan_collection_uuid"])
            self.assertFalse(any(folder.startswith("scrape") for folder, _ in server.pages))
            self.assertEqual(original, config)
            self.assertEqual(0, run(config, client=server, now=2000008000)["submitted"])

    def test_ambiguous_parent_keeps_discovering_more_specific_children(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            server = Server(config)
            server.scopes["."] = "ambiguous_directory"
            server.scopes["child"] = {"uuid": uid(), "path_prefix": "child"}
            server.add("ambiguous.mp4")
            server.add("child/eligible.mp4")
            result = run(config, client=server, now=2000000000)
            self.assertEqual(1, result["submitted"])
            self.assertEqual(["child/eligible.mp4"], [r["relative_path"] for r in server.applies])

    def test_changed_dynamic_policy_does_not_stack_pending_work_for_same_file(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            server = Server(config)
            server.add("file.mp4")
            run(config, client=server, now=2000000000)
            original = deepcopy(server.applies[0])
            server.scopes["."] = {"uuid": uid(), "path_prefix": "."}
            self.assertEqual(0, run(config, client=server, now=2000004000)["submitted"])
            self.assertEqual([original], Journal(config["state_dir"], config).pending())
            self.assertEqual([original], server.applies)

    def test_dynamic_scope_cannot_escape_configured_base_and_saved_scope_cannot_change(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            config["collections"][0]["path_prefix"] = "purchases"
            server = Server(config)
            server.add("purchases/file.mp4")
            server.scopes["purchases"] = {"uuid": uid(), "path_prefix": "."}
            with self.assertRaises(InvalidData):
                run(config, client=server, now=2000000000)
            self.assertEqual([], server.applies)
            server.scopes["purchases"] = {"uuid": uid(), "path_prefix": "purchases"}
            run(config, client=server, now=2000000001)
            with database(config["state_dir"]) as db:
                row = db.execute("SELECT preview,body FROM requests").fetchone()
                body, saved = json.loads(row["body"]), json.loads(row["preview"])
                body["scan_collection_uuid"] = saved["scan_collection_uuid"] = uid()
                db.execute("UPDATE requests SET body=?,preview=?", (encode(body), encode(saved)))
            with self.assertRaises(InvalidData):
                Journal(config["state_dir"], config).pending()

    def test_lost_acceptance_recovers_original_before_discovery_and_never_claims_queued_completion(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            config["max_pending"] = 1
            server = Server(config)
            server.add("first.mp4")
            server.add("second.jpg")
            server.lose_response = True
            with self.assertRaises(Unavailable):
                run(config, client=server, now=2000000000)
            saved = Journal(config["state_dir"], config).pending()[0]
            result = run(config, client=server, now=2000000001)
            self.assertEqual(1, result["pending"])
            self.assertEqual({}, result["observed"])
            self.assertEqual(1, len(server.applies))
            self.assertEqual(saved, server.applies[0])
            server.finish(saved["request_uuid"])
            result = run(config, client=server, now=2000000002)
            self.assertEqual(1, result["observed"]["succeeded"])
            self.assertEqual(1, result["pending"])
            self.assertEqual(["first.mp4", "second.jpg"], [r["relative_path"] for r in server.applies])

    def test_pending_limit_is_global_across_restarts_and_later_ticks(self):
        with tempfile.TemporaryDirectory() as directory:
            config, server = fixture(directory), None
            server = Server(config)
            for n in range(8):
                server.add(f"{n}.mp4")
            result = run(config, client=server, now=2000000000)
            self.assertEqual(2, result["pending"])
            for n in range(3):
                self.assertEqual(0, run(config, client=server, now=2000000001+n)["submitted"])
            self.assertEqual(2, len(server.applies))
            server.finish(server.applies[0]["request_uuid"])
            self.assertEqual(1, run(config, client=server, now=2000000005)["submitted"])
            self.assertEqual(3, len(server.applies))

    def test_nested_new_files_old_mtime_changed_bytes_and_registration_do_not_duplicate(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            server = Server(config)
            server.immediate = True
            run(config, client=server, now=2000000000)
            server.add("purchases/nested/old-date.mp4")
            self.assertEqual(0, run(config, client=server, now=2000000001)["submitted"])
            self.assertEqual(1, run(config, client=server, now=2000004000)["submitted"])
            self.assertEqual(0, run(config, client=server, now=2000008000)["submitted"])
            server.files["purchases/nested/old-date.mp4"] = "changed with the same mtime and size"
            self.assertEqual(1, run(config, client=server, now=2000012000)["submitted"])
            self.assertEqual(2, len(server.applies))

    def test_baseline_indexed_files_and_policy_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            server = Server(config)
            server.immediate = True
            server.add("existing.mp4", existing=True)
            first = run(config, client=server, now=2000000000)
            self.assertEqual({"already_indexed": 1}, first["observed"])
            server.policy = 2
            self.assertEqual(1, run(config, client=server, now=2000004000)["submitted"])

    def test_malformed_receipt_retains_exact_intent(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            server = Server(config)
            server.add("file.mp4")
            server.malformed = True
            with self.assertRaises(InvalidData):
                run(config, client=server, now=2000000000)
            saved = Journal(config["state_dir"], config).pending()
            self.assertEqual(server.applies, saved)
            server.malformed = False
            self.assertEqual(1, run(config, client=server, now=2000000001)["pending"])
            self.assertEqual(1, len(server.applies))

    def test_known_rejection_and_terminal_failure_require_new_file_or_policy_version(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            server = Server(config)
            server.add("file.mp4")
            server.reject = True
            self.assertEqual({"review": 1}, run(config, client=server, now=2000000000)["observed"])
            server.reject = False
            self.assertEqual(0, run(config, client=server, now=2000004000)["submitted"])
            server.files["file.mp4"] = "changed"
            self.assertEqual(1, run(config, client=server, now=2000008000)["submitted"])
            server.finish(server.applies[0]["request_uuid"], "failed")
            self.assertEqual({"failed": 1}, run(config, client=server, now=2000012000)["observed"])
            self.assertEqual(1, len(server.applies))

    def test_nested_collection_wins_and_source_scope_is_excluded(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            child = {"uuid": uid(), "path_prefix": "purchases/creator"}
            config["collections"].append(child)
            config["exclude"] = ["scrapes"]
            server = Server(config)
            server.immediate = True
            for name in ("scrapes/account/a.mp4", "purchases/creator/a.mp4", "purchases/other.mp4"):
                server.add(name)
            run(config, client=server, now=2000000000)
            requests = {r["relative_path"]: r for r in server.applies}
            self.assertEqual({"purchases/creator/a.mp4", "purchases/other.mp4"}, set(requests))
            self.assertEqual(child["uuid"], requests["purchases/creator/a.mp4"]["collection_uuid"])

    def test_changed_directory_pagination_resumes_without_reimporting_completed_files(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            config["entries_per_run"] = 50
            server = Server(config)
            server.immediate = True
            for n in range(51):
                server.add(f"file-{n:02}.mp4")
            self.assertEqual(50, run(config, client=server, now=2000000000)["submitted"])
            server.add("added-before-cursor.mp4")
            run(config, client=server, now=2000000001)
            run(config, client=server, now=2000000002)
            self.assertEqual(52, len(server.applies))
            self.assertEqual(52, len({r["relative_path"] for r in server.applies}))

    def test_busy_backup_scope_changes_and_private_state(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            server = Server(config)
            server.add("file.mp4")
            with exclusive_lock(config["library_lock"]), self.assertRaises(BlockingIOError):
                run(config, client=server, now=2000000000)
            self.assertEqual([], server.applies)
            run(config, client=server, now=2000000000)
            changed = {**config, "collections": [{"uuid": uid(), "path_prefix": "other"}]}
            with self.assertRaises(InvalidData):
                run(changed, client=server, now=2000000001)
            server.finish(server.applies[0]["request_uuid"])
            run(config, client=server, now=2000000002)
            run(changed, client=server, now=2000000003)
            with self.assertRaises(InvalidData):
                Journal(config["state_dir"], {**changed, "endpoint": "http://other.example"})
            with database(config["state_dir"]) as db:
                self.assertEqual(1, db.execute("SELECT count(*) FROM requests").fetchone()[0])
            self.assertEqual(0, (Path(config["state_dir"]) / "intake.sqlite3").stat().st_mode & 0o077)

    def test_invalid_configuration_and_escaping_directory_response(self):
        with tempfile.TemporaryDirectory() as directory:
            config = fixture(directory)
            for change in ({"max_pending": 0}, {"endpoint": "http://example/path"}, {"exclude": ["../escape"]},
                           {"collections": config["collections"] * 2}, {"state_dir": "relative"}):
                with self.subTest(change=change), self.assertRaises(InvalidData):
                    configuration({**config, **change})
            server = Server(config)
            server.add("file.mp4")
            collection = config["collections"][0]
            result = server.directory(collection, config["root_uuid"], ".")
            result["entries"][0]["relative_path"] = "../file.mp4"
            with self.assertRaises(InvalidData):
                page(result, collection, config["root_uuid"], ".")


if __name__ == "__main__":
    unittest.main()
