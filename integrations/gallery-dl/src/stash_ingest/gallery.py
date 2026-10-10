"""Native gallery-dl lifecycle; importing this module never changes live jobs."""

import collections
import copy
import functools
import os
from pathlib import Path
import tempfile
import types
from contextlib import contextmanager
import requests

from gallery_dl import config, exception, job, version
from gallery_dl import path as gallery_path
from gallery_dl.extractor.common import Message

from . import filename, instagram, mirror, social_media, web_media
from .encoding import InvalidData
from .filesystem import destination_lock
from .runs import SourceFailure, SourcePaused, SourceTurnComplete
from .outbox import Capacity
from .publication_lock import PublicationBusy, publication_lock
from .source_window import validate_keywords
from .scan_resume import legacy_cursor

SUPPORTED_VERSION = "1.32.15-dev"


def source_url(extractor):
    # The ytdl: prefix selects an adapter; it is not part of the source URL.
    # Keep it on the extractor so gallery-dl preserves explicit-bridge options.
    if extractor.category in ('ytdl', 'ytdl-generic'):
        return extractor.ytdl_url
    return extractor.url


class SourceSession:
    """Check each source HTTP attempt, including gallery-dl's internal retries."""

    def __init__(self, producer, session):
        self.producer, self.session = producer, session

    def __getattr__(self, name):
        return getattr(self.session, name)

    def request(self, *args, **kwargs):
        self.producer.check()
        url = args[1] if len(args) >= 2 else kwargs.get("url")
        scope = self.producer.reserve_source(url)
        try:
            response = self.session.request(*args, **kwargs)
        except requests.exceptions.Timeout:
            self.producer.fail_source("timeout", scope)
        except requests.exceptions.RequestException:
            self.producer.fail_source("extraction_failed", scope)
        if response.status_code == 429:
            response.close()
            self.producer.fail_source("rate_limited", scope)
        return response


class InitializationLog:
    """Do not silently proceed when gallery-dl skips a configured processor."""

    def __init__(self, owner, logger):
        self.owner, self.logger = owner, logger

    def __getattr__(self, name):
        return getattr(self.logger, name)

    def warning(self, *args, **kwargs):
        if self.owner._native_initializing:
            raise InvalidData("Configured postprocessor could not initialize; run remains unfinished")
        return self.logger.warning(*args, **kwargs)

    def error(self, *args, **kwargs):
        if self.owner._native_initializing:
            raise InvalidData("Configured postprocessor could not initialize; run remains unfinished")
        return self.logger.error(*args, **kwargs)


def callback_owner(callback):
    while isinstance(callback, functools.partial) or hasattr(callback, "__wrapped__"):
        if isinstance(callback, functools.partial):
            callback = callback.args[0] if callback.args and callable(callback.args[0]) else callback.func
        else:
            callback = callback.__wrapped__
    return getattr(callback, "__self__", None)


@contextmanager
def worker_publication(owner, check):
    try:
        with publication_lock(owner.lock_directory, check):
            yield
    except PublicationBusy as exc:
        # gallery-dl may catch an exception in a child job or finalizer. Keep
        # the temporary stop on the shared producer so neither a parent nor
        # the worker can subsequently mistake that traversal for success.
        owner.producer.publication_failure = exc
        raise


def publication_guard(method):
    @functools.wraps(method)
    def guarded(self, *args, **kwargs):
        check = self.producer.root.verify if method.__name__ == "handle_finalize" else self.producer.check
        with worker_publication(self, check):
            return method(self, *args, **kwargs)
    return guarded


def publication_callback(owner, callback):
    @functools.wraps(callback)
    def guarded(*args, **kwargs):
        with worker_publication(owner, owner.producer.root.verify):
            return callback(*args, **kwargs)
    return guarded


@contextmanager
def atomic_text(path, encoding="utf-8", newline=None):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=".stash-write-", suffix=".tmp", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding=encoding, newline=newline) as output:
            yield output
            output.flush()
            os.fsync(output.fileno())
        if path.exists():
            os.chmod(temporary, path.stat().st_mode & 0o777)
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def twitter_collection_queue(extractor, url, data):
    """A profile dispatcher routes to dated post collections, not a post itself."""
    from gallery_dl.extractor import twitter

    child = data.get("_extractor")
    allowed = (twitter.TwitterTimelineExtractor, twitter.TwitterTweetsExtractor,
               twitter.TwitterMediaExtractor, twitter.TwitterWithRepliesExtractor,
               twitter.TwitterHighlightsExtractor, twitter.TwitterLikesExtractor)
    return (isinstance(extractor, twitter.TwitterUserExtractor)
            and set(data) <= {"_extractor", "category", "subcategory"}
            and data.get("category", "twitter") == "twitter"
            and data.get("subcategory", "user") == "user"
            and child in allowed and child.from_url(url) is not None)


def twitter_evidence(extractor):
    """Keep original membership before gallery-dl turns videos into previews."""
    if not all(hasattr(extractor, name) for name in ("_extract_files", "_extract_media_source", "_transform_tweet")):
        return
    extract_files, transform, media_source = extractor._extract_files, extractor._transform_tweet, extractor._extract_media_source
    captured = {}

    def files(self, data, tweet):
        entries = (data.get("extended_entities") or {}).get("media")
        captured["tweet"] = tweet
        captured["media"] = copy.deepcopy(entries) if isinstance(entries, list) else None
        return extract_files(data, tweet)

    def transform_tweet(self, tweet):
        data = dict(transform(tweet))
        # The public transformer keeps reply_id/conversation_id but otherwise
        # loses the stable account ID needed to distinguish self-replies.
        legacy = tweet.get("legacy", tweet)
        if legacy.get("in_reply_to_user_id_str"):
            data["reply_user_id"] = legacy["in_reply_to_user_id_str"]
        if captured.get("tweet") is tweet and captured.get("media") is not None:
            if isinstance(data.get("legacy"), dict):
                data["legacy"] = {**data["legacy"], "extended_entities": {"media": captured["media"]}}
            else:
                data["extended_entities"] = {"media": captured["media"]}
        # _transform_tweet's first-media helper must not propagate that first
        # attachment ID onto every output through file.update(tdata).
        data.pop("media_id", None)
        return data

    def file_source(self, dest, media):
        result = media_source(dest, media)
        value = media.get("id_str", media.get("id"))
        if value is not None:
            dest["media_id"] = value
        return result

    extractor._extract_files = types.MethodType(files, extractor)
    extractor._transform_tweet = types.MethodType(transform_tweet, extractor)
    extractor._extract_media_source = types.MethodType(file_source, extractor)


class NativeDownloadJob(job.DownloadJob):
    """Use only with a claimed Producer and a shared persistent lock directory.

    Child jobs inherit these objects through gallery-dl's normal queue path.
    Callers own configuration fingerprints, heartbeat startup and final outcome.
    """

    def __init__(self, extractor, parent=None, *, producer=None, lock_directory=None):
        if version.__version__ != SUPPORTED_VERSION:
            raise InvalidData("Gallery-dl runtime changed; validate the native adapter before use")
        self.producer = producer if producer is not None else parent.producer
        self.lock_directory = Path(lock_directory if lock_directory is not None else parent.lock_directory).resolve(strict=True)
        self.producer.check()
        self._native_prepared, self._native_lock, self._native_url = None, None, None
        self._native_phase = None
        self._native_initializing = False
        self._native_parent = parent
        self._native_source_date = None if parent is None else parent._native_queued_date
        self._native_queued_date = None
        self._native_collection_child = False
        self._native_collection_route = None
        self._native_scope = None
        super().__init__(extractor, parent)
        extractor = self.extractor
        if parent is None and self.producer.source_category is not None and extractor.category != self.producer.source_category:
            raise InvalidData("Extractor does not match this worker profile's source category")
        if parent is None and source_url(extractor) != self.producer.lease.run["target_url"]:
            raise InvalidData("Extractor target differs from the claimed collection")
        if (parent is not None and self._native_source_date is None and not self.producer.window.traversal
                and not (parent._native_collection_child and extractor.category == parent.extractor.category
                         and extractor.category in ('instagram', 'bluesky', 'tiktok', 'twitter'))):
            raise InvalidData("Child extraction has no approved source-post window")
        self.producer.window.configure(extractor, inherited=parent is not None)
        if hasattr(extractor, "_async_items"):
            extractor.items = extractor._async_items
        request = extractor.request

        def guarded_request(*args, **kwargs):
            self.producer.check()
            if self._native_scope is None:
                raise InvalidData("Source request preceded its service reservation")
            # Only the source request receives this proxy. The downloader's
            # session remains usable while its current file finishes.
            if len(args) >= 3:
                args = list(args)
                args[2] = SourceSession(self.producer, args[2] or extractor.session)
            else:
                kwargs["session"] = SourceSession(self.producer, kwargs.get("session") or extractor.session)
            return request(*args, **kwargs)

        extractor.request = guarded_request

    def run(self):
        self.producer.check()
        validate_keywords(self.extractor)
        try:
            result = super().run()
            if self.producer.source_failure is not None:
                raise self.producer.source_failure
            if self.producer.publication_failure is not None:
                raise self.producer.publication_failure
            if self._native_parent is None and not result:
                self.producer.traversed()
            return result
        finally:
            self._release()

    def _init(self):
        self.producer.check()
        self._native_scope = self.producer.reserve_source(source_url(self.extractor))
        self._source_operation(super()._init)
        if self.extractor.category in web_media.CATEGORIES:
            web_media.install(self.extractor, self.producer, self._native_scope)
        elif self.extractor.category == "twitter":
            twitter_evidence(self.extractor)
        elif self.extractor.category == "instagram":
            instagram.install(self.extractor)
        elif self.extractor.category in ("kemono", "coomer"):
            mirror.install(self.extractor)
        elif self.extractor.category in ('bluesky', 'tiktok'):
            social_media.install(self.extractor)
        elif self.extractor.category in ('ytdl', 'ytdl-generic'):
            from . import ytdl_source
            ytdl_source.install(self.extractor, self.producer)

    def _source_operation(self, call, *args):
        try:
            return call(*args)
        except (InvalidData, Capacity, SourcePaused, SourceFailure, SourceTurnComplete, exception.ControlException):
            raise
        except (exception.ExtractionError, requests.exceptions.RequestException) as exc:
            if isinstance(exc, exception.AuthenticationError):
                code = "authentication"
            elif isinstance(exc, exception.AuthorizationError):
                code = "access_denied"
            elif isinstance(exc, exception.ChallengeError):
                code = "challenge"
            elif isinstance(exc, exception.NotFoundError) or getattr(exc, "status", None) == 404:
                code = "not_found"
            elif isinstance(exc, requests.exceptions.Timeout):
                code = "timeout"
            elif getattr(exc, "status", None) == 429:
                code = "rate_limited"
            elif getattr(exc, "status", None) in (401, 403):
                code = "access_denied"
            else:
                code = "extraction_failed"
            self.producer.fail_source(code, self._native_scope)

    def dispatch(self, messages):
        def guarded():
            self.producer.check()
            iterator = iter(messages)
            while True:
                self.producer.check()
                try:
                    kind, url, data = self._source_operation(next, iterator)
                except StopIteration:
                    return
                if kind == Message.Queue and (instagram.collection_queue(self.extractor, url, data)
                                              or social_media.collection_queue(self.extractor, url, data)
                                              or twitter_collection_queue(self.extractor, url, data)):
                    # This is routing to a post collection, not a dated post.
                    # The child applies the original window to each source item.
                    self._native_collection_route = (url, data["_extractor"])
                    try:
                        yield kind, url, dict(data)
                    finally:
                        self._native_collection_route = None
                    continue
                value = self._native_source_date or self.producer.window.published(data, self.extractor.category)
                if self.producer.window.contains(value):
                    # Upstream augments/mutates keywords in place. Preserve the
                    # extractor's post for its subsequent attachment messages.
                    yield kind, url, dict(data)
        try:
            return super().dispatch(guarded())
        except SourceTurnComplete:
            raise exception.StopExtraction() from None
        except PublicationBusy:
            raise exception.StopExtraction() from None
        except (InvalidData, Capacity, SourcePaused) as exc:
            self.producer.failure_code = ("outbox_capacity" if isinstance(exc, Capacity) else
                                          "source_lease_lost" if isinstance(exc, SourcePaused) else "source_rejected")
            raise exception.AbortExtraction(str(exc)) from None

    @publication_guard
    def initialize(self, kwdict=None):
        if kwdict is None:
            raise InvalidData("Native file processing requires an accepted source post")
        self._check_directory(kwdict)
        if not self.extractor.config("download", True):
            raise InvalidData("A native download run cannot disable downloads; use enrichment work")
        named = config.getg("postprocessor") or {}
        overrides = self.extractor.config("postprocessor-options") or {}
        for options in self.extractor.config_accumulate("postprocessors") or ():
            if isinstance(options, str):
                options = named.get(options, {"name": options})
            elif "type" in options:
                options = {**(named.get(options["type"]) or {}), **options}
            options = {**options, **overrides}
            if options.get("async"):
                raise InvalidData("Native file completion requires synchronous postprocessors")
            if "gallery_catalog_hook" in str(options.get("function", "")):
                raise InvalidData("Remove legacy catalog writers before using the native adapter")
        self._native_initializing = True
        try:
            super().initialize(kwdict)
        finally:
            self._native_initializing = False
        self.hooks = collections.defaultdict(list, self.hooks)
        self._archive_write_file = self._archive_write_skip = self._archive_write_after = False
        # Upstream does not initialize the skip exception when skip is false.
        self._native_skip_rule = self._skipexc = getattr(self, "_skipexc", None)
        if self.producer.legacy_resume and self._native_skip_rule is None:
            # Old full-history/no-skip runs did not use checkpoint stop rules.
            self.producer.resume_cursor = ""
        if self.producer.resume_cursor or self.producer.replay_archive:
            self._skipexc = None
        seen = set()
        for callbacks in list(self.hooks.values()):
            for callback in list(callbacks):
                pp = callback_owner(callback)
                if pp is None or id(pp) in seen:
                    continue
                seen.add(id(pp))
                function = getattr(pp, "function", None)
                if function and "gallery_catalog_hook" in getattr(function, "__module__", ""):
                    raise InvalidData("Remove legacy catalog writers before using the native adapter")
                if type(pp).__name__ == "MetadataPP" and getattr(pp, "omode", None) == "w":
                    pp.open = types.MethodType(lambda self, path: atomic_text(path, self.encoding, self.newline), pp)
                if type(pp).__name__ == "ExecPP":
                    original = pp._exec

                    def strict(args, shell, original=original):
                        self.producer.configuration_check()
                        result = original(args, shell)
                        if result:
                            raise InvalidData("Postprocessor failed; download remains unfinished")
                        return result

                    pp._exec = strict
        self.hooks["prepare"].insert(0, self._prepare)
        self.hooks["file"].insert(0, self._postprocess)
        self.hooks["file"].append(self._before_rename)
        self.hooks["after"].append(self._complete)
        self.hooks["skip"].insert(0, self._repair_skip)
        self.hooks["skip"].append(functools.partial(self._complete, skipped=True))
        self.hooks["error"].insert(0, self._download_error)
        for event, callbacks in self.hooks.items():
            self.hooks[event] = [publication_callback(self, callback) for callback in callbacks]
        filename.install(self)
        original_exists = self.pathfmt.exists

        def exists():
            if original_exists():
                return True
            # The converter atomically publishes MKV before deleting GIF. A
            # crash before our completion/archive write must recover that final
            # output even if the website can no longer serve the original.
            return bool(self.extractor.config("skip", True) and self._converted_gif(self.pathfmt))

        self.pathfmt.exists = exists
        original_download = self.download

        def download(url):
            if self._native_prepared is None:
                raise InvalidData("Download has no durable source capture")
            self.producer.report_download(self._native_prepared, "started")
            self._native_phase = "download"
            return original_download(url)

        self.download = download

    def get_logger(self, name):
        logger = super().get_logger(name)
        return InitializationLog(self, logger) if name == "postprocessor" else logger

    @publication_guard
    def handle_directory(self, kwdict):
        self.producer.check()
        if self.pathfmt is not None:
            self._check_directory(kwdict)
        super().handle_directory(kwdict)
        self.producer.relative(self.pathfmt.realdirectory)

    def _check_directory(self, kwdict):
        self.producer.check()
        candidate = copy.copy(self.pathfmt) if self.pathfmt is not None else gallery_path.PathFormat(self.extractor)
        # Gallery-dl's set_directory only formats paths. Validate the proposed
        # destination before its init/post callbacks can touch the filesystem.
        candidate.set_directory(dict(kwdict))
        self.producer.relative(candidate.realdirectory)

    @publication_guard
    def handle_url(self, url, kwdict):
        self.producer.check()
        self._native_prepared, self._native_phase = None, None
        if self.extractor.category in ("kemono", "coomer", *web_media.CATEGORIES) and kwdict.get('extension', '').lower() not in mirror.VISUAL:
            # Keep non-playable source evidence, but do not download audio or
            # archives which cannot produce a supported native file receipt.
            kept = dict(kwdict, _url=url, source_extractor_url=source_url(self.extractor),
                        native_file_exclusion='unsupported_image_or_video_extension')
            prepared = self.producer.prepare(kept)
            self.producer.report_download(prepared, "excluded", reason_code="unsupported_media")
            old_cursor = None
            if self.producer.legacy_resume:
                candidate = copy.copy(self.pathfmt)
                candidate.set_filename(dict(kwdict))
                old_cursor = legacy_cursor(self, candidate)
            cursor, replay = self.producer.cursor(prepared, legacy_cursor=old_cursor)
            self.producer.checkpoint(cursor, replay, False)
            self.log.info('Excluded unsupported source file type: %s', kwdict.get('extension', ''))
            return
        self._native_url = url
        try:
            return super().handle_url(url, kwdict)
        except InvalidData:
            if self._native_phase == "postprocess":
                self.producer.report_download(self._native_prepared, "failed", reason_code="postprocess_failed")
            raise
        except SourceFailure:
            if self._native_phase == "download":
                self.producer.report_download(self._native_prepared, "failed", reason_code="source_failure")
            raise
        except (OSError, requests.exceptions.RequestException):
            if self._native_phase in ("download", "postprocess"):
                self.producer.report_download(self._native_prepared, "failed", reason_code=(
                    "postprocess_failed" if self._native_phase == "postprocess" else "download_failed"))
            raise
        finally:
            self._release()

    @publication_guard
    def handle_finalize(self):
        return super().handle_finalize()

    def handle_queue(self, url, kwdict):
        self.producer.check()
        if self._native_collection_route == (url, kwdict.get("_extractor")):
            self._native_collection_child = True
            try:
                return super().handle_queue(url, kwdict)
            finally:
                self._native_collection_child = False
        value = self._native_source_date or self.producer.window.published(kwdict, self.extractor.category)
        if not self.producer.window.contains(value):
            return
        self._native_queued_date = value
        try:
            return super().handle_queue(url, kwdict)
        finally:
            self._native_queued_date = None

    def _release(self):
        if self._native_lock is not None:
            self._native_lock.__exit__(None, None, None)
            self._native_lock = None

    def _prepare(self, pathfmt):
        self._release()
        pathfmt.kwdict.pop("_meta_path", None)
        filename.install(self).begin(pathfmt.kwdict)
        kept = dict(pathfmt.kwdict, _url=self._native_url, source_extractor_url=source_url(self.extractor))
        self._native_prepared = self.producer.prepare(kept)
        old_cursor = legacy_cursor(self, pathfmt) if self.producer.legacy_resume else None
        self._native_cursor, self._native_replay = self.producer.cursor(self._native_prepared, legacy_cursor=old_cursor)
        if not self.producer.resume_cursor and not self.producer.replay_archive and self._skipexc is None:
            self._skipexc = self._native_skip_rule
            if self._native_replay:
                self._skipcnt = -1
        saved = {key: getattr(pathfmt, key, None) for key in ("filename", "path", "realpath", "temppath")}
        try:
            pathfmt.build_path()
            relative = self.producer.relative(pathfmt.realpath)
        finally:
            for key, value in saved.items():
                setattr(pathfmt, key, value)
        # Transformations can change the extension; retain one lock for the
        # logical output stem and all its temporary/final encodings.
        identity = str(Path(relative).with_suffix(""))
        lock = destination_lock(self.lock_directory, self.producer.context["root_uuid"], identity, self.producer.check)
        lock.__enter__()
        self._native_lock = lock

    def _before_rename(self, pathfmt):
        path = Path(pathfmt.temppath)
        if path.is_file():
            self.producer.relative(path)
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)

    def _postprocess(self, pathfmt):
        self._native_phase = "postprocess"

    def _download_error(self, pathfmt):
        if self._native_prepared is None:
            raise InvalidData("Failed download has no durable source capture")
        self.producer.report_download(self._native_prepared, "failed", reason_code="download_failed")
        self._native_phase = None
        self._release()

    def _repair_skip(self, pathfmt):
        if self.archive is not None and self.archive.check(pathfmt.kwdict):
            return
        if not pathfmt.extension or not Path(pathfmt.realpath).is_file():
            return
        self._postprocess(pathfmt)
        for event in ("file", "after"):
            for callback in list(self.hooks[event]):
                if type(callback_owner(callback)).__name__ in {"MetadataPP", "ExecPP"}:
                    callback(pathfmt)

    def _complete(self, pathfmt, skipped=False):
        if self._native_prepared is None:
            raise InvalidData("File completion has no durable source capture")
        path = Path(pathfmt.realpath) if pathfmt.extension and pathfmt.realpath else None
        original_path = None
        converted = self._converted_gif(pathfmt)
        if converted is not None:
            original_path = path
            path = converted
        completed = path is not None and path.is_file()
        if not completed and not skipped:
            raise InvalidData("Successful download has no resolved final file; it remains unfinished")
        if completed:
            self.producer.complete(self._native_prepared, path, original_path=original_path)
            self._native_phase = None
            if self.archive is not None:
                self.archive.add(pathfmt.kwdict)
        else:
            archived = self.archive is not None and self.archive.check(pathfmt.kwdict)
            self.producer.report_download(self._native_prepared, "skipped", reason_code=(
                "archive_entry_without_file" if archived else "existing_without_file"))
            self._native_phase = None
        self._release()
        try:
            self.producer.checkpoint(self._native_cursor, self._native_replay, completed)
        except SourcePaused:
            # Completed bytes and their queued event survive a lost lease or
            # outage. The next source boundary stops before more extraction.
            pass

    def _converted_gif(self, pathfmt):
        if pathfmt.extension and pathfmt.realpath and filename.install(self).gif:
            original = Path(pathfmt.realpath)
            if (original.suffix.lower() == ".gif" and not original.is_file()
                    and original.with_suffix(".mkv").is_file()):
                return original.with_suffix(".mkv")
        return None
