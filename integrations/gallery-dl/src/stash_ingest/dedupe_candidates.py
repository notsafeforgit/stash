"""Bounded candidate discovery; fclones never receives a removal command."""

import os
from pathlib import Path
import selectors
import signal
import stat
import subprocess
import time

from .album_client import integer
from .dedupe_client import relative_path
from .encoding import InvalidData, decode

MAX_REPORT_BYTES = 64 << 20
MAX_PAIRS = 100000
EXTENSIONS = {"jpg", "jpeg", "png", "gif", "gifv", "avif", "webp", "heic",
              "mp4", "m4v", "mkv", "wmv", "mov", "webm", "avi", "flv", "mpg", "mpeg"}
SCRAPED = {"twitter", "bluesky", "reddit", "thisvid", "onlyfans", "fansly", "patreon"}


def directory_identity(value):
    path = Path(value).absolute()
    if path.resolve(strict=True) != path or not path.is_dir():
        raise InvalidData("Dedupe directories must be canonical, existing directories")
    info = path.stat()
    return {"path": str(path), "device": info.st_dev, "inode": info.st_ino}


def check_directory(value):
    if directory_identity(value["path"]) != value:
        raise InvalidData("Dedupe directory or mount changed")


def file_info(root, path):
    path = Path(path)
    if not path.is_absolute():
        raise InvalidData("Fclones must return absolute media paths")
    try:
        relative = relative_path(path.relative_to(root["path"]).as_posix())
    except ValueError:
        raise InvalidData("Fclones candidate lies outside the reviewed root") from None
    if str(path) != str(Path(root["path"]) / relative) or path.resolve(strict=True) != path:
        raise InvalidData("Fclones candidate traverses a symlink or noncanonical path")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_dev != root["device"]:
            raise InvalidData("Fclones candidate must be a regular file on the reviewed filesystem")
        return relative, info
    finally:
        os.close(fd)


def pairs_from_report(body, root, *, all_content=False):
    check_directory(root)
    report = decode(body, MAX_REPORT_BYTES)
    if (not isinstance(report, dict) or not isinstance(report.get("groups"), list)
            or len(report["groups"]) > MAX_PAIRS):
        raise InvalidData("Invalid or oversized fclones group report")
    pairs, seen = [], set()
    for group in report["groups"]:
        if not isinstance(group, dict):
            raise InvalidData("Invalid fclones group")
        size = integer(group.get("file_len"), 0, (1 << 63) - 1)
        files = group.get("files")
        if not isinstance(files, list) or not 2 <= len(files) <= MAX_PAIRS + 1:
            raise InvalidData("Invalid fclones file list")
        if len(pairs) + len(files) - 1 > MAX_PAIRS:
            raise InvalidData("Dedupe report exceeds 100000 pairs")
        members = []
        for path in files:
            if not isinstance(path, str) or path in seen:
                raise InvalidData("Repeated or invalid fclones path")
            seen.add(path)
            relative, info = file_info(root, path)
            if Path(relative).suffix.lower()[1:] not in EXTENSIONS:
                raise InvalidData("Fclones returned an unsupported media extension")
            if not all_content and not any(name in str(Path(relative).parent).lower() for name in SCRAPED):
                raise InvalidData("Fclones returned a path outside the scraped scope")
            if info.st_size != size:
                raise InvalidData("Fclones candidate changed size after discovery")
            members.append((info.st_mtime_ns, relative))
        if size == 0:
            continue
        # The old `remove --priority newest` retains the oldest copy. Preserve
        # that preference, using paths to resolve equal mtimes deterministically.
        members.sort()
        pairs.extend({"keep_path": members[0][1], "remove_path": member[1]} for member in members[1:])
    check_directory(root)
    return pairs


def discover(executable, root, *, all_content=False, timeout=7200):
    if not Path(executable).is_absolute():
        raise InvalidData("Use an absolute fclones executable path")
    command = [str(executable), "group", root["path"], "--match-links", "--hidden",
               "--ignore-case", "--format", "json", "--hash-fn", "blake3"]
    if not all_content:
        command += ["--path", "**{" + ",".join(sorted(SCRAPED)) + "}**"]
    command += ["--name", "*.{" + ",".join(sorted(EXTENSIONS)) + "}",
                "--exclude", "**/*.{nfo,part,sqlite,sqlite3,db,json,jsonl}"]
    check_directory(root)
    child = subprocess.Popen(command, stdout=subprocess.PIPE, start_new_session=True)
    deadline, chunks, size = time.monotonic() + timeout, [], 0
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(child.stdout, selectors.EVENT_READ)
            while True:
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not selector.select(remaining):
                    raise InvalidData("Fclones discovery timed out")
                chunk = os.read(child.stdout.fileno(), 65536)
                if not chunk:
                    break
                size += len(chunk)
                if size > MAX_REPORT_BYTES:
                    raise InvalidData("Fclones report exceeds its byte limit")
                chunks.append(chunk)
        if child.wait(timeout=max(0.01, deadline - time.monotonic())) != 0:
            raise InvalidData("Fclones discovery failed; no removals submitted")
        return pairs_from_report(b"".join(chunks), root, all_content=all_content)
    except subprocess.TimeoutExpired:
        raise InvalidData("Fclones discovery timed out") from None
    finally:
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGKILL)
        child.wait()
        child.stdout.close()
