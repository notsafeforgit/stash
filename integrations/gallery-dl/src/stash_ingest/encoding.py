"""Strict, bounded JSON at producer persistence and HTTP boundaries."""

from datetime import date, datetime, timezone
import hashlib
import json
import math
import re
import uuid

MAX_PAYLOAD_BYTES = 4 << 20
MAX_EVENT_BYTES = 5 << 20
MAX_FILE_EVENT_BYTES = 16384
MAX_BATCH_BYTES = 16 << 20
MAX_DEPTH = 64


class InvalidData(ValueError):
    pass


_JSON_NUMBER = re.compile(r"-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?\Z")


class _ExactFloat(float):
    """A retained JSON token; arithmetic still uses its ordinary Python value."""

    def __new__(cls, token):
        if not isinstance(token, str) or not _JSON_NUMBER.fullmatch(token):
            raise InvalidData("Invalid JSON number token")
        result = super().__new__(cls, token)
        result.token = token
        return result

    def __reduce__(self):
        return type(self), (self.token,)


class _ExactZero(int):
    # Other JSON integer tokens already round-trip exactly through Python int.
    token = "-0"

    def __new__(cls, token="-0"):
        if token != "-0":
            raise InvalidData("Invalid JSON integer token")
        return super().__new__(cls, 0)

    def __reduce__(self):
        return type(self), (self.token,)


def identifier(value):
    try:
        parsed = uuid.UUID(value)
    except (AttributeError, ValueError, TypeError):
        raise InvalidData("Expected a canonical UUID") from None
    if not parsed.int or str(parsed) != value:
        raise InvalidData("Expected a canonical UUID")
    return value


def utc_now():
    return datetime.now(timezone.utc).isoformat(timespec="microseconds")


def timestamp(value):
    if isinstance(value, datetime):
        # gallery-dl uses naive UTC datetimes in several extractors.
        if value.tzinfo is None:
            value = value.replace(tzinfo=timezone.utc)
        return value.astimezone(timezone.utc).isoformat()
    if isinstance(value, date):
        return value.isoformat()
    raise InvalidData("Expected a date")


def validate_tree(value, depth=0):
    if depth > MAX_DEPTH:
        raise InvalidData("JSON nesting exceeds the protocol limit")
    exact = False
    if isinstance(value, (_ExactFloat, _ExactZero)):
        if not isinstance(value.token, str) or not _JSON_NUMBER.fullmatch(value.token):
            raise InvalidData("Invalid retained JSON number")
        exact = True
    elif isinstance(value, str):
        try:
            value.encode("utf-8")
        except UnicodeError:
            raise InvalidData("JSON contains invalid Unicode") from None
    elif value is None or isinstance(value, (bool, int)):
        pass
    elif isinstance(value, float):
        if not math.isfinite(value):
            raise InvalidData("JSON contains a non-finite number")
    elif isinstance(value, dict):
        for key, child in value.items():
            if not isinstance(key, str):
                raise InvalidData("JSON object keys must be strings")
            validate_tree(key, depth + 1)
            exact = validate_tree(child, depth + 1) or exact
    elif isinstance(value, list):
        for child in value:
            exact = validate_tree(child, depth + 1) or exact
    else:
        raise InvalidData("Unsupported JSON value")
    return exact


def _exact_json(value):
    # Only checkpoint reads opt into token preservation. Keep the ordinary
    # encoder for all other traffic; no existing event digest changes.
    if isinstance(value, (_ExactFloat, _ExactZero)):
        return value.token
    if isinstance(value, dict):
        return "{" + ",".join(_exact_json(k) + ":" + _exact_json(value[k]) for k in sorted(value)) + "}"
    if isinstance(value, list):
        return "[" + ",".join(_exact_json(v) for v in value) + "]"
    return json.dumps(value, ensure_ascii=False, allow_nan=False, separators=(",", ":"))


def encode(value, limit=MAX_EVENT_BYTES):
    exact = validate_tree(value)
    text = (_exact_json(value) if exact else json.dumps(value, ensure_ascii=False, allow_nan=False,
                                                      sort_keys=True, separators=(",", ":")))
    body = text.encode("utf-8")
    if len(body) > limit:
        raise InvalidData("JSON exceeds the protocol byte limit")
    return body


def native_json(value, limit):
    """Native canonical bodies; ordinary event wire bytes keep their own hash."""
    body = encode(value, limit).replace(b"\xe2\x80\xa8", b"\\u2028").replace(b"\xe2\x80\xa9", b"\\u2029")
    if len(body) > limit:
        raise InvalidData("Native JSON exceeds the protocol byte limit")
    return body


def decode(body, limit=MAX_EVENT_BYTES, *, preserve_numbers=False):
    if not isinstance(body, bytes) or len(body) > limit:
        raise InvalidData("Invalid JSON byte length")

    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise InvalidData("Duplicate JSON object key")
            result[key] = value
        return result

    def invalid_number(_):
        raise InvalidData("Invalid JSON number")

    try:
        value = json.loads(body.decode("utf-8"), object_pairs_hook=pairs, parse_constant=invalid_number,
                           parse_float=_ExactFloat if preserve_numbers else float,
                           parse_int=(lambda token: _ExactZero() if token == "-0" else int(token)) if preserve_numbers else int)
        validate_tree(value)
    except (UnicodeError, RecursionError, ValueError, OverflowError) as exc:
        raise InvalidData("Invalid JSON document") from exc
    return value


def digest(body):
    return hashlib.sha256(body).hexdigest()
