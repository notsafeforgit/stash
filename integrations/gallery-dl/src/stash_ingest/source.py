"""Source identities from captured evidence, never local filenames or num."""

import re
from urllib.parse import urlsplit

from .encoding import InvalidData
from . import instagram, mirror


class UnsupportedSource(InvalidData):
    pass


def _id(value):
    if type(value) is int:
        value = str(value)
    if (not isinstance(value, str) or not value or value != value.strip()
            or len(value) > 1024 or any(ord(c) < 32 or 127 <= ord(c) < 160 for c in value)):
        raise InvalidData("Captured source has no usable identifier")
    return value


def _agree(values):
    values = {_id(v) for v in values if v is not None and v != ""}
    if len(values) != 1:
        raise InvalidData("Captured source identifiers are absent or contradictory")
    return values.pop()


def _context(source):
    category = source.get("category")
    category = category.lower() if isinstance(category, str) else None
    for _ in range(32):
        parent = source.get("_reddit")
        if isinstance(parent, dict) and parent.get("id"):
            source, category = parent, "reddit"
            continue
        if category not in ("imgur", "redgifs"):
            return source, category.lower() if isinstance(category, str) else None
        if source.get("_parent") is not None:
            parent = source["_parent"]
            if not isinstance(parent, dict) or not isinstance(parent.get("category"), str) or not parent["category"]:
                raise InvalidData("Captured extractor parent has no category")
            source, category = parent, parent["category"].lower()
            continue
        return source, category.lower() if isinstance(category, str) else None
    raise InvalidData("Captured extractor ancestry exceeds limit")


def _numeric(value):
    if not re.fullmatch(r"[0-9]{1,30}", value):
        raise InvalidData("Captured source post ID is not numeric")
    return value


def _bluesky(data):
    author = data.get("author")
    did = author.get("did") if isinstance(author, dict) else None
    key = data.get("post_id")
    did = _id(did) if did is not None and did != "" else None
    key = _id(key) if key is not None and key != "" else None
    uri = data.get("uri")
    if uri is not None and uri != "":
        if not isinstance(uri, str) or not uri.startswith("at://"):
            raise InvalidData("Invalid captured Bluesky post URI")
        parts = uri[5:].split("/")
        if (len(parts) != 3 or parts[1] != "app.bsky.feed.post"
                or did not in (None, "", parts[0]) or key not in (None, "", parts[2])):
            raise InvalidData("Captured Bluesky post identifiers disagree")
        did, key = parts[0], parts[2]
    did, key = _id(did), _id(key)
    if (not re.fullmatch(r"did:(?:plc:[A-Za-z0-9]+|web:[A-Za-z0-9.:-]+)", did)
            or not re.fullmatch(r"[A-Za-z0-9._~-]+", key)):
        raise InvalidData("Invalid captured Bluesky post identity")
    return did + "/" + key


def post(source):
    data, category = _context(source)
    if category == "reddit":
        value = _id(data.get("id"))
    elif category == "twitter":
        legacy = data.get("legacy") or data
        value = _agree([data.get("tweet_id"), data.get("rest_id"), legacy.get("id_str")])
    elif category == "bluesky":
        value = _bluesky(data)
    elif category in {"tiktok", "patreon", "fansly"}:
        value = _numeric(_id(data.get("id")))
    elif category == "instagram":
        evidence = instagram.manifest(data)
        if evidence is not None:
            return {"namespace": "native:instagram", "value": evidence["post_id"]}
        if data.get("type") in ("story", "highlight"):
            raise UnsupportedSource("Instagram story/highlight containers need a separate identity adapter")
        value = _numeric(_agree([data.get("post_id"), data.get("sidecar_media_id")]))
    elif category in {"kemono", "coomer"}:
        service = data.get("service")
        if not isinstance(service, str) or not re.fullmatch(r"[a-z0-9][a-z0-9_.-]*", service.lower()):
            raise InvalidData("Captured mirror post has no valid service")
        parts = [_id(data.get(key)) for key in ("user", "id")]
        if any(any(c in part for c in "/\\?#%") for part in parts):
            raise InvalidData("Captured mirror post contains an ambiguous path component")
        namespace = "mirror:" + category + ":" + service.lower()
        if len(namespace) > 128:
            raise InvalidData("Captured mirror service namespace exceeds limit")
        return {"namespace": namespace, "value": _id("/".join(parts))}
    else:
        raise UnsupportedSource("This extractor still needs a native post identity adapter")
    return {"namespace": "native:" + category, "value": value}


def metadata(source):
    """Source text and post dates, without substituting an attachment caption."""
    data, category = _context(source)
    text_keys, date_keys = ("content", "selftext", "title"), ("date",)
    if category == "bluesky":
        text_keys, date_keys = ("text",), ("createdAt", "date")
    elif category == "tiktok":
        text_keys = ("desc", "title")
    elif category == "instagram":
        text_keys, date_keys = ("description",), (("date",) if data.get("type") in instagram.CONTAINERS else ("post_date",))
    elif category == "patreon":
        date_keys = ("published_at", "date")
    elif category in {"kemono", "coomer"}:
        # The extractor's date can mean mirror import time instead.
        date_keys = ("published",)
    result = {}
    for field, keys in (("title", ("title",)), ("original_text", text_keys),
                        ("published_at", date_keys), ("language", ("lang", "language"))):
        for key in keys:
            if isinstance(data.get(key), str) and data[key]:
                result[field] = data[key]
                break
    if result.get("published_at"):
        result["date_basis"] = "source"
    return result


def reddit_media(value):
    if not isinstance(value, str):
        return None
    if value.startswith("ytdl:"):
        value = value[5:]
    try:
        parsed = urlsplit(value)
        if (parsed.scheme not in {"http", "https"} or parsed.username is not None
                or parsed.password is not None or parsed.port is not None):
            return None
        parts = parsed.path.strip("/").split("/")
        if parsed.hostname in {"i.redd.it", "preview.redd.it"} and len(parts) == 1:
            stem, dot, ext = parts[0].rpartition(".")
            if not dot or ext.lower() not in {"jpg", "jpeg", "png", "webp", "avif", "jxl", "gif", "mp4", "webm"}:
                return None
        elif parsed.hostname == "v.redd.it" and len(parts) <= 2:
            stem = parts[0]
        else:
            return None
        return stem if re.fullmatch(r"[A-Za-z0-9]+", stem) else None
    except ValueError:
        return None


def _twitter_url(value):
    if not isinstance(value, str):
        return None
    try:
        url = urlsplit(value)
        if url.scheme not in {"https", "http"} or url.username is not None or url.port is not None:
            return None
        if url.hostname == "pbs.twimg.com" and url.path.startswith("/media/"):
            # Original, named-size and query-format encodings retain a media key.
            return (url.hostname, url.path.split(":", 1)[0].rsplit(".", 1)[0])
        if url.hostname == "video.twimg.com":
            return (url.hostname, url.path)
    except ValueError:
        pass
    return None


def attachment(source):
    """Return the exact observed attachment or require review before download."""
    ref = post(source)
    data, category = _context(source)
    download_url = source.get("_url")
    if category == "reddit":
        parents = data.get("crosspost_parent_list")
        if parents:
            if not isinstance(parents, list) or not isinstance(parents[-1], dict):
                raise InvalidData("Invalid Reddit crosspost evidence")
            parent = parents[-1]
            parent_id = _id(parent.get("id"))
            if data.get("crosspost_parent") not in {None, "", "t3_" + parent_id}:
                raise InvalidData("Reddit crosspost identity is contradictory")
            data = parent
        selected = reddit_media(download_url)
        gallery = data.get("gallery_data")
        if isinstance(gallery, dict):
            known = {_id(item["media_id"]) for item in gallery.get("items", [])
                     if isinstance(item, dict) and item.get("media_id") is not None}
            if selected in known:
                return {"namespace": ref["namespace"], "value": selected}
        elif not data.get("is_gallery") and gallery is None:
            values = [reddit_media(data.get(k)) for k in ("url", "url_overridden_by_dest")]
            for key in ("media", "secure_media"):
                video = (data.get(key) or {}).get("reddit_video") or {}
                values += [reddit_media(video.get(k)) for k in ("fallback_url", "dash_url", "hls_url")]
            known = {v for v in values if v is not None}
            if len(known) > 1:
                raise InvalidData("Reddit post media identifiers disagree")
            if selected is not None and known == {selected}:
                return {"namespace": ref["namespace"], "value": selected}
    elif category == "twitter":
        legacy = data.get("legacy") or data
        entities = (legacy.get("extended_entities") or {}).get("media")
        if not isinstance(entities, list):
            raise UnsupportedSource("Twitter capture needs its original attachment manifest")
        declared = source.get("media_id")
        selected = _twitter_url(download_url)
        matches = set()
        for item in entities:
            if not isinstance(item, dict):
                continue
            key = _agree([item.get("id_str"), item.get("id")])
            if declared is not None:
                if _id(declared) == key:
                    matches.add(key)
                continue
            urls = [item.get("media_url_https"), item.get("media_url")]
            urls += [v.get("url") for v in (item.get("video_info") or {}).get("variants", []) if isinstance(v, dict)]
            if selected is not None and any(_twitter_url(url) == selected for url in urls):
                matches.add(key)
        if len(matches) == 1:
            return {"namespace": ref["namespace"], "value": matches.pop()}
    elif category == "instagram":
        evidence = instagram.manifest(data)
        if evidence is not None:
            selected = _numeric(_id(data.get("media_id")))
            if any(item is not None and item["id"] == selected for item in evidence["items"]):
                return {"namespace": ref["namespace"], "value": selected}
    elif category in ("kemono", "coomer"):
        evidence = mirror.manifest(data)
        if evidence is not None:
            selected = mirror.reference(data.get("path"), category)
            if any(item is not None and item['id'] == selected for item in evidence['items']):
                return {"namespace": ref["namespace"], "value": selected}
    raise UnsupportedSource("Downloaded media cannot be uniquely matched to the captured source list")
