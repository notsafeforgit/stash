"""Bounded metadata-only account pages for native source-post discovery.

A page is listing evidence, not an accepted post match or job completion.
The caller owns durable delivery, cursor advancement and account/job scope.
"""

import copy
import re
import sys
import time
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

from . import metadata_fetch
from .encoding import InvalidData, decode, encode
from .metadata_bundle import Bundle, ERRORS as METADATA_ERRORS, MAX_BYTES, _native_size, public_url
from .retention import POLICY

SCHEMA = "stash-discovery-page-v1"
MAX_RECORDS = 4096
ERRORS = METADATA_ERRORS | {"unsupported_profile", "pagination_stalled"}
KEYS = {"schema", "url", "retention_policy", "extractor_version", "cursor", "next_cursor", "complete", "records"}


def profile_platform(url):
    try:
        public_url(url)
    except UnicodeError:
        raise InvalidData("Expected a supported discovery profile") from None
    parsed = urlsplit(url)
    if (not url.startswith("https://") or parsed.netloc.lower() != parsed.hostname
            or parsed.port is not None or parsed.fragment):
        raise InvalidData("Expected a supported discovery profile")
    if (parsed.hostname in {"x.com", "www.x.com", "twitter.com", "www.twitter.com"}
            and re.fullmatch(r"/(?:id:[0-9]+|[A-Za-z0-9_]{1,30})/timeline/?", parsed.path)
            and not parsed.query):
        return "twitter"
    if (parsed.hostname in {"reddit.com", "www.reddit.com"}
            and re.fullmatch(r"/user/[A-Za-z0-9_-]{1,40}/submitted/", parsed.path)
            and parse_qsl(parsed.query, keep_blank_values=True) in ([], [("sort", "new")])):
        return "reddit"
    raise InvalidData("Expected a supported discovery profile")


def page_cursor(platform, value):
    if value is None:
        return None
    key = "cursor" if platform == "twitter" else "after"
    if not isinstance(value, dict) or set(value) != {key}:
        raise InvalidData("Invalid discovery cursor")
    token = value[key]
    try:
        size = len(token.encode("utf-8")) if isinstance(token, str) else 0
    except UnicodeError:
        raise InvalidData("Invalid discovery cursor") from None
    if (not 1 <= size <= 8192
            or any(ord(c) < 32 or ord(c) == 127 for c in token)
            or (platform == "reddit" and not re.fullmatch(r"t3_[a-z0-9]+", token))):
        raise InvalidData("Invalid discovery cursor")
    return {key: token}


def validate_page(value, url, extractor_version, cursor=None):
    platform = profile_platform(url)
    cursor = page_cursor(platform, cursor)
    try:
        version_size = len(extractor_version.encode("utf-8")) if isinstance(extractor_version, str) else 0
    except UnicodeError:
        raise InvalidData("Invalid discovery extractor version") from None
    if not 1 <= version_size <= 128 or any(c in extractor_version for c in "\r\n\x00"):
        raise InvalidData("Invalid discovery extractor version")
    value = decode(encode(value, MAX_BYTES), MAX_BYTES, preserve_numbers=True)
    if (not isinstance(value, dict) or set(value) != KEYS or value["schema"] != SCHEMA
            or value["url"] != url or value["retention_policy"] != POLICY
            or value["extractor_version"] != extractor_version or value["cursor"] != cursor
            or type(value["complete"]) is not bool):
        raise InvalidData("Discovery page belongs to another request or policy")
    following = page_cursor(platform, value["next_cursor"])
    if ((value["complete"] and following is not None)
            or (not value["complete"] and (following is None or following == cursor))):
        raise InvalidData("Discovery page does not advance its cursor")
    # Reuse compact record reconstruction, retention, timestamp and expansion
    # checks. Discovery permits empty pages and never resumes child fetches.
    transcript = Bundle(url, extractor_version, max_records=MAX_RECORDS).checkpoint()
    transcript["records"] = value["records"]
    value["records"] = Bundle(url, extractor_version, transcript, max_records=MAX_RECORDS).checkpoint()["records"]
    _native_size(value, MAX_BYTES)
    return value


class _PageComplete(Exception):
    pass


def collect(url, settings, cursor=None, *, factory=None, check=lambda: None, reserve_source=None):
    from gallery_dl import config, extractor, util, version
    from gallery_dl.extractor.common import Extractor, Message
    from .gallery import SUPPORTED_VERSION, twitter_evidence

    if version.__version__ != SUPPORTED_VERSION:
        return {"error": "runtime_changed"}
    try:
        platform = profile_platform(url)
    except InvalidData:
        return {"error": "unsupported_profile"}
    try:
        cursor = page_cursor(platform, cursor)
    except InvalidData:
        return {"error": "invalid_checkpoint"}
    try:
        config.clear()
        config._config.update(metadata_fetch.safe_config(settings))
        Extractor.request_timestamp = time.time()
        request_url = url
        if platform == "twitter":
            config._config["extractor"].setdefault("twitter", {}).update({
                "cursor": (cursor or {}).get("cursor") or True, "search-pagination": "cursor",
                "articles": False, "ratelimit": "abort", "showmore": False, "quoted-expand": False,
            })
        elif cursor:
            parsed = urlsplit(url)
            query = [*parse_qsl(parsed.query), ("after", cursor["after"])]
            request_url = urlunsplit((parsed.scheme, parsed.netloc, parsed.path, urlencode(query), ""))
        check()
        if reserve_source is not None and not reserve_source(url):
            raise metadata_fetch.SourceBusy()
        target = (factory or extractor.find)(request_url)
        if (target is None or target.category != platform
                or type(target).subcategory != ("timeline" if platform == "twitter" else "user")):
            return {"error": "unsupported_profile"}
        target.initialize()
        pause = util.build_duration_func(target.config("sleep-extractor"))
        if pause is not None:
            target.sleep(pause(), "extractor")
        bundle = Bundle(url, version.__version__, max_records=MAX_RECORDS)
        result = {"schema": SCHEMA, "url": url, "retention_policy": POLICY,
                  "extractor_version": version.__version__, "cursor": cursor,
                  "next_cursor": None, "complete": False, "records": []}
        if platform == "twitter":
            twitter_evidence(target)
            original_update = target._update_cursor

            def update(value):
                returned = original_update(value)
                if value:
                    result["next_cursor"] = {"cursor": target._cursor}
                    raise _PageComplete()
                return returned

            target._update_cursor = update
        else:
            original_call, seen = target.api._call, False

            def call(endpoint, params=None, *args, **kwargs):
                nonlocal seen
                listing = endpoint.startswith("/user/") and endpoint.endswith("/.json")
                if listing and seen:
                    raise _PageComplete()
                payload = original_call(endpoint, params, *args, **kwargs)
                if listing:
                    seen = True
                    after = payload["data"]["after"]
                    result["next_cursor"] = {"after": after} if after else None
                return payload

            target.api._call = call
        base = None
        try:
            for kind, media_url, original in target:
                check()
                data = {**original, **target.kwdict, "category": platform,
                        "subcategory": target.subcategory, "source_extractor_url": url}
                if kind == Message.Directory:
                    base = bundle.append("post", data, base=base)
                elif kind == Message.Url:
                    data["_url"] = media_url
                    bundle.append("media", data, base=base)
                elif kind == Message.Queue:
                    # Preserve parent evidence and the external reference;
                    # account discovery never follows or downloads the child.
                    data["discovery_external_reference"] = public_url(media_url)
                    bundle.append("post", data, base=base)
            if target.status:
                raise RuntimeError("Account listing did not finish")
            result["complete"], result["next_cursor"] = True, None
        except _PageComplete:
            if result["next_cursor"] is None or result["next_cursor"] == cursor:
                return {"error": "pagination_stalled"}
        result["records"] = bundle.checkpoint()["records"]
        return validate_page(result, url, version.__version__, cursor)
    except InvalidData:
        return {"error": "result_too_large"}
    except Exception as exc:
        return {"error": metadata_fetch.classify(exc)}


def fetch(url, settings, cursor=None, *, timeout=180, check=lambda: None, reserve_source=None):
    from .gallery import SUPPORTED_VERSION
    cursor = page_cursor(profile_platform(url), cursor)
    metadata_fetch.safe_config(settings)
    if type(timeout) not in {int, float} or not 0 < timeout <= 600:
        raise InvalidData("Discovery fetch timeout must be at most ten minutes")
    request = {"url": url, "settings": settings, "resume": copy.deepcopy(cursor)}
    if reserve_source is not None:
        request["source_pacing"] = True
    try:
        result = metadata_fetch._exchange([sys.executable, "-B", "-m", "stash_ingest.discovery_fetch"],
                                          encode(request, metadata_fetch.INPUT_LIMIT), timeout, check, reserve_source)
        if (isinstance(result, dict) and set(result) == {"error"}
                and isinstance(result["error"], str) and result["error"] in ERRORS):
            return result
        return validate_page(result, url, SUPPORTED_VERSION, cursor)
    except (InvalidData, OSError):
        return {"error": "worker_failed"}


if __name__ == "__main__":
    raise SystemExit(metadata_fetch.main(collect))
