"""Native gallery-dl lifecycle; importing this module never changes live jobs."""

import collections
import copy
import functools
import os
from pathlib import Path
import tempfile
import types
from contextlib import contextmanager

from gallery_dl import config, exception, job, version
from gallery_dl import path as gallery_path

from . import filename
from .encoding import InvalidData
from .filesystem import destination_lock
from .runs import SourcePaused
from .outbox import Capacity
from .source_window import published, validate_keywords

SUPPORTED_VERSION = "1.32.15-dev"


class SourceSession:
    """Check each source HTTP attempt, including gallery-dl's internal retries."""

    def __init__(self, producer, session):
        self.producer, self.session = producer, session

    def __getattr__(self, name):
        return getattr(self.session, name)

    def request(self, *args, **kwargs):
        self.producer.check()
        return self.session.request(*args, **kwargs)


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
    while isinstance(callback, functools.partial):
        callback = callback.args[0] if callback.args and callable(callback.args[0]) else callback.func
    return getattr(callback, "__self__", None)


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
        self._native_initializing = False
        self._native_parent = parent
        self._native_source_date = None if parent is None else parent._native_queued_date
        self._native_queued_date = None
        super().__init__(extractor, parent)
        extractor = self.extractor
        if parent is None and self.producer.source_category is not None and extractor.category != self.producer.source_category:
            raise InvalidData("Extractor does not match this worker profile's source category")
        if parent is None and extractor.url != self.producer.lease.run["target_url"]:
            raise InvalidData("Extractor target differs from the claimed collection")
        if parent is not None and self._native_source_date is None:
            raise InvalidData("Child extraction has no approved source-post window")
        self.producer.window.configure(extractor, inherited=parent is not None)
        if hasattr(extractor, "_async_items"):
            extractor.items = extractor._async_items
        request = extractor.request

        def guarded_request(*args, **kwargs):
            self.producer.check()
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
            if self._native_parent is None and not result:
                self.producer.traversed()
            return result
        finally:
            self._release()

    def _init(self):
        self.producer.check()
        super()._init()
        if self.extractor.category == "twitter":
            twitter_evidence(self.extractor)

    def dispatch(self, messages):
        def guarded():
            self.producer.check()
            iterator = iter(messages)
            while True:
                self.producer.check()
                try:
                    kind, url, data = next(iterator)
                except StopIteration:
                    return
                value = self._native_source_date or published(data, self.extractor.category)
                if self.producer.window.contains(value):
                    # Upstream augments/mutates keywords in place. Preserve the
                    # extractor's post for its subsequent attachment messages.
                    yield kind, url, dict(data)
        try:
            return super().dispatch(guarded())
        except (InvalidData, Capacity, SourcePaused) as exc:
            self.producer.failure_code = ("outbox_capacity" if isinstance(exc, Capacity) else
                                          "source_lease_lost" if isinstance(exc, SourcePaused) else "source_rejected")
            raise exception.AbortExtraction(str(exc)) from None

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
        self._native_skip_rule = self._skipexc
        if self.producer.resume_cursor:
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
        self.hooks["file"].append(self._before_rename)
        self.hooks["after"].append(self._complete)
        self.hooks["skip"].insert(0, self._repair_skip)
        self.hooks["skip"].append(functools.partial(self._complete, skipped=True))
        self.hooks["error"].append(lambda _: self._release())
        filename.install(self)

    def get_logger(self, name):
        logger = super().get_logger(name)
        return InitializationLog(self, logger) if name == "postprocessor" else logger

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

    def handle_url(self, url, kwdict):
        self.producer.check()
        self._native_url = url
        try:
            return super().handle_url(url, kwdict)
        finally:
            self._release()

    def handle_queue(self, url, kwdict):
        self.producer.check()
        value = self._native_source_date or published(kwdict, self.extractor.category)
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
        kept = dict(pathfmt.kwdict, _url=self._native_url, source_extractor_url=self.extractor.url)
        self._native_prepared = self.producer.prepare(kept)
        self._native_cursor, self._native_replay = self.producer.cursor(self._native_prepared)
        if not self.producer.resume_cursor and self._skipexc is None:
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

    def _repair_skip(self, pathfmt):
        if self.archive is not None and self.archive.check(pathfmt.kwdict):
            return
        if not pathfmt.extension or not Path(pathfmt.realpath).is_file():
            return
        for event in ("file", "after"):
            for callback in list(self.hooks[event]):
                if type(callback_owner(callback)).__name__ in {"MetadataPP", "ExecPP"}:
                    callback(pathfmt)

    def _complete(self, pathfmt, skipped=False):
        if self._native_prepared is None:
            raise InvalidData("File completion has no durable source capture")
        path = Path(pathfmt.realpath) if pathfmt.extension and pathfmt.realpath else None
        if (path is not None and not path.is_file() and path.suffix.lower() == ".gif"
                and filename.install(self).gif and path.with_suffix(".mkv").is_file()):
            path = path.with_suffix(".mkv")
        completed = path is not None and path.is_file()
        if not completed and not skipped:
            raise InvalidData("Successful download has no resolved final file; it remains unfinished")
        if completed:
            self.producer.complete(self._native_prepared, path)
            if self.archive is not None:
                self.archive.add(pathfmt.kwdict)
        self._release()
        try:
            self.producer.checkpoint(self._native_cursor, self._native_replay, completed)
        except SourcePaused:
            # Completed bytes and their queued event survive a lost lease or
            # outage. The next source boundary stops before more extraction.
            pass
