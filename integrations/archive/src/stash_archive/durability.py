"""Flush a retained inode set before acknowledging its filesystem boundary."""

import ctypes
import os
import re
import sys

from .storage import InvalidArchive


def syncfs_function():
    # Linux syncfs waits for this filesystem's data and metadata writeback,
    # with the guarantees of fsync on each file. Preserve per-file fsync on
    # platforms without that primitive; never use asynchronous writeback.
    if sys.platform != "linux":
        return None
    version = re.match(r"([0-9]+)\.([0-9]+)", os.uname().release)
    # Older kernels do not report filesystem writeback errors through syncfs.
    if version is None or tuple(map(int, version.groups())) < (5, 8):
        return None
    if hasattr(os, "syncfs"):
        return os.syncfs
    library = ctypes.CDLL(None, use_errno=True)
    syncfs = getattr(library, "syncfs", None)
    if syncfs is None:
        return None
    syncfs.argtypes = (ctypes.c_int,)
    syncfs.restype = ctypes.c_int

    def flush(fd):
        if syncfs(fd) != 0:
            code = ctypes.get_errno()
            raise OSError(code, os.strerror(code))

    return flush


class FilesystemFlush:
    def __init__(self, path, identity):
        self.path, self.identity = path, identity
        self.flush = syncfs_function()
        self.fd = None

    def __enter__(self):
        # Open before making links so filesystem writeback errors observed
        # through this descriptor cover the whole capture, including its end.
        fd = os.open(self.path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        info = os.fstat(fd)
        if (info.st_dev, info.st_ino) != self.identity:
            os.close(fd)
            raise InvalidArchive("Artwork flush directory was replaced")
        self.fd = fd
        return self

    def file(self, fd):
        if self.flush is None:
            os.fsync(fd)

    def finish(self):
        if self.flush is not None:
            self.flush(self.fd)

    def __exit__(self, *_):
        os.close(self.fd)
        self.fd = None
