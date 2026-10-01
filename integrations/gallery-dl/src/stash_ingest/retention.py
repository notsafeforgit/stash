"""A producer copy of gallery-dl-retained-v1, verified by the Go policy fixtures.

Only the persisted copy is reduced. Extractor metadata stays intact for download
selection, filename formatting and other postprocessors.
"""

from datetime import date

from .encoding import InvalidData, MAX_DEPTH, MAX_PAYLOAD_BYTES, encode, timestamp
from . import media_references, account_profiles

POLICY = "gallery-dl-retained-v1"
SECRET_KEYS = frozenset("password passwd cookie cookies authorization access_token "
                       "refresh_token api_key apikey client_secret headers session".split())
PRIVATE_KEYS = frozenset({"_reddit", "_parent", "_url"})


def serializable(value, depth=0):
    if depth > MAX_DEPTH:
        raise InvalidData("Source metadata nesting exceeds the protocol limit")
    if isinstance(value, date):
        return timestamp(value)
    if isinstance(value, dict):
        result = {}
        for key, child in value.items():
            if not isinstance(key, str):
                raise InvalidData("Source metadata keys must be strings")
            if key.lower().replace("-", "_") in SECRET_KEYS:
                continue
            if key.startswith("_") and key not in PRIVATE_KEYS:
                continue
            result[key] = serializable(child, depth + 1)
        return result
    if isinstance(value, (list, tuple)):
        return [serializable(child, depth + 1) for child in value]
    if value is None or isinstance(value, (str, bool, int, float)):
        return value
    raise InvalidData("Unsupported public source metadata value")


def retain(metadata):
    if not isinstance(metadata, dict):
        raise InvalidData("Source metadata must be an object")
    result = account_profiles.retain(media_references.retain(serializable(metadata)))
    encode(result, MAX_PAYLOAD_BYTES)
    return result
