"""The ordered capture/file boundary used by the gallery-dl adapter."""

from dataclasses import dataclass
from pathlib import Path
import uuid

from .encoding import InvalidData, digest, encode, identifier, utc_now
from .retention import POLICY, retain
from .runs import SourceFailure, SourcePaused
from .source_window import SourceWindow
from . import source
from .scan_resume import PREFIX as LEGACY_CURSOR_PREFIX


@dataclass(frozen=True)
class Prepared:
    event_uuid: str
    attachment: dict
    source: dict


class Producer:
    def __init__(self, outbox, lease, root, *, extractor_version, configuration_check=None, source_category=None):
        lease.check()
        run = lease.run
        if (outbox.producer != lease.client.producer or outbox.endpoint != lease.client.endpoint
                or run["operation"] != "download" or run.get("root_uuid") is None):
            raise InvalidData("Producer must own a download run at its permitted media root")
        self.outbox, self.lease, self.root = outbox, lease, root
        self.source_category = source_category
        if configuration_check is not None and not callable(configuration_check):
            raise InvalidData("Invalid worker configuration check")
        self.configuration_check = configuration_check or (lambda: None)
        self.failure_code = None
        self.source_failure = None
        self.window = SourceWindow(run.get("window"))

        self.path_prefix = run["path_prefix"]
        if (not isinstance(self.path_prefix, str) or not self.path_prefix
                or self.path_prefix.startswith("/") or "\\" in self.path_prefix
                or any(ord(c) < 32 for c in self.path_prefix)
                or (self.path_prefix != "." and any(p in ("", ".", "..") for p in self.path_prefix.split("/")))):
            raise InvalidData("Run destination must be a portable relative path")
        self.context = {"protocol": 1, "producer_uuid": outbox.producer,
                        "run_uuid": identifier(run["uuid"]), "collection_uuid": identifier(run["collection_uuid"]),
                        "collection_revision": run["collection_revision"], "root_uuid": identifier(run["root_uuid"])}
        self.extractor_version = extractor_version
        self.items_seen = run["progress"]["items_seen"]
        self.files_completed = run["progress"]["files_completed"]
        self.resume_cursor = run["progress"]["cursor"]
        recovery = run.get("recovery") or {}
        self.replay_archive = recovery.get("replay_archive", False)
        if type(self.replay_archive) is not bool:
            raise InvalidData("Invalid archive recovery policy")
        self.replay_seen = 0
        self.replay_limit = max(64, self.items_seen + 64)

    def check(self):
        self.lease.check()
        self.root.verify()
        self.configuration_check()
        if self.source_failure is not None:
            raise self.source_failure

    def fail_source(self, code, scope):
        self.check()
        if self.source_failure is None:
            self.source_failure = SourceFailure(code, scope)
        raise self.source_failure from None

    def reserve_source(self, url):
        self.check()
        try:
            return self.lease.reserve_source(url)
        except SourceFailure as exc:
            self.fail_source(exc.code, exc.scope)

    def relative(self, path):
        relative = self.root.relative(path)
        if (self.path_prefix != "." and relative != self.path_prefix
                and not relative.startswith(self.path_prefix + "/")):
            raise InvalidData("Destination is outside the claimed collection prefix")
        return relative

    def prepare(self, metadata):
        self.check()
        kept = retain(metadata)
        post = source.post(kept)
        metadata = source.metadata(kept)
        event_id = str(uuid.uuid4())
        event = {**self.context, "event_uuid": event_id, "kind": "source.capture", "observed_at": utc_now(),
                 "extractor_version": self.extractor_version, "retention_policy": POLICY,
                 "post": post, "metadata": metadata, "source": kept}
        # Preserve evidence even when attachment matching requires a new adapter.
        self.outbox.enqueue(encode(event))
        return Prepared(event_id, source.attachment(kept), kept)

    def complete(self, prepared, path):
        # This file may finish after ownership loss. Persist it first; the next
        # source boundary and run checkpoint still require the live lease.
        self.relative(path)
        relative, size, sha256 = self.root.completed(path)
        suffix = Path(relative).suffix.lower()
        if suffix in {".jpg", ".jpeg", ".png", ".webp", ".avif", ".gif", ".jxl", ".bmp"}:
            kind = "image"
        elif suffix in {".mp4", ".mkv", ".webm", ".mov", ".avi", ".m4v", ".wmv", ".mpeg", ".mpg", ".ts"}:
            kind = "scene"
        else:
            raise InvalidData("Completed file is not a supported image or video")
        event = {**self.context, "event_uuid": str(uuid.uuid4()), "kind": "file.completed", "observed_at": utc_now(),
                 "relative_path": relative, "size": size, "sha256": sha256, "media_kind": kind,
                 "source": {"capture_event_uuid": prepared.event_uuid, "attachment": prepared.attachment}}
        return self.outbox.enqueue(encode(event))

    @property
    def legacy_resume(self):
        return self.resume_cursor.startswith(LEGACY_CURSOR_PREFIX)

    def cursor(self, prepared, *, legacy_cursor=None):
        key = digest(encode([source.post(prepared.source), prepared.attachment,
                             prepared.source.get("type")]))
        replay = bool(self.resume_cursor)
        if replay:
            self.replay_seen += 1
            candidate = legacy_cursor if self.legacy_resume else key
            if candidate == self.resume_cursor:
                self.resume_cursor = ""
            elif self.replay_seen > self.replay_limit:
                raise SourcePaused("Saved source cursor was not found within bounded replay")
        return key, replay

    def checkpoint(self, cursor, replay, completed):
        if replay:
            return
        self.items_seen += 1
        self.files_completed += int(completed)
        self.lease.progress(self.items_seen, self.files_completed, cursor)

    def traversed(self):
        if self.resume_cursor:
            raise SourcePaused("Saved source cursor was not encountered; run remains unfinished")
