"""Resume physical deduplication using saved intents and native server receipts."""

import argparse
from collections import Counter
from contextlib import closing, contextmanager, nullcontext
import fcntl
import json
import os
from pathlib import Path
import sqlite3
import stat
import sys
import tempfile
import uuid

from .catalog_snapshot import sync_directory
from .catalog_source import source_time
from .catalog_upload import read_regular
from .client import Unavailable
from .dedupe_candidates import MAX_PAIRS, MAX_REPORT_BYTES, check_directory, directory_identity, discover, pairs_from_report
from .dedupe_client import BLOCKED, LIMIT, REJECTIONS, DeduplicationClient, validate_pair, validate_preview, validate_receipt, validate_request
from .encoding import InvalidData, decode, digest, encode, identifier, utc_now
from .publication_lock import PublicationBarrier

FORMAT = "stash-file-deduplication-v1"
JOURNAL_NAME = "dedupe.sqlite3"
APPLICATION_ID = 0x53444450


@contextmanager
def journal_database(directory, *, create=False):
    path = Path(directory) / JOURNAL_NAME
    # No real client used the earlier development-only JSON format. Refuse it
    # rather than silently abandoning an uncertain request from that version.
    if (Path(directory) / "active.json").exists() or (Path(directory) / "active.json").is_symlink():
        raise InvalidData("Recover the earlier development dedupe journal before using this client")
    if not path.exists() and not path.is_symlink() and create:
        fd, temporary = tempfile.mkstemp(prefix=".dedupe-create-", dir=directory)
        os.close(fd)
        try:
            with closing(sqlite3.connect(temporary)) as db:
                db.executescript(f"""
                    PRAGMA synchronous=FULL;
                    BEGIN IMMEDIATE;
                    PRAGMA application_id={APPLICATION_ID};
                    PRAGMA user_version=1;
                    CREATE TABLE runs(uuid TEXT PRIMARY KEY, manifest BLOB NOT NULL,
                        sha256 TEXT NOT NULL, summary BLOB);
                    CREATE UNIQUE INDEX one_active_run ON runs((1)) WHERE summary IS NULL;
                    CREATE TABLE operations(run_uuid TEXT NOT NULL REFERENCES runs(uuid),
                        uuid TEXT NOT NULL, intent BLOB, result BLOB,
                        PRIMARY KEY(run_uuid, uuid), CHECK(intent IS NOT NULL OR result IS NOT NULL));
                    COMMIT;
                """)
            os.link(temporary, path)
            sync_directory(Path(directory))
        finally:
            os.unlink(temporary)
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise InvalidData("Dedupe database must be a private, owned regular file")
    with closing(sqlite3.connect(path.as_uri() + "?mode=rw", uri=True, timeout=5)) as db:
        db.execute("PRAGMA trusted_schema=OFF")
        db.execute("PRAGMA foreign_keys=ON")
        db.execute("PRAGMA synchronous=FULL")
        if (db.execute("PRAGMA application_id").fetchone()[0] != APPLICATION_ID
                or db.execute("PRAGMA user_version").fetchone()[0] != 1):
            raise InvalidData("Unsupported dedupe database")
        with db:
            db.execute("BEGIN IMMEDIATE")
            yield db

def private_directory(path):
    path = Path(path).absolute()
    path.mkdir(mode=0o700, exist_ok=True)
    identity = directory_identity(path)
    info = path.stat()
    if info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise InvalidData("Dedupe state directory must be private and owned by this user")
    return path, identity


@contextmanager
def exclusive_lock(path):
    path = Path(path).absolute()
    if path.parent.resolve(strict=True) != path.parent:
        raise InvalidData("Dedupe lock parent must be canonical")
    fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode):
            raise InvalidData("Dedupe lock must be a regular file")
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)

        def check():
            current = path.lstat()
            if (not stat.S_ISREG(current.st_mode)
                    or (current.st_dev, current.st_ino) != (info.st_dev, info.st_ino)):
                raise InvalidData("Dedupe lock was replaced")

        check()
        yield check
    finally:
        os.close(fd)


class Journal:
    def __init__(self, directory, config):
        self.directory = directory
        self.config = config
        self.path = directory / JOURNAL_NAME
        with journal_database(directory) as db:
            active = db.execute("SELECT uuid,sha256,length(manifest) FROM runs WHERE summary IS NULL").fetchone()
            if active is None:
                raise InvalidData("No pending dedupe run")
            self.run_uuid = identifier(active[0])
            if active[2] > MAX_REPORT_BYTES:
                raise InvalidData("Dedupe manifest exceeds its size limit")
            body = db.execute("SELECT manifest FROM runs WHERE uuid=?", (self.run_uuid,)).fetchone()[0]
            rows = db.execute("SELECT uuid,length(intent),length(result) FROM operations WHERE run_uuid=? LIMIT ?",
                              (self.run_uuid, MAX_PAIRS + 1)).fetchall()
            if len(rows) > MAX_PAIRS or any(any(n is not None and n > 2 * LIMIT for n in row[1:]) for row in rows):
                raise InvalidData("Dedupe operations exceed their bounds")
            saved = {row[0]: row[1:] for row in db.execute(
                "SELECT uuid,intent,result FROM operations WHERE run_uuid=?", (self.run_uuid,))}
        if digest(body) != active[1]:
            raise InvalidData("Dedupe manifest changed")
        manifest = decode(body, MAX_REPORT_BYTES)
        if (not isinstance(manifest, dict) or manifest.get("format") != FORMAT
                or manifest.get("run_uuid") != self.run_uuid or manifest.get("config") != config):
            raise InvalidData("Saved dedupe scope differs; recover with its original configuration")
        source_time(manifest.get("created_at"))
        records = manifest.get("pairs")
        if not isinstance(records, list) or len(records) > MAX_PAIRS:
            raise InvalidData("Invalid saved dedupe pair list")
        self.records, self.intents, self.results = {}, {}, {}
        removed, keepers = set(), set()
        # Validate every saved item before making any network request. A bad
        # later record cannot cause a partially applied newly loaded plan.
        for record in records:
            if not isinstance(record, dict) or set(record) != {"uuid", "pair"}:
                raise InvalidData("Invalid saved dedupe record")
            key = identifier(record["uuid"])
            pair = validate_pair(record["pair"])
            if (key in self.records or pair["root_uuid"] != config["root_uuid"]
                    or pair["remove_path"] in removed):
                raise InvalidData("Repeated dedupe identity or removal")
            removed.add(pair["remove_path"])
            keepers.add(pair["keep_path"])
            self.records[key] = pair
            intent = decode(saved[key][0], 2 * LIMIT) if key in saved and saved[key][0] is not None else None
            if intent is not None:
                if not isinstance(intent, dict) or set(intent) != {"preview", "request"}:
                    raise InvalidData("Invalid saved dedupe intent")
                preview = validate_preview(intent["preview"], pair)
                request = validate_request(intent["request"], pair)
                if (not preview["eligible"] or request["signature"] != preview["signature"]
                        or request["request_uuid"] != key):
                    raise InvalidData("Saved dedupe intent changed its preview")
                self.intents[key] = intent
            result = decode(saved[key][1], 2 * LIMIT) if key in saved and saved[key][1] is not None else None
            if result is not None:
                self.validate_result(key, result)
                self.results[key] = result
        if saved.keys() - self.records.keys():
            raise InvalidData("Dedupe operation is absent from its manifest")
        if removed & keepers:
            raise InvalidData("Dedupe plan removes a selected survivor")

    @classmethod
    def has_active(cls, directory):
        try:
            with journal_database(directory) as db:
                return db.execute("SELECT 1 FROM runs WHERE summary IS NULL").fetchone() is not None
        except FileNotFoundError:
            return False

    @classmethod
    def create(cls, directory, config, pairs):
        if len(pairs) > MAX_PAIRS:
            raise InvalidData("Dedupe pair limit exceeded")
        run = str(uuid.uuid4())
        manifest = {"format": FORMAT, "run_uuid": run, "config": config, "created_at": utc_now(),
                    "pairs": [{"uuid": str(uuid.uuid4()), "pair": validate_pair({"root_uuid": config["root_uuid"], **pair})}
                              for pair in pairs]}
        body = encode(manifest, MAX_REPORT_BYTES)
        with journal_database(directory, create=True) as db:
            if db.execute("SELECT 1 FROM runs WHERE summary IS NULL").fetchone() is not None:
                raise InvalidData("An existing dedupe run must be recovered first")
            db.execute("INSERT INTO runs(uuid,manifest,sha256) VALUES(?,?,?)", (run, body, digest(body)))
        return cls(directory, config)

    def load(self, key, kind):
        if kind not in ("request", "result"):
            raise InvalidData("Invalid dedupe record kind")
        column = "intent" if kind == "request" else "result"
        with journal_database(self.directory) as db:
            row = db.execute("SELECT " + column + " FROM operations WHERE run_uuid=? AND uuid=?",
                             (self.run_uuid, key)).fetchone()
        return decode(row[0], 2 * LIMIT) if row and row[0] is not None else None

    def guard(self, db):
        row = db.execute("SELECT summary FROM runs WHERE uuid=?", (self.run_uuid,)).fetchone()
        if row is None or row[0] is not None:
            raise InvalidData("Dedupe run is no longer active")

    def validate_result(self, key, result):
        if not isinstance(result, dict):
            raise InvalidData("Invalid saved dedupe result")
        if result.get("state") == "committed" and set(result) == {"state", "receipt"}:
            if key not in self.intents:
                raise InvalidData("Committed dedupe is missing its saved intent")
            validate_receipt(result["receipt"], self.intents[key]["request"])
        elif (result.get("state") == "review" and set(result) == {"state", "reason"}
              and result.get("reason") in BLOCKED | REJECTIONS):
            pass
        else:
            raise InvalidData("Invalid saved dedupe result")

    def save_intent(self, key, preview):
        pair = self.records[key]
        validate_preview(preview, pair)
        if not preview["eligible"]:
            raise InvalidData("Cannot submit an ineligible dedupe preview")
        intent = {"preview": preview, "request": {**pair, "request_uuid": key, "signature": preview["signature"]}}
        with journal_database(self.directory) as db:
            self.guard(db)
            if db.execute("SELECT 1 FROM operations WHERE run_uuid=? AND uuid=?", (self.run_uuid, key)).fetchone():
                raise InvalidData("Dedupe intent already exists")
            db.execute("INSERT INTO operations(run_uuid,uuid,intent) VALUES(?,?,?)",
                       (self.run_uuid, key, encode(intent, 2 * LIMIT)))
        self.intents[key] = intent

    def save_result(self, key, result):
        self.validate_result(key, result)
        with journal_database(self.directory) as db:
            self.guard(db)
            cursor = db.execute("""INSERT INTO operations(run_uuid,uuid,result) VALUES(?,?,?)
                ON CONFLICT(run_uuid,uuid) DO UPDATE SET result=excluded.result WHERE operations.result IS NULL""",
                (self.run_uuid, key, encode(result, 2 * LIMIT)))
            if cursor.rowcount != 1:
                raise InvalidData("Dedupe result already exists")
        self.results[key] = result

    def report(self):
        counts = Counter(result["state"] for result in self.results.values())
        reasons = Counter(result["reason"] for result in self.results.values() if result["state"] == "review")
        return {"run_uuid": self.run_uuid, "pairs": len(self.records), "committed": counts["committed"],
                "review": counts["review"], "pending": len(self.records) - len(self.results),
                "review_reasons": dict(reasons), "finished": len(self.records) == len(self.results),
                "all_removed": len(self.records) == counts["committed"]}

    def finish(self):
        report = self.report()
        if not report["finished"]:
            raise InvalidData("Cannot finish dedupe with unresolved requests")
        with journal_database(self.directory) as db:
            self.guard(db)
            count = db.execute("SELECT count(*) FROM operations WHERE run_uuid=? AND result IS NOT NULL",
                               (self.run_uuid,)).fetchone()[0]
            if count != len(self.records):
                raise InvalidData("Dedupe result persistence is incomplete")
            db.execute("UPDATE runs SET summary=? WHERE uuid=?", (encode(report, LIMIT), self.run_uuid))
        return report


def apply_saved(client, journal, check=lambda: None, *, boundary=None):
    for key, pair in journal.records.items():
        check()
        if key in journal.results:
            continue
        # Discovery is only a hint. Each pair gets a fresh, independently
        # releasable mutation boundary before any preview/recovery/apply call.
        # A backup taking the lock between pairs leaves the saved run resumable.
        with (boundary() if boundary else nullcontext(check)) as guarded:
            guarded()
            apply_pair(client, journal, key, pair, guarded)
    check()
    return journal.finish()


def apply_pair(client, journal, key, pair, check):
    if key not in journal.intents:
        try:
            preview = client.preview(pair)
        except Unavailable as error:
            if error.code != "file_not_found" or error.status != 404:
                raise
            journal.save_result(key, {"state": "review", "reason": error.code})
            return
        if not preview["eligible"]:
            journal.save_result(key, {"state": "review", "reason": preview["blocked_reason"]})
            return
        # Earlier removals can advance the owner's revision. Preview each
        # next pair only after recovering/committing the preceding one.
        journal.save_intent(key, preview)
    request = journal.intents[key]["request"]
    check()
    receipt = client.receipt(request)
    if receipt is None:
        check()
        try:
            receipt = client.apply(request)
        except Unavailable as error:
            if (error.status, error.code) not in {
                    (409, "deduplication_preview_changed"), (409, "deduplication_bytes_differ"),
                    (404, "file_not_found")}:
                raise
            # A replay racing a prior response may see a changed preview.
            # Prefer its committed receipt over classifying it as rejected.
            receipt = client.receipt(request)
            if receipt is None:
                journal.save_result(key, {"state": "review", "reason": error.code})
                return
    validate_receipt(receipt, request)
    journal.save_result(key, {"state": "committed", "receipt": receipt})


def run(args):
    client = DeduplicationClient(args.endpoint, args.key_env, timeout=args.request_timeout,
                                 key_file=getattr(args, "key_file", None))
    state, state_identity = private_directory(args.state_dir)
    root = directory_identity(args.root)
    roots = sorted({str(Path(p).absolute()) for p in args.lock_root})
    config = {"endpoint": client.endpoint, "root_uuid": identifier(args.root_uuid), "root": root,
              "worker_roots": [directory_identity(p) for p in roots],
              "library_lock": str(Path(args.library_lock).absolute()), "all_content": args.all_content}
    with exclusive_lock(state / "run.lock") as state_check:
        def check():
            state_check()
            check_directory(state_identity)
            check_directory(root)
            for worker_root in config["worker_roots"]:
                check_directory(worker_root)

        @contextmanager
        def boundary():
            with exclusive_lock(config["library_lock"]) as library_check:
                with PublicationBarrier(roots, timeout=args.lock_timeout) as barrier:
                    def guarded():
                        check()
                        library_check()
                        barrier.check()

                    guarded()
                    yield guarded

        check()
        if Journal.has_active(state):
            journal = Journal(state, config)
        else:
            # Fclones never mutates media. Do not block downloads and backups
            # for a full-library scan; Stash rechecks each candidate and proves
            # equal bytes under its own file-lifetime/deletion guards at Apply.
            if args.report:
                pairs = pairs_from_report(read_regular(args.report, MAX_REPORT_BYTES), root,
                                          all_content=args.all_content)
            else:
                pairs = discover(args.fclones, root, all_content=args.all_content)
            check()
            journal = Journal.create(state, config, pairs)
        return apply_saved(client, journal, check, boundary=boundary)


def main(argv=None):
    parser = argparse.ArgumentParser(description="Find duplicate files and resume verified native Stash removals.")
    for name in ("endpoint", "root", "root-uuid", "state-dir", "library-lock"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--lock-root", action="append", required=True, help="Every inventoried native worker lock root")
    parser.add_argument("--key-env", default="STASH_API_KEY")
    parser.add_argument("--key-file", help="Private application key file; overrides the key environment variable")
    parser.add_argument("--fclones", default="/home/andrew/.cargo/bin/fclones")
    parser.add_argument("--report", help="Use a saved fclones JSON report instead of running discovery")
    parser.add_argument("--all-content", action="store_true")
    parser.add_argument("--request-timeout", type=float, default=900)
    parser.add_argument("--lock-timeout", type=float, default=300)
    args = parser.parse_args(argv)
    try:
        report = run(args)
        print(json.dumps(report, sort_keys=True))
        return 3 if report["review"] else 0
    except BlockingIOError:
        print("Backup, dedupe or state lock is busy; pending candidates retained", file=sys.stderr)
        return 2
    except (InvalidData, Unavailable) as error:
        print(str(error), file=sys.stderr)
        return 1
    except (OSError, sqlite3.Error):
        print("Dedupe state or filesystem operation failed; saved requests retained for recovery", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        return 130


if __name__ == "__main__":
    raise SystemExit(main())
