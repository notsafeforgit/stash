#!/usr/bin/env python3
# Install the host integration package and its local native dependencies before
# cutover. The original installed script remains pinned until then.
from __future__ import annotations

import os
import stat
import sys
import io
import re
import json
import time
import gzip
import shutil
import signal
import tarfile
import hashlib
import base64
import argparse
import tempfile
import subprocess
import shlex
import fcntl
import sqlite3
import logging
from concurrent.futures import ThreadPoolExecutor
import boto3
from botocore.config import Config
from botocore.exceptions import ClientError, NoCredentialsError, PartialCredentialsError
from native_backup import NativeBackupSession
from contextlib import contextmanager
from datetime import datetime

try:
    from logging_journald import JournaldLogHandler, check_journal_stream
except Exception:
    JournaldLogHandler = None

    def check_journal_stream() -> bool:
        return False


# Force unbuffered stdout/stderr for immediate systemd journalctl logging
# Stream buffering is managed by the service (PYTHONUNBUFFERED=1).

# ============================================================
# ASSUMPTIONS / DESIGN
# ============================================================
# - Lifecycle expires obsolete=true objects at creation age >=180 days,
#   then expires their noncurrent versions after 1 day.
# - Cleanup only changes tags; direct S3 deletion is disabled.
# - Missing files/collections on the available source are intentional deletions
#   by policy. No separate deletion receipt or approval is required.
# - An unavailable source filesystem stops publication and obsolete tagging.
# - Master manifest is the restore allowlist
#
# Tarball model (partitioned, non-overlapping):
#   - Leaf tar-bearing dirs => recursive tarball (stable key: tarballs/<safe>.tar)
#   - Dirs with direct tar-eligible files AND tar-bearing descendants => files-only tarball
#       stable key: tarballs/<safe>.__root__.tar
#   - No ancestor/descendant duplication
#
# Compatibility retained:
#   - Old dated tarballs tarballs/<safe>_YYYYMMDD.tar still recognized for recursive units
#   - JSONL footprint loader accepts old {"rel_dir":"...","fp":"..."} and rewrites to {"unit":"R:...","fp":"..."}
# ============================================================

# ==========================================
# CONFIGURATION
# ==========================================
BASE_DIR = "/tank/media/porn/"
SOURCE_VIEW = None
LEDGER_DIR = "/tank/media/backup_ledgers/"
TMP_BASE_DIR = "/home/andrew/s3_backup_tmp/"

# rclone remotes
REMOTE_PATH = "glacier:video-backup-andrew/"
STANDARD_REMOTE_PATH = "s3-standard:metadata-backup-andrew/"

VIDEO_EXTENSIONS = (
    ".mp4", ".mkv", ".avi", ".mov", ".webm",
    ".wmv", ".flv", ".m4v", ".mpg", ".mpeg", ".gifv"
)

# Files that go into tarballs
TAR_INCLUDE_EXTENSIONS = (
    ".jpg", ".jpeg", ".png", ".gif", ".bmp",
    ".tiff", ".webp", ".heic", ".nfo", ".avif"
)

WINDOW_DAYS = 3
BATCH_SIZE = 250
PROGRESS_EVERY = 25

# ==========================================
# INCREMENTAL TAR SETTINGS (BASE + DELTAS)
# ==========================================
# Existing tarballs/ objects (stable or dated) are treated as the BASELINE.
# We now create append-only DELTA packs under tarballs/delta/ so daily changes do not
# force re-uploading the whole performer/unit snapshot.
#
# Delta packs include:
#   - new/changed tar-eligible files since the unit's last successful delta
#   - an embedded tombstone list of deleted tar-eligible files (relative paths)
#
# Restore = extract base tarball(s), then apply deltas in chronological order, then apply tombstones.
TAR_DELTA_PREFIX = "tarballs/delta"
TAR_NEW_BASE_PREFIX = "tarballs/base"   # used only when a unit has no existing baseline tarball
DELTA_OVERLAP_SECONDS = 2 * 86400       # overlap window to make missed runs safer
COMPACT_DELTA_THRESHOLD = 30           # optional: if a unit accrues >N deltas, you can compact into a new base
OBSOLETE_TAG_KEY = "obsolete"
OBSOLETE_TAG_VALUE = "true"
# If you ever want fewer delta objects, adjust the *timer frequency* rather than changing code.

# Tar partition kinds
UNIT_KIND_RECURSIVE = "recursive"   # leaf tar subtree
UNIT_KIND_ROOTFILES = "rootfiles"   # files directly in dir only
ROOTFILES_KEY_SUFFIX = ".__root__"

# Legacy footprint detection (old v1 format)
V1_FP_RE = re.compile(r"^\d+_\d+_[0-9a-fA-F]{8}$")

# Detect old dated tarballs (recursive-unit legacy only)
DATED_TAR_RE = re.compile(r"^(?P<safe>.+)_(?P<date>\d{8})\.tar$")

# Files
RUN_LOCK_PATH = os.path.join(LEDGER_DIR, ".backup_run.lock")
TARBALL_FP_JSONL = os.path.join(LEDGER_DIR, "tarball_footprints.jsonl")
TAR_DELTA_DB = os.path.join(LEDGER_DIR, "tar_delta_state.sqlite3")

# Internal video manifests
VIDEO_PREV_MANIFEST = os.path.join(LEDGER_DIR, "video_manifest_previous.nul")

TOMBSTONES_LEDGER = os.path.join(LEDGER_DIR, "tombstones_ledger.txt")  # optional audit trail
VIDEO_TOMBSTONE_PROGRESS = os.path.join(LEDGER_DIR, "video_tombstone_progress.nul")

# Safety limits
MAX_VIDEO_DELETE_MARKERS_PER_RUN = 12000
MAX_TARBALL_DELETE_MARKERS_PER_RUN = 2000
MAX_EMPTY_DIR_TARBALL_TOMBSTONES_PER_RUN = 2000
MAX_DELETED_DIR_TARBALL_TOMBSTONES_PER_RUN = 2000
MAX_REPLACED_DATED_TARBALL_TOMBSTONES_PER_RUN = 2000
MAX_OBSOLETE_TAG_OPS_PER_RUN = 2000  # max S3 object-tagging ops per run (compaction/GC)

# ==========================================
# GLOBALS & INIT
# ==========================================
# Runtime initialization is explicit so imports and --help never touch state.
current_epoch = 0
RUN_ID = ""
TMP_DIR = ""
TARBALL_TMP_DIR = ""
DELTA_TMP_DIR = ""
SOURCE_MOUNT = "/tank/media"
# Check the source dataset itself: /tank can remain mounted while /tank/media
# is unavailable. An ordinary directory at the mountpoint is not a valid source.
VIDEO_SCAN = {}
REMOTE_TAR_KEYS = set()

# -----------------------
# LOGGING
# -----------------------
# Verbosity: 0=quiet, 1=normal, 2=verbose, 3=trace
LOG_LEVEL = 1

# Python logging levels:
#   - INFO/DEBUG/TRACE go to stdout (picked up by journald)
#   - WARNING+ go to stderr (picked up by journald with higher priority)
TRACE_LEVEL_NUM = 5
logging.addLevelName(TRACE_LEVEL_NUM, "TRACE")

def _trace(self, message, *args, **kwargs):
    if self.isEnabledFor(TRACE_LEVEL_NUM):
        self._log(TRACE_LEVEL_NUM, message, args, **kwargs)

logging.Logger.trace = _trace  # type: ignore[attr-defined]

_LOGGER = logging.getLogger("s3_log_backup")

class _RunIdFilter(logging.Filter):
    def __init__(self, run_id: str):
        super().__init__()
        self.run_id = run_id

    def filter(self, record: logging.LogRecord) -> bool:
        record.run_id = self.run_id  # injected field for formatting
        return True


class _MaxLevelFilter(logging.Filter):
    def __init__(self, max_level: int):
        super().__init__()
        self.max_level = max_level

    def filter(self, record: logging.LogRecord) -> bool:
        return record.levelno < self.max_level


def configure_logging(verbosity: int, run_id: str, log_file: str | None = None) -> None:
    level = (
        logging.WARNING if verbosity <= 0
        else logging.INFO if verbosity == 1
        else logging.DEBUG if verbosity == 2
        else TRACE_LEVEL_NUM
    )

    _LOGGER.handlers.clear()
    _LOGGER.propagate = False
    _LOGGER.setLevel(level)

    interactive = sys.stdout.isatty() and sys.stderr.isatty()

    journal_socket = None
    if JournaldLogHandler is not None:
        journal_socket = getattr(JournaldLogHandler, "SOCKET_PATH", "/run/systemd/journal/socket")

    use_journald = (
        not interactive
        and JournaldLogHandler is not None
        and (
            check_journal_stream()
            or (journal_socket is not None and os.path.exists(str(journal_socket)))
        )
    )

    run_filter = _RunIdFilter(run_id)

    plain_fmt = "%(levelname)s [run=%(run_id)s pid=%(process)d] %(message)s"
    timed_fmt = "%(asctime)s %(levelname)s [run=%(run_id)s pid=%(process)d] %(message)s"
    time_fmt = "%Y-%m-%dT%H:%M:%S%z"

    if use_journald:
        handler = JournaldLogHandler(use_message_id=False)
        handler.setLevel(logging.NOTSET)
        handler.addFilter(run_filter)
        handler.setFormatter(logging.Formatter(fmt=plain_fmt))
        _LOGGER.addHandler(handler)
    else:
        stream_formatter = (
            logging.Formatter(fmt=timed_fmt, datefmt=time_fmt)
            if interactive
            else logging.Formatter(fmt=plain_fmt)
        )

        # stdout handler for < WARNING
        h_out = logging.StreamHandler(stream=sys.stdout)
        h_out.setLevel(level)
        h_out.addFilter(_MaxLevelFilter(logging.WARNING))
        h_out.addFilter(run_filter)
        h_out.setFormatter(stream_formatter)

        # stderr handler for WARNING+
        h_err = logging.StreamHandler(stream=sys.stderr)
        h_err.setLevel(max(logging.WARNING, level))
        h_err.addFilter(run_filter)
        h_err.setFormatter(stream_formatter)

        _LOGGER.addHandler(h_out)
        _LOGGER.addHandler(h_err)

    if log_file:
        file_formatter = logging.Formatter(fmt=timed_fmt, datefmt=time_fmt)
        fh = logging.FileHandler(log_file)
        fh.setLevel(level)
        fh.addFilter(run_filter)
        fh.setFormatter(file_formatter)
        _LOGGER.addHandler(fh)


def log(msg: str, level: int = 1) -> None:
    """Back-compat shim for the existing codebase.

    - Keeps the existing `log(msg, level)` call sites.
    - Treats messages prefixed with WARNING:/ERROR:/FATAL: as higher priority regardless of verbosity.
    """
    if isinstance(msg, str):
        if msg.startswith("FATAL:") or msg.startswith("ERROR:"):
            _LOGGER.error(msg)
            return
        if msg.startswith("WARNING:"):
            _LOGGER.warning(msg)
            return

    # Respect the legacy verbosity gate for non-warning/info chatter.
    if LOG_LEVEL < level:
        return

    if level >= 3:
        _LOGGER.log(TRACE_LEVEL_NUM, msg)
    elif level == 2:
        _LOGGER.debug(msg)
    else:
        _LOGGER.info(msg)


def _cmd_str(cmd) -> str:
    try:
        return shlex.join(cmd)
    except Exception:
        return " ".join(str(x) for x in cmd)

def run_cmd(
    cmd: list[str],
    *,
    description: str | None = None,
    long_running: bool = False,
    capture: bool = False,
    check: bool = True,
    env: dict | None = None,
    cwd: str | None = None,
    stdin=None,
) -> subprocess.CompletedProcess:
    """
    Run a command with sane logging behavior under both terminal and systemd/journald.

    - In normal mode (LOG_LEVEL=1), we keep logs minimal.
    - In verbose/trace (LOG_LEVEL>=2), we log the command line before executing.
    - For long-running/noisy commands, do NOT capture output (let it stream to journald).
    - For short commands where output is important, set capture=True.

    If check=True, failures are logged and re-raised.
    """
    cmd_str = _cmd_str(cmd)

    if LOG_LEVEL >= 2:
        if description:
            _LOGGER.debug("%s: %s", description, cmd_str)
        else:
            _LOGGER.debug("Running: %s", cmd_str)

    # Decide capture behavior
    use_capture = bool(capture) and not bool(long_running)

    try:
        if use_capture:
            return subprocess.run(
                cmd,
                stdin=stdin,
                capture_output=True,
                text=True,
                check=check,
                env=env,
                cwd=cwd,
            )
        else:
            # stream stdout/stderr directly (best for journald and progress output)
            return subprocess.run(
                cmd,
                stdin=stdin,
                check=check,
                env=env,
                cwd=cwd,
            )
    except subprocess.CalledProcessError as e:
        # In trace mode, include traceback; otherwise just a clear error line.
        msg = f"Command failed (rc={e.returncode}): {cmd_str}"
        if use_capture:
            # Avoid dumping huge output; keep it tight.
            if e.stderr:
                tail = "\n".join(e.stderr.strip().splitlines()[-25:])
                msg += f"\n--- stderr (tail) ---\n{tail}"
            if e.stdout and LOG_LEVEL >= 3:
                tail = "\n".join(e.stdout.strip().splitlines()[-25:])
                msg += f"\n--- stdout (tail) ---\n{tail}"

        if LOG_LEVEL >= 3:
            _LOGGER.exception(msg)
        else:
            _LOGGER.error(msg)
        raise


def tag_legacy_dated_tarballs_obsolete(
    conn,
    unit,
    dated_by_safe: dict,
    delete_budget: DeleteBudget,
    tagger: S3ObjectTagger | None,
    dry_run: bool,
    tagged_epoch: int,
    keep_key: str | None = None,
):
    if unit["kind"] != UNIT_KIND_RECURSIVE:
        return

    safe = safe_name_for_rel_dir(unit["rel_dir"])
    candidates = dated_by_safe.get(safe, [])

    for _date_int, key in candidates:
        # Skip the one explicit legacy key we still want to preserve, if any.
        if keep_key and key == keep_key:
            continue

        # Don't retry keys we've already recorded as obsolete-tagged.
        if db_gc_tag_is_marked(conn, key):
            continue

        if not delete_budget.allow_tag_op(key):
            break

        if tagger and tagger.ensure_tag(
            key,
            OBSOLETE_TAG_KEY,
                OBSOLETE_TAG_VALUE,
                dry_run=dry_run,
        ):
            with conn:
                db_gc_mark_tagged(conn, key, tagged_epoch)


def tag_superseded_stable_tarballs_obsolete(
    conn,
    local_unit_ids: set,
    stable_keys: set,
    delete_budget: DeleteBudget,
    tagger: S3ObjectTagger | None,
    dry_run: bool,
    tagged_epoch: int,
):
    """
    Tag old top-level stable tarballs obsolete once a newer full base exists.

    Important: a stable tarball with newer deltas is still the restore baseline until
    compaction creates a tarballs/base/... replacement, so deltas alone are not enough
    to make the stable tarball obsolete.
    """
    if not local_unit_ids or not stable_keys:
        return

    base_key_map = db_base_key_map_for_units(conn, local_unit_ids)
    candidates = []
    already_marked = 0
    still_active = 0
    no_new_base = 0

    for unit_id in sorted(local_unit_ids):
        parsed = parse_unit_id(unit_id)
        stable_key = stable_tarball_key_for_unit(parsed["rel_dir"], parsed["kind"])
        if stable_key not in stable_keys:
            continue

        base_key = base_key_map.get(unit_id)
        if not base_key or base_key == stable_key:
            still_active += 1
            continue

        if not base_key.startswith(TAR_NEW_BASE_PREFIX + "/"):
            no_new_base += 1
            continue

        if db_gc_tag_is_marked(conn, stable_key):
            already_marked += 1
            continue

        candidates.append((unit_id, stable_key, base_key))

    if not candidates:
        log(
            "Stable tarball obsolete reconciliation: no untagged superseded stable tarballs "
            f"(active={still_active}, no_new_base={no_new_base}, already_marked={already_marked})",
            2,
        )
        return

    log(
        f"Stable tarball obsolete reconciliation: tagging {len(candidates)} superseded stable tarballs "
        f"({OBSOLETE_TAG_KEY}={OBSOLETE_TAG_VALUE})",
        1,
    )

    tagged = 0
    failed = 0
    skipped_budget = 0

    for unit_id, stable_key, base_key in candidates:
        if not delete_budget.allow_tag_op(stable_key):
            skipped_budget = len(candidates) - tagged - failed
            break

        if dry_run:
            log(f"[DRY RUN] Would tag superseded stable tarball obsolete: {stable_key} (new_base={base_key})", 1)
            tagged += 1
            continue

        if tagger and tagger.ensure_tag(stable_key, OBSOLETE_TAG_KEY, OBSOLETE_TAG_VALUE, dry_run=False):
            with conn:
                db_gc_mark_tagged(conn, stable_key, tagged_epoch)
            tagged += 1
        else:
            log(f"WARNING: failed to tag superseded stable tarball obsolete: {stable_key} (unit={unit_id})", 1)
            failed += 1

    log(
        "Stable tarball obsolete reconciliation results: "
        f"tagged={tagged} failed={failed} skipped_budget={skipped_budget}",
        1,
    )


DELTA_KEY_RE = re.compile(
    r"^tarballs/delta/(?P<safe>.+)/(?P<kind>[RF])/(?P<day>\d{8})/"
    r"(?P<stamp>\d{8}_\d{6})_.+\.tar$"
)


def parse_delta_tarball_key(key: str):
    m = DELTA_KEY_RE.match(key)
    if not m:
        return None

    stamp = m.group("stamp")
    try:
        created_epoch = int(datetime.strptime(stamp, "%Y%m%d_%H%M%S").timestamp())
    except ValueError:
        return None

    return {
        "safe": m.group("safe"),
        "kind_code": m.group("kind"),
        "created_epoch": created_epoch,
    }


def db_get_delta_key_tag_state(conn, key: str):
    row = conn.execute(
        "SELECT unit_id, obsolete_tagged_epoch FROM delta_keys WHERE key=? LIMIT 1",
        (key,),
    ).fetchone()
    if not row:
        return None
    return {"unit_id": row[0], "obsolete_tagged": row[1] is not None}


def db_base_state_map_for_units(conn, unit_ids: set):
    if not unit_ids:
        return {}
    placeholders = ",".join("?" for _ in unit_ids)
    q = f"SELECT unit_id, base_key, base_mod_epoch FROM unit_state WHERE unit_id IN ({placeholders})"
    rows = conn.execute(q, tuple(sorted(unit_ids))).fetchall()
    return {
        uid: {"base_key": bk, "base_mod_epoch": int(epoch or 0)}
        for uid, bk, epoch in rows
        if bk
    }


def list_remote_delta_tarball_keys(dry_run: bool):
    if dry_run:
        log("[DRY RUN] Would list remote delta tarballs via rclone lsf.", 1)
        return []

    log("Listing remote delta tarballs for obsolete reconciliation...", 1)
    cmd = ["rclone", "lsf", "-R", "--files-only", f"{REMOTE_PATH}{TAR_DELTA_PREFIX}"]
    res = run_cmd(cmd, capture=True, check=False)
    if res.returncode != 0:
        log("WARNING: remote delta lsf failed; skipping obsolete delta reconciliation.", 1)
        if res.stderr:
            log(res.stderr.strip(), 2)
        return []

    keys = []
    for raw in res.stdout.splitlines():
        rel = raw.strip()
        if not rel or not rel.endswith(".tar"):
            continue
        keys.append(f"{TAR_DELTA_PREFIX}/{rel.lstrip('/')}")

    log(f"Remote delta tarballs listed: {len(keys)}", 1)
    return keys


def tag_superseded_remote_delta_tarballs_obsolete(
    conn,
    local_unit_ids: set,
    dry_run: bool,
    delete_budget: DeleteBudget,
    tagger: S3ObjectTagger | None,
    tagged_epoch: int,
):
    """
    Tag remote delta packs obsolete once a newer full base exists, including
    old/orphan delta objects that are missing from delta_keys.
    """
    if not local_unit_ids:
        return

    safe_kind_to_unit_id = {}
    for unit_id in sorted(local_unit_ids):
        parsed = parse_unit_id(unit_id)
        kind_code = "R" if parsed["kind"] == UNIT_KIND_RECURSIVE else "F"
        safe_kind_to_unit_id[(safe_name_for_rel_dir(parsed["rel_dir"]), kind_code)] = unit_id

    base_state = db_base_state_map_for_units(conn, local_unit_ids)
    candidates = []
    skipped_unknown_unit = 0
    skipped_no_new_base = 0
    skipped_newer_than_base = 0
    skipped_already_marked = 0
    skipped_unparsed = 0

    for key in list_remote_delta_tarball_keys(dry_run=dry_run):
        parsed = parse_delta_tarball_key(key)
        if not parsed:
            skipped_unparsed += 1
            continue

        unit_id = safe_kind_to_unit_id.get((parsed["safe"], parsed["kind_code"]))
        if not unit_id:
            skipped_unknown_unit += 1
            continue

        state = base_state.get(unit_id)
        base_key = state["base_key"] if state else None
        base_epoch = state["base_mod_epoch"] if state else 0
        if not base_key or not base_key.startswith(TAR_NEW_BASE_PREFIX + "/"):
            skipped_no_new_base += 1
            continue

        if parsed["created_epoch"] >= base_epoch:
            skipped_newer_than_base += 1
            continue

        delta_state = db_get_delta_key_tag_state(conn, key)
        if delta_state and delta_state["obsolete_tagged"]:
            skipped_already_marked += 1
            continue
        if (not delta_state) and db_gc_tag_is_marked(conn, key):
            skipped_already_marked += 1
            continue

        candidates.append((unit_id, key, delta_state is not None))

    if not candidates:
        log(
            "Delta tarball obsolete reconciliation: no untagged superseded remote deltas "
            f"(already_marked={skipped_already_marked}, newer_than_base={skipped_newer_than_base}, "
            f"no_new_base={skipped_no_new_base}, unknown_unit={skipped_unknown_unit}, unparsed={skipped_unparsed})",
            2,
        )
        return

    log(
        f"Delta tarball obsolete reconciliation: tagging {len(candidates)} superseded remote delta packs "
        f"({OBSOLETE_TAG_KEY}={OBSOLETE_TAG_VALUE})",
        1,
    )

    tagged = 0
    failed = 0
    skipped_budget = 0

    for unit_id, key, in_delta_db in candidates:
        if not delete_budget.allow_tag_op(key):
            skipped_budget = len(candidates) - tagged - failed
            break

        if dry_run:
            log(f"[DRY RUN] Would tag superseded delta pack obsolete: {key}", 1)
            tagged += 1
            continue

        if tagger and tagger.ensure_tag(key, OBSOLETE_TAG_KEY, OBSOLETE_TAG_VALUE, dry_run=False):
            with conn:
                if in_delta_db:
                    db_mark_delta_keys_obsolete_tagged(conn, unit_id, [key], tagged_epoch)
                else:
                    db_gc_mark_tagged(conn, key, tagged_epoch)
            tagged += 1
        else:
            log(f"WARNING: failed to tag superseded delta pack obsolete: {key} (unit={unit_id})", 1)
            failed += 1

    log(
        "Delta tarball obsolete reconciliation results: "
        f"tagged={tagged} failed={failed} skipped_budget={skipped_budget}",
        1,
    )


def fp_tag(fp):
    if not fp:
        return "none"
    if isinstance(fp, str) and fp.startswith("v2:"):
        return "v2"
    if isinstance(fp, str) and V1_FP_RE.match(fp):
        return "v1"
    return "unknown"


def parse_rclone_modtime_to_epoch(s: str):
    """
    rclone lsjson ModTime is typically RFC3339 (e.g. 2026-02-23T05:54:51.123456789Z).
    We only need coarse epoch seconds.
    """
    if not s:
        return None
    s = s.strip()
    try:
        # Normalize Zulu
        if s.endswith("Z"):
            s = s[:-1] + "+00:00"
        # Drop sub-second precision if fromisoformat chokes on 9-digit nanos
        if "." in s:
            # keep up to microseconds
            head, tail = s.split(".", 1)
            frac = tail
            tz = ""
            if "+" in frac:
                frac, tz = frac.split("+", 1)
                tz = "+" + tz
            elif "-" in frac[1:]:
                # beware date separator; timezone '-' appears after seconds, so safe to split on last '-'
                # but easiest: find last '-' after 'T'
                idx = frac.rfind("-")
                if idx != -1:
                    frac, tz = frac[:idx], frac[idx:]
            frac = (frac[:6]).ljust(6, "0")
            s = head + "." + frac + tz
        return int(datetime.fromisoformat(s).timestamp())
    except Exception:
        return None

# ==========================================
# UTILITIES
# ==========================================
def tombstone_remote_accidental_nonvideo_objects(dry_run: bool, delete_budget: int = 2000):
    """
    Tombstone currently visible remote objects in the Glacier bucket that are:
      - NOT under tarballs/
      - NOT recognized video files

    This cleans up old accidental loose image/NFO uploads.
    Only obsolete tags are written; S3 Lifecycle performs expiration.
    """
    log("Auditing remote Glacier bucket for accidental non-video visible objects...", 1)

    if dry_run:
        log("[DRY RUN] Would list remote visible objects and tombstone accidental non-video keys.", 1)

    # current visible objects only (not versions)
    cmd = ["rclone", "lsf", "-R", "--files-only", REMOTE_PATH]
    if dry_run:
        # still useful to run in dry-run so you can see what it *would* do
        pass

    # rclone lsf output can be very large; stream it line-by-line to avoid buffering the full listing in memory.
    if LOG_LEVEL >= 2:
        _LOGGER.debug("Running (stream): %s", _cmd_str(cmd))

    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, text=True)
    if proc.stdout is None:
        log("WARNING: could not read rclone listing stdout; skipping accidental non-video cleanup.", 1)
        return {"candidates": 0, "deleted": 0, "already_absent": 0, "failed": 0}

    candidates = []
    total_visible = 0

    for raw in proc.stdout:
        key = raw.removesuffix("\n")
        if not key:
            continue
        total_visible += 1

        # Keep tarballs
        if key.startswith("tarballs/"):
            continue

        # Keep videos
        ext = os.path.splitext(key)[1].lower()
        if ext in VIDEO_EXTENSIONS:
            continue

        # Everything else in Glacier bucket is unexpected/junk for your design
        candidates.append(key)


    # Ensure the listing completed successfully before taking any destructive action.
    proc.stdout.close()
    rc = proc.wait()
    if rc != 0:
        log(f"WARNING: remote audit listing failed (rc={rc}); skipping accidental non-video cleanup.", 1)
        return {"candidates": 0, "deleted": 0, "already_absent": 0, "failed": 0}

    log(f"Remote visible objects scanned: {total_visible}", 1)
    log(f"Accidental non-video visible object candidates: {len(candidates)}", 1)

    if candidates and LOG_LEVEL >= 1:
        for k in candidates[:50]:
            log(f"  - {k}", 1)
        if len(candidates) > 50:
            log("  ... (truncated)", 1)

    if len(candidates) > delete_budget:
        log(
            f"WARNING: accidental non-video cleanup candidates ({len(candidates)}) exceed budget ({delete_budget}). "
            f"NOT tombstoning. Investigate and rerun with a higher budget if expected.",
            1
        )
        return {"candidates": len(candidates), "deleted": 0, "already_absent": 0, "failed": 0}

    deleted = 0
    already_absent = 0
    failed = 0

    tagger = S3ObjectTagger(REMOTE_S3_BUCKET, REMOTE_S3_PREFIX)
    for key in candidates:
        if tagger.ensure_tag(key, OBSOLETE_TAG_KEY, OBSOLETE_TAG_VALUE, dry_run=dry_run):
            deleted += 1
        else:
            failed += 1

    log(
        f"Accidental non-video cleanup results: candidates={len(candidates)} "
        f"tagged_or_absent={deleted} failed={failed}",
        1
    )

    return {
        "candidates": len(candidates),
        "deleted": deleted,
        "already_absent": already_absent,
        "failed": failed,
    }

def _real(p: str) -> str:
    return os.path.realpath(p)

def cleanup_stale_tmp():
    """Remove abandoned run workspaces while holding the exclusive run lock.

    Runtime directories are also created under that lock, so no other running
    backup can own one. Uploads that need retrying live in LEDGER_DIR instead.
    """
    current = _real(TMP_DIR) if TMP_DIR else None
    with os.scandir(TMP_BASE_DIR) as entries:
        for entry in entries:
            if not re.fullmatch(r"run_\d{8}_\d{6}_\d+_[a-z0-9_]{8}", entry.name):
                continue
            if not entry.is_dir(follow_symlinks=False) or _real(entry.path) == current:
                continue
            log(f"Removing abandoned backup workspace: {entry.path}", 1)
            shutil.rmtree(entry.path)

def cleanup_tmp():
    """
    Only delete the per-run TMP_DIR, and only if it is safely under TMP_BASE_DIR.
    """
    try:
        if not os.path.exists(TMP_DIR):
            return

        tmp_real = _real(TMP_DIR)
        base_real = _real(TMP_BASE_DIR)

        if tmp_real == base_real:
            log(f"Refusing to delete TMP_BASE_DIR: {TMP_BASE_DIR}", 1)
            return
        if not tmp_real.startswith(base_real + os.sep):
            log(f"Refusing to delete TMP_DIR not under TMP_BASE_DIR: {TMP_DIR}", 1)
            return

        log(f"Cleaning up temporary directory: {TMP_DIR}", 1)
        shutil.rmtree(tmp_real)
    except Exception as e:
        log(f"WARNING: cleanup_tmp() failed: {e}", 1)

def handle_shutdown(signal_num, frame):
    log(f"\nSignal {signal_num} received, cleaning up...", 1)
    cleanup_tmp()
    raise SystemExit(1)

@contextmanager
def exclusive_run_lock(lock_path: str):
    """
    Prevent overlapping runs (systemd timer + manual run).
    """
    fd = os.open(lock_path, os.O_CREAT | os.O_RDWR, 0o600)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield
    finally:
        try:
            fcntl.flock(fd, fcntl.LOCK_UN)
        finally:
            os.close(fd)

def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def atomic_write_bytes(path: str, data: bytes, mode: int = 0o600):
    """
    Atomic write for bytes.
    """
    dir_name = os.path.dirname(path)
    os.makedirs(dir_name, exist_ok=True)
    fd, tmp_path = tempfile.mkstemp(prefix=os.path.basename(path) + ".", dir=dir_name)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        os.chmod(tmp_path, mode)
        os.replace(tmp_path, path)
        sync_directory(dir_name)
    finally:
        if os.path.exists(tmp_path):
            try:
                os.unlink(tmp_path)
            except OSError:
                pass

def atomic_write_lines(path: str, lines, mode: int = 0o600):
    atomic_write_bytes(path, "".join(lines).encode("utf-8"), mode=mode)

def atomic_copy(src: str, dst: str, mode: int = 0o600):
    dir_name = os.path.dirname(dst)
    os.makedirs(dir_name, exist_ok=True)
    fd, tmp_path = tempfile.mkstemp(prefix=os.path.basename(dst) + ".", dir=dir_name)
    try:
        with os.fdopen(fd, "wb") as out, open(src, "rb") as inp:
            shutil.copyfileobj(inp, out, length=1024 * 1024)
            out.flush()
            os.fsync(out.fileno())
        os.chmod(tmp_path, mode)
        os.replace(tmp_path, dst)
        sync_directory(dir_name)
    finally:
        if os.path.exists(tmp_path):
            try:
                os.unlink(tmp_path)
            except OSError:
                pass

def append_tombstone(s3_rel_key: str):
    """
    Optional audit trail.
    """
    try:
        with open(TOMBSTONES_LEDGER, "a", encoding="utf-8") as f:
            f.write(f"{s3_rel_key},{int(time.time())}\n")
    except Exception:
        pass

_S3_DELETE_CLIENT = None
_S3_DELETE_CLIENT_INIT_FAILED = False


def _get_s3_delete_client():
    global _S3_DELETE_CLIENT, _S3_DELETE_CLIENT_INIT_FAILED

    if _S3_DELETE_CLIENT is not None:
        return _S3_DELETE_CLIENT
    if _S3_DELETE_CLIENT_INIT_FAILED:
        return None

    try:
        _S3_DELETE_CLIENT = boto3.client("s3")
        return _S3_DELETE_CLIENT
    except Exception as e:
        _S3_DELETE_CLIENT_INIT_FAILED = True
        log(f"WARNING: could not initialize boto3 S3 client for delete markers: {e}", 1)
        return None


def _full_s3_key(rel_key: str) -> str:
    rel_key = rel_key.lstrip("/")
    return f"{REMOTE_S3_PREFIX}{rel_key}"


def load_video_tombstone_progress() -> set[str]:
    """
    Load successfully tombstoned video keys for the current VIDEO_PREV_MANIFEST baseline.
    Stored as an internal NUL-delimited manifest for filename safety.
    """
    done: set[str] = set()
    if not os.path.exists(VIDEO_TOMBSTONE_PROGRESS):
        return done

    try:
        for rec in iter_valid_video_manifest_records_nul(VIDEO_TOMBSTONE_PROGRESS):
            done.add(rec.decode("utf-8", "surrogateescape"))
    except Exception as e:
        log(f"WARNING: could not read {VIDEO_TOMBSTONE_PROGRESS}: {e}", 1)

    return done


def write_video_tombstone_progress(keys) -> None:
    """
    Rewrite the progress file atomically as sorted NUL-delimited keys.
    Used to prune stale entries and keep the file compact.
    """
    uniq = sorted(set(keys), key=lambda s: s.encode("utf-8", "surrogateescape"))
    payload = b"".join(k.encode("utf-8", "surrogateescape") + b"\0" for k in uniq)
    atomic_write_bytes(VIDEO_TOMBSTONE_PROGRESS, payload, mode=0o600)


def append_video_tombstone_progress(key: str) -> None:
    """
    Append one successfully tombstoned video key and fsync so crash recovery
    is much less likely to retry it.
    """
    os.makedirs(os.path.dirname(VIDEO_TOMBSTONE_PROGRESS), exist_ok=True)
    try:
        with open(VIDEO_TOMBSTONE_PROGRESS, "ab") as f:
            f.write(key.encode("utf-8", "surrogateescape"))
            f.write(b"\0")
            f.flush()
            os.fsync(f.fileno())
    except Exception as e:
        log(f"WARNING: could not append {key} to {VIDEO_TOMBSTONE_PROGRESS}: {e}", 1)


def clear_video_tombstone_progress() -> None:
    try:
        os.unlink(VIDEO_TOMBSTONE_PROGRESS)
    except FileNotFoundError:
        pass
    except OSError as e:
        log(f"WARNING: could not remove {VIDEO_TOMBSTONE_PROGRESS}: {e}", 1)


def ensure_s3_delete_marker(s3_rel_key_for_log: str, dry_run: bool) -> str:
    """Reject obsolete callers of the former direct-deletion helper."""
    raise RuntimeError("Direct S3 deletion is disabled; use obsolete=true lifecycle tags")


# ==========================================
# S3 TAGGING (for tag-gated lifecycle GC)
# ==========================================
def parse_remote_bucket_and_prefix(remote_path: str):
    """Parse rclone remote path like 'glacier:bucket/prefix/' -> (bucket, 'prefix/')."""
    if ":" not in remote_path:
        return None, ""
    after = remote_path.split(":", 1)[1]
    after = after.lstrip("/")
    bucket, _, rest = after.partition("/")
    rest = rest.strip()
    if rest.endswith("/"):
        rest = rest[:-1]
    prefix = (rest + "/") if rest else ""
    return bucket, prefix

REMOTE_S3_BUCKET, REMOTE_S3_PREFIX = parse_remote_bucket_and_prefix(REMOTE_PATH)

def assert_s3_access_ready(
    *,
    require_glacier_rclone: bool,
    require_standard_rclone: bool,
    require_boto3_delete: bool,
    require_boto3_tagging: bool,
    aws_region: str | None = None,
) -> None:
    """
    Fail fast before doing any work if required S3 access is not available.

    Checks:
      - rclone access to Glacier remote
      - rclone access to Standard metadata remote
      - boto3 auth + bucket access for delete-marker operations
      - boto3 auth + bucket access for tagging operations
    """
    # --- rclone remotes ---
    if require_glacier_rclone:
        res = run_cmd(
            ["rclone", "lsf", REMOTE_PATH, "--max-depth", "1"],
            check=False,
            capture=True,
            description="Preflight Glacier remote access",
        )
        if res.returncode != 0:
            raise RuntimeError(
                f"Preflight failed: rclone cannot access Glacier remote {REMOTE_PATH} "
                f"(rc={res.returncode})"
            )

    if require_standard_rclone:
        res = run_cmd(
            ["rclone", "lsf", STANDARD_REMOTE_PATH, "--max-depth", "1"],
            check=False,
            capture=True,
            description="Preflight Standard remote access",
        )
        if res.returncode != 0:
            raise RuntimeError(
                f"Preflight failed: rclone cannot access Standard remote {STANDARD_REMOTE_PATH} "
                f"(rc={res.returncode})"
            )

    # Nothing boto3-related requested
    if not (require_boto3_delete or require_boto3_tagging):
        return

    if not REMOTE_S3_BUCKET:
        raise RuntimeError("Preflight failed: could not parse S3 bucket from REMOTE_PATH")

    try:
        s3 = boto3.client("s3", region_name=aws_region) if aws_region else boto3.client("s3")
    except Exception as e:
        raise RuntimeError(f"Preflight failed: could not initialize boto3 S3 client: {e}") from e

    # Credential sanity
    try:
        sts = boto3.client("sts", region_name=aws_region) if aws_region else boto3.client("sts")
        sts.get_caller_identity()
    except Exception as e:
        raise RuntimeError(f"Preflight failed: boto3 credentials are not usable: {e}") from e

    # Bucket access sanity
    try:
        s3.list_objects_v2(Bucket=REMOTE_S3_BUCKET, Prefix=REMOTE_S3_PREFIX, MaxKeys=1)
    except Exception as e:
        raise RuntimeError(
            f"Preflight failed: boto3 cannot list/access s3://{REMOTE_S3_BUCKET}/{REMOTE_S3_PREFIX}: {e}"
        ) from e

    # Optional deeper checks against a harmless probe key.
    probe_key = f"{REMOTE_S3_PREFIX}__chatgpt_preflight_probe__"

    if require_boto3_delete:
        try:
            s3.head_object(Bucket=REMOTE_S3_BUCKET, Key=probe_key)
        except ClientError as e:
            code = str(e.response.get("Error", {}).get("Code", ""))
            http = e.response.get("ResponseMetadata", {}).get("HTTPStatusCode")
            if code not in {"404", "NoSuchKey", "NotFound"} and http != 404:
                raise RuntimeError(
                    f"Preflight failed: boto3 head_object permission check failed for delete path ({code or http})"
                ) from e
        except Exception as e:
            raise RuntimeError(
                f"Preflight failed: boto3 delete-path probe failed: {e}"
            ) from e

    if require_boto3_tagging:
        try:
            s3.get_object_tagging(Bucket=REMOTE_S3_BUCKET, Key=probe_key)
        except ClientError as e:
            code = str(e.response.get("Error", {}).get("Code", ""))
            http = e.response.get("ResponseMetadata", {}).get("HTTPStatusCode")
            # 404/NoSuchKey is fine: it proves auth/client path works.
            if code not in {"404", "NoSuchKey", "NotFound"} and http != 404:
                raise RuntimeError(
                    f"Preflight failed: boto3 tagging probe failed ({code or http})"
                ) from e
        except Exception as e:
            raise RuntimeError(
                f"Preflight failed: boto3 tagging-path probe failed: {e}"
            ) from e

class S3ObjectTagger:
    def __init__(self, bucket: str, prefix: str = "", region: str | None = None):
        if not bucket:
            raise RuntimeError("S3ObjectTagger requires a bucket name")

        self.bucket = bucket
        self.prefix = prefix or ""
        self.s3 = boto3.client("s3", region_name=region) if region else boto3.client("s3")

    def _full_key(self, rel_key: str) -> str:
        rel_key = rel_key.lstrip("/")
        return f"{self.prefix}{rel_key}"

    def ensure_tag(self, rel_key: str, tag_key: str, tag_value: str, dry_run: bool) -> bool:
        """Add/ensure a tag on the CURRENT version of an object key (does not change object data)."""
        full_key = self._full_key(rel_key)

        if dry_run:
            log(f"[DRY RUN] Would tag s3://{self.bucket}/{full_key} {tag_key}={tag_value}", 1)
            return True

        marking_obsolete = tag_key == OBSOLETE_TAG_KEY and tag_value == OBSOLETE_TAG_VALUE
        if marking_obsolete:
            assert_source_ready()

        try:
            existing = self.s3.get_object_tagging(Bucket=self.bucket, Key=full_key).get("TagSet", [])
        except (NoCredentialsError, PartialCredentialsError):
            log("WARNING: AWS credentials not available for S3 tagging; skipping tag operations.", 1)
            self.enabled = False
            return False
        except ClientError as e:
            code = e.response.get("Error", {}).get("Code", "Unknown")
            if code in {"NoSuchKey", "NotFound", "404"}:
                return True
            log(f"WARNING: get_object_tagging failed for {full_key} ({code}); skipping.", 1)
            return False

        # Merge tag set (preserve existing tags)
        tagset = {t.get("Key"): t.get("Value") for t in existing if t.get("Key")}
        if tagset.get(tag_key) == tag_value:
            return True

        tagset[tag_key] = tag_value
        merged = [{"Key": k, "Value": v} for k, v in tagset.items()]

        # A source outage during the metadata request must not become evidence
        # of deletion. This guard also covers legacy/orphan cleanup callers.
        if marking_obsolete:
            assert_source_ready()
        try:
            self.s3.put_object_tagging(Bucket=self.bucket, Key=full_key, Tagging={"TagSet": merged})
            return True
        except ClientError as e:
            code = e.response.get("Error", {}).get("Code", "Unknown")
            log(f"WARNING: put_object_tagging failed for {full_key} ({code})", 1)
            return False

    def clear_obsolete(self, rel_key: str) -> bool:
        """Protect a returning current object before size-only reconciliation.

        Never fetch object contents. A missing object will be uploaded normally.
        Preserve unrelated tags, including when removing the last obsolete tag.
        """
        full_key = self._full_key(rel_key)
        try:
            tags = self.s3.get_object_tagging(Bucket=self.bucket, Key=full_key).get("TagSet", [])
            if not any(t["Key"] == OBSOLETE_TAG_KEY for t in tags):
                return True
            retained = [t for t in tags if t["Key"] != OBSOLETE_TAG_KEY]
            self.s3.put_object_tagging(Bucket=self.bucket, Key=full_key, Tagging={"TagSet": retained})
            return True
        except ClientError as error:
            code = error.response.get("Error", {}).get("Code", "Unknown")
            if code in {"NoSuchKey", "NotFound", "404"}:
                return True
            log(f"WARNING: could not clear obsolete tag for {full_key} ({code})", 1)
            return False


# ==========================================
# DELETE BUDGET / CIRCUIT BREAKERS
# ==========================================
class DeleteBudget:
    def __init__(self):
        self.video_delete_markers = 0

        self.total_tarball_tombstones = 0
        self.empty_dir_tarball_tombstones = 0
        self.deleted_dir_tarball_tombstones = 0
        self.replaced_dated_tarball_tombstones = 0
        self.obsolete_tag_ops = 0

    def allow_video_tombstones(self, count: int) -> bool:
        return count <= MAX_VIDEO_DELETE_MARKERS_PER_RUN

    def allow_tarball_tombstone(self, reason: str, key: str) -> bool:
        if self.total_tarball_tombstones >= MAX_TARBALL_DELETE_MARKERS_PER_RUN:
            log(f"WARNING: tarball tombstone total safety limit reached; skipping {key}", 1)
            return False

        if reason == "empty_dir":
            if self.empty_dir_tarball_tombstones >= MAX_EMPTY_DIR_TARBALL_TOMBSTONES_PER_RUN:
                log(f"WARNING: empty-dir tarball tombstone limit reached; skipping {key}", 1)
                return False
            self.empty_dir_tarball_tombstones += 1

        elif reason == "deleted_dir":
            if self.deleted_dir_tarball_tombstones >= MAX_DELETED_DIR_TARBALL_TOMBSTONES_PER_RUN:
                log(f"WARNING: deleted-dir tarball tombstone limit reached; skipping {key}", 1)
                return False
            self.deleted_dir_tarball_tombstones += 1

        elif reason == "replaced_dated":
            if self.replaced_dated_tarball_tombstones >= MAX_REPLACED_DATED_TARBALL_TOMBSTONES_PER_RUN:
                log(f"WARNING: replaced-dated tarball tombstone limit reached; skipping {key}", 1)
                return False
            self.replaced_dated_tarball_tombstones += 1

        self.total_tarball_tombstones += 1
        return True

    def allow_tag_op(self, key: str) -> bool:
        if self.obsolete_tag_ops >= MAX_OBSOLETE_TAG_OPS_PER_RUN:
            log(f"WARNING: obsolete tag op limit reached; skipping tag for {key}", 1)
            return False
        self.obsolete_tag_ops += 1
        return True

    def summary(self):
        return (
            "Delete budget summary: "
            f"tar_total={self.total_tarball_tombstones}, "
            f"tar_empty={self.empty_dir_tarball_tombstones}, "
            f"tar_deleted_dir={self.deleted_dir_tarball_tombstones}, "
            f"tar_replaced_dated={self.replaced_dated_tarball_tombstones}, "
            f"tag_ops={self.obsolete_tag_ops}"
        )

# ==========================================
# TARBALL UNIT HELPERS (partitioned, non-overlapping)
# ==========================================
def safe_name_for_rel_dir(rel_dir: str) -> str:
    """
    Must match historical safe-name behavior for recursive units to preserve old key matching.
    """
    return rel_dir.replace("/", "_").replace("\\", "_").replace(",", "_")

def make_unit_id(rel_dir: str, kind: str) -> str:
    """
    Unit IDs:
      R:<rel_dir>  recursive tar unit
      F:<rel_dir>  files-only (__root__) tar unit
    """
    prefix = "R:" if kind == UNIT_KIND_RECURSIVE else "F:"
    return prefix + rel_dir

def parse_unit_id(unit_id: str):
    if unit_id.startswith("R:"):
        return {"rel_dir": unit_id[2:], "kind": UNIT_KIND_RECURSIVE}
    if unit_id.startswith("F:"):
        return {"rel_dir": unit_id[2:], "kind": UNIT_KIND_ROOTFILES}
    # Backward-compatible fallback: bare rel_dir from old JSONL schema => recursive
    return {"rel_dir": unit_id, "kind": UNIT_KIND_RECURSIVE}

def stable_tarball_key_for_unit(rel_dir: str, kind: str) -> str:
    safe = safe_name_for_rel_dir(rel_dir)
    if kind == UNIT_KIND_RECURSIVE:
        return f"tarballs/{safe}.tar"
    if kind == UNIT_KIND_ROOTFILES:
        return f"tarballs/{safe}{ROOTFILES_KEY_SUFFIX}.tar"
    raise ValueError(f"Unknown unit kind: {kind}")


def base_tarball_key_for_unit(rel_dir: str, kind: str, created_epoch: int) -> str:
    """
    Append-only baseline tarball key.

    We intentionally do NOT overwrite a stable base key, because:
      - you want old baselines to be eligible for deletion at (creation + 180d) once a newer baseline exists
      - tag-gated lifecycle expiration uses object age since creation
    """
    safe = safe_name_for_rel_dir(rel_dir)
    k = "R" if kind == UNIT_KIND_RECURSIVE else "F"
    dt = datetime.fromtimestamp(int(created_epoch))
    day = dt.strftime("%Y%m%d")
    stamp = dt.strftime("%Y%m%d_%H%M%S")
    return f"{TAR_NEW_BASE_PREFIX}/{safe}/{k}/{day}/{stamp}_{RUN_ID}.tar"


def delta_tarball_key_for_unit(rel_dir: str, kind: str, created_epoch: int) -> str:
    """
    Append-only delta pack key. Organized for human browsing; does not rely on S3 'directories'.
    """
    safe = safe_name_for_rel_dir(rel_dir)
    k = "R" if kind == UNIT_KIND_RECURSIVE else "F"
    dt = datetime.fromtimestamp(created_epoch)
    day = dt.strftime("%Y%m%d")
    stamp = dt.strftime("%Y%m%d_%H%M%S")
    # Example: tarballs/delta/<safe>/<R|F>/20260226/<stamp>_<runid>.tar
    return f"{TAR_DELTA_PREFIX}/{safe}/{k}/{day}/{stamp}_{RUN_ID}.tar"

def local_tar_tmp_path_for_unit(rel_dir: str, kind: str) -> str:
    return os.path.join(TARBALL_TMP_DIR, os.path.basename(stable_tarball_key_for_unit(rel_dir, kind)))

def detect_safe_name_collisions(local_rel_dirs):
    """
    Hard-fail if two different rel_dirs map to same historical safe name.
    """
    by_safe = {}
    for rel_dir in local_rel_dirs:
        safe = safe_name_for_rel_dir(rel_dir)
        by_safe.setdefault(safe, []).append(rel_dir)

    collisions = {k: v for k, v in by_safe.items() if len(v) > 1}
    if not collisions:
        log("Safe-name collision check: no collisions detected.", 1)
        return

    log("FATAL: safe-name collisions detected (different dirs map to same tarball basename).", 1)
    for safe, dirs in sorted(collisions.items()):
        log(f"  safe='{safe}'", 1)
        for d in sorted(dirs):
            log(f"    - {d}", 1)
    raise SystemExit(3)

# ==========================================
# FOOTPRINTS (v1 + v2)
# ==========================================
def compute_v1_footprint(files_to_add) -> str:
    total_size = 0
    basenames = []
    for p in files_to_add:
        try:
            total_size += os.path.getsize(p)
        except FileNotFoundError:
            pass
        basenames.append(os.path.basename(p))
    name_string = "".join(sorted(basenames))
    name_hash = hashlib.md5(name_string.encode()).hexdigest()[:8]
    return f"{len(files_to_add)}_{total_size}_{name_hash}"

def compute_v2_footprint(files_to_add, subdir: str) -> str:
    """
    v2:
      - relpath + size for all files
      - hash contents for .nfo only (cheap, catches important edits)
    """
    total_size = 0
    h = hashlib.blake2b(digest_size=8)

    rels = []
    for p in files_to_add:
        rel = os.path.relpath(p, subdir).replace(os.sep, "/")
        rels.append((rel.lower(), rel, p))
    rels.sort()

    for _, rel, p in rels:
        try:
            sz = os.path.getsize(p)
        except FileNotFoundError:
            sz = -1

        total_size += max(sz, 0)
        h.update(rel.encode("utf-8", "surrogatepass"))
        h.update(b"\0")
        h.update(str(sz).encode("ascii"))
        h.update(b"\0")

        if p.lower().endswith(".nfo"):
            try:
                with open(p, "rb") as f:
                    data = f.read()
                nh = hashlib.blake2b(data, digest_size=8).hexdigest()
                h.update(nh.encode("ascii"))
            except FileNotFoundError:
                h.update(b"missing")
            h.update(b"\0")

    return f"v2:{len(files_to_add)}_{total_size}_{h.hexdigest()}"

# ==========================================
# JSONL FOOTPRINT STORE (unit-based, backward-compatible loader)
# ==========================================
def load_tarball_fp_store():
    """
    Returns dict unit_id -> fp (fp may be None)

    Accepts:
      NEW lines: {"unit":"R:foo/bar","fp":"v2:..."}
      OLD lines: {"rel_dir":"foo/bar","fp":"v2:..."}  -> treated as recursive unit
    """
    store = {}
    if not os.path.exists(TARBALL_FP_JSONL):
        return store

    with open(TARBALL_FP_JSONL, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
                unit_id = obj.get("unit")
                if not unit_id:
                    rel_dir = obj.get("rel_dir")
                    if rel_dir and rel_dir != ".":
                        unit_id = make_unit_id(rel_dir, UNIT_KIND_RECURSIVE)
                fp = obj.get("fp", None)

                if unit_id:
                    parsed = parse_unit_id(unit_id)
                    if parsed["rel_dir"] and parsed["rel_dir"] != ".":
                        store[unit_id] = fp
            except json.JSONDecodeError:
                raise ValueError("Corrupt tarball footprint record; refusing to discard backup state")
    return store

def save_tarball_fp_store(store: dict):
    """
    Atomic write. NEW schema only: {unit, fp}
    """
    lines = []
    for unit_id in sorted(store.keys()):
        obj = {"unit": unit_id, "fp": store[unit_id]}
        lines.append(json.dumps(obj, ensure_ascii=False, separators=(",", ":")) + "\n")
    atomic_write_lines(TARBALL_FP_JSONL, lines, mode=0o600)
    log(f"Saved footprint store: {TARBALL_FP_JSONL} ({len(store)} entries)", 2)


# ==========================================
# DELTA STATE DB (for incremental tar packs)
# ==========================================
# We use a small local sqlite DB to avoid:
#   - re-uploading the same file in overlapping delta windows
#   - missing deletions (so restores don't resurrect junk)
# This DB is backed up to STANDARD_REMOTE_PATH/ledgers/ by backup_ledgers_to_standard().
#
# Schema:
#   unit_state(unit_id PRIMARY KEY, base_key, base_mod_epoch, last_success_epoch, last_delta_epoch, last_delta_key)
#   file_index(unit_id, relpath PRIMARY KEY within unit, ctime, size, nfohash)
#   delta_keys(unit_id, created_epoch, key)  (append-only log used to build restore manifests)

def open_delta_db(path: str):
    conn = sqlite3.connect(path, timeout=30)
    conn.execute("PRAGMA journal_mode=WAL;")
    conn.execute("PRAGMA synchronous=FULL;")
    conn.execute("""
        CREATE TABLE IF NOT EXISTS unit_state(
            unit_id TEXT PRIMARY KEY,
            base_key TEXT,
            base_mod_epoch INTEGER,
            last_success_epoch INTEGER,
            last_delta_epoch INTEGER,
            last_delta_key TEXT
        )
    """)
    conn.execute("""
        CREATE TABLE IF NOT EXISTS file_index(
            unit_id TEXT NOT NULL,
            relpath TEXT NOT NULL,
            ctime INTEGER,
            size INTEGER,
            nfohash TEXT,
            PRIMARY KEY(unit_id, relpath)
        )
    """)
    conn.execute("CREATE INDEX IF NOT EXISTS idx_file_index_unit ON file_index(unit_id)")
    conn.execute("""
        CREATE TABLE IF NOT EXISTS delta_keys(
            unit_id TEXT NOT NULL,
            created_epoch INTEGER NOT NULL,
            key TEXT NOT NULL,
            tombstoned_epoch INTEGER,
            obsolete_tagged_epoch INTEGER
        )
    """)
    conn.execute("CREATE INDEX IF NOT EXISTS idx_delta_keys_unit ON delta_keys(unit_id, created_epoch)")
    # DB migration: older deployments may not have tombstoned_epoch
    try:
        conn.execute("ALTER TABLE delta_keys ADD COLUMN tombstoned_epoch INTEGER")
    except sqlite3.OperationalError:
        pass
    conn.execute("CREATE INDEX IF NOT EXISTS idx_delta_keys_tombstoned ON delta_keys(unit_id, tombstoned_epoch)")
    # DB migration: older deployments may not have obsolete_tagged_epoch
    try:
        conn.execute("ALTER TABLE delta_keys ADD COLUMN obsolete_tagged_epoch INTEGER")
    except sqlite3.OperationalError:
        pass
    conn.execute("CREATE INDEX IF NOT EXISTS idx_delta_keys_obsolete_tagged ON delta_keys(unit_id, obsolete_tagged_epoch)")

    # Track arbitrary keys we've tagged obsolete (e.g., superseded base tarballs)
    conn.execute("""
        CREATE TABLE IF NOT EXISTS gc_tags(
            key TEXT PRIMARY KEY,
            tagged_epoch INTEGER NOT NULL
        )
    """)
    columns = {row[1] for row in conn.execute("PRAGMA table_info(unit_state)")}
    if "retired" not in columns:
        conn.execute("ALTER TABLE unit_state ADD COLUMN retired INTEGER NOT NULL DEFAULT 0")
    conn.execute("""
        CREATE TABLE IF NOT EXISTS video_uploads(
            path TEXT PRIMARY KEY, size INTEGER NOT NULL,
            mtime_ns INTEGER NOT NULL, ctime_ns INTEGER NOT NULL
        )
    """)
    conn.execute("""
        CREATE TABLE IF NOT EXISTS pending_gc(
            key TEXT PRIMARY KEY, action TEXT NOT NULL CHECK(action IN ('tag','delete'))
        )
    """)
    conn.execute("CREATE TABLE IF NOT EXISTS pending_archives(key TEXT PRIMARY KEY, payload TEXT NOT NULL)")
    conn.commit()
    sync_directory(os.path.dirname(path))
    return conn

def db_get_unit_state(conn, unit_id: str):
    row = conn.execute(
        "SELECT base_key, base_mod_epoch, last_success_epoch, last_delta_epoch, last_delta_key, retired FROM unit_state WHERE unit_id=?",
        (unit_id,)
    ).fetchone()
    if not row:
        return None
    return {
        "base_key": row[0],
        "base_mod_epoch": row[1] or 0,
        "last_success_epoch": row[2] or 0,
        "last_delta_epoch": row[3] or 0,
        "last_delta_key": row[4],
        "retired": bool(row[5]),
    }

def db_upsert_unit_state(conn, unit_id: str, base_key: str, base_mod_epoch: int, last_success_epoch: int, last_delta_epoch: int, last_delta_key: str):
    conn.execute("""
        INSERT INTO unit_state(unit_id, base_key, base_mod_epoch, last_success_epoch, last_delta_epoch, last_delta_key)
        VALUES(?,?,?,?,?,?)
        ON CONFLICT(unit_id) DO UPDATE SET
            base_key=excluded.base_key,
            base_mod_epoch=excluded.base_mod_epoch,
            last_success_epoch=excluded.last_success_epoch,
            last_delta_epoch=excluded.last_delta_epoch,
            last_delta_key=excluded.last_delta_key,
            retired=0
    """, (unit_id, base_key, int(base_mod_epoch), int(last_success_epoch), int(last_delta_epoch), last_delta_key))

def db_load_file_index(conn, unit_id: str):
    """
    Return dict relpath -> (ctime, size, nfohash)
    """
    out = {}
    for relpath, ctime, size, nfohash in conn.execute(
        "SELECT relpath, ctime, size, nfohash FROM file_index WHERE unit_id=?",
        (unit_id,)
    ):
        out[relpath] = (ctime or 0, -1 if size is None else size, nfohash)
    return out

def db_apply_file_index(conn, unit_id: str, current_meta: dict):
    """
    Replace/merge file_index rows for this unit to match current_meta:
      current_meta[relpath] = (ctime, size, nfohash, full_path)
    Uses UPSERTs and deletes removed relpaths.
    """
    existing = set(r[0] for r in conn.execute("SELECT relpath FROM file_index WHERE unit_id=?", (unit_id,)))
    current = set(current_meta.keys())

    to_delete = existing - current
    if to_delete:
        conn.executemany("DELETE FROM file_index WHERE unit_id=? AND relpath=?", [(unit_id, rp) for rp in to_delete])

    conn.executemany(
        """
        INSERT INTO file_index(unit_id, relpath, ctime, size, nfohash)
        VALUES(?,?,?,?,?)
        ON CONFLICT(unit_id, relpath) DO UPDATE SET
          ctime=excluded.ctime,
          size=excluded.size,
          nfohash=excluded.nfohash
        """,
        [(unit_id, rp, int(ct), int(sz), nh) for rp, (ct, sz, nh) in current_meta.items()]
    )

def db_record_delta_key(conn, unit_id: str, created_epoch: int, key: str):
    # tombstoned_epoch may not exist in older DBs; try new schema then fallback.
    try:
        conn.execute(
            "INSERT INTO delta_keys(unit_id, created_epoch, key, tombstoned_epoch, obsolete_tagged_epoch) VALUES(?,?,?,NULL,NULL)",
            (unit_id, int(created_epoch), key)
        )
    except sqlite3.OperationalError:
        conn.execute(
            "INSERT INTO delta_keys(unit_id, created_epoch, key) VALUES(?,?,?)",
            (unit_id, int(created_epoch), key)
        )



def db_list_obsolete_delta_keys_for_unit(conn, unit_id: str, cutoff_epoch: int, min_epoch: int = 0):
    """
    List delta object keys for a unit that are older than cutoff_epoch and not yet tombstoned.
    Used during compaction to tag old delta packs as obsolete so lifecycle can expire them at age>=180d.
    """
    rows = conn.execute(
        """
        SELECT key FROM delta_keys
        WHERE unit_id=?
          AND created_epoch < ?
          AND created_epoch >= ?
          AND tombstoned_epoch IS NULL
          AND obsolete_tagged_epoch IS NULL
        ORDER BY created_epoch ASC
        """,
        (unit_id, int(cutoff_epoch), int(min_epoch))
    ).fetchall()
    return [r[0] for r in rows]

def db_mark_delta_keys_obsolete_tagged(conn, unit_id: str, keys: list, tagged_epoch: int):
    if not keys:
        return
    conn.executemany(
        "UPDATE delta_keys SET obsolete_tagged_epoch=? WHERE unit_id=? AND key=?",
        [(int(tagged_epoch), unit_id, k) for k in keys]
    )

def db_list_delta_keys_for_units(conn, unit_ids: set):
    """
    Return list of delta keys for the provided units.

    We only include deltas created on/after the unit's current baseline (base_mod_epoch),
    so if you later "compact" into a new base tarball, older deltas automatically fall out
    of the restore allowlist.
    """
    if not unit_ids:
        return []
    placeholders = ",".join("?" for _ in unit_ids)
    q = f"""
        SELECT dk.key
        FROM delta_keys dk
        JOIN unit_state us ON us.unit_id = dk.unit_id
        WHERE dk.unit_id IN ({placeholders})
          AND dk.created_epoch >= COALESCE(us.base_mod_epoch, 0)
          AND dk.tombstoned_epoch IS NULL
          AND dk.obsolete_tagged_epoch IS NULL
          AND us.retired=0
        ORDER BY dk.created_epoch ASC, dk.rowid ASC
    """
    rows = conn.execute(q, tuple(sorted(unit_ids))).fetchall()
    return [r[0] for r in rows]


def db_list_base_keys_for_units(conn, unit_ids: set):
    if not unit_ids:
        return []
    placeholders = ",".join("?" for _ in unit_ids)
    q = f"SELECT base_key FROM unit_state WHERE unit_id IN ({placeholders})"
    rows = conn.execute(q, tuple(sorted(unit_ids))).fetchall()
    out = []
    for (bk,) in rows:
        if bk:
            out.append(bk)
    return out
def db_gc_tag_is_marked(conn, key: str) -> bool:
    row = conn.execute("SELECT 1 FROM gc_tags WHERE key=? LIMIT 1", (key,)).fetchone()
    return bool(row)

def db_gc_mark_tagged(conn, key: str, tagged_epoch: int):
    conn.execute(
        "INSERT OR REPLACE INTO gc_tags(key, tagged_epoch) VALUES(?,?)",
        (key, int(tagged_epoch))
    )




# ==========================================
# SCANDIR HELPERS
# ==========================================
def iter_rel_dirs_scandir(base_dir: str):
    """
    Yield relpaths of all subdirectories (excluding '.').
    """
    base_real = _real(base_dir)
    stack = [base_real]
    while stack:
        cur = stack.pop()
        try:
            with os.scandir(cur) as it:
                for entry in it:
                    if entry.is_dir(follow_symlinks=False):
                        rel = os.path.relpath(entry.path, base_real)
                        if rel != ".":
                            yield rel
                        stack.append(entry.path)
        except FileNotFoundError:
            raise

def gather_tar_files_scandir(root_dir: str):
    """
    Collect files under root_dir that match TAR_INCLUDE_EXTENSIONS (recursive).
    """
    out = []
    stack = [root_dir]
    while stack:
        cur = stack.pop()
        try:
            with os.scandir(cur) as it:
                for e in it:
                    if e.is_dir(follow_symlinks=False):
                        stack.append(e.path)
                    elif e.is_file(follow_symlinks=False):
                        if e.name.lower().endswith(TAR_INCLUDE_EXTENSIONS):
                            out.append(e.path)
        except FileNotFoundError:
            continue
    return out

def db_base_key_map_for_units(conn, unit_ids: set) -> dict:
    if not unit_ids:
        return {}
    placeholders = ",".join("?" for _ in unit_ids)
    q = f"SELECT unit_id, base_key FROM unit_state WHERE unit_id IN ({placeholders})"
    rows = conn.execute(q, tuple(sorted(unit_ids))).fetchall()
    return {uid: bk for (uid, bk) in rows if bk}


def gather_tar_files_direct(dir_path: str):
    """
    Collect only direct files under dir_path that match TAR_INCLUDE_EXTENSIONS (non-recursive).
    """
    out = []
    try:
        with os.scandir(dir_path) as it:
            for e in it:
                if e.is_file(follow_symlinks=False) and e.name.lower().endswith(TAR_INCLUDE_EXTENSIONS):
                    out.append(e.path)
    except FileNotFoundError:
        pass
    return out


def iter_tar_entries_recursive(root_dir: str):
    """
    Yield (full_path, relpath, stat) for tar-eligible files under root_dir (recursive).
    relpath is POSIX-style relative to root_dir.
    """
    stack = [root_dir]
    while stack:
        cur = stack.pop()
        try:
            with os.scandir(cur) as it:
                for e in it:
                    try:
                        if e.is_dir(follow_symlinks=False):
                            stack.append(e.path)
                        elif e.is_file(follow_symlinks=False) and e.name.lower().endswith(TAR_INCLUDE_EXTENSIONS):
                            st = e.stat(follow_symlinks=False)
                            rel = os.path.relpath(e.path, root_dir).replace(os.sep, "/")
                            yield (e.path, rel, st)
                    except FileNotFoundError:
                        raise
        except FileNotFoundError:
            raise

def iter_tar_entries_direct(dir_path: str):
    """
    Yield (full_path, relpath, stat) for tar-eligible files directly in dir_path (non-recursive).
    relpath is just the filename (POSIX-style).
    """
    try:
        with os.scandir(dir_path) as it:
            for e in it:
                if e.is_file(follow_symlinks=False) and e.name.lower().endswith(TAR_INCLUDE_EXTENSIONS):
                    try:
                        st = e.stat(follow_symlinks=False)
                    except FileNotFoundError:
                        raise
                    yield (e.path, e.name, st)
    except FileNotFoundError:
        raise

def compute_nfo_hash(path: str):
    """
    Cheap content hash for .nfo to detect edits even if mtimes are preserved.
    """
    try:
        with open(path, "rb") as f:
            data = f.read()
        return hashlib.blake2b(data, digest_size=8).hexdigest()
    except FileNotFoundError:
        return None


def scan_changes_scandir(init_run: bool):
    """Inventory every video; only successfully checked/uploaded signatures count.

    An empty upload ledger deliberately schedules a full size-only reconciliation
    against S3. The old local manifest is not evidence of successful uploads.
    """
    global VIDEO_SCAN
    assert_source_ready()
    conn = open_delta_db(TAR_DELTA_DB)
    try:
        uploaded = {r[0]: tuple(r[1:]) for r in conn.execute(
            "SELECT path, size, mtime_ns, ctime_ns FROM video_uploads"
        )}
        cleanup_keys = {r[0] for r in conn.execute("SELECT key FROM pending_gc UNION SELECT key FROM gc_tags")}
    finally:
        conn.close()
    VIDEO_SCAN = {}
    pending = []
    tar_dirs = set()
    base = _real(BASE_DIR)
    stack = [base]
    while stack:
        directory = stack.pop()
        with os.scandir(directory) as entries:
            for entry in entries:
                if entry.is_dir(follow_symlinks=False):
                    stack.append(entry.path)
                elif entry.is_file(follow_symlinks=False):
                    ext = os.path.splitext(entry.name)[1].lower()
                    if ext in VIDEO_EXTENSIONS:
                        rel = os.path.relpath(entry.path, base).replace(os.sep, "/")
                        validate_list_path(rel)
                        st = entry.stat(follow_symlinks=False)
                        signature = (st.st_size, st.st_mtime_ns, st.st_ctime_ns)
                        VIDEO_SCAN[rel] = signature
                        if init_run or uploaded.get(rel) != signature or rel in cleanup_keys:
                            pending.append(rel)
                    elif ext in TAR_INCLUDE_EXTENSIONS:
                        tar_dirs.add(directory)
    log(f"Video inventory: total={len(VIDEO_SCAN)} pending={len(pending)}", 1)
    return sorted(pending), tar_dirs

# ==========================================
# TARBALL UNIT PARTITIONING (non-overlapping)
# ==========================================
def collect_partitioned_tar_units(base_dir: str):
    """
    Build non-overlapping tar units across the tree:
      - recursive units for leaf tar-bearing dirs
      - rootfiles units for dirs with direct tar files + tar-bearing descendants
    Returns:
      tar_units: list of dicts {unit_id, kind, rel_dir, abs_dir}
    """
    base_real = _real(base_dir)
    tar_units = []

    def walk(dir_path: str):
        direct_has_tar = False
        child_dirs = []

        try:
            with os.scandir(dir_path) as it:
                for e in it:
                    if e.is_dir(follow_symlinks=False):
                        child_dirs.append(e.path)
                    elif e.is_file(follow_symlinks=False):
                        if e.name.lower().endswith(TAR_INCLUDE_EXTENSIONS):
                            direct_has_tar = True
        except FileNotFoundError:
            raise

        child_has_tar_subtree = False
        for c in child_dirs:
            if walk(c):
                child_has_tar_subtree = True

        has_tar_subtree = direct_has_tar or child_has_tar_subtree

        rel_dir = os.path.relpath(dir_path, base_real)
        if rel_dir != "." and has_tar_subtree:
            if direct_has_tar and child_has_tar_subtree:
                kind = UNIT_KIND_ROOTFILES
                tar_units.append({
                    "unit_id": make_unit_id(rel_dir, kind),
                    "kind": kind,
                    "rel_dir": rel_dir,
                    "abs_dir": dir_path,
                })
            elif direct_has_tar and not child_has_tar_subtree:
                kind = UNIT_KIND_RECURSIVE
                tar_units.append({
                    "unit_id": make_unit_id(rel_dir, kind),
                    "kind": kind,
                    "rel_dir": rel_dir,
                    "abs_dir": dir_path,
                })
            # else: no direct tar files here, only descendants => no unit at this level

        return has_tar_subtree

    walk(base_real)
    tar_units.sort(key=lambda u: (u["rel_dir"].lower(), u["kind"]))
    return tar_units

def select_affected_tar_units(tar_units, modified_subdirs_abs: set, fp_store: dict, init_run: bool):
    # Deleting a file need not change any surviving file's timestamps. Comparing
    # every unit's file index is necessary even on an otherwise quiet day.
    log(f"Evaluating all {len(tar_units)} tar units, including deletion-only changes.", 1)
    return list(tar_units)

def expected_historical_tar_units(fp_store: dict):
    return set(fp_store.keys())

# ==========================================
# REMOTE TARBALL LISTING (single call per run)
# ==========================================
def list_remote_tarballs_once(dry_run: bool):
    # LIST/HEAD metadata only, including append-only bases and deltas. No restore
    # or GET of archived object contents is needed to verify chain membership.
    res = run_cmd([
        "rclone", "lsjson", f"{REMOTE_PATH}tarballs", "-R", "--files-only",
        "--fast-list", "--no-mimetype", "--use-server-modtime",
    ], capture=True)
    items = json.loads(res.stdout)
    if not isinstance(items, list):
        raise RuntimeError("Invalid remote tarball inventory")
    tar_keys, stable_keys, dated_by_safe, modtimes = set(), set(), {}, {}
    for item in items:
        if item.get("IsDir"):
            continue
        name = item.get("Path") or item.get("Name")
        if not name or not name.endswith(".tar"):
            continue
        key = f"tarballs/{name}"
        validate_list_path(key)
        tar_keys.add(key)
        epoch = parse_rclone_modtime_to_epoch(item.get("ModTime") or "")
        if epoch is not None:
            modtimes[key] = epoch
        if "/" in name:
            continue
        match = DATED_TAR_RE.match(name)
        if match:
            dated_by_safe.setdefault(match.group("safe"), []).append((int(match.group("date")), key))
        else:
            stable_keys.add(key)
    for values in dated_by_safe.values():
        values.sort(reverse=True)
    log(f"Remote tar inventory: {len(tar_keys)} objects", 1)
    return tar_keys, stable_keys, dated_by_safe, modtimes

def build_current_tarball_map_by_unit(unit_ids, tar_keys, stable_keys, dated_by_safe):
    """
    For each unit_id, determine "current tarball key" for manifest / deletion logic.
      - stable exists => current is stable
      - recursive only: newest dated by safe (legacy remote tarballs)
      - else None
    """
    current_by_unit = {}

    for unit_id in sorted(unit_ids):
        p = parse_unit_id(unit_id)
        rel_dir = p["rel_dir"]
        kind = p["kind"]
        stable_key = stable_tarball_key_for_unit(rel_dir, kind)

        if stable_key in stable_keys:
            current = stable_key
            reason = "stable-exists"
        else:
            current = None
            if kind == UNIT_KIND_RECURSIVE:
                safe = safe_name_for_rel_dir(rel_dir)
                candidates = dated_by_safe.get(safe, [])
                current = candidates[0][1] if candidates else None
                reason = "newest-dated" if current else "none"
            else:
                reason = "none"

        current_by_unit[unit_id] = current

        if LOG_LEVEL >= 2:
            log(f"[TARMAP] unit={unit_id} current={current or '(none)'} reason={reason}", 2)

    return current_by_unit

def sanity_check_tarballs(expected_unit_ids, current_by_unit, db_base_key_map: dict | None = None):
    """
    Warn only for historically expected units (not brand new local units).
    """
    db_base_key_map = db_base_key_map or {}
    expected_unit_ids = list(expected_unit_ids)
    covered_by_legacy = [u for u in expected_unit_ids if current_by_unit.get(u)]
    covered_by_db_base = [u for u in expected_unit_ids if (not current_by_unit.get(u)) and db_base_key_map.get(u)]
    missing = [
        u for u in expected_unit_ids
        if not current_by_unit.get(u) and not db_base_key_map.get(u)
    ]
    if missing:
        log(
            f"WARNING: {len(missing)} expected tar units have NO legacy remote tarball "
            "and NO delta DB baseline.",
            1,
        )
        log(
            f"Tarball sanity coverage: legacy={len(covered_by_legacy)} "
            f"db_base={len(covered_by_db_base)} missing={len(missing)}",
            1,
        )
        for u in missing[:50]:
            log(f"  - Missing tarball/baseline for unit: {u}", 1)
        if len(missing) > 50:
            log("  ... (truncated)", 1)
    else:
        log(
            "Tarball sanity check: all expected historical tar units have legacy tarball "
            "or delta DB baseline coverage.",
            1,
        )

# ==========================================
# RECONCILE DELETED/RETIRED TAR UNITS
# ==========================================
def reconcile_deleted_tar_units(fp_store, local_unit_ids, current_by_unit, dry_run, delete_budget):
    """Retire local state now; remote cleanup is deferred until publication."""
    conn = open_delta_db(TAR_DELTA_DB)
    try:
        active = {r[0] for r in conn.execute("SELECT unit_id FROM unit_state WHERE retired=0")}
        retired = (set(fp_store) | active) - set(local_unit_ids)
        if len(retired) > MAX_DELETED_DIR_TARBALL_TOMBSTONES_PER_RUN:
            raise RuntimeError(f"Refusing to retire {len(retired)} tar units: safety limit exceeded")
        for uid in sorted(retired):
            if dry_run:
                log(f"[DRY RUN] Would retire tar unit {uid}", 1)
                continue
            state = db_get_unit_state(conn, uid)
            old_keys = [current_by_unit.get(uid)]
            if state:
                old_keys.append(state["base_key"])
            old_keys.extend(r[0] for r in conn.execute("SELECT key FROM delta_keys WHERE unit_id=?", (uid,)))
            with conn:
                queue_gc(conn, old_keys)
                # Keep a retirement marker even if this was a legacy unit with
                # no SQLite state. Its still-visible old archive must not be
                # adopted if the same unit reappears before cleanup completes.
                conn.execute("""
                    INSERT INTO unit_state(unit_id, retired) VALUES (?, 1)
                    ON CONFLICT(unit_id) DO UPDATE SET retired=1, base_key=NULL,
                      base_mod_epoch=0, last_success_epoch=0, last_delta_epoch=0,
                      last_delta_key=NULL
                """, (uid,))
                conn.execute("DELETE FROM file_index WHERE unit_id=?", (uid,))
                conn.execute("DELETE FROM delta_keys WHERE unit_id=?", (uid,))
            fp_store.pop(uid, None)
            current_by_unit[uid] = None
        if not dry_run:
            save_tarball_fp_store(fp_store)
    finally:
        conn.close()

# ==========================================
# PHASE 2: TARBALL SNAPSHOT + UPLOAD (versioning mode, unit-based)
# ==========================================
def gather_tar_files_for_unit(unit):
    if unit["kind"] == UNIT_KIND_ROOTFILES:
        return gather_tar_files_direct(unit["abs_dir"])
    return gather_tar_files_scandir(unit["abs_dir"])

def process_tarballs_versioning(
    modified_units,
    fp_store,
    current_by_unit,
    dry_run: bool,
    delete_budget: DeleteBudget
):
    """
    For each affected tar unit:
      - compute v2 footprint
      - if unchanged: skip
      - if changed: create stable tarball
      - if replacing a dated recursive tarball: deletefile(old dated key)
      - if unit now empty: tombstone current tarball and remove fp entry
    """
    unit_list = list(modified_units)
    total_batches = (len(unit_list) + BATCH_SIZE - 1) // BATCH_SIZE

    log(f"Tarball phase: evaluating {len(unit_list)} affected tar units (batch_size={BATCH_SIZE})", 1)

    for i in range(0, len(unit_list), BATCH_SIZE):
        batch = unit_list[i:i + BATCH_SIZE]
        batch_num = i // BATCH_SIZE + 1
        log(f"\nProcessing Tarball Batch {batch_num} of {total_batches}...", 1)

        processed = 0
        created = 0
        skipped_v2_match = 0
        migrated_v1_to_v2 = 0
        selfhealed_none_to_v2 = 0
        tombstoned_empty = 0

        for unit in batch:
            unit_id = unit["unit_id"]
            rel_dir = unit["rel_dir"]
            kind = unit["kind"]
            abs_dir = unit["abs_dir"]

            processed += 1
            if processed % PROGRESS_EVERY == 0:
                log(
                    f"  ...progress: {processed}/{len(batch)} "
                    f"(created={created}, migrated={migrated_v1_to_v2}, skipped_v2={skipped_v2_match})",
                    1
                )

            old_key = current_by_unit.get(unit_id)
            log(f"[TAR] eval unit={unit_id} kind={kind} rel_dir={rel_dir} current_remote={old_key or '(none)'}", 2)

            files_to_add = gather_tar_files_for_unit(unit)

            if LOG_LEVEL >= 2:
                n_nfo = sum(1 for p in files_to_add if p.lower().endswith(".nfo"))
                log(f"[TAR]   found tar_files={len(files_to_add)} (nfo={n_nfo})", 2)

            # Unit now empty => tombstone existing tarball, remove fp
            if not files_to_add:
                if old_key:
                    if delete_budget.allow_tarball_tombstone("empty_dir", old_key):
                        log(f"[TAR]   action=TOMBSTONE_EMPTY old={old_key}", 2)
                        log(f"  -> {unit_id}: no tar-eligible files; tombstoning {old_key}", 1)
                        ensure_s3_delete_marker(old_key, dry_run=dry_run)
                    else:
                        log(f"  -> {unit_id}: empty, but tombstone skipped by safety budget ({old_key})", 1)
                else:
                    log(f"[TAR]   action=SKIP_EMPTY (no current remote tarball)", 2)

                fp_store.pop(unit_id, None)
                current_by_unit[unit_id] = None
                tombstoned_empty += 1
                continue

            v1_current = compute_v1_footprint(files_to_add) if kind == UNIT_KIND_RECURSIVE else None
            v2_current = compute_v2_footprint(files_to_add, abs_dir)
            stored_fp = fp_store.get(unit_id, None)

            log(f"[TAR]   stored_fp={fp_tag(stored_fp)}", 2)
            if LOG_LEVEL >= 3:
                log(f"[TAR]   v2={v2_current}" + (f" | v1={v1_current}" if v1_current else ""), 3)

            # Already v2 and matches
            if isinstance(stored_fp, str) and stored_fp.startswith("v2:") and stored_fp == v2_current:
                log(f"[TAR]   action=SKIP (v2 match)", 2)
                skipped_v2_match += 1
                continue

            # Legacy v1->v2 migration only applies to recursive units
            if (
                kind == UNIT_KIND_RECURSIVE and
                isinstance(stored_fp, str) and
                V1_FP_RE.match(stored_fp) and
                stored_fp == v1_current
            ):
                fp_store[unit_id] = v2_current
                log(f"[TAR]   action=MIGRATE (v1->v2, no upload)", 2)
                migrated_v1_to_v2 += 1
                continue

            # Stored none/unknown self-heal baseline if existing current remote is dated tar and date >= dir ctime date
            # (recursive units only; avoids unnecessary one-time uploads for old unchanged dirs)
            if not stored_fp and kind == UNIT_KIND_RECURSIVE:
                old_key = current_by_unit.get(unit_id)
                if old_key and re.search(r"_\d{8}\.tar$", old_key):
                    try:
                        dir_ctime = os.path.getctime(abs_dir)
                        dir_ctime_int = int(datetime.fromtimestamp(dir_ctime).strftime("%Y%m%d"))
                        old_date_int = int(old_key.split("_")[-1].replace(".tar", ""))
                        if old_date_int >= dir_ctime_int:
                            fp_store[unit_id] = v2_current
                            log(f"[TAR]   action=SELFHEAL (baseline none->v2, no upload)", 2)
                            selfhealed_none_to_v2 += 1
                            continue
                    except Exception as e:
                        log(f"[TAR]   selfheal-check failed: {e}", 3)

            # CHANGE detected => create stable tarball
            stable_key = stable_tarball_key_for_unit(rel_dir, kind)
            local_tarball_path = local_tar_tmp_path_for_unit(rel_dir, kind)

            log(f"[TAR]   action=CREATE stable={stable_key}", 1)

            if dry_run:
                log(f"[DRY RUN] Would create snapshot: {stable_key} ({len(files_to_add)} files)", 1)
            else:
                log(f"  -> Creating snapshot: {stable_key} ({len(files_to_add)} files)", 1)
                with tarfile.open(local_tarball_path, "w") as tar:
                    for file_path in files_to_add:
                        arcname = os.path.relpath(file_path, abs_dir)
                        if file_path.lower().endswith(".nfo"):
                            with open(file_path, "rb") as f:
                                data = f.read()
                            compressed_data = gzip.compress(data)
                            tarinfo = tarfile.TarInfo(name=arcname + ".gz")
                            tarinfo.size = len(compressed_data)
                            tarinfo.mtime = os.path.getmtime(file_path)
                            tar.addfile(tarinfo, io.BytesIO(compressed_data))
                        else:
                            tar.add(file_path, arcname=arcname)

            created += 1
            fp_store[unit_id] = v2_current

            # If replacing a dated recursive tarball, tombstone it now
            old_key = current_by_unit.get(unit_id)
            if (
                kind == UNIT_KIND_RECURSIVE and
                old_key and re.search(r"_\d{8}\.tar$", old_key) and
                old_key != stable_key
            ):
                if delete_budget.allow_tarball_tombstone("replaced_dated", old_key):
                    log(f"[TAR]   action=TOMBSTONE_OLD_DATED old={old_key}", 1)
                    log(f"  -> Replacing dated tarball; tombstoning old {old_key}", 1)
                    ensure_s3_delete_marker(old_key, dry_run=dry_run)
                else:
                    log(f"  -> Would replace dated tarball, but tombstone skipped by safety budget: {old_key}", 1)

            # Stable becomes current (if upload later fails, next run remote listing will reconcile)
            current_by_unit[unit_id] = stable_key

        log(
            "Batch summary: "
            f"processed={processed}/{len(batch)}, created={created}, "
            f"skipped_v2_match={skipped_v2_match}, migrated_v1_to_v2={migrated_v1_to_v2}, "
            f"selfhealed_none_to_v2={selfhealed_none_to_v2}, "
            f"tombstoned_empty={tombstoned_empty}",
            1
        )

        # Upload stable tarballs created in this batch
        if not dry_run:
            if os.path.isdir(TARBALL_TMP_DIR) and any(name.endswith(".tar") for name in os.listdir(TARBALL_TMP_DIR)):
                cmd = [
                    "rclone", "move",
                    TARBALL_TMP_DIR, f"{REMOTE_PATH}tarballs/",
                    "--transfers", "8",
                    "--s3-chunk-size", "16M",
                    "--fast-list",
                    "--use-server-modtime",
                    "--no-update-modtime",
                    "-v"
                ]
                log(f"--- Uploading Batch {batch_num} tarballs to S3 ---", 1)
                run_cmd(cmd, long_running=True)
                log("Uploaded batch tarballs to S3.", 1)
                os.makedirs(TARBALL_TMP_DIR, exist_ok=True)
            else:
                log(f"Batch {batch_num}: no tarballs created locally; skipping tarball upload.", 2)

        # Commit footprint store every batch
        if not dry_run:
            save_tarball_fp_store(fp_store)


# ==========================================
# PHASE 2B: INCREMENTAL TARBALL DELTAS (BASE + DELTA PACKS)
# ==========================================
def tar_ctime_matches(current_ns, recorded):
    """Compare legacy seconds at their recorded precision; new indexes use ns.

    Reinterpreting old seconds as nanoseconds causes false modifications for
    every unchanged historical image. Once evaluated, equivalent old signatures
    are upgraded to the observed nanoseconds for future comparisons.
    """
    recorded = int(recorded or 0)
    if 0 < recorded < 1_000_000_000_000 and current_ns >= 1_000_000_000_000:
        return current_ns // 1_000_000_000 == recorded
    return current_ns == recorded


def collect_current_tar_meta_for_unit(unit):
    """
    Return dict relpath -> (ctime_epoch, size, nfohash or None, full_path)
    relpath is relative to the unit root (POSIX).
    """
    abs_dir = unit["abs_dir"]
    kind = unit["kind"]

    meta = {}
    it = iter_tar_entries_direct(abs_dir) if kind == UNIT_KIND_ROOTFILES else iter_tar_entries_recursive(abs_dir)
    for full_path, relpath, st in it:
        ctime = st.st_ctime_ns
        size = int(getattr(st, "st_size", -1))
        nfohash = None
        if full_path.lower().endswith(".nfo"):
            nfohash = compute_nfo_hash(full_path)
        validate_list_path(relpath)
        meta[relpath] = (ctime, size, nfohash, full_path)
    return meta

def write_delta_tarball(local_path: str, unit, base_key: str, from_epoch: int, to_epoch: int, add_files: list, deleted_relpaths: list):
    """
    Create a delta tarball containing:
      - new/changed files listed in add_files (tuples full_path, relpath)
      - __delta_meta__.json
      - __tombstones__.txt (optional)
    """
    os.makedirs(os.path.dirname(local_path), exist_ok=True)

    meta_obj = {
        "unit_id": unit["unit_id"],
        "kind": unit["kind"],
        "rel_dir": unit["rel_dir"],
        "base_key": base_key,
        "from_epoch": int(from_epoch),
        "to_epoch": int(to_epoch),
        "created_epoch": int(to_epoch),
        "run_id": RUN_ID,
    }
    meta_bytes = (json.dumps(meta_obj, ensure_ascii=False, indent=2) + "\n").encode("utf-8")

    tomb_bytes = None
    if deleted_relpaths:
        tomb_bytes = ("\n".join(deleted_relpaths) + "\n").encode("utf-8")

    with tarfile.open(local_path, "w") as tar:
        # meta first
        ti = tarfile.TarInfo(name="__delta_meta__.json")
        ti.size = len(meta_bytes)
        ti.mtime = to_epoch
        tar.addfile(ti, io.BytesIO(meta_bytes))

        if tomb_bytes is not None:
            ti = tarfile.TarInfo(name="__tombstones__.txt")
            ti.size = len(tomb_bytes)
            ti.mtime = to_epoch
            tar.addfile(ti, io.BytesIO(tomb_bytes))

        # payload files
        abs_dir = unit["abs_dir"]
        for full_path, relpath in add_files:
            arcname = relpath
            if full_path.lower().endswith(".nfo"):
                with open(full_path, "rb") as f:
                    data = f.read()
                compressed_data = gzip.compress(data)
                tarinfo = tarfile.TarInfo(name=arcname + ".gz")
                tarinfo.size = len(compressed_data)
                tarinfo.mtime = os.path.getmtime(full_path)
                tar.addfile(tarinfo, io.BytesIO(compressed_data))
            else:
                tar.add(full_path, arcname=arcname)

def ensure_unit_baseline_state(conn, unit_id, current_base_key, base_mod_epoch, cutover_now):
    state = db_get_unit_state(conn, unit_id)
    if state and not state["retired"]:
        return state
    return {
        "base_key": None if state else current_base_key,
        "base_mod_epoch": int(base_mod_epoch or 0),
        "last_success_epoch": 0, "last_delta_epoch": 0,
        "last_delta_key": None, "retired": bool(state),
    }

def process_tarballs_incremental_deltas(
    affected_units, fp_store, current_by_unit, dated_by_safe, tar_mod_epoch_by_key,
    dry_run, delete_budget, cutover_now, compact, tagger,
):
    conn = open_delta_db(TAR_DELTA_DB)
    try:
        for unit in affected_units:
            uid = unit["unit_id"]
            meta = collect_current_tar_meta_for_unit(unit)
            if not meta:
                raise RuntimeError(f"Tar unit disappeared during the scan: {uid}")
            state = db_get_unit_state(conn, uid)
            previous = db_load_file_index(conn, uid) if state and not state["retired"] else {}
            base = state["base_key"] if state and not state["retired"] else None
            deltas = db_list_delta_keys_for_units(conn, {uid}) if base else []
            legacy = current_by_unit.get(uid) if not state else None

            # A matching legacy fingerprint proves the legacy base represents
            # this local snapshot; seed its file index without downloading it.
            if legacy and legacy in REMOTE_TAR_KEYS and not db_gc_tag_is_marked(conn, legacy):
                fp = compute_v2_footprint([v[3] for v in meta.values()], unit["abs_dir"])
                if fp_store.get(uid) == fp:
                    if not dry_run:
                        with conn:
                            db_upsert_unit_state(conn, uid, legacy, tar_mod_epoch_by_key.get(legacy, 0), current_epoch, 0, None)
                            db_apply_file_index(conn, uid, {rp: v[:3] for rp, v in meta.items()})
                    continue

            rebuild = (
                not base or not previous or base not in REMOTE_TAR_KEYS
                or db_gc_tag_is_marked(conn, base)
                or any(key not in REMOTE_TAR_KEYS for key in deltas)
                or (compact and len(deltas) >= COMPACT_DELTA_THRESHOLD)
            )
            now = time.time_ns() // 1_000_000_000
            if rebuild:
                key = base_tarball_key_for_unit(unit["rel_dir"], unit["kind"], now)
                log(f"[BASE] {uid}: building complete baseline {key}", 1)
                if dry_run:
                    continue
                local = os.path.join(TARBALL_TMP_DIR, hashlib.sha256(key.encode()).hexdigest() + ".tar")
                create_full_tarball(local, unit, meta)
                old_keys = [base, current_by_unit.get(uid)] + deltas
                if unit["kind"] == UNIT_KIND_RECURSIVE:
                    old_keys += [k for _, k in dated_by_safe.get(safe_name_for_rel_dir(unit["rel_dir"]), [])]
                payload = prepare_archive(conn, local, {
                    "key": key, "unit_id": uid, "rebuild": True, "now": now,
                    "old_keys": [k for k in old_keys if k and k != key],
                    "meta": {rp: v[:3] for rp, v in meta.items()},
                    "footprint": compute_v2_footprint([v[3] for v in meta.values()], unit["abs_dir"]),
                })
                finish_archive(conn, payload, fp_store)
                os.unlink(local)
                continue

            deleted = sorted(set(previous) - set(meta))
            added = []
            for relpath, (ct, size, content_hash, full_path) in meta.items():
                old = previous.get(relpath)
                # No wall-clock cutoff: every difference from committed state
                # remains pending until uploaded successfully.
                if old is None or size != old[1] or (relpath.lower().endswith(".nfo") and content_hash != old[2]) or (not relpath.lower().endswith(".nfo") and not tar_ctime_matches(ct, old[0])):
                    added.append((full_path, relpath))
            if not added and not deleted:
                # Upgrade matching legacy precision without uploading payloads.
                # Never invent the missing nanoseconds by multiplying seconds.
                if not dry_run and any(previous[rp][0] != value[0] for rp, value in meta.items()):
                    with conn:
                        db_apply_file_index(conn, uid, {rp: v[:3] for rp, v in meta.items()})
                # Legacy units may never have had a footprint entry. Persist
                # membership even when their file index is already complete.
                if uid not in fp_store and not dry_run:
                    fp_store[uid] = compute_v2_footprint([v[3] for v in meta.values()], unit["abs_dir"])
                    save_tarball_fp_store(fp_store)
                continue
            key = delta_tarball_key_for_unit(unit["rel_dir"], unit["kind"], now)
            log(f"[DELTA] {uid}: add={len(added)} delete={len(deleted)}", 1)
            if dry_run:
                continue
            local = os.path.join(DELTA_TMP_DIR, hashlib.sha256(key.encode()).hexdigest() + ".tar")
            write_delta_tarball(local, unit, base, state["last_success_epoch"], now, added, deleted)
            if collect_current_tar_meta_for_unit(unit) != meta:
                raise RuntimeError(f"Source changed while archiving {uid}; retry required")
            payload = prepare_archive(conn, local, {
                "key": key, "unit_id": uid, "rebuild": False, "now": now,
                "base": base, "base_epoch": state["base_mod_epoch"],
                "meta": {rp: v[:3] for rp, v in meta.items()},
            })
            finish_archive(conn, payload, fp_store)
            os.unlink(local)
    finally:
        conn.close()
# ==========================================
# VIDEO MANIFEST (NUL-delimited) HELPERS
# ==========================================
def iter_nul_records(fp, chunk_size=1024 * 1024):
    """
    Yield raw bytes records split by NUL.
    """
    buf = b""
    while True:
        chunk = fp.read(chunk_size)
        if not chunk:
            if buf:
                yield buf
            break
        buf += chunk
        parts = buf.split(b"\0")
        buf = parts.pop()
        for part in parts:
            yield part

def normalize_rel_bytes(b: bytes) -> bytes:
    if b.startswith(b"./"):
        b = b[2:]
    return b

def is_video_path_bytes(b: bytes) -> bool:
    if not b or b.endswith(b"/"):
        return False
    s = b.decode("utf-8", "surrogateescape")
    ext = os.path.splitext(s)[1].lower()
    return ext in VIDEO_EXTENSIONS

def iter_valid_video_manifest_records_nul(path: str):
    """
    Yields normalized raw bytes records for videos only, from a NUL-delimited manifest.
    """
    with open(path, "rb") as f:
        for rec in iter_nul_records(f):
            rec = normalize_rel_bytes(rec)
            if not rec:
                continue
            if not is_video_path_bytes(rec):
                continue
            yield rec

def iter_deleted_videos_from_manifests(prev_path: str, curr_path: str):
    """
    Merge-diff sorted NUL-delimited manifests (prev - curr).
    Yields UTF-8/surrogateescape decoded relative paths.
    """
    ip = iter(iter_valid_video_manifest_records_nul(prev_path))
    ic = iter(iter_valid_video_manifest_records_nul(curr_path))

    try:
        lp = next(ip)
    except StopIteration:
        return

    try:
        lc = next(ic)
    except StopIteration:
        while True:
            yield lp.decode("utf-8", "surrogateescape")
            try:
                lp = next(ip)
            except StopIteration:
                return

    while True:
        if lp == lc:
            try:
                lp = next(ip)
            except StopIteration:
                return
            try:
                lc = next(ic)
            except StopIteration:
                while True:
                    yield lp.decode("utf-8", "surrogateescape")
                    try:
                        lp = next(ip)
                    except StopIteration:
                        return
        elif lp < lc:
            yield lp.decode("utf-8", "surrogateescape")
            try:
                lp = next(ip)
            except StopIteration:
                return
        else:
            try:
                lc = next(ic)
            except StopIteration:
                while True:
                    yield lp.decode("utf-8", "surrogateescape")
                    try:
                        lp = next(ip)
                    except StopIteration:
                        return

def write_text_video_manifest_from_nul(nul_manifest_path: str, out_fp_text):
    """
    Convert sorted NUL-delimited internal manifest to newline-delimited text for master manifest.
    """
    for rec in iter_valid_video_manifest_records_nul(nul_manifest_path):
        out_fp_text.write(rec.decode("utf-8", "surrogateescape"))
        out_fp_text.write("\n")

# ==========================================
# PHASE 3: VIDEO MANIFEST + REMOTE TOMBSTONES FOR DELETIONS
# ==========================================
def map_videos_and_tombstone_deletions(dry_run, delete_budget):
    """Plan deletions from the same scan used for uploads; never delete here."""
    current = os.path.join(TMP_DIR, "video_manifest_current.nul")
    records = sorted(VIDEO_SCAN, key=lambda p: p.encode("utf-8"))
    atomic_write_bytes(current, b"".join(p.encode("utf-8") + b"\0" for p in records))
    deleted = list(iter_deleted_videos_from_manifests(VIDEO_PREV_MANIFEST, current)) if os.path.exists(VIDEO_PREV_MANIFEST) else []
    conn = open_delta_db(TAR_DELTA_DB)
    try:
        # Include uploads made by a run that failed before publishing its manifest.
        deleted = set(deleted) | {r[0] for r in conn.execute("SELECT path FROM video_uploads") if r[0] not in VIDEO_SCAN}
        if not delete_budget.allow_video_tombstones(len(deleted)):
            raise RuntimeError(f"Refusing {len(deleted)} video deletions: safety limit exceeded; manifest unchanged")
        if not dry_run:
            with conn:
                queue_gc(conn, deleted, "tag")
    finally:
        conn.close()
    return current


# ==========================================
# PHASE 4: VIDEO UPLOAD + MASTER MANIFEST
# ==========================================
def remote_upload_head(key):
    # HEAD only: this neither retrieves nor restores Glacier payloads.
    client = _get_s3_delete_client()
    if client is None:
        raise RuntimeError("S3 client unavailable for upload reconciliation")
    try:
        head = client.head_object(Bucket=REMOTE_S3_BUCKET, Key=_full_s3_key(key))
    except Exception as error:
        code = getattr(error, "response", {}).get("Error", {}).get("Code")
        if str(code) in {"404", "NoSuchKey", "NotFound"}:
            return None
        raise
    return head


def remote_upload_matches(key, size, metadata_key, value):
    head = remote_upload_head(key)
    return head is not None and head["ContentLength"] == size and head.get("Metadata", {}).get(metadata_key) == value


def remote_video_matches(key, local, size, signature):
    head = remote_upload_head(key)
    if head is None or head["ContentLength"] != size:
        return False
    metadata = head.get("Metadata", {})
    if metadata.get("backup-source-signature") == signature:
        return True
    # Older rclone uploads predate our signature header. Compare their MD5
    # evidence locally, so an initial audit does not reupload unchanged Glacier
    # objects. Multipart uploads store this in Md5chksum; their ETag is not MD5.
    encoded = metadata.get("md5chksum")
    try:
        checksum = base64.b64decode(encoded, validate=True).hex() if encoded else str(head.get("ETag", "")).strip('"')
    except (ValueError, TypeError):
        return False
    if not re.fullmatch('[0-9a-fA-F]{32}', checksum):
        return False
    with open(local, "rb") as source:
        return hashlib.file_digest(source, "md5").hexdigest() == checksum.lower()


def upload_new_videos_if_any(has_new_videos, dry_run):
    if dry_run or not has_new_videos:
        return
    with open(os.path.join(TMP_DIR, "new_videos.txt"), encoding="utf-8") as source:
        pending = [line.removesuffix("\n") for line in source]
    if not pending:
        raise RuntimeError("Expected pending video uploads but the upload list is empty")
    conn = open_delta_db(TAR_DELTA_DB)
    try:
        with conn:
            queue_gc(conn, pending, "tag")
    finally:
        conn.close()
    tagger = S3ObjectTagger(REMOTE_S3_BUCKET, REMOTE_S3_PREFIX)
    def upload(path):
        expected = VIDEO_SCAN[path]
        local = os.path.join(BASE_DIR, path)
        def verify_source():
            st = os.stat(local, follow_symlinks=False)
            if not stat.S_ISREG(st.st_mode) or (st.st_size, st.st_mtime_ns, st.st_ctime_ns) != expected:
                raise RuntimeError(f"Video changed during backup: {path!r}; retry required")
        verify_source()
        signature = hashlib.sha256(json.dumps(expected).encode()).hexdigest()
        if not tagger.clear_obsolete(path):
            raise RuntimeError("Cannot safely reuse video path: clearing obsolete tag failed")
        if not remote_video_matches(path, local, expected[0], signature):
            # Size alone cannot establish that a changed video was uploaded.
            run_cmd(["rclone", "copyto", local, REMOTE_PATH + path, "--ignore-times",
                     "--metadata", "--metadata-set", "backup-source-signature=" + signature,
                     "--s3-chunk-size", "16M", "--s3-upload-concurrency", "4", "--stats", "30s"], long_running=True)
            if not remote_upload_matches(path, expected[0], "backup-source-signature", signature):
                raise RuntimeError(f"Uploaded video could not be verified: {path!r}")
        verify_source()
        db = open_delta_db(TAR_DELTA_DB)
        try:
            with db:
                db.execute("""INSERT INTO video_uploads(path,size,mtime_ns,ctime_ns) VALUES (?,?,?,?)
                    ON CONFLICT(path) DO UPDATE SET size=excluded.size,
                    mtime_ns=excluded.mtime_ns,ctime_ns=excluded.ctime_ns""", (path, *expected))
                db.execute("DELETE FROM gc_tags WHERE key=?", (path,))
                db.execute("DELETE FROM pending_gc WHERE key=?", (path,))
        finally:
            db.close()
    with ThreadPoolExecutor(max_workers=8) as pool:
        # Consume all futures; successful files commit independently of failures.
        results = list(pool.map(upload, pending))


def prepare_archive(conn, local, payload):
    with open(local, "rb") as source:
        os.fsync(source.fileno())
        payload["sha256"] = hashlib.file_digest(source, "sha256").hexdigest()
    payload["size"] = os.path.getsize(local)
    directory = os.path.join(os.path.dirname(TAR_DELTA_DB), "pending-archives")
    os.makedirs(directory, mode=0o700, exist_ok=True)
    durable = os.path.join(directory, hashlib.sha256(payload["key"].encode()).hexdigest() + ".tar")
    # The staging directory can be on a different filesystem.
    atomic_copy(local, durable)
    sync_directory(os.path.dirname(directory))
    payload["local"] = durable
    with conn:
        conn.execute("INSERT INTO pending_archives VALUES (?,?)", (payload["key"], json.dumps(payload)))
    return payload


def finish_archive(conn, payload, fp_store):
    key = payload["key"]
    if not remote_upload_matches(key, payload["size"], "backup-sha256", payload["sha256"]):
        local = payload["local"]
        with open(local, "rb") as source:
            if hashlib.file_digest(source, "sha256").hexdigest() != payload["sha256"]:
                raise RuntimeError("Prepared archive failed integrity verification")
        run_cmd(["rclone", "copyto", local, REMOTE_PATH + key, "--ignore-times", "--metadata",
                 "--metadata-set", "backup-sha256=" + payload["sha256"],
                 "--s3-chunk-size", "16M", "--stats", "30s"], long_running=True)
        if not remote_upload_matches(key, payload["size"], "backup-sha256", payload["sha256"]):
            raise RuntimeError("Uploaded archive could not be verified")
    uid = payload["unit_id"]
    with conn:
        if payload["rebuild"]:
            queue_gc(conn, payload["old_keys"])
            conn.execute("DELETE FROM delta_keys WHERE unit_id=?", (uid,))
            db_upsert_unit_state(conn, uid, key, payload["now"], payload["now"], 0, None)
        else:
            db_upsert_unit_state(conn, uid, payload["base"], payload["base_epoch"], payload["now"], payload["now"], key)
            db_record_delta_key(conn, uid, payload["now"], key)
        db_apply_file_index(conn, uid, payload["meta"])
        conn.execute("DELETE FROM pending_archives WHERE key=?", (key,))
    REMOTE_TAR_KEYS.add(key)
    if payload.get("footprint"):
        fp_store[uid] = payload["footprint"]
        save_tarball_fp_store(fp_store)
    try:
        os.unlink(payload["local"])
        sync_directory(os.path.dirname(payload["local"]))
    except FileNotFoundError:
        pass


def resume_archives(fp_store):
    conn = open_delta_db(TAR_DELTA_DB)
    try:
        for row in conn.execute("SELECT payload FROM pending_archives ORDER BY rowid").fetchall():
            finish_archive(conn, json.loads(row[0]), fp_store)
    finally:
        conn.close()

def build_and_upload_master_manifest(current_video_manifest_nul, current_tarball_keys, dry_run, catalog=None):
    if dry_run:
        log("[DRY RUN] No manifest will be published.", 1)
        return
    if catalog is None:
        raise RuntimeError("A validated restore catalog is required before publication")
    assert_source_ready()
    # Both forms describe exactly the same validated scan. JSON is authoritative
    # and self-contained; the text allowlist is retained for existing tools.
    manifest = os.path.join(TMP_DIR, "current_manifest.json")
    atomic_write_bytes(manifest, (json.dumps(catalog, ensure_ascii=False, separators=(",", ":")) + "\n").encode("utf-8"))
    keys = [v["key"] for v in catalog["videos"]] + sorted(set(current_tarball_keys))
    for key in keys:
        validate_list_path(key)
    legacy = os.path.join(TMP_DIR, "current_manifest.txt")
    atomic_write_lines(legacy, [key + "\n" for key in keys])
    if SOURCE_VIEW is None:
        raise RuntimeError("Native publication requires a retained source checkpoint")
    with open(manifest, "rb") as incoming:
        body = incoming.read()
    SOURCE_VIEW.prepare_master(body)
    assert_source_ready()
    run_cmd(["rclone", "copyto", legacy, f"{STANDARD_REMOTE_PATH}current_manifest.txt"], long_running=True)
    # Commit point: no remote tombstoning/tagging happens before this succeeds.
    assert_source_ready()
    SOURCE_VIEW.commit_master(body)

# ==========================================
# LEDGER BACKUP TO STANDARD BUCKET
# ==========================================
def backup_ledgers_to_standard(dry_run: bool):
    """
    Back up critical local ledgers:
      - latest copies
      - per-run snapshots
    """
    ledger_paths = [
        TARBALL_FP_JSONL,
        TAR_DELTA_DB,
        VIDEO_PREV_MANIFEST,
        TOMBSTONES_LEDGER,
        VIDEO_TOMBSTONE_PROGRESS,
    ]

    existing = [p for p in ledger_paths if os.path.exists(p)]
    if not existing:
        log("No ledger files to back up.", 1)
        return

    for path in existing:
        name = os.path.basename(path)
        latest_dst = f"{STANDARD_REMOTE_PATH}ledgers/latest/{name}"
        snapshot_dst = f"{STANDARD_REMOTE_PATH}ledgers/runs/{RUN_ID}/{name}"

        if dry_run:
            log(f"[DRY RUN] Would backup ledger {path} -> {latest_dst}", 1)
            log(f"[DRY RUN] Would backup ledger {path} -> {snapshot_dst}", 1)
            continue

        if path.endswith(".sqlite3"):
            snapshot = os.path.join(TMP_DIR, "ledger-" + name)
            source_conn = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
            dest_conn = sqlite3.connect(snapshot)
            try:
                source_conn.backup(dest_conn)
            finally:
                source_conn.close()
                dest_conn.close()
            path = snapshot
        log(f"Backing up ledger: {name}", 1)
        run_cmd([
"rclone", "copyto", path, latest_dst
])
        run_cmd([
"rclone", "copyto", path, snapshot_dst
])

# ==========================================
# MAIN
# ==========================================


def initialize_runtime():
    global current_epoch, RUN_ID, TMP_DIR, TARBALL_TMP_DIR, DELTA_TMP_DIR
    current_epoch = int(time.time())
    RUN_ID = datetime.now().strftime("%Y%m%d_%H%M%S") + f"_{os.getpid()}"
    os.makedirs(TMP_BASE_DIR, exist_ok=True)
    os.makedirs(LEDGER_DIR, exist_ok=True)
    TMP_DIR = tempfile.mkdtemp(prefix=f"run_{RUN_ID}_", dir=TMP_BASE_DIR)
    TARBALL_TMP_DIR = os.path.join(TMP_DIR, "tarballs")
    DELTA_TMP_DIR = os.path.join(TMP_DIR, "tar_deltas")
    os.makedirs(TARBALL_TMP_DIR)
    os.makedirs(DELTA_TMP_DIR)

def assert_source_ready():
    if SOURCE_MOUNT and not os.path.ismount(SOURCE_MOUNT):
        raise RuntimeError(f"Backup source filesystem is not mounted: {SOURCE_MOUNT}")
    live = str(SOURCE_VIEW.live_media) if SOURCE_VIEW is not None else BASE_DIR
    if not os.path.isdir(live):
        raise RuntimeError(f"Backup source directory is unavailable: {live}")
    if SOURCE_MOUNT and os.stat(live).st_dev != os.stat(SOURCE_MOUNT).st_dev:
        raise RuntimeError("Backup source is on an unexpected filesystem")
    if SOURCE_VIEW is not None:
        SOURCE_VIEW.check_source()
    for source in {live, BASE_DIR}:
        with os.scandir(source) as entries:
            if next(entries, None) is None:
                raise RuntimeError("Backup source is empty; refusing to replace the restore manifest")

def validate_list_path(path):
    # Installed rclone supports --files-from-raw, but not NUL-delimited input.
    # Fail before changing remote state instead of silently misaddressing a key.
    if not path or path.startswith("/") or any(p in ("", ".", "..") for p in path.split("/")):
        raise ValueError(f"Invalid relative backup path: {path!r}")
    if any(c in path for c in ("\n", "\r", "\0")):
        raise ValueError(f"Path cannot be represented by rclone's file list: {path!r}")
    path.encode("utf-8")

def queue_gc(conn, keys, action="tag"):
    conn.executemany(
        "INSERT OR IGNORE INTO pending_gc(key, action) VALUES (?, ?)",
        [(key, action) for key in set(keys) if key],
    )

def create_full_tarball(local_path, unit, current_meta):
    with tarfile.open(local_path, "w") as archive:
        for relpath, (_ct, _size, _hash, path) in sorted(current_meta.items()):
            if path.lower().endswith(".nfo"):
                with open(path, "rb") as source:
                    payload = gzip.compress(source.read(), mtime=0)
                info = tarfile.TarInfo(relpath + ".gz")
                info.size = len(payload)
                info.mtime = os.path.getmtime(path)
                archive.addfile(info, io.BytesIO(payload))
            else:
                archive.add(path, arcname=relpath, recursive=False)
    if collect_current_tar_meta_for_unit(unit) != current_meta:
        raise RuntimeError(f"Source changed while archiving {unit['unit_id']}; retry required")

def build_restore_catalog(tar_units):
    conn = open_delta_db(TAR_DELTA_DB)
    try:
        uploaded = {r[0]: tuple(r[1:]) for r in conn.execute("SELECT path,size,mtime_ns,ctime_ns FROM video_uploads")}
        missing_videos = [p for p, signature in VIDEO_SCAN.items() if uploaded.get(p) != signature]
        if missing_videos:
            raise RuntimeError(f"Manifest refused: {len(missing_videos)} videos have no successful upload record")
        units = []
        for unit in tar_units:
            state = db_get_unit_state(conn, unit["unit_id"])
            if not state or state["retired"] or not state["base_key"]:
                raise RuntimeError(f"Manifest refused: no active baseline for {unit['unit_id']}")
            deltas = db_list_delta_keys_for_units(conn, {unit["unit_id"]})
            required = [state["base_key"]] + deltas
            if any(key not in REMOTE_TAR_KEYS for key in required):
                raise RuntimeError(f"Manifest refused: incomplete archive chain for {unit['unit_id']}")
            units.append({k: unit[k] for k in ("unit_id", "rel_dir", "kind")} | {"base_key": state["base_key"], "deltas": deltas})
        return {
            "format": "s3-log-backup", "version": 3, "run_id": RUN_ID,
            "created_epoch": SOURCE_VIEW.created_epoch,
            "videos": [{"key": p, "size": VIDEO_SCAN[p][0]} for p in sorted(VIDEO_SCAN)],
            "units": units,
        }
    finally:
        conn.close()

def finish_remote_cleanup(catalog, tagger, delete_budget):
    assert_source_ready()
    protected = {v["key"] for v in catalog["videos"]}
    for unit in catalog["units"]:
        protected.update([unit["base_key"], *unit["deltas"]])
    conn = open_delta_db(TAR_DELTA_DB)
    failed = []
    video_tags = 0
    try:
        for key, action in conn.execute("SELECT key, action FROM pending_gc ORDER BY key").fetchall():
            if key in protected:
                with conn:
                    conn.execute("DELETE FROM pending_gc WHERE key=?", (key,))
                continue
            # Migrate queued legacy 'delete' actions to tag-only cleanup too.
            if not key.startswith("tarballs/"):
                validate_list_path(key)
                if video_tags >= MAX_VIDEO_DELETE_MARKERS_PER_RUN:
                    continue
                # A path reintroduced after the scan is not safe to tombstone.
                live = SOURCE_VIEW.live_media if SOURCE_VIEW is not None else BASE_DIR
                if os.path.lexists(os.path.join(live, key)):
                    continue
                video_tags += 1
                ok = tagger.ensure_tag(key, OBSOLETE_TAG_KEY, OBSOLETE_TAG_VALUE, dry_run=False)
            else:
                if not delete_budget.allow_tag_op(key):
                    continue
                # A baseline rebuilt because it was already missing needs no
                # further cleanup. Do not retry tagging an absent key forever.
                ok = key not in REMOTE_TAR_KEYS or tagger.ensure_tag(key, OBSOLETE_TAG_KEY, OBSOLETE_TAG_VALUE, dry_run=False)
            if ok:
                with conn:
                    conn.execute("DELETE FROM pending_gc WHERE key=?", (key,))
                    db_gc_mark_tagged(conn, key, int(time.time()))
                    if not key.startswith("tarballs/"):
                        conn.execute("DELETE FROM video_uploads WHERE path=?", (key,))
            else:
                failed.append(key)
    finally:
        conn.close()
    if failed:
        raise RuntimeError(f"Manifest published, but {len(failed)} cleanup operations failed; queued for retry")

def _run_backup(args):
    global REMOTE_TAR_KEYS, TAR_DELTA_DB
    assert_source_ready()
    defer_cleanup = getattr(args, "defer_cleanup", False)
    # Dry runs work on an isolated SQLite snapshot, including schema migrations.
    if args.dry_run:
        original_db = TAR_DELTA_DB
        TAR_DELTA_DB = os.path.join(TMP_DIR, "dry-run-state.sqlite3")
        if os.path.exists(original_db):
            # The run lock excludes the backup writer. Copy any WAL as well;
            # even a mode=ro SQLite connection can create live -shm/-wal files.
            shutil.copyfile(original_db, TAR_DELTA_DB)
            if os.path.exists(original_db + "-wal"):
                shutil.copyfile(original_db + "-wal", TAR_DELTA_DB + "-wal")
    budget = DeleteBudget()
    assert_s3_access_ready(
        require_glacier_rclone=True, require_standard_rclone=not args.dry_run,
        require_boto3_delete=False,
        require_boto3_tagging=not args.dry_run,
        aws_region=args.aws_region,
    )
    fp_store = load_tarball_fp_store()
    if not args.dry_run:
        resume_archives(fp_store)
    dirs = list(iter_rel_dirs_scandir(BASE_DIR))
    detect_safe_name_collisions(dirs)
    tar_units = collect_partitioned_tar_units(BASE_DIR)
    local_ids = {u["unit_id"] for u in tar_units}
    conn = open_delta_db(TAR_DELTA_DB)
    try:
        historical = set(fp_store) | {r[0] for r in conn.execute("SELECT unit_id FROM unit_state WHERE retired=0")}
    finally:
        conn.close()
    if len(historical - local_ids) > MAX_DELETED_DIR_TARBALL_TOMBSTONES_PER_RUN:
        raise RuntimeError("Tar-unit retirement safety limit exceeded; manifest unchanged")
    pending, tar_hints = scan_changes_scandir(init_run=args.init_run)
    # Check video deletion limits before any archive uploads or state retirement.
    current_video_manifest = map_videos_and_tombstone_deletions(args.dry_run, budget)
    tar_keys, stable, dated, modtimes = list_remote_tarballs_once(args.dry_run)
    REMOTE_TAR_KEYS = tar_keys
    current = build_current_tarball_map_by_unit(local_ids | historical, tar_keys, stable, dated)
    reconcile_deleted_tar_units(fp_store, local_ids, current, args.dry_run, budget)
    if pending:
        atomic_write_lines(os.path.join(TMP_DIR, "new_videos.txt"), [p + "\n" for p in pending])
    upload_new_videos_if_any(bool(pending), args.dry_run)
    process_tarballs_incremental_deltas(
        select_affected_tar_units(tar_units, tar_hints, fp_store, args.init_run),
        fp_store, current, dated, modtimes, args.dry_run, budget,
        args.delta_cutover_now, args.compact, None,
    )
    if args.dry_run:
        return
    assert_source_ready()
    # Also detect a disappearing subtree or partition migration during packing.
    if {u["unit_id"] for u in collect_partitioned_tar_units(BASE_DIR)} != local_ids:
        raise RuntimeError("Source directory layout changed during backup; manifest unchanged")
    catalog = build_restore_catalog(tar_units)
    # The native database, retained originals, worker state and this exact media
    # selection must be verified in Standard before publishing or retiring keys.
    catalog["native_archive"] = SOURCE_VIEW.publish(catalog, [
        TARBALL_FP_JSONL, TAR_DELTA_DB, VIDEO_PREV_MANIFEST,
        TOMBSTONES_LEDGER, VIDEO_TOMBSTONE_PROGRESS,
    ])
    tar_keys = [key for u in catalog["units"] for key in [u["base_key"], *u["deltas"]]]
    # Recoverable per-run state is uploaded before publishing the self-contained
    # manifest. Any failure above or here leaves remote cleanup unexecuted.
    backup_ledgers_to_standard(False)
    build_and_upload_master_manifest(current_video_manifest, tar_keys, False, catalog)
    assert_source_ready()
    atomic_copy(current_video_manifest, VIDEO_PREV_MANIFEST)
    if defer_cleanup:
        log("Backup published. All remote cleanup is deferred; queued cleanup remains pending.", 1)
        return
    tagger = S3ObjectTagger(REMOTE_S3_BUCKET, REMOTE_S3_PREFIX, region=args.aws_region)
    finish_remote_cleanup(catalog, tagger, budget)
    # Preserve the existing orphan/legacy-object maintenance, but only after
    # publication. Failure of these optional audits cannot invalidate a catalog.
    conn = open_delta_db(TAR_DELTA_DB)
    try:
        tag_superseded_stable_tarballs_obsolete(
            conn, local_ids, stable, budget, tagger, False, int(time.time()),
        )
        tag_superseded_remote_delta_tarballs_obsolete(
            conn, local_ids, False, budget, tagger, int(time.time()),
        )
    finally:
        conn.close()
    tombstone_remote_accidental_nonvideo_objects(dry_run=False, delete_budget=2000)
    log(budget.summary(), 1)

def open_native_session(args):
    config = getattr(args, "native_config", None) or os.environ.get("STASH_NATIVE_BACKUP_CONFIG")
    if not config:
        raise RuntimeError("Native backup requires --native-config or STASH_NATIVE_BACKUP_CONFIG")
    bucket, prefix = parse_remote_bucket_and_prefix(STANDARD_REMOTE_PATH)
    client = boto3.client("s3", region_name=args.aws_region,
                          config=Config(max_pool_connections=20, retries={"mode": "standard", "max_attempts": 5}))
    return NativeBackupSession(config, RUN_ID, BASE_DIR, bucket, prefix, client)


def run_backup(args):
    global TAR_DELTA_DB, BASE_DIR, SOURCE_VIEW
    original_db, original_base, original_view = TAR_DELTA_DB, BASE_DIR, SOURCE_VIEW
    try:
        assert_source_ready()
        if not args.dry_run:
            SOURCE_VIEW = open_native_session(args)
            BASE_DIR = os.fspath(SOURCE_VIEW.media_path)
        try:
            _run_backup(args)
        finally:
            # The complete backup is durable even if later tagging failed.
            # Once this attempt stops using the view, retain only its receipts.
            if not args.dry_run and SOURCE_VIEW.committed:
                SOURCE_VIEW.finish()
    finally:
        TAR_DELTA_DB, BASE_DIR, SOURCE_VIEW = original_db, original_base, original_view

def main(argv=None):
    parser = argparse.ArgumentParser(description="S3 Deep Archive backup with durable upload tracking and restorable manifests.")
    parser.add_argument("--dry-run", action="store_true", help="Read-only remote planning using an isolated local state copy.")
    parser.add_argument("--init-run", action="store_true", help="Reconcile every video against S3, regardless of its timestamps.")
    parser.add_argument("--delta-cutover-now", action="store_true", help="Compatibility flag; complete baseline coverage is always required.")
    parser.add_argument("--compact", action="store_true")
    parser.add_argument("--defer-cleanup", action="store_true", help="Publish the backup and keep obsolete tagging queued; returning videos still have obsolete tags cleared.")
    parser.add_argument("--aws-region")
    parser.add_argument("--native-config", help="Private host checkpoint/worker inventory JSON (or STASH_NATIVE_BACKUP_CONFIG). Required except for dry runs.")
    parser.add_argument("--verbose", action="store_true")
    parser.add_argument("--trace", action="store_true")
    parser.add_argument("--log-file")
    args = parser.parse_args(argv)
    global LOG_LEVEL
    LOG_LEVEL = 3 if args.trace else 2 if args.verbose else 1
    os.makedirs(LEDGER_DIR, exist_ok=True)
    try:
        with exclusive_run_lock(RUN_LOCK_PATH):
            try:
                initialize_runtime()
                configure_logging(LOG_LEVEL, RUN_ID, log_file=args.log_file)
                for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
                    signal.signal(sig, handle_shutdown)
                if not args.dry_run:
                    cleanup_stale_tmp()
                marker = os.path.join(LEDGER_DIR, ".backup-interrupted.json")
                if not args.dry_run:
                    assert_source_ready()
                    atomic_write_bytes(marker, json.dumps({"run_id": RUN_ID, "started": time.time()}).encode())
                run_backup(args)
                if not args.dry_run:
                    os.unlink(marker)
                    sync_directory(LEDGER_DIR)
            finally:
                cleanup_tmp()
    except BlockingIOError:
        configure_logging(LOG_LEVEL, RUN_ID, log_file=args.log_file)
        log("Another backup run is already in progress.", 1)
        return 2
    log("Simulation completed." if args.dry_run else "Process completed.", 1)
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
