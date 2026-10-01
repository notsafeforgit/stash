"""Trim redundant Reddit media renditions in saved copies, never live extractors."""

import re
from urllib.parse import urlsplit


def _url(value):
    if not isinstance(value, str) or not value.startswith(('https://', 'http://')):
        return False
    if any(ord(c) < 32 or ord(c) == 127 for c in value):
        return False
    try:
        parsed = urlsplit(value)
        if re.search(r'%(?![0-9a-fA-F]{2})', parsed.path + parsed.fragment):
            return False
        # urlsplit accepts textual ports; Go's source URL parser rejects them.
        authority = parsed.netloc.rsplit('@', 1)[-1]
        suffix = authority[authority.rfind(']') + 1:] if authority.startswith('[') else authority
        if ':' in suffix and not re.fullmatch(r'[0-9]*', suffix.rsplit(':', 1)[-1]):
            return False
        return bool(parsed.hostname)
    except ValueError:
        return False


def _usable(value):
    return isinstance(value, dict) and any(_url(value.get(k)) for k in ('u', 'url', 'gif', 'mp4'))


def _size(value):
    def number(key, other):
        raw = value.get(key, value.get(other, 0))
        if isinstance(raw, str) and not re.fullmatch(r'[+-]?[0-9]+', raw.strip()):
            return 0
        if isinstance(raw, (str, int)) and len(str(raw).strip()) > 128:
            return 0
        try:
            return max(0, int(raw))
        except (ValueError, TypeError, OverflowError):
            return 0
    w, h = number('x', 'width'), number('y', 'height')
    return w * h, w, h


def _best(values):
    candidates = [v for v in values if _usable(v)] if isinstance(values, list) else []
    return max(candidates, key=_size, default=None)


def _gallery_image(item):
    # s is the full-size source, p scaled previews, and o obfuscated previews.
    if _usable(item.get('s')):
        item.pop('p', None)
        item.pop('o', None)
        return
    best = _best(item.get('p'))
    if best:
        item['p'] = [best]
        item.pop('o', None)
        return
    item.pop('p', None)
    best = _best(item.get('o'))
    if best:
        item['o'] = [best]
    else:
        item.pop('o', None)


def _preview_image(item):
    if _usable(item.get('source')):
        item.pop('resolutions', None)
    else:
        best = _best(item.get('resolutions'))
        if best:
            item['resolutions'] = [best]
        else:
            item.pop('resolutions', None)
    variants = item.get('variants')
    if isinstance(variants, dict):
        clear = (_usable(item.get('source')) or bool(_best(item.get('resolutions')))
                 or any(isinstance(v, dict) and _usable(v.get('source'))
                        for k, v in variants.items() if k in {'gif', 'mp4'}))
        for kind, variant in list(variants.items()):
            if not isinstance(variant, dict):
                continue
            _preview_image(variant)
            # Animated encodings are distinct media; blurred copies are not.
            if clear and kind in {'obfuscated', 'nsfw', 'nsfw_blurred', 'blurred'}:
                del variants[kind]
        if not variants:
            item.pop('variants', None)


def _reddit_image_id(url):
    if not _url(url):
        return None
    parsed = urlsplit(url)
    if parsed.hostname not in {'i.redd.it', 'preview.redd.it'}:
        return None
    return parsed.path.rsplit('/',1)[-1].rsplit('.',1)[0]


def _post(data):
    originals = set()
    # Only post-level URLs: a per-file _url must not change the shared body.
    for key in ('url', 'url_overridden_by_dest'):
        value = data.get(key)
        if _url(value) and urlsplit(value).hostname == 'i.redd.it':
            if mid := _reddit_image_id(value):
                originals.add(mid)
    gallery = data.get('media_metadata')
    sources = set()
    if isinstance(gallery, dict):
        for mid, item in gallery.items():
            if not isinstance(item, dict):
                continue
            _gallery_image(item)
            if mid and _usable(item.get('s')):
                sources.add(mid)
    video = any(isinstance(data.get(k), dict) and isinstance(data[k].get('reddit_video'), dict)
                and any(_url(data[k]['reddit_video'].get(u)) for u in ('fallback_url', 'dash_url', 'hls_url'))
                for k in ('media', 'secure_media'))
    preview = data.get('preview')
    if isinstance(preview, dict):
        images = preview.get('images')
        if isinstance(images, list):
            kept = []
            for item in images:
                if not isinstance(item, dict):
                    kept.append(item)
                    continue
                _preview_image(item)
                src = item.get('source') or _best(item.get('resolutions')) or {}
                mid = _reddit_image_id(src.get('url')) if isinstance(src, dict) else None
                variants = item.get('variants')
                animated = isinstance(variants, dict) and bool(set(variants) & {'gif', 'mp4'})
                if video or (not animated and (mid in originals or item.get('id') in sources or mid in sources)):
                    continue
                kept.append(item)
            if kept:
                preview['images'] = kept
            else:
                preview.pop('images', None)
        if video:
            preview.pop('reddit_video_preview', None)
        if not set(preview) - {'enabled'}:
            data.pop('preview', None)
    has_media = bool(originals or sources or video)
    if isinstance(gallery, dict):
        has_media |= any(isinstance(v, dict) and bool(_best(v.get('p')) or _best(v.get('o')))
                         for v in gallery.values())
    preview = data.get('preview')
    if isinstance(preview, dict):
        images = preview.get('images')
        if isinstance(images, list):
            has_media |= any(isinstance(v, dict) and (_usable(v.get('source')) or bool(_best(v.get('resolutions')))) for v in images)
        has_media |= isinstance(preview.get('reddit_video_preview'), dict)
    if has_media:
        for key in ('thumbnail', 'thumbnail_width', 'thumbnail_height'):
            data.pop(key, None)
    return data


def retain(value, reddit=False):
    """Copy metadata, keeping a source or the largest available preview.

    Unknown extractor fields and stream manifests remain untouched. Parent posts,
    crossposts, comments and enrichment attachments follow the same policy.
    """
    if isinstance(value, list):
        return [retain(v, reddit) for v in value]
    if not isinstance(value, dict):
        return value
    reddit = reddit or value.get('category') == 'reddit'
    result = {k: retain(v, k == '_reddit' or reddit and k in {'crosspost_parent_list', 'comment', 'enrichment_attachments'})
              for k, v in value.items()}
    return _post(result) if reddit else result
