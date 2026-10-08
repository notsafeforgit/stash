"""Apply the claimed source-post window before gallery-dl file processing."""

from datetime import datetime, timedelta, timezone
import math

from .encoding import InvalidData
from .source import _agree, _context, _id, _numeric, UnsupportedSource
from .windows import normalize
from . import ytdl_media, web_media

EPOCH = datetime(1970, 1, 1, tzinfo=timezone.utc)
PROTECTED_KEYWORDS = frozenset(("category", "subcategory", "date", "created_utc", "created_at",
                                "id", "tweet_id", "rest_id", "id_str", "legacy", "_reddit",
                                "post_id", "post_date", "sidecar_media_id", "media_id", "instagram_media",
                                "published", "service", "mirror_media"))


def _datetime(value):
    if isinstance(value, str) and "T" in value:
        value = datetime.fromisoformat(value)
    if not isinstance(value, datetime) or not value or value.year == 1:
        raise ValueError()
    # gallery-dl's pinned datetime helpers produce naive UTC values, never
    # machine-local time. Do not apply the worker's timezone to those values.
    if value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


def published(metadata, category):
    """Date of the source post, including its wrapper when it is a repost."""
    data, category = _context({**metadata, "category": category})
    if category not in ("reddit", "twitter", "instagram", "coomer", "kemono", "bluesky", "tiktok", 'ytdl', 'ytdl-generic', *web_media.CATEGORIES):
        raise UnsupportedSource("This extractor needs a native source-date adapter")
    try:
        if category in web_media.CATEGORIES:
            web_media.post(data, category)
            return _datetime(data.get('date'))
        if category in ('ytdl', 'ytdl-generic'):
            value = ytdl_media.published(data)
            if value is None:
                raise InvalidData('Undated or day-only yt-dlp metadata cannot prove a publication-time window')
            return value
        if category == 'bluesky':
            return _datetime(data.get('createdAt'))
        if category == 'tiktok':
            return EPOCH + timedelta(seconds=int(_numeric(_id(data.get('createTime')))))
        if category in ("coomer", "kemono"):
            # The transformed date can fall back to the mirror import date.
            # Only the original publication timestamp owns a source window.
            return _datetime(data.get("published"))
        if category == "instagram":
            from .instagram import CONTAINERS, manifest
            if data.get("type") in CONTAINERS:
                if manifest(data) is None:
                    raise ValueError()
                return _datetime(data.get("date"))
            return _datetime(data.get("post_date"))
        if category == "reddit":
            if "created_utc" in data:
                value = data["created_utc"]
                if type(value) not in (int, float) or not math.isfinite(value):
                    raise ValueError()
                return EPOCH + timedelta(seconds=value)
            return _datetime(data.get("date"))
        if category == "twitter":
            legacy = data.get("legacy") or data
            key = _agree((data.get("tweet_id"), data.get("rest_id"), legacy.get("id_str")))
            if not key.isascii() or not key.isdecimal():
                raise ValueError()
            number = int(key)
            if number >= 300_000_000_000_000:
                # gallery-dl's transformed date drops subsecond precision. The
                # original Snowflake retains the milliseconds needed at either
                # boundary, including when transform=false.
                return EPOCH + timedelta(milliseconds=(number >> 22) + 1_288_834_974_657)
            if "created_at" in legacy:
                return _datetime(datetime.strptime(legacy["created_at"], "%a %b %d %H:%M:%S %z %Y"))
            return _datetime(data.get("date"))
    except (ValueError, TypeError, OverflowError):
        raise InvalidData("Source post has no usable publication date; its window cannot be verified") from None


class SourceWindow:
    def __init__(self, value):
        self.window = normalize(value)
        self.since = datetime.fromisoformat(self.window["since"]) if self.window["since"] else None
        self.until = datetime.fromisoformat(self.window["until"])
        self.traversal = self.window.get('basis') == 'traversal'

    def contains(self, value):
        return self.traversal or ((self.since is None or value >= self.since) and value < self.until)

    def published(self, metadata, category):
        # None is an explicit absence of a date requirement for a configured
        # scan, never a replacement publication timestamp in source metadata.
        return None if self.traversal else published(metadata, category)

    def configure(self, extractor, *, inherited=False):
        """Override obsolete date limits on this instance, leaving global config alone."""
        original = extractor.config

        def configuration(key, default=None):
            if key == "init":
                # File/post hooks need an accepted source post and its checked
                # destination, even when the inherited config requests eager init.
                return "lazy"
            if self.traversal:
                return original(key, default)
            if key in ("date-after", "date-before"):
                # These generic gallery-dl predicates can stop at an old pinned
                # post. Our predicate handles out-of-order posts without abort.
                return None
            if key == "date-min":
                return default if inherited or self.since is None else (self.since - EPOCH).total_seconds()
            if key == "date-max":
                # Reddit's API filter is inclusive and may compare fractional
                # timestamps. The native predicate enforces exclusive `until`.
                return default if inherited else (self.until - EPOCH).total_seconds()
            return original(key, default)

        extractor.config = configuration


def validate_keywords(extractor):
    protected = PROTECTED_KEYWORDS
    if extractor.category in ('coomer', 'kemono'):
        protected = protected | {'user', 'path', 'file', 'attachments', 'content', 'native_file_exclusion'}
    elif extractor.category == 'bluesky':
        protected = protected | {'author', 'uri', 'embed', 'createdAt', 'filename', 'bluesky_media'}
    elif extractor.category == 'tiktok':
        protected = protected | {'createTime', 'imagePost', 'video', 'image', 'type', 'tiktok_media'}
    elif extractor.category in ('ytdl', 'ytdl-generic'):
        protected = protected | {'extractor_key', 'webpage_url', 'timestamp', 'upload_date', 'ytdl_media',
                                 'channel_id', 'uploader_id', '_type', 'entries'}
    if extractor.category in web_media.CATEGORIES:
        protected = protected | {'web_media', 'web_media_url', 'id_string', 'creator', 'photo', 'photos',
                                 'url', 'video_url', 'audio_url', 'timestamp', 'native_file_exclusion'}
    for key in ("keywords", "keywords-global"):
        values = extractor.config(key)
        if values and (not isinstance(values, dict) or protected.intersection(values)):
            raise InvalidData("Extractor keywords cannot replace native source identity or publication dates")
