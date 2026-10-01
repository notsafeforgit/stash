"""Validate and import a frozen legacy n8n receipt directory into its native outbox."""

import argparse
from contextlib import closing
import json
import os
from pathlib import Path
import sqlite3
import stat
import sys

from .encoding import InvalidData, identifier
from .n8n_receipts import LegacyReceipts, MAX_BYTES, MAX_FILES, MAX_RECEIPT, TOKEN, review
from .outbox import Capacity, Outbox


def stamp(value):
    # Reading may update atime; it does not change the source evidence.
    return (value.st_dev, value.st_ino, value.st_mode, value.st_size, value.st_mtime_ns, value.st_ctime_ns)


def snapshot(path):
    # Provision an offline directory snapshot at the common backup boundary.
    # Pin that directory and reject nonregular entries, unsafe names, changing
    # files and unbounded input. No path from receipt content is ever opened.
    fd = os.open(Path(path).absolute(), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        names = sorted(os.listdir(fd))
        if len(names) > MAX_FILES:
            raise Capacity("Legacy n8n input exceeds the bounded snapshot capacity")
        records, total, stats = [], 0, {}
        for name in names:
            if not name.endswith(".json") or not TOKEN.fullmatch(name[:-5]):
                raise InvalidData("Unrecognized file in legacy n8n receipt snapshot")
            source = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
            try:
                before = os.fstat(source)
                if not stat.S_ISREG(before.st_mode) or not 1 <= before.st_size <= MAX_RECEIPT:
                    raise InvalidData("Legacy n8n receipt must be a bounded regular file")
                total += before.st_size
                if total > MAX_BYTES:
                    raise Capacity("Legacy n8n input exceeds the bounded snapshot capacity")
                chunks, size = [], 0
                while data := os.read(source, min(65536, MAX_RECEIPT + 1 - size)):
                    chunks.append(data)
                    size += len(data)
                    if size > MAX_RECEIPT:
                        raise InvalidData("Legacy n8n receipt changed while reading")
                if stamp(os.fstat(source)) != stamp(before) or size != before.st_size:
                    raise InvalidData("Legacy n8n receipt changed while reading")
                stats[name] = stamp(before)
                records.append((name[:-5], b"".join(chunks)))
            finally:
                os.close(source)
        if sorted(os.listdir(fd)) != names or any(
                stamp(os.stat(name, dir_fd=fd, follow_symlinks=False)) != before for name, before in stats.items()):
            raise InvalidData("Legacy n8n snapshot changed while reading")
        return records
    finally:
        os.close(fd)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--receipts", required=True, help="Frozen receipt-directory snapshot; read only")
    parser.add_argument("--source", required=True, help="Stable receipt-source UUID from the migration manifest")
    parser.add_argument("--outbox", help="Native n8n producer outbox; required with --apply")
    parser.add_argument("--endpoint", help="Stash origin bound to that outbox; no network requests are made")
    parser.add_argument("--producer", help="Producer UUID bound to that outbox")
    parser.add_argument("--expected-sha256", help="Reviewed input_sha256 from preflight; required with --apply")
    parser.add_argument("--apply", action="store_true", help="Commit the complete snapshot in one transaction")
    args = parser.parse_args(argv)
    try:
        identifier(args.source)
        if args.apply and not all((args.outbox, args.endpoint, args.producer, args.expected_sha256)):
            raise InvalidData("Applying legacy receipts requires an explicit outbox binding and reviewed snapshot digest")
        records = snapshot(args.receipts)
        result = review(records)
        if args.expected_sha256 is not None and args.expected_sha256 != result["input_sha256"]:
            raise InvalidData("Legacy n8n snapshot differs from the reviewed digest")
        result.update(source_uuid=args.source, state="validated", added=0, replayed=0)
        if args.apply:
            with closing(Outbox(args.outbox, args.endpoint, args.producer)) as box:
                result.update(LegacyReceipts(box).import_snapshot(args.source, records, args.expected_sha256))
            result["state"] = "imported"
        print(json.dumps(result, sort_keys=True))
        return 0
    except (InvalidData, Capacity) as error:
        message = str(error)
    except (OSError, sqlite3.Error):
        message = "Legacy receipt snapshot or native outbox storage is unavailable"
    print(json.dumps({"error": message, "state": "not_imported"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
