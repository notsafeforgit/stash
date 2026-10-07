"""Scheduled/pre-backup dedupe configuration and bounded completion cooldown."""

import argparse
import json
import os
from pathlib import Path
import sqlite3
import stat
import sys
import tempfile
import time

from .catalog_snapshot import sync_directory
from .catalog_upload import read_regular
from .client import Unavailable
from .dedupe import Journal, exclusive_lock, private_directory, run
from .encoding import InvalidData, decode, identifier
from .endpoint import origin

FORMAT = "stash-host-dedupe-v1"
CONFIG_LIMIT = 65536


def absolute(value):
    if (not isinstance(value, str) or not value.startswith("/") or str(Path(value)) != value
            or ".." in Path(value).parts or any(ord(c) < 32 for c in value)):
        raise InvalidData("Host dedupe paths must be canonical absolute paths")
    return value


def configuration(value):
    required = {"format", "endpoint", "root", "root_uuid", "state_dir", "library_lock",
                "lock_roots", "api_key_file", "fclones", "stamp_file"}
    if not isinstance(value, dict) or set(value) != required or value["format"] != FORMAT:
        raise InvalidData("Invalid host dedupe configuration")
    identifier(value["root_uuid"])
    if origin(value["endpoint"]) != value["endpoint"]:
        raise InvalidData("Host dedupe endpoint must be a canonical origin")
    for key in ("root", "state_dir", "library_lock", "api_key_file", "fclones", "stamp_file"):
        absolute(value[key])
    roots = value["lock_roots"]
    if (not isinstance(roots, list) or not 1 <= len(roots) <= 256
            or len(set(absolute(root) for root in roots)) != len(roots)):
        raise InvalidData("Host dedupe requires distinct inventoried worker lock roots")
    return value


def load(filename):
    return configuration(decode(read_regular(filename, CONFIG_LIMIT), CONFIG_LIMIT))


def recent_stamp(filename, now):
    try:
        body = read_regular(filename, 64)
    except FileNotFoundError:
        return False
    text = body.decode("ascii").strip()
    if not text.isdecimal():
        raise InvalidData("Invalid dedupe completion timestamp")
    stamp = int(text)
    # A clock correction must not suppress maintenance indefinitely.
    return 0 <= now - stamp < 86400


def publish_stamp(filename, now):
    path = Path(filename)
    if path.parent.resolve(strict=True) != path.parent:
        raise InvalidData("Dedupe timestamp parent must be canonical")
    if path.exists() or path.is_symlink():
        if not stat.S_ISREG(path.lstat().st_mode):
            raise InvalidData("Dedupe timestamp must be a regular file")
    fd, temporary = tempfile.mkstemp(prefix=".dedupe-stamp-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            output.write((str(now) + "\n").encode("ascii"))
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        sync_directory(path.parent)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def execute(value, *, before_backup=False, all_content=False):
    value = configuration(value)
    state, _ = private_directory(value["state_dir"])
    with exclusive_lock(state / "launcher.lock"):
        # Pending requests always recover, even if an earlier completed run left
        # a recent stamp. Pre-backup discovery deliberately ignores the cooldown.
        if not before_backup and not Journal.has_active(state) and recent_stamp(value["stamp_file"], int(time.time())):
            return {"skipped": True, "reason": "daily_cooldown"}
        args = argparse.Namespace(endpoint=value["endpoint"], root=value["root"], root_uuid=value["root_uuid"],
                                  state_dir=value["state_dir"], library_lock=value["library_lock"],
                                  lock_root=value["lock_roots"], key_file=value["api_key_file"], key_env="STASH_API_KEY",
                                  fclones=value["fclones"], all_content=all_content, report=None,
                                  request_timeout=900, lock_timeout=300)
        result = run(args)
        if not result.get("finished") or result.get("pending") != 0:
            raise InvalidData("Dedupe did not finish; completion timestamp retained")
        publish_stamp(value["stamp_file"], int(time.time()))
        return result


def main(argv=None):
    parser = argparse.ArgumentParser(description="Run native dedupe for a scheduled or pre-backup host invocation")
    parser.add_argument("--config", required=True)
    parser.add_argument("--before-backup", action="store_true")
    parser.add_argument("--all-content", action="store_true")
    args = parser.parse_args(argv)
    try:
        result = execute(load(args.config), before_backup=args.before_backup, all_content=args.all_content)
    except BlockingIOError:
        result = {"skipped": True, "reason": "busy"}
    except (InvalidData, Unavailable, UnicodeError) as error:
        print(str(error), file=sys.stderr)
        return 1
    except (OSError, sqlite3.Error):
        print("Host dedupe failed; pending state retained and completion timestamp unchanged", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        return 130
    # Review is a completed maintenance pass, not a claim that those pairs were
    # removed. Keep it explicit in the JSON and permit the ordinary backup.
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
