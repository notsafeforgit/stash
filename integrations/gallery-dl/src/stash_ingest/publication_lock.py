"""Cooperative filesystem boundary shared by native workers and host backups.

The gate prevents new writers from starving a waiting backup. Acquire every
backup barrier before asking Stash for its database writer guard. Lock files
are permanent coordination objects; never remove or replace them during use.
"""

from contextlib import contextmanager
import fcntl
import math
import os
from pathlib import Path
import stat
import threading
import time

from .encoding import InvalidData

GATE = "native-publication-gate.lock"
ACTIVE = "native-publication-active.lock"
_local = threading.local()


class PublicationBusy(BlockingIOError):
    """A valid filesystem boundary is temporarily held by another operation."""


def _directory(path):
    path = Path(path).resolve(strict=True)
    info = path.stat()
    if not stat.S_ISDIR(info.st_mode):
        raise InvalidData("Publication lock root is not a directory")
    return path, (info.st_dev, info.st_ino)


def _open(path):
    fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise InvalidData("Publication lock must be a regular file")
        return fd
    except BaseException:
        os.close(fd)
        raise


def _deadline(timeout):
    if type(timeout) not in (int, float) or not math.isfinite(timeout) or timeout <= 0:
        raise InvalidData("Publication lock timeout must be positive")
    return time.monotonic() + timeout


def _wait(fd, mode, deadline, check):
    while True:
        check()
        if time.monotonic() >= deadline:
            raise PublicationBusy("Timed out waiting for the filesystem publication boundary")
        try:
            fcntl.flock(fd, mode | fcntl.LOCK_NB)
            return
        except BlockingIOError:
            time.sleep(min(0.05, max(0, deadline - time.monotonic())))


def _verify(path, identity, gate, active):
    if _directory(path)[1] != identity:
        raise InvalidData("Publication lock directory changed")
    for name, fd in ((GATE, gate), (ACTIVE, active)):
        current, opened = (path / name).lstat(), os.fstat(fd)
        if (not stat.S_ISREG(current.st_mode)
                or (current.st_dev, current.st_ino) != (opened.st_dev, opened.st_ino)):
            raise InvalidData("Publication lock file was replaced")


@contextmanager
def publication_lock(directory, check=lambda: None, *, timeout=300):
    """One download/mutation, reentrant within this thread and physical root."""
    path, identity = _directory(directory)
    held = getattr(_local, "held", None)
    if held is None:
        _local.held = held = set()
    if identity in held:
        # Current-file completion must remain possible after lease loss.
        yield
        return
    deadline = _deadline(timeout)
    gate, active = None, None
    try:
        gate = _open(path / GATE)
        active = _open(path / ACTIVE)
        _wait(gate, fcntl.LOCK_SH, deadline, check)
        _wait(active, fcntl.LOCK_SH, deadline, check)
        _verify(path, identity, gate, active)
        fcntl.flock(gate, fcntl.LOCK_UN)
        held.add(identity)
        try:
            yield
        finally:
            held.remove(identity)
    finally:
        if active is not None:
            os.close(active)
        if gate is not None:
            os.close(gate)


class PublicationBarrier:
    """Host backup exclusion across explicitly inventoried worker lock roots.

    Call release as soon as immutable filesystem views are retained, before
    packing/copying/uploading the large archive. Context exit is also safe after
    an early release or failed acquisition. This does not stop legacy workers.
    """
    def __init__(self, directories, *, timeout=300):
        roots = {}
        for directory in directories:
            path, identity = _directory(directory)
            roots[identity] = path
        if not roots:
            raise InvalidData("A publication barrier requires worker lock directories")
        self.roots = sorted(roots.items())
        self.timeout, self.handles, self.acquired, self.used = timeout, [], False, False

    def __enter__(self):
        if self.used:
            raise InvalidData("A publication barrier cannot be reacquired")
        self.used = True
        deadline = _deadline(self.timeout)
        try:
            for identity, path in self.roots:
                if identity in getattr(_local, "held", ()):
                    raise InvalidData("A backup barrier must precede worker publication")
                gate = _open(path / GATE)
                self.handles.append(gate)
                active = _open(path / ACTIVE)
                self.handles.append(active)
                # Hold all gates through capture. New worker mutations cannot
                # pass a queued backup while an older mutation is finishing.
                _wait(gate, fcntl.LOCK_EX, deadline, lambda: None)
                _wait(active, fcntl.LOCK_EX, deadline, lambda: None)
                _verify(path, identity, gate, active)
            self.acquired = True
            return self
        except BaseException:
            self.release()
            raise

    def release(self):
        self.acquired = False
        while self.handles:
            os.close(self.handles.pop())

    def check(self):
        """Recheck the held boundary before a later filesystem/API operation."""
        if not self.acquired or len(self.handles) != len(self.roots) * 2:
            raise InvalidData("Publication barrier is not held")
        for index, (identity, path) in enumerate(self.roots):
            _verify(path, identity, self.handles[2 * index], self.handles[2 * index + 1])

    def __exit__(self, *_):
        self.release()
