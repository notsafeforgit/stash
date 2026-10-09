"""Bounded content-addressed storage. A manifest is the publication point."""

import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import stat
import tempfile
import zlib

LEGACY_CHUNK_SIZE = 1 << 20
SQLITE_CHUNK_SIZE = 4 << 20
CHUNK_SIZE = 64 << 20
MAX_MANIFEST = 128 << 20
RESERVE_BYTES = 50 << 30
HEX = re.compile(r"[0-9a-f]{64}\Z")


class InvalidArchive(ValueError):
    pass


def require_space(path, additional=0, reserve=RESERVE_BYTES):
    if min(additional, reserve) < 0:
        raise InvalidArchive("Space budgets cannot be negative")
    if shutil.disk_usage(path).free < additional + reserve:
        raise InvalidArchive("Insufficient free space for the archive and reserved headroom")


def regular(path):
    path = Path(path)
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise InvalidArchive("An archive input must be a regular file, not a symbolic link")
    return info


def open_regular(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise InvalidArchive("An archive object must be a regular file")
        return os.fdopen(fd, "rb")
    except BaseException:
        os.close(fd)
        raise


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def publish_bytes(path, body):
    """Do not replace another writer's artifact, even after an interrupted run."""
    path = Path(path)
    fd, temp = tempfile.mkstemp(prefix=".publish-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            output.write(body)
            output.flush()
            os.fsync(output.fileno())
        os.link(temp, path, follow_symlinks=False)
        sync_directory(path.parent)
    finally:
        os.unlink(temp)


def json_bytes(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True,
                       separators=(",", ":"), allow_nan=False) + "\n").encode("utf-8")


def object_path(root, checksum):
    if not isinstance(checksum, str) or not HEX.fullmatch(checksum):
        raise InvalidArchive("Invalid object digest")
    # One fixed directory: no path components can come from an archive entry.
    path = Path(root) / "objects"
    if not stat.S_ISDIR(path.lstat().st_mode):
        raise InvalidArchive("The object directory must not be a symbolic link")
    return path / (checksum + ".gz")


def store_file(root, source, *, reserve=RESERVE_BYTES, expected_md5=None, chunk_size=CHUNK_SIZE):
    if type(chunk_size) is not int or chunk_size not in (LEGACY_CHUNK_SIZE, SQLITE_CHUNK_SIZE, CHUNK_SIZE):
        raise InvalidArchive("Unsupported archive chunk size")
    if expected_md5 is not None and (not isinstance(expected_md5, str) or not re.fullmatch(r"[0-9a-f]{32}", expected_md5)):
        raise InvalidArchive("Invalid retained artwork checksum")
    before = regular(source)
    chunks, digest, size = [], hashlib.sha256(), 0
    artwork_digest = hashlib.md5(usedforsecurity=False) if expected_md5 is not None else None
    with open_regular(source) as incoming:
        opened = os.fstat(incoming.fileno())
        if (before.st_dev, before.st_ino) != (opened.st_dev, opened.st_ino):
            raise InvalidArchive("Archive input changed before opening")
        while raw := incoming.read(chunk_size):
            digest.update(raw)
            if artwork_digest is not None:
                artwork_digest.update(raw)
            size += len(raw)
            buffer = io.BytesIO()
            with gzip.GzipFile(filename="", mode="wb", fileobj=buffer,
                               compresslevel=6, mtime=0) as compressor:
                compressor.write(raw)
            encoded = buffer.getvalue()
            encoded_hash = hashlib.sha256(encoded).hexdigest()
            target = object_path(root, encoded_hash)
            descriptor = {"sha256": encoded_hash, "encoded_size": len(encoded),
                          "size": len(raw), "raw_sha256": hashlib.sha256(raw).hexdigest()}
            if target.exists() or target.is_symlink():
                # A collision or interrupted local object never becomes a valid
                # reference merely because its filename exists.
                read_chunk(root, descriptor)
            else:
                require_space(root, len(encoded), reserve)
                publish_bytes(target, encoded)
            chunks.append(descriptor)
        after = os.fstat(incoming.fileno())
    # Removing another link to a retained artwork inode changes ctime, without
    # changing these bytes. Only pins with an expected content checksum use
    # this exception; ordinary live inputs retain their strict ctime guard.
    signature = lambda st: (st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns,
                            st.st_ctime_ns if artwork_digest is None else None)
    if signature(before) != signature(after) or signature(after) != signature(regular(source)):
        raise InvalidArchive("Archive input changed while being read")
    if artwork_digest is not None and artwork_digest.hexdigest() != expected_md5:
        raise InvalidArchive("Retained artwork checksum mismatch")
    return {"sha256": digest.hexdigest(), "size": size, "chunks": chunks}


def check_descriptor(value, *, chunk_size=CHUNK_SIZE):
    if not isinstance(value, dict) or set(value) != {"sha256", "encoded_size", "size", "raw_sha256"}:
        raise InvalidArchive("Invalid chunk descriptor")
    if any(not isinstance(value[k], str) or not HEX.fullmatch(value[k]) for k in ("sha256", "raw_sha256")):
        raise InvalidArchive("Invalid chunk digest")
    if type(value["size"]) is not int or not 0 < value["size"] <= chunk_size:
        raise InvalidArchive("Invalid uncompressed chunk size")
    if type(value["encoded_size"]) is not int or not 0 < value["encoded_size"] <= chunk_size + 65536:
        raise InvalidArchive("Invalid compressed chunk size")


def read_chunk(root, descriptor):
    check_descriptor(descriptor)
    with open_regular(object_path(root, descriptor["sha256"])) as incoming:
        encoded = incoming.read(descriptor["encoded_size"] + 1)
    if len(encoded) != descriptor["encoded_size"] or hashlib.sha256(encoded).hexdigest() != descriptor["sha256"]:
        raise InvalidArchive("Compressed archive object is missing, truncated or corrupt")
    try:
        with gzip.GzipFile(fileobj=io.BytesIO(encoded)) as stream:
            raw = stream.read(descriptor["size"] + 1)
            if len(raw) != descriptor["size"] or stream.read(1):
                raise InvalidArchive("Uncompressed chunk size does not match its descriptor")
    except (OSError, EOFError, zlib.error) as error:
        raise InvalidArchive("Invalid compressed archive object") from error
    if hashlib.sha256(raw).hexdigest() != descriptor["raw_sha256"]:
        raise InvalidArchive("Uncompressed archive object digest mismatch")
    return raw


def write_artifact(root, artifact, output=None, check_space=None):
    digest, size = hashlib.sha256(), 0
    for descriptor in artifact["chunks"]:
        raw = read_chunk(root, descriptor)
        digest.update(raw)
        size += len(raw)
        if output is not None:
            if check_space is not None:
                check_space(len(raw))
            output.write(raw)
    if size != artifact["size"] or digest.hexdigest() != artifact["sha256"]:
        raise InvalidArchive("Artifact contents or chunk order do not match the manifest")


def load_manifest(root):
    root = Path(root)
    if not stat.S_ISDIR(root.lstat().st_mode):
        raise InvalidArchive("The archive must be a regular directory")
    with open_regular(root / "manifest.json") as incoming:
        body = incoming.read(MAX_MANIFEST + 1)
    if len(body) > MAX_MANIFEST:
        raise InvalidArchive("Archive manifest exceeds the supported size")
    return decode_json(body)


def decode_json(body):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise InvalidArchive("Duplicate manifest field")
            result[key] = value
        return result
    try:
        def invalid_constant(value):
            raise InvalidArchive("Invalid JSON numeric constant")
        return json.loads(body, object_pairs_hook=unique, parse_constant=invalid_constant)
    except (ValueError, UnicodeError) as error:
        raise InvalidArchive("Invalid archive manifest") from error
