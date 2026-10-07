"""Preserve saved scrape lists while publishing reviewed native source changes."""

import os
from pathlib import Path
import stat
import tempfile

from .catalog_upload import read_regular
from .encoding import InvalidData, digest
from .launcher_inputs import _REDDIT_USER, reddit_name, twitter_target
from .source_management import conflict

MAX_LIST_BYTES = 8 << 20


def publish(path, body, *, replace=False):
    """Durably publish private state, or an explicitly checked existing list."""
    path = Path(path)
    mode = 0o600
    if replace:
        current = path.lstat()
        if not stat.S_ISREG(current.st_mode):
            raise InvalidData("Saved scrape list must be a regular file")
        mode = stat.S_IMODE(current.st_mode)
    fd, temporary = tempfile.mkstemp(prefix=".native-source-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            output.write(body)
            output.flush()
            os.fchmod(output.fileno(), mode)
            os.fsync(output.fileno())
        if replace:
            os.replace(temporary, path)
        else:
            os.link(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def account(platform, line):
    line = line.split("#", 1)[0].strip()
    if not line:
        return None
    try:
        if platform == "reddit":
            match = _REDDIT_USER.fullmatch(line)
            return ("handle", reddit_name(match[1])) if match else None
        if platform == "twitter":
            target = twitter_target(line.split()[0])
            return ("id", target.rsplit("/", 1)[1]) if "/i/user/" in target else ("handle", target.rsplit("/", 1)[1])
    except InvalidData:
        return None  # Preserve unrelated or historically ignored input lines.
    raise InvalidData("Unsupported saved source list platform")


def key(reference):
    kind, value = reference
    return kind, (value.lstrip("0") or "0") if kind == "id" else value.casefold()


def change(body, platform, references, *, add=None):
    if len(body) > MAX_LIST_BYTES:
        raise InvalidData("Saved source list exceeds its size limit")
    try:
        value = body.decode("utf-8")
    except UnicodeError:
        raise InvalidData("Saved source list must be UTF-8") from None
    wanted = {key(reference) for reference in references}
    matched, retained, removed = [], [], 0
    for line in value.splitlines(keepends=True):
        found = account(platform, line)
        if found is not None and key(found) in wanted:
            matched.append(found)
            if add is None:
                removed += 1
                continue
        retained.append(line)
    if add is not None and not matched:
        # The caller validates the account and uses only these fixed formats.
        expected = ("id" if platform == "twitter" else "handle", add)
        if wanted != {key(expected)}:
            raise InvalidData("List registration must identify exactly one account")
        url = "https://x.com/i/user/" + add if platform == "twitter" else "https://reddit.com/user/" + add + "/submitted/"
        if account(platform, url) != expected:
            raise InvalidData("Invalid saved source account")
        eol = "\r\n" if "\r\n" in value else "\n"
        if value and not value.endswith(("\n", "\r")):
            retained.append(eol)
        retained.append(url + eol)
    after = "".join(retained).encode("utf-8")
    if len(after) > MAX_LIST_BYTES:
        raise InvalidData("Updated saved source list exceeds its size limit")
    return after, matched, removed


def apply(path, before, after):
    """Replay only the saved bytes; never reconstruct against a later edit."""
    current = read_regular(path, MAX_LIST_BYTES)
    if current == after:
        return
    if current != before:
        raise conflict()
    publish(path, after, replace=True)
    if digest(read_regular(path, MAX_LIST_BYTES)) != digest(after):
        raise conflict()
