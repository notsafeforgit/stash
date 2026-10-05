"""Pinned Instagram source membership before output filtering or reordering."""

import copy
import math
import types

from .encoding import InvalidData

FIELD = "instagram_media"
CONTAINERS = ("story", "highlight")


def manifest(data):
    """Validate explicit extractor evidence, including story/container identity."""
    from .source import _agree, _id, _numeric

    value = data.get(FIELD)
    if value is None:
        return None
    required = {"version", "post_id", "album", "items"}
    if (not isinstance(value, dict) or not required <= value.keys() or value.keys() - required - {"container"}
            or type(value["version"]) is not int or value["version"] != 1 or type(value["album"]) is not bool
            or not isinstance(value["items"], list) or not 1 <= len(value["items"]) <= 4096):
        raise InvalidData("Invalid Instagram source attachment manifest")
    post_id = _numeric(_id(value["post_id"]))
    container = value.get("container")
    if data.get("type") in CONTAINERS:
        if (not isinstance(container, dict) or set(container) != {"id", "type"}
                or container["type"] != data["type"] or _id(container["id"]) != _id(data.get("post_id"))
                or post_id != _numeric(_id(data.get("media_id"))) or value["album"]):
            raise InvalidData("Instagram story identity differs from its observed container or media")
    elif container is not None or post_id != _numeric(_agree([data.get("post_id"), data.get("sidecar_media_id")])):
        raise InvalidData("Instagram attachment list belongs to another post")
    items = []
    for item in value["items"]:
        if item is None:
            items.append(None)
            continue
        if (not isinstance(item, dict) or set(item) != {"id", "kind"}
                or item["kind"] not in ("image", "video", "unknown")):
            raise InvalidData("Invalid Instagram source attachment")
        items.append({"id": _numeric(_id(item["id"])), "kind": item["kind"]})
    if not value["album"] and (len(items) != 1 or items[0] is None or items[0]["id"] != post_id):
        raise InvalidData("A single Instagram post must identify its own media")
    return {**value, "post_id": post_id, "items": items}


def _entry(item, *, static_videos=True):
    from .source import _id, _numeric

    if item is None or isinstance(item, dict) and item.get("pk") is None:
        return None
    if not isinstance(item, dict):
        raise InvalidData("Invalid original Instagram media item")
    kind = ("video" if item.get("media_type") == 2 or item.get("video_versions") else
            "image" if item.get("media_type") == 1 or item.get("image_versions2") else "unknown")
    if not static_videos and item.get("original_media_type") == 1 and item.get("media_type") != 1:
        kind = "image"
    return {"id": _numeric(_id(item["pk"])), "kind": kind}


def install(extractor):
    """Keep source order, including missing files, and split story containers."""
    from gallery_dl.extractor.common import Message
    from .source import _id, _numeric

    if getattr(extractor, "_native_instagram_evidence", False):
        return
    if extractor.config("audio", False) or extractor.config("previews", False) or extractor.config("covers", False):
        raise InvalidData("Native Instagram intake requires media without extra audio, preview or highlight-cover outputs")
    parse, original_items = extractor._parse_post, extractor.items

    def parse_post(self, post):
        story = "items" in post
        original = post["items"] if story else post.get("carousel_media", [post])
        if not isinstance(original, (list, tuple)) or not 1 <= len(original) <= 4096:
            raise InvalidData("Instagram source has no bounded original attachment list")
        entries = [_entry(item, static_videos=self._static_video) for item in original]
        originals = {}
        if story:
            for item, entry in zip(original, entries):
                if entry is None:
                    continue
                stamp = item.get("taken_at")
                if type(stamp) not in (int, float) or not math.isfinite(stamp) or stamp <= 0:
                    raise InvalidData("Instagram story lacks its original publication date")
                originals[entry["id"]] = (entry, stamp)
        result = parse(post)
        if story:
            for file in result["_files"]:
                # The pinned parser includes music-sticker audio even when
                # items() later omits it under the required audio=false policy.
                if file.get("audio_url"):
                    continue
                media_id = _numeric(_id(file["media_id"]))
                if media_id not in originals:
                    raise InvalidData("Instagram story lacks its original media identity")
                entry, stamp = originals[media_id]
                # Keep container metadata as evidence. The native post/date are
                # the individual story, shared across tray and highlight views.
                file["date"] = self.parse_timestamp(stamp)
                file[FIELD] = {"version": 1, "post_id": media_id, "album": False, "items": [entry],
                               "container": {"id": _id(result["post_id"]), "type": result["type"]}}
        else:
            result[FIELD] = {"version": 1, "post_id": _numeric(_id(result["post_id"])),
                             "album": "carousel_media" in post, "items": entries}
        return result

    def items(self):
        for kind, url, data in original_items():
            if data.get("type") in CONTAINERS:
                if kind == Message.Directory:
                    continue
                if kind == Message.Url:
                    # Filtering a reel container by its latest seen timestamp
                    # can drop older stories still inside the requested window.
                    manifest(data)
                    yield Message.Directory, "", copy.deepcopy(data)
            yield kind, url, data

    extractor._parse_post = types.MethodType(parse_post, extractor)
    extractor.items = types.MethodType(items, extractor)
    extractor._native_instagram_evidence = True


def collection_queue(extractor, url, data):
    """Only the pinned profile dispatcher may queue undated child collections."""
    from gallery_dl.extractor.instagram import (InstagramUserExtractor, InstagramPostsExtractor,
        InstagramStoriesExtractor, InstagramHighlightsExtractor, InstagramPhotosExtractor,
        InstagramReelsExtractor, InstagramTaggedExtractor)

    child = data.get("_extractor")
    allowed = (InstagramPostsExtractor, InstagramStoriesExtractor, InstagramHighlightsExtractor,
               InstagramPhotosExtractor, InstagramReelsExtractor, InstagramTaggedExtractor)
    return (isinstance(extractor, InstagramUserExtractor) and set(data) <= {"_extractor", "category", "subcategory"}
            and data.get("category", "instagram") == "instagram" and data.get("subcategory", "user") == "user"
            and child in allowed and child.from_url(url) is not None)
