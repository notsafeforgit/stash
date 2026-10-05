"""Original Kemono/Coomer membership, before configured download selection."""

import copy
import re
import types
from urllib.parse import urlsplit

from .encoding import InvalidData

FIELD = 'mirror_media'
IMAGES = frozenset('jpg jpeg png webp avif jxl bmp'.split())
VIDEOS = frozenset('mp4 mkv webm mov avi m4v wmv mpeg mpg ts'.split())
VISUAL = IMAGES | VIDEOS | {'gif'}
INLINE = re.compile(r'src="(?:https?://(?:kemono\.cr|coomer\.st))?(/inline/[^\"]+|/[0-9a-f]{2}/[0-9a-f]{2}/[0-9a-f]{64}\.[^\"]+)')


def reference(value, category):
    """A source path is an identifier, never a local filesystem grant."""
    from .source import _id

    value = _id(value).replace('\\', '/')
    if len(value.encode('utf-8')) > 1024 or re.search(r'%(?![0-9a-fA-F]{2})', value):
        raise InvalidData('Mirror attachment has no bounded original source path')
    if value.startswith(('https://', 'http://')):
        try:
            parsed = urlsplit(value)
            if (not re.fullmatch(category + r'\.(?:cr|st|su|party)', parsed.hostname or '')
                    or parsed.username is not None or parsed.password is not None or parsed.port is not None
                    or '?' in value or '#' in value):
                raise ValueError()
            value = parsed.path
            if value.startswith('/data/'):
                value = value[5:]
        except ValueError:
            raise InvalidData('Mirror attachment has no stable original source path') from None
    if (not value.startswith('/') or value.startswith('//') or value in ('/', '/data')
            or any(part in ('.', '..') for part in value.split('/'))
            or '?' in value or '#' in value):
        raise InvalidData('Mirror attachment has no stable original source path')
    return _id(value)


def _entry(value, category):
    if value is None:
        return None
    if not isinstance(value, dict):
        raise InvalidData('Invalid original mirror attachment')
    if not value.get('path'):
        return None
    key = reference(value['path'], category)
    extension = key.rsplit('.', 1)[-1].lower()
    # GIFs can be converted to scenes by the retained host postprocessor.
    kind = 'image' if extension in IMAGES else 'video' if extension in VIDEOS else 'unknown'
    return {'id': key, 'kind': kind}


def membership(data, category):
    attachments = data.get('attachments')
    if not isinstance(attachments, (list, tuple)) or len(attachments) > 4096:
        raise InvalidData('Mirror post needs its bounded original attachment list')
    items = [_entry(item, category) for item in attachments]
    seen = {item['id'] for item in items if item is not None}
    primary = data.get('file')
    if primary:
        item = _entry(primary, category)
        # A primary-file alias of an attachment is not another source slot.
        # Repeated positions within the attachment array remain intact.
        if item is None or item['id'] not in seen:
            items.insert(0, item)
            if item is not None:
                seen.add(item['id'])
    content = data.get('content') or ''
    if not isinstance(content, str):
        raise InvalidData('Mirror inline media requires original post text')
    for match in INLINE.finditer(content):
        item = _entry({'path': match[1]}, category)
        if item['id'] not in seen:
            items.append(item)
            seen.add(item['id'])
            if len(items) > 4096:
                raise InvalidData('Mirror source attachment list exceeds 4096 entries')
    if len(items) > 4096:
        raise InvalidData('Mirror source attachment list exceeds 4096 entries')
    return items


def manifest(data):
    from .source import post

    evidence = data.get(FIELD)
    if evidence is None:
        return None
    if (not isinstance(evidence, dict) or set(evidence) != {'version', 'post', 'items'}
            or type(evidence['version']) is not int or evidence['version'] != 1
            or evidence['post'] != post(data) or not isinstance(evidence['items'], list)):
        raise InvalidData('Invalid mirror source attachment manifest')
    items = membership(data, data['category'].lower())
    if evidence['items'] != items:
        raise InvalidData('Mirror attachment manifest differs from its original post fields')
    return evidence


def install(extractor):
    """Retain all source slots while the pinned extractor selects downloads."""
    from .source import post
    from gallery_dl.extractor.kemono import KemonoUserExtractor, KemonoPostExtractor, KemonoPostsExtractor

    if getattr(extractor, '_native_mirror_evidence', False):
        return
    if not isinstance(extractor, (KemonoUserExtractor, KemonoPostExtractor, KemonoPostsExtractor)):
        raise InvalidData('This mirror collection needs a separate native identity and traversal adapter')
    if extractor.config('original', False) is not True:
        raise InvalidData('Native Kemono/Coomer downloads require original=true')
    build = extractor._build_file_generators

    def generators(self, filetypes):
        original = build(filetypes)

        def files(data):
            context = {**data, 'category': self.category}
            data[FIELD] = {'version': 1, 'post': post(context), 'items': membership(context, self.category)}
            for generate in original:
                # Pinned generators annotate files, and items() pops file IDs.
                # Keep the original post lists stable across all its captures.
                selected = copy.deepcopy(data)
                selected['attachments'] = [item for item in selected['attachments'] if item and item.get('path')]
                if not selected.get('file') or not selected['file'].get('path'):
                    selected['file'] = {}
                yield from generate(selected)
        return (files,)

    extractor._build_file_generators = types.MethodType(generators, extractor)
    extractor._native_mirror_evidence = True
