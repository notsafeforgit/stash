"""Resume physical deduplication using saved intents and native server receipts."""

import argparse
from collections import Counter
from contextlib import contextmanager
import fcntl
import json
import os
from pathlib import Path
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


def write_once(path, value, limit=2 * LIMIT):
    """Publish complete private bytes without replacing an existing intent."""
    body = encode(value, limit)
    fd, temporary = tempfile.mkstemp(prefix=".pending-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            output.write(body)
            output.flush()
            os.fsync(output.fileno())
        os.link(temporary, path)
        sync_directory(path.parent)
    finally:
        os.unlink(temporary)
    return digest(body)


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
        active = decode(read_regular(directory / "active.json", LIMIT), LIMIT)
        if not isinstance(active, dict) or set(active) != {"run_uuid", "sha256"}:
            raise InvalidData("Invalid active dedupe reference")
        self.run_uuid = identifier(active["run_uuid"])
        self.path = directory / self.run_uuid
        if self.path.resolve(strict=True) != self.path:
            raise InvalidData("Dedupe run cannot be a symlink")
        body = read_regular(self.path / "manifest.json", MAX_REPORT_BYTES)
        if digest(body) != active["sha256"]:
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
            intent = self.load(key, "request")
            if intent is not None:
                if not isinstance(intent, dict) or set(intent) != {"preview", "request"}:
                    raise InvalidData("Invalid saved dedupe intent")
                preview = validate_preview(intent["preview"], pair)
                request = validate_request(intent["request"], pair)
                if (not preview["eligible"] or request["signature"] != preview["signature"]
                        or request["request_uuid"] != key):
                    raise InvalidData("Saved dedupe intent changed its preview")
                self.intents[key] = intent
            result = self.load(key, "result")
            if result is not None:
                self.validate_result(key, result)
                self.results[key] = result
        if removed & keepers:
            raise InvalidData("Dedupe plan removes a selected survivor")

    @classmethod
    def create(cls, directory, config, pairs):
        if (directory / "active.json").exists() or (directory / "active.json").is_symlink():
            raise InvalidData("An existing dedupe run must be recovered first")
        if len(pairs) > MAX_PAIRS:
            raise InvalidData("Dedupe pair limit exceeded")
        run = str(uuid.uuid4())
        path = directory / run
        path.mkdir(mode=0o700)
        manifest = {"format": FORMAT, "run_uuid": run, "config": config, "created_at": utc_now(),
                    "pairs": [{"uuid": str(uuid.uuid4()), "pair": validate_pair({"root_uuid": config["root_uuid"], **pair})}
                              for pair in pairs]}
        checksum = write_once(path / "manifest.json", manifest, MAX_REPORT_BYTES)
        sync_directory(directory)
        write_once(directory / "active.json", {"run_uuid": run, "sha256": checksum})
        return cls(directory, config)

    def load(self, key, kind):
        try:
            body = read_regular(self.path / (key + "." + kind + ".json"), 2 * LIMIT)
        except FileNotFoundError:
            return None
        return decode(body, 2 * LIMIT)

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
        write_once(self.path / (key + ".request.json"), intent)
        self.intents[key] = intent

    def save_result(self, key, result):
        self.validate_result(key, result)
        write_once(self.path / (key + ".result.json"), result)
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
        summary = self.path / "summary.json"
        if summary.exists():
            if decode(read_regular(summary, LIMIT), LIMIT) != report:
                raise InvalidData("Dedupe summary differs from its receipts")
        else:
            write_once(summary, report)
        (self.directory / "active.json").unlink()
        sync_directory(self.directory)
        return report


def apply_saved(client, journal, check=lambda: None):
    for key, pair in journal.records.items():
        check()
        if key in journal.results:
            continue
        if key not in journal.intents:
            try:
                preview = client.preview(pair)
            except Unavailable as error:
                if error.code != "file_not_found" or error.status != 404:
                    raise
                journal.save_result(key, {"state": "review", "reason": error.code})
                continue
            if not preview["eligible"]:
                journal.save_result(key, {"state": "review", "reason": preview["blocked_reason"]})
                continue
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
                    continue
        validate_receipt(receipt, request)
        journal.save_result(key, {"state": "committed", "receipt": receipt})
    check()
    return journal.finish()


def run(args):
    client = DeduplicationClient(args.endpoint, args.key_env, timeout=args.request_timeout)
    state, state_identity = private_directory(args.state_dir)
    root = directory_identity(args.root)
    roots = sorted({str(Path(p).absolute()) for p in args.lock_root})
    config = {"endpoint": client.endpoint, "root_uuid": identifier(args.root_uuid), "root": root,
              "worker_roots": [directory_identity(p) for p in roots],
              "library_lock": str(Path(args.library_lock).absolute()), "all_content": args.all_content}
    with exclusive_lock(state / "run.lock") as state_check:
        with exclusive_lock(config["library_lock"]) as library_check:
            with PublicationBarrier(roots, timeout=args.lock_timeout) as barrier:
                def check():
                    state_check()
                    library_check()
                    check_directory(state_identity)
                    check_directory(root)
                    for worker_root in config["worker_roots"]:
                        check_directory(worker_root)
                    barrier.check()

                check()
                if (state / "active.json").exists() or (state / "active.json").is_symlink():
                    journal = Journal(state, config)
                else:
                    if args.report:
                        pairs = pairs_from_report(read_regular(args.report, MAX_REPORT_BYTES), root,
                                                  all_content=args.all_content)
                    else:
                        pairs = discover(args.fclones, root, all_content=args.all_content)
                    check()
                    journal = Journal.create(state, config, pairs)
                return apply_saved(client, journal, check)


def main(argv=None):
    parser = argparse.ArgumentParser(description="Find duplicate files and resume verified native Stash removals.")
    for name in ("endpoint", "root", "root-uuid", "state-dir", "library-lock"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--lock-root", action="append", required=True, help="Every inventoried native worker lock root")
    parser.add_argument("--key-env", default="STASH_API_KEY")
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
        print("Backup, dedupe or state lock is busy; no new work started", file=sys.stderr)
        return 2
    except (InvalidData, Unavailable) as error:
        print(str(error), file=sys.stderr)
        return 1
    except OSError:
        print("Dedupe filesystem operation failed; saved requests retained for recovery", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        return 130


if __name__ == "__main__":
    raise SystemExit(main())
