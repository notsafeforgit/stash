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
from .metadata_bundle import Bundle, ERRORS, MAX_BYTES, MAX_RECORDS, MAX_REFERENCES, public_url

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
    ancestors = bundle.ancestors(parent)
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


class SourceBusy(RuntimeError):
    pass


def classify(exc):
    name = type(exc).__name__
    if name == "SourceBusy":
        return "source_busy"
    if name == "RateLimited" or getattr(exc, "status", None) == 429:
        return "rate_limited"
    if getattr(exc, "status", None) == 404:
        return "not_found"
    return {"AuthenticationError": "authentication", "AuthorizationError": "access_denied",
            "AuthRequired": "access_denied", "ChallengeError": "challenge", "NotFoundError": "not_found",
            "NoExtractorError": "unsupported_extractor", "Timeout": "timeout", "ReadTimeout": "timeout",
            "ConnectTimeout": "timeout"}.get(name, "extraction_failed")


def collect(url, settings, resume=None, *, factory=None, check=lambda: None, reserve_source=None):
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
        # Initialization can authenticate or contact the website. Acquire its
        # service before initialization, including newly discovered children.
        if reserve_source is not None and not reserve_source(target.url):
            raise SourceBusy()
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
        if not any(item["kind"] == "post" or "retained_capture" in item for item in bundle.value["records"]):
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


def _exchange(command, body, timeout, check=lambda: None, reserve_source=None):
    """Bound both pipes and wall time, including a child stuck writing logs."""
    check()
    with subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                          stderr=subprocess.DEVNULL, start_new_session=os.name == "posix") as process:
        output, offset = bytearray(), 0
        paced = reserve_source is not None
        sending = body + b"\n" if paced else body
        result, finished, contacts = None, False, 0
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
                                offset += os.write(process.stdin.fileno(), sending[offset:offset + 65536])
                            except BrokenPipeError:
                                offset = len(sending)
                            if offset == len(sending):
                                selector.unregister(process.stdin)
                                if not paced:
                                    process.stdin.close()
                        else:
                            chunk = os.read(process.stdout.fileno(), 65536)
                            if chunk:
                                output.extend(chunk)
                                if len(output) > MAX_BYTES + (64 if paced else 0):
                                    _terminate(process)
                                    return {"error": "result_too_large"}
                                if paced:
                                    while b"\n" in output:
                                        line, _, rest = output.partition(b"\n")
                                        output = bytearray(rest)
                                        frame = decode(bytes(line), MAX_BYTES + 64, preserve_numbers=True)
                                        if not isinstance(frame, dict) or finished:
                                            raise InvalidData("Invalid metadata worker frame")
                                        if set(frame) == {"contact"}:
                                            contacts += 1
                                            if contacts > MAX_RECORDS + MAX_REFERENCES or offset != len(sending):
                                                raise InvalidData("Too many metadata source requests")
                                            url = public_url(frame["contact"])
                                            check()
                                            allowed = reserve_source(url)
                                            check()
                                            if type(allowed) is not bool:
                                                raise InvalidData("Invalid metadata source reservation")
                                            sending, offset = encode({"allowed": allowed}) + b"\n", 0
                                            selector.register(process.stdin, selectors.EVENT_WRITE)
                                        elif set(frame) == {"result"}:
                                            result, finished = frame["result"], True
                                        else:
                                            raise InvalidData("Invalid metadata worker frame")
                            else:
                                selector.unregister(process.stdout)
                if process.returncode:
                    return {"error": "worker_failed"}
                if paced:
                    if output or not finished:
                        raise InvalidData("Incomplete metadata worker response")
                    return result
                return decode(bytes(output), MAX_BYTES, preserve_numbers=True)
            except BaseException:
                _terminate(process)
                raise


def fetch(url, settings, resume=None, *, timeout=180, check=lambda: None, reserve_source=None):
    """Caller owns durable checkpoint publication and source lease management."""
    from .gallery import SUPPORTED_VERSION
    public_url(url)
    safe_config(settings)
    if type(timeout) not in {int, float} or not 0 < timeout <= 600:
        raise InvalidData("Metadata fetch timeout must be at most ten minutes")
    if resume is not None:
        Bundle(url, SUPPORTED_VERSION, resume)
    request = {"url": url, "settings": settings, "resume": resume}
    if reserve_source is not None:
        request["source_pacing"] = True
    body = encode(request, INPUT_LIMIT)
    try:
        result = _exchange([sys.executable, "-B", "-m", "stash_ingest.metadata_fetch"], body, timeout, check, reserve_source)
        if (isinstance(result, dict) and set(result) == {"error"}
                and isinstance(result["error"], str) and result["error"] in ERRORS):
            return result
        return Bundle(url, SUPPORTED_VERSION, result).checkpoint()
    except (InvalidData, OSError):
        return {"error": "worker_failed"}


def main(collector=None):
    # Private child protocol: config/website access enters through stdin and is
    # never part of the returned transcript, logs or exception strings.
    output = os.dup(sys.stdout.fileno())
    with open(os.devnull, "w") as silent:
        os.dup2(silent.fileno(), sys.stdout.fileno())
        os.dup2(silent.fileno(), sys.stderr.fileno())
    logging.disable(logging.CRITICAL)
    runtime_validated = False
    paced = False
    stream = os.fdopen(output, "wb")
    try:
        raw = sys.stdin.buffer.readline(INPUT_LIMIT + 2)
        request = decode(raw.removesuffix(b"\n"), INPUT_LIMIT, preserve_numbers=True)
        if not isinstance(request, dict) or set(request) not in (
                {"url", "settings", "resume"}, {"url", "settings", "resume", "source_pacing"}):
            raise InvalidData("Invalid metadata fetch request")
        paced = "source_pacing" in request
        if paced and request["source_pacing"] is not True:
            raise InvalidData("Invalid metadata source protocol")
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
        def reserve_source(url):
            stream.write(encode({"contact": public_url(url)}, 16384) + b"\n")
            stream.flush()
            reply = decode(sys.stdin.buffer.readline(256), 255)
            if not isinstance(reply, dict) or set(reply) != {"allowed"} or type(reply["allowed"]) is not bool:
                raise InvalidData("Invalid metadata source reservation")
            return reply["allowed"]

        result = (collector or collect)(request["url"], request["settings"], request["resume"],
                                        reserve_source=reserve_source if paced else None)
    except InvalidData:
        result = {"error": "invalid_checkpoint" if runtime_validated else "runtime_changed"}
    except Exception as exc:
        result = {"error": classify(exc)}
    with stream:
        if paced:
            stream.write(encode({"result": result}, MAX_BYTES + 64) + b"\n")
        else:
            stream.write(encode(result, MAX_BYTES))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
