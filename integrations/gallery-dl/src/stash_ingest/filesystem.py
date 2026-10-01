"""Reviewed local roots, shared destination locks and flushed final file claims."""

from contextlib import contextmanager
import fcntl
import hashlib
import os
from pathlib import Path
import stat

from .encoding import InvalidData


class Root:
    def __init__(self, path, identity):
        self.path = Path(path).resolve(strict=True)
        self.identity = tuple(identity)
        self.verify()

    @staticmethod
    def probe(path):
        status = Path(path).stat()
        if not stat.S_ISDIR(status.st_mode):
            raise InvalidData("Media root is not a directory")
        return (status.st_dev, status.st_ino)

    def verify(self):
        if self.probe(self.path) != self.identity:
            raise InvalidData("Media root changed or its mount is unavailable")

    def relative(self, path):
        self.verify()
        try:
            value = Path(path).resolve().relative_to(self.path).as_posix()
        except ValueError:
            raise InvalidData("Output path escapes the reviewed media root") from None
        if "\\" in value or any(ord(c) < 32 for c in value):
            raise InvalidData("Output path is not portable")
        return value

    def completed(self, path):
        relative = self.relative(path)
        if relative == "." or relative.lower().endswith(".part"):
            raise InvalidData("Only a final file can be queued")
        parts = relative.split("/")
        directories = [os.open(self.path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)]
        file = None
        try:
            root_stat = os.fstat(directories[0])
            if (root_stat.st_dev, root_stat.st_ino) != self.identity:
                raise InvalidData("Media root changed while opening the file")
            for part in parts[:-1]:
                directories.append(os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                           dir_fd=directories[-1]))
            file = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directories[-1])
            before = os.fstat(file)
            if not stat.S_ISREG(before.st_mode) or before.st_size < 1:
                raise InvalidData("Completed output is not a nonempty regular file")
            os.fsync(file)
            sha = hashlib.sha256()
            while block := os.read(file, 1 << 20):
                sha.update(block)
            after = os.fstat(file)
            current = os.stat(relative, dir_fd=directories[0], follow_symlinks=False)

            def snapshot(value):
                return value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns, value.st_ctime_ns

            if snapshot(before) != snapshot(after) or snapshot(after) != snapshot(current):
                raise InvalidData("Completed output changed during hashing")
            if self.relative(path) != relative:
                raise InvalidData("Completed output moved during hashing")
            for directory in reversed(directories):
                os.fsync(directory)
            self.verify()
            return relative, before.st_size, sha.hexdigest()
        finally:
            if file is not None:
                os.close(file)
            for directory in reversed(directories):
                os.close(directory)


@contextmanager
def destination_lock(directory, root_uuid, relative, check):
    """Shared by host and container using the same physical lock directory."""
    directory = Path(directory).resolve(strict=True)
    bucket = int(hashlib.sha256((root_uuid + "\x00" + relative).encode()).hexdigest()[:8], 16) % 4096
    fd = os.open(directory / f"{bucket:03x}.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        # Do not block indefinitely with a dead source lease behind an old
        # worker's still-running download. The caller can retry its native run.
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise InvalidData("Destination is held by another download worker") from None
        check()
        yield
    finally:
        os.close(fd)
