"""The ordered capture/file boundary used by the gallery-dl adapter."""

from dataclasses import dataclass, field
from pathlib import Path
import time
import uuid

from .client import Unavailable
from .encoding import InvalidData, digest, encode, identifier, utc_now
from .retention import POLICY, retain
from .runs import SourceFailure, SourcePaused, SourceTurnComplete
from .source_window import SourceWindow
from . import source
from .scan_resume import PREFIX as LEGACY_CURSOR_PREFIX


@dataclass(frozen=True)
class Prepared:
    event_uuid: str
    attachment: dict
    source: dict
    transfer_sequence: int
    owner_uuid: str
    fence: int
    _reports: dict = field(default_factory=dict, repr=False, compare=False)


class Producer:
    def __init__(self, outbox, lease, root, *, extractor_version, configuration_check=None, source_category=None,
                 clock=time.monotonic, scan_mode='initial'):
        lease.check()
        run = lease.run
        if (outbox.producer != lease.client.producer or outbox.endpoint != lease.client.endpoint
                or run["operation"] != "download" or run.get("root_uuid") is None):
            raise InvalidData("Producer must own a download run at its permitted media root")
        self.outbox, self.lease, self.root = outbox, lease, root
        self.source_category = source_category
        self.scan_mode = scan_mode
        self.routine_post_items = 0
        self.routine_post_new = False
        self.routine_post_attachments = set()
        if configuration_check is not None and not callable(configuration_check):
            raise InvalidData("Invalid worker configuration check")
        self.configuration_check = configuration_check or (lambda: None)
        self.failure_code = None
        self.source_failure = None
        self.publication_failure = None
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
        self.owner_uuid = identifier(run.get("owner_uuid"))
        self.fence = run.get("fence")
        if self.owner_uuid != lease.owner or type(self.fence) is not int or not 1 <= self.fence <= 2**53 - 1:
            raise InvalidData("Download reporting requires the owned source-run attempt")
        self.extractor_version = extractor_version
        self.items_seen = run["progress"]["items_seen"]
        self.files_completed = run["progress"]["files_completed"]
        self.resume_cursor = run["progress"]["cursor"]
        self.last_cursor = self.resume_cursor
        self.pagination = run['progress'].get('pagination')
        if self.pagination is not None:
            from .pagination import validate
            self.pagination = validate(self.pagination)
            self.resume_cursor = self.pagination['item']
        self.saved_pagination = self.pagination
        self.pagination_started = False
        self.pending_page = None
        self.turn_replay = bool(self.resume_cursor)
        self.clock = clock
        self.resumed_work_deadline = None
        recovery = run.get("recovery") or {}
        self.replay_archive = recovery.get("replay_archive", False)
        if type(self.replay_archive) is not bool:
            raise InvalidData("Invalid archive recovery policy")
        self.replay_seen = 0
        self.replay_limit = max(4096 if self.saved_pagination else 64, self.items_seen + 64)

    def check(self, *, turn=True):
        self.lease.check()
        self.root.verify()
        self.configuration_check()
        if self.source_failure is not None:
            raise self.source_failure
        if self.publication_failure is not None:
            raise self.publication_failure
        if not turn:
            return
        if not self.turn_replay:
            self.lease.check_turn()
        elif not self.resume_cursor:
            # Replay may consume the entire server turn. Give useful work one
            # bounded slice after it catches up, shared by every child extractor.
            # Lease ownership, cancellation and source cooldowns still apply.
            now = self.clock()
            if self.resumed_work_deadline is None:
                self.resumed_work_deadline = now + 300
            if now >= self.resumed_work_deadline:
                raise SourceTurnComplete("Resumed source time budget reached; checkpointing for the next turn")

    def fail_source(self, code, scope):
        self.check(turn=False)
        if self.source_failure is None:
            self.source_failure = SourceFailure(code, scope)
        raise self.source_failure from None

    def reserve_source(self, url):
        self.check()
        try:
            return self.lease.reserve_source(url)
        except SourceFailure as exc:
            self.fail_source(exc.code, exc.scope)

    def request_source(self, url):
        from . import request_budget
        try:
            return request_budget.request(self, url)
        except (Unavailable, InvalidData, SourcePaused):
            self.lease.failure = 'source_request_unavailable'
            raise SourcePaused('Cannot confirm shared source request budget') from None

    def report_source(self, permit, response=None):
        from . import request_budget
        try:
            return request_budget.report(self, permit, response)
        except (Unavailable, InvalidData, SourcePaused):
            self.lease.failure = 'source_request_report_unavailable'
            raise SourcePaused('Source request result remains unconfirmed') from None

    def relative(self, path):
        relative = self.root.relative(path)
        if (self.path_prefix != "." and relative != self.path_prefix
                and not relative.startswith(self.path_prefix + "/")):
            raise InvalidData("Destination is outside the claimed collection prefix")
        return relative

    def prepare(self, metadata):
        return self._prepare(metadata)

    def deleted(self, metadata, attachment, status):
        # The parent post identifies the missing external attachment even when
        # its deleted API item has no downloadable rendition. This is a failed
        # source transfer, never a download/archive acknowledgement.
        prepared = self._prepare(dict(metadata, native_media_status=status), attachment=attachment)
        self.report_download(prepared, 'failed', reason_code='source_failure')
        self.routine_post_new = True
        cursor, replay = self.cursor(prepared)
        self.checkpoint(cursor, replay, False)

    def _prepare(self, metadata, *, attachment=None):
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
        # Outbox sequence numbers survive acknowledgement and restart. They
        # order transfers without a per-process counter that can collide when
        # several child extractors share one source attempt.
        sequence = self.outbox.db.execute("SELECT seq FROM events WHERE event_uuid=?", (event_id,)).fetchone()[0]
        if not 1 <= sequence <= 2**53 - 1:
            raise InvalidData("Download transfer sequence is outside the supported range")
        return Prepared(event_id, source.attachment(kept) if attachment is None else attachment,
                        kept, sequence, self.owner_uuid, self.fence)

    def report_download(self, prepared, state, *, file_event_uuid=None, reason_code=None):
        # Reporting a finished/failed current file does not require a live
        # lease. The server validates its original historical run attempt.
        phase = "started" if state == "started" else "terminal"
        intent = {"state": state}
        if file_event_uuid is not None:
            intent["file_event_uuid"] = file_event_uuid
        if reason_code is not None:
            intent["reason_code"] = reason_code
        previous = prepared._reports.get(phase)
        if previous is None:
            event = {**self.context, "event_uuid": str(uuid.uuid4()), "kind": "attachment.download",
                     "observed_at": utc_now(), "owner_uuid": prepared.owner_uuid, "fence": prepared.fence,
                     "transfer_sequence": prepared.transfer_sequence, "capture_event_uuid": prepared.event_uuid,
                     "attachment": prepared.attachment, **intent}
            previous = prepared._reports[phase] = (intent, encode(event))
        elif previous[0] != intent:
            raise InvalidData("A download transfer already has a different outcome")
        # Preserve exact bytes if enqueue was interrupted, or a fallback URL
        # asks to record the same start again.
        return self.outbox.enqueue(previous[1])

    def complete(self, prepared, path, *, original_path=None):
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
        if original_path is not None:
            event["transformation"] = {"kind": "gif-to-video", "original_relative_path": self.relative(original_path)}
        event_id = self.outbox.enqueue(encode(event))
        self.report_download(prepared, "downloaded", file_event_uuid=event_id)
        return event_id

    @property
    def legacy_resume(self):
        return self.resume_cursor.startswith(LEGACY_CURSOR_PREFIX)

    def cursor(self, prepared, *, legacy_cursor=None):
        key = digest(encode([source.post(prepared.source), prepared.attachment,
                             prepared.source.get("type")]))
        if self.saved_pagination is not None and not self.pagination_started:
            # A profile can revisit stories or another feed before reaching
            # its saved page. Even the same media there cannot acknowledge the
            # unfinished attachment within the original feed's page.
            return key, True
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
            if not self.resume_cursor and self.pending_page is not None and self.saved_pagination is None:
                # Upgrade an old item-only checkpoint only after finding its
                # anchor. Earlier pages must not replace its useful progress.
                self.pagination = {**self.pending_page, 'item': self.last_cursor}
                self.pagination_started = True
                self._progress()
            return
        self.items_seen += 1
        self.files_completed += int(completed)
        self.last_cursor = cursor
        if self.pagination is not None:
            self.pagination = {**self.pagination, 'item': cursor}
        self._progress()

    def _progress(self):
        extra = {} if self.pagination is None else {'pagination': dict(self.pagination)}
        self.lease.progress(self.items_seen, self.files_completed, self.last_cursor, **extra)

    def begin_page(self, page):
        """Checkpoint a page start before fetching it; never skip a partial page."""
        from .pagination import validate
        page = validate(page)
        self.pending_page = page
        if self.saved_pagination is None and self.resume_cursor:
            return
        if not self.pagination_started and self.saved_pagination is not None:
            expected = {**self.saved_pagination, 'item': ''}
            if page != expected:
                raise InvalidData('Source pagination does not match the saved extractor and page')
            self.pagination_started = True
            return
        if self.pagination is not None and page == {**self.pagination, 'item': ''}:
            return  # A retry of the same HTTP page has not consumed that page.
        if self.pagination_started and self.resume_cursor:
            # The partial page was replayed in full, including any new or moved
            # items. Its old attachment may have been removed by the source.
            self.resume_cursor = ''
        self.pagination_started = True
        self.pagination = page
        self._progress()

    def traversed(self):
        if self.saved_pagination is not None and not self.pagination_started:
            raise SourcePaused('Saved pagination extractor was not reached')
        if self.saved_pagination is not None and self.pagination_started:
            # The source may have removed the last checkpointed item from its
            # final page. The whole page was processed through normal delivery.
            self.resume_cursor = ''
        if self.resume_cursor:
            raise SourcePaused("Saved source cursor was not encountered; run remains unfinished")
