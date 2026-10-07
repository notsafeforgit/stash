"""Read application credentials without exporting private file contents."""

import os
import stat

from .client import Unavailable
from .encoding import InvalidData


def application_key(key_env="STASH_API_KEY", key_file=None):
    key = os.environ.get(key_env, "")
    if key_file is not None:
        fd = os.open(key_file, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, "rb") as source:
            info = os.fstat(source.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
                raise InvalidData("Application key file must be private and owned by this user")
            body = source.read(8193)
        if len(body) > 8192:
            raise InvalidData("Application key exceeds its byte limit")
        try:
            key = body.decode("ascii").rstrip("\r\n")
        except UnicodeError:
            raise InvalidData("Invalid application key encoding") from None
    if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
        raise Unavailable("stash_application_key_missing")
    return key
