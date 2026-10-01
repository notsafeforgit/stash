"""Resolve exact source URLs through Stash's authorized current definitions."""

from urllib.parse import urlsplit

from .client import Unavailable
from .encoding import InvalidData, encode, identifier


def target_url(value):
    if (not isinstance(value, str) or not value or value.strip() != value
            or any(ord(c) < 32 or 127 <= ord(c) < 160 for c in value)):
        raise InvalidData("Invalid source target URL")
    try:
        parsed = urlsplit(value)
        if (len(value.encode("utf-8")) > 8192 or parsed.scheme not in ("http", "https")
                or not parsed.hostname or parsed.username is not None):
            raise ValueError
    except ValueError:
        raise InvalidData("Expected an HTTP source URL without login credentials") from None
    return value


def lookup_collections(client, targets, root_uuid):
    """No implicit alias substitution, creation, or selection among candidates."""
    if root_uuid is not None:
        identifier(root_uuid)
    if (not isinstance(targets, list) or not 1 <= len(targets) <= 50
            or any(not isinstance(target, str) for target in targets) or len(set(targets)) != len(targets)):
        raise InvalidData("Collection lookup requires 1 to 50 distinct source URLs")
    targets = [target_url(target) for target in targets]
    if client.capabilities().get("collection_lookup") is not True:
        raise Unavailable("incompatible_collection_lookup")
    result = client._request("POST", "/collections/lookup", encode({"root_uuid": root_uuid, "targets": targets}, 512 << 10),
                             max_response_bytes=4 << 20)
    try:
        if (not isinstance(result, dict) or "root_uuid" not in result or result["root_uuid"] != root_uuid
                or not isinstance(result.get("targets"), list) or len(result["targets"]) != len(targets)):
            raise InvalidData("Mismatched collection lookup")
        found, seen = [], set()
        for target, item in zip(targets, result["targets"], strict=True):
            if (not isinstance(item, dict) or item.get("target_url") != target
                    or not isinstance(item.get("candidates"), list) or len(item["candidates"]) > 128
                    or type(item.get("has_more")) is not bool or (item["has_more"] and len(item["candidates"]) != 128)):
                raise InvalidData("Mismatched collection candidates")
            candidates = []
            for candidate in item["candidates"]:
                if not isinstance(candidate, dict):
                    raise InvalidData("Invalid collection candidate")
                collection = identifier(candidate.get("collection_uuid"))
                revision, state = candidate.get("collection_revision"), candidate.get("state")
                if (collection in seen or type(revision) is not int or not 1 <= revision <= 2147483647
                        or state not in ("active", "disabled", "retired")):
                    raise InvalidData("Invalid collection candidate")
                seen.add(collection)
                candidates.append({"collection_uuid": collection, "collection_revision": revision, "state": state})
            state = "unresolved" if not candidates else "ambiguous"
            if len(candidates) == 1 and not item["has_more"]:
                state = "resolved" if candidates[0]["state"] == "active" else candidates[0]["state"]
            found.append({"target_url": target, "state": state, "candidates": candidates, "has_more": item["has_more"]})
        return {"root_uuid": root_uuid, "targets": found}
    except InvalidData:
        raise Unavailable("invalid_collection_lookup") from None
