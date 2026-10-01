"""Portable worker policies with local paths, assets and website access references."""

from contextlib import contextmanager
import copy
import hashlib
from importlib.metadata import distribution, PackageNotFoundError
import os
from pathlib import Path
import re
import stat
import threading

from .encoding import InvalidData, decode, digest, encode, identifier
from .events import sha256
from .filesystem import Root

SCHEMA = "stash-gallery-worker-v1"
CONFIG_LIMIT = 1 << 20
ASSET_LIMIT = 4 << 20
GALLERY_COMMIT = "c40eb2a42fbaa1d26a2bb7c96804b7f47d1f73f8"
GALLERY_VERSION = "1.32.15.dev0"
YTDLP_VERSION = "2026.9.27.232945.dev0"
REFERENCE = re.compile(r"\$\{stash:([a-z][a-z0-9_]{0,63})\}")
ACCESS_OPTIONS = frozenset(("username", "password", "cookies", "cookies-from-browser", "proxy",
                            "headers", "http-headers", "authorization", "oauth", "netrc", "client-id"))
_active_configuration = threading.Lock()


def _access_option(key):
    key = key.lower().replace("_", "-")
    return key in ACCESS_OPTIONS or bool(re.search(r"(?:^|-)(?:token|password|secret|api-key|apikey)(?:-|$)", key))


def _json_file(path):
    with open(path, "rb") as file:
        return decode(file.read(CONFIG_LIMIT + 1), CONFIG_LIMIT)


def _path(value, base):
    if not isinstance(value, str) or not value or any(ord(c) < 32 for c in value):
        raise InvalidData("Invalid local worker path")
    value = Path(value).expanduser()
    return (value if value.is_absolute() else base / value).resolve()


def _pointer(document, pointer):
    if not isinstance(pointer, str) or len(pointer) > 4096 or (pointer and not pointer.startswith("/")):
        raise InvalidData("Local JSON references require a JSON Pointer")
    if pointer:
        for segment in pointer[1:].split("/"):
            if re.search(r"~(?![01])", segment):
                raise InvalidData("Invalid JSON Pointer escape")
            segment = segment.replace("~1", "/").replace("~0", "~")
            try:
                if isinstance(document, list) and re.fullmatch(r"0|[1-9][0-9]*", segment):
                    document = document[int(segment)]
                elif isinstance(document, dict):
                    document = document[segment]
                else:
                    raise KeyError()
            except (KeyError, IndexError):
                raise InvalidData("Local JSON reference was not found") from None
    return document


def _snapshot(status):
    return status.st_dev, status.st_ino, status.st_size, status.st_mtime_ns, status.st_ctime_ns


def _asset(path, expected):
    if not sha256(expected):
        raise InvalidData("A worker asset requires its reviewed SHA-256")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        before = os.fstat(fd)
        if not stat.S_ISREG(before.st_mode) or not 0 < before.st_size <= ASSET_LIMIT:
            raise InvalidData("Worker asset must be a bounded regular file")
        sha = hashlib.sha256()
        remaining = ASSET_LIMIT + 1
        while remaining and (block := os.read(fd, min(remaining, 65536))):
            remaining -= len(block)
            sha.update(block)
        after = os.fstat(fd)
        current = path.stat()
        if (sha.hexdigest() != expected or _snapshot(before) != _snapshot(after)
                or _snapshot(after) != _snapshot(current) or not remaining):
            raise InvalidData("Worker asset differs from its reviewed digest")
        return _snapshot(after)
    finally:
        os.close(fd)


def runtime_identity():
    try:
        gallery, ytdlp = distribution("gallery-dl"), distribution("yt-dlp")
        direct = decode((gallery.read_text("direct_url.json") or "{}").encode())
        vcs = direct.get("vcs_info") if isinstance(direct, dict) else None
        if (gallery.version != GALLERY_VERSION or ytdlp.version != YTDLP_VERSION
                or not isinstance(vcs, dict) or vcs.get("commit_id") != GALLERY_COMMIT):
            raise InvalidData("Worker requires the validated gallery-dl and yt-dlp runtime")
    except PackageNotFoundError:
        raise InvalidData("Install the pinned gallery-dl worker runtime before use") from None
    files = sorted(Path(__file__).parent.glob("*.py"))
    sources = {file.name: digest(file.read_bytes()) for file in files}
    return {"gallery_dl": GALLERY_COMMIT, "yt_dlp": YTDLP_VERSION, "adapter_sha256": digest(encode(sources))}


class Configuration:
    def __init__(self, filename):
        filename = Path(filename).resolve(strict=True)
        value = _json_file(filename)
        if (not isinstance(value, dict) or set(value) != {"schema", "root", "locks", "gallery", "bindings"}
                or value["schema"] != SCHEMA or not isinstance(value["gallery"], dict)
                or not isinstance(value["bindings"], dict) or len(value["bindings"]) > 64):
            raise InvalidData("Invalid native gallery-dl worker configuration")
        self.root_uuid, self.root = self._root(value["root"], filename.parent, media=True)
        _, self.locks = self._root(value["locks"], filename.parent, media=False)
        self.assets = {}
        self._values = {"media_root": str(self.root.path)}
        self._kinds = {"media_root": "path"}
        assets = {}
        for name, binding in value["bindings"].items():
            if (not re.fullmatch(r"[a-z][a-z0-9_]{0,63}", name) or name == "media_root"
                    or not isinstance(binding, dict)):
                raise InvalidData("Invalid or reserved worker binding")
            kind = binding.get("kind")
            self._kinds[name] = kind
            if kind == "path" and set(binding) == {"kind", "path"}:
                resolved = str(_path(binding["path"], filename.parent))
            elif kind == "asset" and set(binding) == {"kind", "path", "sha256"}:
                path = _path(binding["path"], filename.parent)
                self.assets[path] = _asset(path, binding["sha256"])
                assets[name] = binding["sha256"]
                resolved = str(path)
            elif kind == "private" and set(binding) == {"kind", "file", "pointer"}:
                resolved = _pointer(_json_file(_path(binding["file"], filename.parent)), binding["pointer"])
            elif kind == "private" and set(binding) == {"kind", "env"}:
                reference = binding["env"]
                if not isinstance(reference, str) or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", reference):
                    raise InvalidData("Invalid website access environment reference")
                if reference not in os.environ:
                    raise InvalidData("Website access environment reference is unavailable")
                resolved = os.environ[reference]
            else:
                raise InvalidData("Invalid local worker binding definition")
            self._values[name] = resolved
        self._gallery = self._expand(value["gallery"])
        self._validate(self._gallery)
        self.policy_sha256 = digest(encode({"version": SCHEMA, "operation": "download",
                                           "runtime": runtime_identity(), "gallery": value["gallery"],
                                           "assets": assets}, CONFIG_LIMIT))

    @staticmethod
    def _root(value, base, *, media):
        fields = {"path", "identity", "uuid"} if media else {"path", "identity"}
        if (not isinstance(value, dict) or set(value) != fields or not isinstance(value["identity"], list)
                or len(value["identity"]) != 2 or any(type(n) is not int or n < 0 for n in value["identity"])):
            raise InvalidData("Worker directories require reviewed device/inode identities")
        return (identifier(value["uuid"]) if media else None), Root(_path(value["path"], base), value["identity"])

    def _expand(self, value, *, access=False):
        if isinstance(value, dict):
            result = {}
            for key, child in value.items():
                if key == "function" and child:
                    module, separator, function = child.rpartition(":") if isinstance(child, str) else ("", "", "")
                    reference = REFERENCE.fullmatch(module)
                    if (reference is None or self._kinds.get(reference[1]) != "asset"
                            or not separator or not function.isidentifier()):
                        raise InvalidData("Python postprocessors require a reviewed worker asset")
                private = access or _access_option(key)
                if (private and child and not isinstance(child, bool)
                        and not (isinstance(child, str) and REFERENCE.fullmatch(child)
                                 and self._kinds.get(REFERENCE.fullmatch(child)[1]) == "private")):
                    raise InvalidData("Website access settings must use local private references")
                result[key] = self._expand(child, access=private)
            return result
        if isinstance(value, list):
            return [self._expand(item, access=access) for item in value]
        if not isinstance(value, str):
            return value
        if match := REFERENCE.fullmatch(value):
            name = match[1]
            if name not in self._values:
                raise InvalidData("Worker configuration references an unknown binding")
            if self._kinds[name] == "private" and not access:
                raise InvalidData("Private references are only allowed in website access settings")
            return copy.deepcopy(self._values[name])

        def replace(match):
            name = match[1]
            if name not in self._values or self._kinds[name] == "private" or not isinstance(self._values[name], str):
                raise InvalidData("Only worker paths and assets can be interpolated into strings")
            return self._values[name]
        result = REFERENCE.sub(replace, value)
        if "${stash:" in result:
            raise InvalidData("Malformed worker binding reference")
        return result

    @staticmethod
    def _validate(value):
        if isinstance(value, dict):
            if "gallery_catalog_hook" in str(value.get("function", "")):
                raise InvalidData("Native workers cannot use legacy catalog writers")
            for child in value.values():
                Configuration._validate(child)
        elif isinstance(value, list):
            for child in value:
                Configuration._validate(child)

    def check(self):
        self.root.verify()
        self.locks.verify()
        for path, expected in self.assets.items():
            if _snapshot(path.stat()) != expected:
                raise InvalidData("A reviewed worker asset changed during source execution")

    @contextmanager
    def activate(self):
        from gallery_dl import config
        if not _active_configuration.acquire(blocking=False):
            raise InvalidData("Run each native gallery-dl worker in its own process")
        previous = copy.deepcopy(config._config)
        try:
            self.check()
            config.clear()
            config._config.update(copy.deepcopy(self._gallery))
            yield
        finally:
            config.clear()
            config._config.update(previous)
            _active_configuration.release()
