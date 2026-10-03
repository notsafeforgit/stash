"""Isolated metadata-only gallery-dl extraction for native enrichment.

No DownloadJob, path formatting, archive or postprocessor is constructed.
Use fetch() from a worker; collect() owns process-global gallery-dl state and
is only for the isolated child and fixtures.
"""

import copy
import logging
import os
import selectors
import signal
import subprocess
import sys
import time

from .encoding import InvalidData, decode, encode
from .metadata_bundle import Bundle, ERRORS, MAX_BYTES, public_url

NETWORK_KEYS = frozenset({"username", "password", "cookies", "cookies-domain", "cookies-select",
                          "cookies-from-browser", "client-id", "client-secret", "refresh-token", "access-token",
                          "user-agent", "browser", "headers", "proxy", "source-address", "verify",
                          "sleep-request", "sleep-extractor", "api", "endpoint", "api-key", "token"})
POSTS = {"reddit": {"submission"}, "twitter": {"tweet"}, "bluesky": {"post"},
         "tiktok": {"post"}, "instagram": {"post", "reel"}, "kemono": {"post"},
         "coomer": {"post"}, "patreon": {"post"}, "fansly": {"post"}}
CHILDREN = {"redgifs": {"image"}, "imgur": {"image", "album", "gallery"}}
INPUT_LIMIT = MAX_BYTES + (2 << 20)


def is_post(target):
    # Kemono/coomer replace the instance subcategory with the mirror service
    # for config lookup. The extractor class still distinguishes a post from
    # a whole creator feed; the instance label cannot make that decision.
    return type(target).subcategory in POSTS.get(target.category, ())


def _configure_context(target, bundle, parent):
    """Preserve gallery-dl parent-specific access settings on child-only retry.

    Mirrors the pinned Job._build_config_path, without constructing a job.
    Category transfer is always disabled for retained source evidence.
    """
    ancestors = []
    while parent is not None:
        ancestors.append(bundle.metadata(parent)["category"])
        parent = bundle.value["records"][parent]["parent"]
    parents, previous = (), None
    for category in [*reversed(ancestors), target.category]:
        if previous is not None and category != previous and category not in parents:
            parents += (previous,)
        previous = category
    paths = []
    for category in parents:
        paths.extend([(category + ">" + target.category, target.subcategory), (category + ">*", target.subcategory)])
    if parents or target.basecategory:
        paths.append((target.category, target.subcategory))
    if target.basecategory:
        if target.basesubcategory:
            paths.append((target.basesubcategory, target.subcategory))
        paths.append((target.basecategory, target.subcategory))
    if paths:
        target._cfgpath = paths
        target.config, target.config_accumulate = target._config_shared, target._config_shared_accumulate


def safe_config(settings):
    """Retain source access/pacing while disabling writers and custom execution."""
    if not isinstance(settings, dict) or not isinstance(settings.get("extractor", {}), dict):
        raise InvalidData("Expected gallery-dl settings")
    encode(settings, 1 << 20)

    def clean(value):
        result = {}
        for key, child in value.items():
            if key in NETWORK_KEYS:
                result[key] = copy.deepcopy(child)
            elif isinstance(child, dict) and key not in {
                    "actions", "keywords", "keywords-global", "raw-options", "postprocessor", "downloader"}:
                result[key] = clean(child)
        result.update({"retries": 0, "retries-api": 0, "timeout": 30, "async": False,
                       "postprocessors": [], "archive": None, "write-pages": False,
                       "cookies-update": False, "download": False, "skip": False,
                       "original": True, "category-transfer": False})
        return result

    extractors = clean(settings.get("extractor", {}))
    extractors.setdefault("sleep-request", 5)
    extractors.setdefault("sleep-extractor", 0)
    for category, values in {
        "reddit": {"comments": 0, "morecomments": False, "recursion": 0, "previews": False, "videos": True},
        "twitter": {"replies": True, "retweets": True, "quoted": False, "conversations": False,
                    "text-tweets": True, "cards": False, "videos": True, "images": True, "twitpic": False},
    }.items():
        selected = extractors.setdefault(category, {})
        selected.update(values)
        # A more specific subcategory must not restore profile traversal.
        for subcategory in POSTS[category]:
            selected.setdefault(subcategory, {}).update(values)
    return {"extractor": extractors, "cache": {"file": ":memory:"},
            "output": {"mode": "null"}, "downloader": {"enabled": False}}


class RateLimited(RuntimeError):
    pass


def classify(exc):
    name = type(exc).__name__
    if name == "RateLimited" or getattr(exc, "status", None) == 429:
        return "rate_limited"
    if getattr(exc, "status", None) == 404:
        return "not_found"
    return {"AuthenticationError": "authentication", "AuthorizationError": "access_denied",
            "AuthRequired": "access_denied", "ChallengeError": "challenge", "NotFoundError": "not_found",
            "NoExtractorError": "unsupported_extractor", "Timeout": "timeout", "ReadTimeout": "timeout",
            "ConnectTimeout": "timeout"}.get(name, "extraction_failed")


def collect(url, settings, resume=None, *, factory=None, check=lambda: None):
    from gallery_dl import config, extractor, util, version
    from gallery_dl.extractor.common import Extractor, Message
    from .gallery import SUPPORTED_VERSION, twitter_evidence

    if version.__version__ != SUPPORTED_VERSION:
        return {"error": "runtime_changed"}
    try:
        bundle = Bundle(url, version.__version__, resume)
    except InvalidData:
        return {"error": "invalid_checkpoint" if resume is not None else "not_a_post_url"}
    # Accessors hold references to this dictionary: never replace it.
    config.clear()
    config._config.update(safe_config(settings))
    Extractor.request_timestamp = time.time()
    find = factory or extractor.find
    seen = set()

    def walk(target, parent=None, depth=0):
        check()
        _configure_context(target, bundle, parent)
        target.initialize()
        pause = util.build_duration_func(target.config("sleep-extractor"))
        if pause is not None:
            target.sleep(pause(), "extractor")
        if target.category == "twitter":
            twitter_evidence(target)
        base = None
        for kind, media_url, original in target:
            check()
            data = {**original, **target.kwdict, "category": target.category,
                    "subcategory": target.subcategory, "source_extractor_url": target.url}
            if kind == Message.Directory:
                base = bundle.append("post", data, parent, base)
            elif kind == Message.Url:
                data["_url"] = media_url
                bundle.append("media", data, parent, base)
            elif kind == Message.Queue:
                context = bundle.append("context", data, parent, base)
                child(media_url, context, depth + 1)
        if target.status:
            raise RuntimeError("Extractor did not finish")

    def child(url, parent, depth):
        public_url(url)
        key = url, parent
        if key in seen:
            return
        seen.add(key)
        entry = {"url": url, "parent": parent, "depth": depth, "reason": "external_reference_only"}
        check()
        # A failed child cannot leave a seemingly complete partial child. The
        # compact parent transcript is retained for a later child-only retry.
        before = copy.deepcopy(bundle.checkpoint())
        previous_seen = set(seen)
        try:
            target = find(url)
            if target is None or target.subcategory not in CHILDREN.get(target.category, ()) or depth > 2:
                bundle.reference("unresolved", entry)
                return
            walk(target, parent, depth)
        except Exception as exc:
            restored = Bundle(bundle.value["url"], version.__version__, before)
            bundle.__dict__.update(restored.__dict__)
            seen.clear()
            seen.update(previous_seen)
            entry["reason"] = "result_too_large" if isinstance(exc, InvalidData) else classify(exc)
            bundle.reference("unresolved" if entry["reason"] in {"not_found", "unsupported_extractor"}
                             else "pending", entry)

    try:
        if resume is not None:
            pending = list(bundle.value["pending"])
            bundle.value["pending"] = []
            for item in pending:
                child(item["url"], item["parent"], item["depth"])
        else:
            check()
            target = find(url)
            if target is None:
                return {"error": "unsupported_extractor"}
            if not is_post(target):
                return {"error": "not_a_post_url"}
            walk(target)
        if not any(item["kind"] == "post" for item in bundle.value["records"]):
            return {"error": "not_found"}
        return bundle.checkpoint()
    except InvalidData:
        return {"error": "result_too_large"}
    except Exception as exc:
        return {"error": classify(exc)}


def _terminate(process):
    if os.name == "posix":
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    elif process.poll() is None:
        process.kill()
    process.wait()


def _exchange(command, body, timeout, check=lambda: None):
    """Bound both pipes and wall time, including a child stuck writing logs."""
    check()
    with subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                          stderr=subprocess.DEVNULL, start_new_session=os.name == "posix") as process:
        output, offset = bytearray(), 0
        deadline = time.monotonic() + timeout
        with selectors.DefaultSelector() as selector:
            try:
                os.set_blocking(process.stdin.fileno(), False)
                os.set_blocking(process.stdout.fileno(), False)
                selector.register(process.stdin, selectors.EVENT_WRITE)
                selector.register(process.stdout, selectors.EVENT_READ)
                while selector.get_map() or process.poll() is None:
                    check()
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        _terminate(process)
                        return {"error": "timeout"}
                    interval = 1 if selector.get_map() else 0.05
                    for event, _ in selector.select(min(remaining, interval)):
                        if event.fileobj is process.stdin:
                            try:
                                offset += os.write(process.stdin.fileno(), body[offset:offset + 65536])
                            except BrokenPipeError:
                                offset = len(body)
                            if offset == len(body):
                                selector.unregister(process.stdin)
                                process.stdin.close()
                        else:
                            chunk = os.read(process.stdout.fileno(), 65536)
                            if chunk:
                                output.extend(chunk)
                                if len(output) > MAX_BYTES:
                                    _terminate(process)
                                    return {"error": "result_too_large"}
                            else:
                                selector.unregister(process.stdout)
                if process.returncode:
                    return {"error": "worker_failed"}
                return decode(bytes(output), MAX_BYTES, preserve_numbers=True)
            except BaseException:
                _terminate(process)
                raise


def fetch(url, settings, resume=None, *, timeout=180, check=lambda: None):
    """Caller owns durable checkpoint publication and source lease management."""
    from .gallery import SUPPORTED_VERSION
    public_url(url)
    safe_config(settings)
    if type(timeout) not in {int, float} or not 0 < timeout <= 600:
        raise InvalidData("Metadata fetch timeout must be at most ten minutes")
    if resume is not None:
        Bundle(url, SUPPORTED_VERSION, resume)
    body = encode({"url": url, "settings": settings, "resume": resume}, INPUT_LIMIT)
    try:
        result = _exchange([sys.executable, "-B", "-m", "stash_ingest.metadata_fetch"], body, timeout, check)
        if (isinstance(result, dict) and set(result) == {"error"}
                and isinstance(result["error"], str) and result["error"] in ERRORS):
            return result
        return Bundle(url, SUPPORTED_VERSION, result).checkpoint()
    except (InvalidData, OSError):
        return {"error": "worker_failed"}


def main():
    # Private child protocol: config/website access enters through stdin and is
    # never part of the returned transcript, logs or exception strings.
    output = os.dup(sys.stdout.fileno())
    with open(os.devnull, "w") as silent:
        os.dup2(silent.fileno(), sys.stdout.fileno())
        os.dup2(silent.fileno(), sys.stderr.fileno())
    logging.disable(logging.CRITICAL)
    runtime_validated = False
    try:
        from .configuration import runtime_identity
        runtime_identity()
        runtime_validated = True
        from gallery_dl import downloader
        import requests

        def forbidden(*args, **kwargs):
            raise RuntimeError("Media downloads are disabled during enrichment")

        downloader.find = forbidden
        send = requests.sessions.Session.send

        def guarded(session, request, **kwargs):
            response = send(session, request, **kwargs)
            if response.status_code == 429:
                response.close()
                raise RateLimited()
            return response

        requests.sessions.Session.send = guarded
        request = decode(sys.stdin.buffer.read(INPUT_LIMIT + 1), INPUT_LIMIT, preserve_numbers=True)
        if not isinstance(request, dict) or set(request) != {"url", "settings", "resume"}:
            raise InvalidData("Invalid metadata fetch request")
        result = collect(request["url"], request["settings"], request["resume"])
    except InvalidData:
        result = {"error": "invalid_checkpoint" if runtime_validated else "runtime_changed"}
    except Exception as exc:
        result = {"error": classify(exc)}
    with os.fdopen(output, "wb") as stream:
        stream.write(encode(result, MAX_BYTES))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
