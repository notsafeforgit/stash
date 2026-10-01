"""The queue is bound to one Stash origin, independently of token rotation."""

from urllib.parse import urlsplit

from .encoding import InvalidData


def origin(value):
    if not isinstance(value, str) or any(ord(c) <= 32 for c in value):
        raise InvalidData("Expected a Stash HTTP origin")
    try:
        parsed = urlsplit(value)
        if (parsed.scheme not in {"http", "https"} or not parsed.hostname
                or parsed.username is not None or parsed.password is not None
                or parsed.path not in {"", "/"} or parsed.query or parsed.fragment
                or "?" in value or "#" in value or "\\" in value or "%" in parsed.netloc):
            raise ValueError()
        host = parsed.hostname.encode("idna").decode("ascii").lower()
        if ":" in host:
            host = "[" + host + "]"
        port = parsed.port
        if port is not None and not 1 <= port <= 65535:
            raise ValueError()
        if port == {"http": 80, "https": 443}[parsed.scheme]:
            port = None
        return parsed.scheme + "://" + host + (":" + str(port) if port else "")
    except (ValueError, UnicodeError):
        raise InvalidData("Expected a Stash HTTP origin without credentials or a path") from None
