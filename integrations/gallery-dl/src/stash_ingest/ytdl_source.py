"""Fence yt-dlp's source requests without interrupting an active media transfer.

The pinned gallery-dl bridge creates its YoutubeDL instance inside items() and
reuses it for downloads. Its source requests do not use extractor.request.
Wrap construction only while advancing that iterator; restore the factory
before handing each message back to gallery-dl's downloader.
"""

from contextlib import contextmanager
import functools
import threading

from gallery_dl import ytdl
from yt_dlp.networking.exceptions import HTTPError, RequestError

from .encoding import InvalidData
from .outbox import Capacity
from .runs import SourceFailure, SourcePaused, SourceTurnComplete

_CONSTRUCTION = threading.RLock()
_CONTROL = (InvalidData, Capacity, SourceFailure, SourcePaused, SourceTurnComplete)


class SourceExtraction:
    def __init__(self, extractor, producer):
        self.extractor, self.producer = extractor, producer
        self.active = False
        self.failure = None

    def checked(self, callback, *args, **kwargs):
        if self.failure is not None:
            raise self.failure
        try:
            return callback(*args, **kwargs)
        except _CONTROL as error:
            # The bridge and yt-dlp's ignoreerrors handling can catch these.
            # They must still stop traversal at the outer iterator boundary.
            self.failure = error
            raise

    def protect(self, client):
        request, report_error = client.urlopen, client.report_error
        extract_info = client._YoutubeDL__extract_info
        first = True

        @functools.wraps(extract_info)
        def initial_info(*args, **kwargs):
            nonlocal first
            initial, first = first, False
            result = extract_info(*args, **kwargs)
            if initial and self.active and result and result.get('_type') in ('url', 'url_transparent'):
                # The bridge requests process=False for the initial URL, unlike
                # playlist children. ThisVid delegates to Generic and has no
                # final video ID yet. Resolve that reference while source checks
                # still apply and before a capture or file can be prepared.
                return client.process_ie_result(result, download=False)
            return result

        @functools.wraps(request)
        def source_request(req):
            if not self.active:
                # A prepared file can finish after lease loss or an API outage.
                return request(req)
            self.checked(self.producer.check)
            url = req if isinstance(req, str) else req.url if hasattr(req, 'url') else req.full_url
            scope = self.checked(self.producer.reserve_source, url)
            try:
                return request(req)
            except HTTPError as error:
                if error.status != 429:
                    # Optional probes and redirects can recover from other
                    # HTTP statuses. A terminal error reaches report_error.
                    raise
                error.close()
                self.checked(self.producer.fail_source, 'rate_limited', scope)
            except RequestError as error:
                code = 'timeout' if isinstance(error.cause, TimeoutError) else 'extraction_failed'
                self.checked(self.producer.fail_source, code, scope)

        @functools.wraps(report_error)
        def source_error(*args, **kwargs):
            if self.active and kwargs.get('is_error', True):
                self.checked(self.producer.check)
                scope = self.checked(self.producer.reserve_source, self.extractor.ytdl_url)
                try:
                    report_error(*args, **kwargs)
                except _CONTROL as error:
                    self.failure = error
                    raise
                except Exception:
                    # Keep upstream logging/recovery actions, then make both a
                    # raised DownloadError and ignoreerrors' normal return a
                    # failed native attempt. Interrupts still propagate.
                    pass
                self.checked(self.producer.fail_source, 'extraction_failed', scope)
            return report_error(*args, **kwargs)

        client.urlopen, client.report_error = source_request, source_error
        client._YoutubeDL__extract_info = initial_info

    @contextmanager
    def constructing(self):
        # Native configuration already serializes jobs within one process.
        # This lock also keeps nested fixture/bridge construction well scoped.
        with _CONSTRUCTION:
            original = ytdl.construct_YoutubeDL

            @functools.wraps(original)
            def construct(module, owner, *args, **kwargs):
                if owner is self.extractor and module.__name__ != 'yt_dlp':
                    raise InvalidData('Native source extraction requires the pinned yt-dlp runtime')
                client = original(module, owner, *args, **kwargs)
                if owner is self.extractor:
                    self.protect(client)
                return client

            ytdl.construct_YoutubeDL = construct
            self.active = True
            try:
                yield
            finally:
                self.active = False
                ytdl.construct_YoutubeDL = original

    def messages(self, original):
        iterator = iter(original())
        try:
            while True:
                self.checked(self.producer.check)
                try:
                    with self.constructing():
                        message = next(iterator)
                except Exception:
                    if self.failure is not None:
                        raise self.failure from None
                    raise
                if self.failure is not None:
                    raise self.failure
                yield message
        except StopIteration:
            return
        finally:
            # No factory override survives generator cancellation or failure.
            if hasattr(iterator, 'close'):
                iterator.close()


def install(extractor, producer):
    state = SourceExtraction(extractor, producer)
    original = extractor.items
    extractor.items = lambda: state.messages(original)
