import copy
from contextlib import closing, contextmanager, redirect_stderr, redirect_stdout
import fcntl
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io
import json
import os
from pathlib import Path
import tempfile
import sqlite3
import threading
import sys
import unittest
from unittest.mock import patch
import uuid

from stash_ingest.client import Unavailable
from stash_ingest.dedupe import Journal, apply_saved, exclusive_lock, main, private_directory, journal_database
from stash_ingest.dedupe_candidates import directory_identity, discover, pairs_from_report
from stash_ingest.dedupe_client import DeduplicationClient, validate_preview, validate_receipt
from stash_ingest.encoding import InvalidData, decode, encode
from stash_ingest.publication_lock import ACTIVE, GATE, PublicationBarrier, publication_lock

ROOT = str(uuid.uuid4())
KEEP = str(uuid.uuid4())
REMOVE = str(uuid.uuid4())
MEDIA = str(uuid.uuid4())
STAMP = "2026-10-07T19:00:00+00:00"


def preview(pair, revision=1):
    return {**pair, "eligible": True, "signature": f"{revision:064x}", "root_revision": 1,
            "media_uuid": MEDIA, "kept_file_uuid": KEEP, "removed_file_uuid": REMOVE,
            "bytes": 800, "source_matches": 2, "replace_primary": True}


def receipt(request):
    return {**{k: v for k, v in request.items() if k != "request_uuid"},
            "uuid": request["request_uuid"], "kept_file_uuid": KEEP, "removed_file_uuid": REMOVE,
            "kept_generation": 1, "removed_generation": 2, "media_uuid": MEDIA,
            "sha256": "a" * 64, "committed_at": STAMP}


class MemoryClient:
    endpoint = "http://127.0.0.1:8009"

    def __init__(self, root):
        self.root = root
        self.receipts = {}
        self.calls = []
        self.revision = 1
        self.lose_before = False
        self.lose_after = False
        self.reject = None
        self.blocked = None
        self.journal = None

    def preview(self, pair):
        self.calls.append(("preview", copy.deepcopy(pair)))
        value = preview(pair, self.revision)
        if self.blocked:
            value.update(eligible=False, blocked_reason=self.blocked)
        return value

    def receipt(self, request):
        self.calls.append(("receipt", copy.deepcopy(request)))
        return copy.deepcopy(self.receipts.get(request["request_uuid"]))

    def apply(self, request):
        self.calls.append(("apply", copy.deepcopy(request)))
        # The serialized intent must exist before any operation reaches Stash.
        saved = self.journal.load(request["request_uuid"], "request")
        assert saved["request"] == request
        if self.lose_before:
            self.lose_before = False
            raise Unavailable("network_unavailable")
        if self.reject:
            raise Unavailable(self.reject, 409)
        if request["signature"] != f"{self.revision:064x}":
            raise Unavailable("deduplication_preview_changed", 409)
        os.unlink(self.root / request["remove_path"])
        self.revision += 1
        result = self.receipts[request["request_uuid"]] = receipt(request)
        if self.lose_after:
            self.lose_after = False
            raise Unavailable("network_unavailable")
        return copy.deepcopy(result)


class DedupeTests(unittest.TestCase):
    def fixture(self, directory, count=3):
        base = Path(directory)
        media = base / "media"
        media.mkdir()
        for index in range(count):
            path = media / f"{index}.mp4"
            path.write_bytes(b"fixture\n" * 100)
            os.utime(path, ns=(1000000000 + index, 1000000000 + index))
        state, _ = private_directory(base / "state")
        client = MemoryClient(media)
        config = {"endpoint": client.endpoint, "root_uuid": ROOT, "root": directory_identity(media)}
        body = encode({"groups": [{"file_len": 800, "files": [str(media / f"{i}.mp4") for i in reversed(range(count))]}]})
        pairs = pairs_from_report(body, config["root"], all_content=True)
        journal = Journal.create(state, config, pairs)
        client.journal = journal
        return client, journal, body

    def test_oldest_keeper_sequential_previews_and_lost_committed_response(self):
        with tempfile.TemporaryDirectory() as directory:
            client, journal, _ = self.fixture(directory)
            client.lose_after = True
            with self.assertRaises(Unavailable):
                apply_saved(client, journal)
            self.assertEqual(1, len(client.receipts))
            self.assertEqual(1, len(journal.intents))
            self.assertEqual(0, len(journal.results))
            self.assertEqual(1, sum(kind == "preview" for kind, _ in client.calls))
            client.journal = resumed = Journal(journal.directory, journal.config)
            report = apply_saved(client, resumed)
            self.assertTrue(report["all_removed"])
            self.assertEqual(2, report["committed"])
            self.assertEqual(["0.mp4"], [p.name for p in client.root.iterdir()])
            self.assertEqual(2, sum(kind == "apply" for kind, _ in client.calls))
            self.assertEqual(2, sum(kind == "preview" for kind, _ in client.calls))
            self.assertFalse(Journal.has_active(journal.directory))
            with journal_database(journal.directory) as db:
                summary = db.execute("SELECT summary FROM runs WHERE uuid=?", (journal.run_uuid,)).fetchone()[0]
            self.assertEqual(report, decode(summary))
            self.assertEqual(0, journal.path.stat().st_mode & 0o077)
            self.assertEqual(["dedupe.sqlite3"], [p.name for p in journal.directory.iterdir()])

    def test_lost_uncommitted_response_replays_original_request(self):
        with tempfile.TemporaryDirectory() as directory:
            client, journal, _ = self.fixture(directory, 2)
            client.lose_before = True
            with self.assertRaises(Unavailable):
                apply_saved(client, journal)
            original = next(iter(journal.intents.values()))
            client.journal = resumed = Journal(journal.directory, journal.config)
            self.assertTrue(apply_saved(client, resumed)["all_removed"])
            self.assertEqual([original["request"], original["request"]], [v for k, v in client.calls if k == "apply"])
            self.assertEqual(1, sum(kind == "preview" for kind, _ in client.calls))

    def test_known_rejections_and_blocked_owners_remain_review(self):
        for code in ("deduplication_preview_changed", "deduplication_bytes_differ", "different_media_owners"):
            with self.subTest(code=code), tempfile.TemporaryDirectory() as directory:
                client, journal, _ = self.fixture(directory, 2)
                if code == "different_media_owners":
                    client.blocked = code
                else:
                    client.reject = code
                report = apply_saved(client, journal)
                self.assertTrue(report["finished"])
                self.assertFalse(report["all_removed"])
                self.assertEqual({code: 1}, report["review_reasons"])
                self.assertEqual(2, len(list(client.root.iterdir())))
                self.assertFalse(client.receipts)

    def test_unknown_response_and_uuid_conflict_keep_original_intent_pending(self):
        for code in ("deduplication_request_changed", "invalid_deduplication_receipt", "network_unavailable"):
            with self.subTest(code=code), tempfile.TemporaryDirectory() as directory:
                client, journal, _ = self.fixture(directory, 2)
                client.reject = code
                with self.assertRaises(Unavailable):
                    apply_saved(client, journal)
                self.assertTrue(Journal.has_active(journal.directory))
                self.assertEqual(1, len(journal.intents))
                self.assertFalse(journal.results)
                with self.assertRaises(InvalidData):
                    Journal(journal.directory, {**journal.config, "endpoint": "https://different.example"})
                with self.assertRaises(InvalidData):
                    Journal.create(journal.directory, journal.config, [])

    def test_changed_receipt_and_saved_intent_rejected_before_more_work(self):
        with tempfile.TemporaryDirectory() as directory:
            client, journal, _ = self.fixture(directory)
            client.lose_before = True
            with self.assertRaises(Unavailable):
                apply_saved(client, journal)
            key, intent = next(iter(journal.intents.items()))
            client.receipts[key] = {**receipt(intent["request"]), "remove_path": "different.mp4"}
            with self.assertRaises(InvalidData):
                apply_saved(client, journal)
            self.assertFalse(journal.results)
            value = journal.load(key, "request")
            value["request"]["request_uuid"] = str(uuid.uuid4())
            with journal_database(journal.directory) as db:
                db.execute("UPDATE operations SET intent=? WHERE uuid=?", (encode(value), key))
            calls = len(client.calls)
            with self.assertRaises(InvalidData):
                Journal(journal.directory, journal.config)
            self.assertEqual(calls, len(client.calls))

    def test_crash_before_atomic_finish_does_not_repeat_apply(self):
        with tempfile.TemporaryDirectory() as directory:
            client, journal, _ = self.fixture(directory, 2)
            with patch.object(journal, "finish", side_effect=OSError("crash")), self.assertRaises(OSError):
                apply_saved(client, journal)
            client.journal = resumed = Journal(journal.directory, journal.config)
            self.assertTrue(apply_saved(client, resumed)["all_removed"])
            self.assertFalse(Journal.has_active(journal.directory))
            self.assertEqual(1, sum(kind == "apply" for kind, _ in client.calls))

    def test_database_refuses_foreign_files_and_reopens_pending_snapshot(self):
        with tempfile.TemporaryDirectory() as directory:
            state, _ = private_directory(Path(directory) / "state")
            path = state / "dedupe.sqlite3"
            with closing(sqlite3.connect(path)) as db:
                db.execute("CREATE TABLE unrelated(secret)")
            path.chmod(0o600)
            with self.assertRaises(InvalidData):
                Journal.has_active(state)
            path.unlink()
            with journal_database(state, create=True) as db:
                self.assertEqual(0, db.execute("SELECT count(*) FROM runs").fetchone()[0])
            original = state / "original.sqlite3"
            path.rename(original)
            path.symlink_to(original)
            with self.assertRaises(InvalidData):
                Journal.has_active(state)
        with tempfile.TemporaryDirectory() as directory:
            client, journal, _ = self.fixture(directory, 2)
            client.lose_before = True
            with self.assertRaises(Unavailable):
                apply_saved(client, journal)
            restored, _ = private_directory(Path(directory) / "restored")
            with closing(sqlite3.connect(journal.path)) as source:
                # The committed snapshot includes the manifest and intent.
                with closing(sqlite3.connect(restored / "dedupe.sqlite3")) as destination:
                    source.backup(destination)
            (restored / "dedupe.sqlite3").chmod(0o600)
            client.journal = recovered = Journal(restored, journal.config)
            self.assertEqual(journal.intents, recovered.intents)
            self.assertTrue(apply_saved(client, recovered)["all_removed"])

    def test_report_rejects_escape_symlinks_sidecars_repeats_and_changed_files(self):
        with tempfile.TemporaryDirectory() as directory:
            client, journal, good = self.fixture(directory, 2)
            outside = Path(directory) / "outside.mp4"
            outside.write_bytes(b"fixture\n" * 100)
            link = client.root / "link.mp4"
            link.symlink_to(outside)
            sidecar = client.root / "source.nfo"
            sidecar.write_bytes(outside.read_bytes())
            base = json.loads(good)
            for bad in (str(outside), str(link), str(sidecar), str(client.root / "0.mp4"), "../outside.mp4"):
                body = copy.deepcopy(base)
                body["groups"][0]["files"] = [str(client.root / "0.mp4"), bad]
                with self.subTest(path=bad), self.assertRaises((InvalidData, OSError)):
                    pairs_from_report(encode(body), journal.config["root"], all_content=True)
            with self.assertRaises(InvalidData):
                pairs_from_report(good, journal.config["root"], all_content=False)
            (client.root / "1.mp4").write_bytes(b"changed")
            with self.assertRaises(InvalidData):
                pairs_from_report(good, journal.config["root"], all_content=True)

    def test_real_fclones_discovers_without_deleting(self):
        executable = Path("/home/andrew/.cargo/bin/fclones")
        if not executable.is_file():
            self.skipTest("local fclones not installed")
        with tempfile.TemporaryDirectory() as directory:
            client, journal, _ = self.fixture(directory)
            self.assertEqual([{"keep_path": "0.mp4", "remove_path": "1.mp4"},
                              {"keep_path": "0.mp4", "remove_path": "2.mp4"}],
                             discover(executable, journal.config["root"], all_content=True))
            self.assertEqual(3, len(list(client.root.iterdir())))

    def test_candidate_process_output_and_runtime_are_bounded(self):
        with tempfile.TemporaryDirectory() as directory:
            client, journal, _ = self.fixture(directory, 2)
            executable = Path(directory) / "finder"
            for body, options in (("print('x' * 2048)", {}),
                                  ("import os,time; os.close(1); time.sleep(10)", {"timeout": 0.1})):
                executable.write_text(f"#!{sys.executable}\n" + body + "\n")
                executable.chmod(0o700)
                with patch("stash_ingest.dedupe_candidates.MAX_REPORT_BYTES", 1024), self.assertRaises(InvalidData):
                    discover(executable, journal.config["root"], all_content=True, **options)
                self.assertEqual(2, len(list(client.root.iterdir())))

    def test_boundary_is_rechecked_and_locks_release_after_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with PublicationBarrier([root]) as barrier:
                barrier.check()
                (root / ACTIVE).rename(root / "old-active")
                (root / ACTIVE).touch()
                with self.assertRaises(InvalidData):
                    barrier.check()
            with self.assertRaises(InvalidData):
                barrier.check()
            with PublicationBarrier([root]) as fresh:
                fresh.check()
            with exclusive_lock(root / "library.lock") as check:
                check()
                with self.assertRaises(BlockingIOError):
                    with exclusive_lock(root / "library.lock"):
                        self.fail("must not acquire the same lock twice")
            with exclusive_lock(root / "library.lock") as check:
                check()

    def test_cli_backup_and_all_worker_locks_precede_any_request(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            media, worker = base / "media", base / "worker"
            media.mkdir()
            worker.mkdir()
            for i in range(2):
                (media / f"{i}.mp4").write_bytes(b"fixture\n" * 100)
            body = encode({"groups": [{"file_len": 800, "files": [str(media / f"{i}.mp4") for i in range(2)]}]})
            report_path = base / "report.json"
            report_path.write_bytes(body)
            argv = ["--endpoint", "http://127.0.0.1:8009", "--root", str(media), "--root-uuid", ROOT,
                    "--state-dir", str(base / "state"), "--library-lock", str(base / "library.lock"),
                    "--lock-root", str(worker), "--report", str(report_path), "--all-content", "--lock-timeout", "0.1"]
            client = MemoryClient(media)
            with patch("stash_ingest.dedupe.DeduplicationClient", return_value=client):
                with exclusive_lock(base / "library.lock"), redirect_stderr(io.StringIO()):
                    self.assertEqual(2, main(argv))
                self.assertFalse(client.calls)
                self.assertTrue(Journal.has_active(base / "state"))
                # A blocked apply retains discovery. Recovery must not reread
                # an expensive report, even if it is no longer available.
                report_path.unlink()
                # A worker active on a different thread/process cannot be
                # bypassed. Holding its ordinary shared lock is sufficient.
                fd = os.open(worker / ACTIVE, os.O_RDWR | os.O_CREAT, 0o600)
                try:
                    fcntl.flock(fd, fcntl.LOCK_SH | fcntl.LOCK_NB)
                    with redirect_stderr(io.StringIO()):
                        self.assertEqual(1, main(argv))
                finally:
                    os.close(fd)
                self.assertFalse(client.calls)
                self.assertTrue(Journal.has_active(base / "state"))
                # Blocked candidates finish as review, not successful removal.
                client.blocked = "different_media_owners"
                with redirect_stdout(io.StringIO()) as output:
                    self.assertEqual(3, main(argv))
                self.assertFalse(json.loads(output.getvalue())["all_removed"])
                self.assertFalse(any(kind == "apply" for kind, _ in client.calls))

    def test_discovery_allows_workers_and_backup_but_preview_requires_all_locks(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            media = base / "media"
            media.mkdir()
            workers = [base / "host", base / "n8n"]
            for worker in workers:
                worker.mkdir()
            for i in range(2):
                (media / f"{i}.mp4").write_bytes(b"fixture\n" * 100)
            library_lock = base / "library.lock"
            argv = ["--endpoint", MemoryClient.endpoint, "--root", str(media), "--root-uuid", ROOT,
                    "--state-dir", str(base / "state"), "--library-lock", str(library_lock), "--all-content",
                    "--lock-root", str(workers[0]), "--lock-root", str(workers[1]), "--lock-timeout", "0.1"]
            client = MemoryClient(media)
            client.blocked = "different_media_owners"

            def discover_while_active(*_, **__):
                # Separate flock descriptors exercise real exclusion, without
                # timing-dependent sleeps or needing a full-library scan.
                with exclusive_lock(library_lock), PublicationBarrier(workers, timeout=0.1) as backup:
                    backup.check()
                for worker in workers:
                    with publication_lock(worker, timeout=0.1):
                        pass
                with self.assertRaises(BlockingIOError), exclusive_lock(base / "state" / "run.lock"):
                    self.fail("only discovery's state lock should remain held")
                self.assertFalse(client.calls)
                return [{"keep_path": "0.mp4", "remove_path": "1.mp4"}]

            def guarded_preview(pair):
                for path in [library_lock, *(worker / name for worker in workers for name in (GATE, ACTIVE))]:
                    with self.assertRaises(BlockingIOError), exclusive_lock(path):
                        self.fail("all mutation locks must precede preview")
                return MemoryClient.preview(client, pair)

            with patch("stash_ingest.dedupe.DeduplicationClient", return_value=client), \
                    patch("stash_ingest.dedupe.discover", side_effect=discover_while_active) as discovery, \
                    patch.object(client, "preview", side_effect=guarded_preview), redirect_stdout(io.StringIO()):
                self.assertEqual(3, main(argv))
            discovery.assert_called_once()
            self.assertEqual(["preview"], [kind for kind, _ in client.calls])
            self.assertFalse(Journal.has_active(base / "state"))

    def test_pair_boundary_yields_to_backup_and_recovers_uncertain_removal(self):
        with tempfile.TemporaryDirectory() as directory:
            client, journal, _ = self.fixture(directory)
            base = Path(directory)
            library_lock = base / "library.lock"
            worker = base / "worker"
            worker.mkdir()
            held_by_backup = None
            pause_after_pair = True
            checked_calls = []

            def check_locked():
                for path in (library_lock, worker / GATE, worker / ACTIVE):
                    with self.assertRaises(BlockingIOError), exclusive_lock(path):
                        self.fail("preview, recovery and apply all require exclusion")

            @contextmanager
            def boundary():
                nonlocal held_by_backup, pause_after_pair
                with exclusive_lock(library_lock), PublicationBarrier([worker], timeout=0.1):
                    yield check_locked
                # Downloads and backups can proceed between pairs. Keep the
                # backup lock to force the remaining candidate to wait/retry.
                with publication_lock(worker, timeout=0.1):
                    pass
                if pause_after_pair:
                    pause_after_pair = False
                    held_by_backup = os.open(library_lock, os.O_RDWR)
                    fcntl.flock(held_by_backup, fcntl.LOCK_EX | fcntl.LOCK_NB)

            def guarded(method):
                def call(value):
                    check_locked()
                    checked_calls.append(method.__name__)
                    return method(value)
                return call

            try:
                with patch.object(client, "preview", side_effect=guarded(client.preview)), \
                        patch.object(client, "receipt", side_effect=guarded(client.receipt)), \
                        patch.object(client, "apply", side_effect=guarded(client.apply)):
                    with self.assertRaises(BlockingIOError):
                        apply_saved(client, journal, boundary=boundary)
                    self.assertEqual(1, len(journal.results))
                    self.assertEqual(2, len(list(client.root.iterdir())))
                    os.close(held_by_backup)
                    held_by_backup = None
                    client.journal = resumed = Journal(journal.directory, journal.config)
                    self.assertEqual(journal.run_uuid, resumed.run_uuid)
                    self.assertEqual(journal.records, resumed.records)
                    # Losing a response releases the boundary and leaves the
                    # exact request recoverable, without another removal.
                    client.lose_after = True
                    with self.assertRaises(Unavailable):
                        apply_saved(client, resumed, boundary=boundary)
                    with exclusive_lock(library_lock), publication_lock(worker, timeout=0.1):
                        pass
                    client.journal = recovered = Journal(journal.directory, journal.config)
                    result = apply_saved(client, recovered, boundary=boundary)
                self.assertTrue(result["all_removed"])
                self.assertEqual(2, result["committed"])
                self.assertEqual(["preview", "receipt", "apply", "preview", "receipt", "apply", "receipt"], checked_calls)
                self.assertEqual(["0.mp4"], [p.name for p in client.root.iterdir()])
            finally:
                if held_by_backup is not None:
                    os.close(held_by_backup)

    def test_preview_and_receipt_validation(self):
        pair = {"root_uuid": ROOT, "keep_path": "a.mp4", "remove_path": "b.mp4"}
        good = preview(pair)
        request = {**pair, "request_uuid": str(uuid.uuid4()), "signature": good["signature"]}
        for change in ({"eligible": 1}, {"blocked_reason": "different_media_owners"}, {"bytes": -1},
                       {"root_uuid": str(uuid.uuid4())}, {"signature": "x"}, {"bytes": True}):
            with self.subTest(change=change), self.assertRaises(InvalidData):
                validate_preview({**good, **change}, pair)
        saved = receipt(request)
        for change in ({"uuid": str(uuid.uuid4())}, {"root_uuid": str(uuid.uuid4())}, {"signature": "b" * 64},
                       {"sha256": "x"}, {"kept_generation": True}, {"committed_at": "tomorrow"}):
            with self.subTest(change=change), self.assertRaises(InvalidData):
                validate_receipt({**saved, **change}, request)
        # Original request proof remains usable after canonical UUID adoption.
        validate_receipt({**saved, "media_uuid": str(uuid.uuid4()), "kept_file_uuid": str(uuid.uuid4())}, request)


class HTTPTests(unittest.TestCase):
    def test_real_http_auth_receipt_recovery_redirect_and_bounded_errors(self):
        calls, stored, behavior = [], {}, {}

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_GET(self):
                self.handle_request()

            def do_POST(self):
                self.handle_request()

            def handle_request(self):
                body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
                calls.append((self.command, self.path, self.headers.get("ApiKey"), body))
                status = behavior.get("status", 200)
                if status == 302:
                    self.send_response(status)
                    self.send_header("Location", "/leak")
                    self.end_headers()
                    return
                if behavior:
                    value = behavior.get("body", {"error": "deduplication_request_changed"})
                elif self.path.endswith("/preview"):
                    value = preview(decode(body))
                elif self.path.endswith("/apply"):
                    value = receipt(decode(body))
                    stored[value["uuid"]] = value
                else:
                    value = stored.get(self.path.rsplit("/", 1)[-1])
                    if value is None:
                        status, value = 404, {"error": "not_found"}
                self.send_response(status)
                self.send_header("Content-Type", behavior.get("content_type", "application/json"))
                self.end_headers()
                self.wfile.write(encode(value, 65536))

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            client = DeduplicationClient(f"http://127.0.0.1:{server.server_port}", "DEDUPE_TEST_KEY")
            pair = {"root_uuid": ROOT, "keep_path": "reddit/old.mp4", "remove_path": "reddit/new.mp4"}
            with patch.dict(os.environ, {"DEDUPE_TEST_KEY": "fixture-key"}):
                value = client.preview(pair)
                request = {**pair, "request_uuid": str(uuid.uuid4()), "signature": value["signature"]}
                self.assertIsNone(client.receipt(request))
                result = client.apply(request)
                self.assertEqual(result, client.receipt(request))
                self.assertTrue(all(call[2] == "fixture-key" for call in calls))
                with tempfile.TemporaryDirectory() as directory:
                    key = Path(directory) / "application-key"
                    key.write_text("file-fixture-key\n")
                    key.chmod(0o600)
                    file_client = DeduplicationClient(client.endpoint, "DEDUPE_TEST_KEY", key_file=key)
                    self.assertEqual(result, file_client.receipt(request))
                    self.assertEqual("file-fixture-key", calls[-1][2])
                    self.assertEqual("fixture-key", os.environ["DEDUPE_TEST_KEY"])
                    key.chmod(0o644)
                    with self.assertRaises(InvalidData):
                        file_client.receipt(request)
                for state in ({"status": 302}, {"status": 200, "content_type": "text/html"},
                              {"status": 200, "body": {"padding": "x" * 20000}},
                              {"status": 404, "body": {"error": "file_not_found"}},
                              {"status": 409, "body": {"error": []}}):
                    behavior.clear()
                    behavior.update(state)
                    with self.subTest(state=list(state)), self.assertRaises(Unavailable):
                        client.receipt(request)
                self.assertFalse(any(path == "/leak" for _, path, _, _ in calls))
        finally:
            server.shutdown()
            thread.join()
            server.server_close()


if __name__ == "__main__":
    unittest.main()
