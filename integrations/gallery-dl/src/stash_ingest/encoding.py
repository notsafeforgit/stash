"""Strict, bounded JSON at producer persistence and HTTP boundaries."""

from datetime import date, datetime, timezone
import hashlib
import json
import math
import uuid

MAX_PAYLOAD_BYTES = 4 << 20
MAX_EVENT_BYTES = 5 << 20
MAX_FILE_EVENT_BYTES = 16384
MAX_BATCH_BYTES = 16 << 20
MAX_DEPTH = 64


class InvalidData(ValueError):
    pass


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
    if isinstance(value, str):
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
            validate_tree(child, depth + 1)
    elif isinstance(value, list):
        for child in value:
            validate_tree(child, depth + 1)
    else:
        raise InvalidData("Unsupported JSON value")


def encode(value, limit=MAX_EVENT_BYTES):
    validate_tree(value)
    body = json.dumps(value, ensure_ascii=False, allow_nan=False,
                      sort_keys=True, separators=(",", ":")).encode("utf-8")
    if len(body) > limit:
        raise InvalidData("JSON exceeds the protocol byte limit")
    return body


def decode(body, limit=MAX_EVENT_BYTES):
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
        value = json.loads(body.decode("utf-8"), object_pairs_hook=pairs,
                           parse_constant=invalid_number)
        validate_tree(value)
    except (UnicodeError, RecursionError, ValueError, OverflowError) as exc:
        raise InvalidData("Invalid JSON document") from exc
    return value


def digest(body):
    return hashlib.sha256(body).hexdigest()
