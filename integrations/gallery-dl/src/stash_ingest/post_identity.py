"""Validate the application identity context before preparing new source work."""

from .catalog_source import source_time
from .encoding import InvalidData, identifier


def current_post(value, post):
    identifier(post)
    if not isinstance(value, dict):
        raise InvalidData("Invalid post identity context")
    requested, canonical = value.get("requested"), value.get("canonical")
    for item in (requested, canonical):
        if (not isinstance(item, dict)
                or not {"uuid", "canonical_uuid", "redirect_to", "state", "revision", "created_at"} <= item.keys()):
            raise InvalidData("Invalid post identity")
        identifier(item.get("uuid"))
        identifier(item.get("canonical_uuid"))
        if item.get("redirect_to") is not None:
            identifier(item["redirect_to"])
            if item["redirect_to"] == item["uuid"]:
                raise InvalidData("Post identity redirects to itself")
        if (item.get("state") not in ("active", "forgotten") or type(item.get("revision")) is not int
                or not 1 <= item["revision"] <= 2**53 - 1 or not isinstance(item.get("created_at"), str)):
            raise InvalidData("Invalid post identity state")
        source_time(item["created_at"])
    if (requested["uuid"] != post or requested["canonical_uuid"] != canonical["uuid"]
            or canonical["uuid"] != canonical["canonical_uuid"] or canonical.get("redirect_to") is not None
            or (requested["uuid"] == canonical["uuid"]) != (requested.get("redirect_to") is None)):
        raise InvalidData("Post identity context differs from its request")
    return canonical["uuid"]
