"""Immutable cold-media identities and verified restore, without S3 versions.

The manifest binds object bytes separately from their local restore paths.
Existing objects can be adopted from full-object checksums without thawing or
copying them. Unsupported evidence is an error, never a reason to reupload.
"""

import base64
import hashlib
import os
from pathlib import Path
import re
import zlib

from awscrt.checksums import crc32c, crc64nvme
from stash_archive.storage import open_regular, require_space, sync_directory

ALGORITHMS = {'sha256': ('ChecksumSHA256', 32), 'sha1': ('ChecksumSHA1', 20),
              'crc64nvme': ('ChecksumCRC64NVME', 8), 'crc32c': ('ChecksumCRC32C', 4), 'crc32': ('ChecksumCRC32', 4)}
SHA256 = re.compile(r'[0-9a-f]{64}\Z')
PREFIX = 'media/sha256/'
COLD = {'GLACIER', 'DEEP_ARCHIVE'}


def relative_path(value):
    if (not isinstance(value, str) or not value or any(part in ('', '.', '..') for part in value.split('/'))
            or '\0' in value):
        raise ValueError('Invalid relative media path')
    return value


def validate_store(value):
    if (not isinstance(value, dict) or set(value) != {'bucket', 'prefix'}
            or not isinstance(value['bucket'], str) or not re.fullmatch(r'[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]', value['bucket'])
            or not isinstance(value['prefix'], str)):
        raise ValueError('Invalid media store binding')
    if value['prefix']:
        if not value['prefix'].endswith('/'):
            raise ValueError('Media store prefix must end with a slash')
        relative_path(value['prefix'][:-1])
    return value


def signature(info):
    return info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns


class Digests:
    def __init__(self):
        self.sha256, self.sha1 = hashlib.sha256(), hashlib.sha1()
        self.crc64 = self.crc32c = self.crc32 = self.size = 0

    def update(self, block):
        self.sha256.update(block)
        self.sha1.update(block)
        self.crc64 = crc64nvme(block, self.crc64)
        self.crc32c = crc32c(block, self.crc32c)
        self.crc32 = zlib.crc32(block, self.crc32)
        self.size += len(block)

    def checksum(self, algorithm):
        values = {'sha256': self.sha256.digest(), 'sha1': self.sha1.digest(),
                  'crc64nvme': self.crc64.to_bytes(8, 'big'), 'crc32c': self.crc32c.to_bytes(4, 'big'),
                  'crc32': self.crc32.to_bytes(4, 'big')}
        return base64.b64encode(values[algorithm]).decode('ascii')


def hash_file(path):
    result = Digests()
    with open_regular(path) as source:
        before = signature(os.fstat(source.fileno()))
        while block := source.read(4 << 20):
            result.update(block)
        if before != signature(os.fstat(source.fileno())) or before != signature(Path(path).lstat()):
            raise ValueError('Media changed while computing its content checksums')
    return result


def object_key(sha256):
    if not isinstance(sha256, str) or not SHA256.fullmatch(sha256):
        raise ValueError('Invalid media content SHA-256')
    return PREFIX + sha256


def validate_object(record):
    if not isinstance(record, dict) or set(record) - {'sha256'} != {'size', 'checksum', 'storage_class'}:
        raise ValueError('Invalid immutable media descriptor')
    checksum = record['checksum']
    if (type(record['size']) is not int or record['size'] < 0
            or not isinstance(record['storage_class'], str) or record['storage_class'] not in COLD
            or not isinstance(checksum, dict) or set(checksum) != {'algorithm', 'value'}
            or not isinstance(checksum['algorithm'], str) or checksum['algorithm'] not in ALGORITHMS
            or not isinstance(checksum['value'], str)):
        raise ValueError('Invalid media size, storage class or checksum')
    try:
        raw = base64.b64decode(checksum['value'], validate=True)
    except (ValueError, TypeError):
        raise ValueError('Invalid media checksum encoding') from None
    if (len(raw) != ALGORITHMS[checksum['algorithm']][1]
            or base64.b64encode(raw).decode('ascii') != checksum['value']):
        raise ValueError('Invalid media checksum length or encoding')
    if 'sha256' in record and (not isinstance(record['sha256'], str) or not SHA256.fullmatch(record['sha256'])):
        raise ValueError('Invalid verified media SHA-256')
    return record


def matches_local(record, content):
    validate_object(record)
    checksum = record['checksum']
    return (content.size == record['size'] and content.checksum(checksum['algorithm']) == checksum['value']
            and ('sha256' not in record or content.sha256.hexdigest() == record['sha256']))


def verify_local(record, path):
    if not matches_local(record, hash_file(path)):
        raise ValueError('Media bytes do not match their retained content identity')


def object_from_head(head, *, local=None):
    if not isinstance(head, dict) or head.get('DeleteMarker') or head.get('ChecksumType') != 'FULL_OBJECT':
        raise ValueError('Media requires a full-object S3 checksum; refusing automatic reupload')
    algorithm = next((a for a, (field, _) in ALGORITHMS.items() if head.get(field)), None)
    if algorithm is None:
        raise ValueError('Media has no supported full-object checksum; review is required')
    record = validate_object({'size': head.get('ContentLength'), 'storage_class': head.get('StorageClass'),
                              'checksum': {'algorithm': algorithm, 'value': head[ALGORITHMS[algorithm][0]]}})
    if local is not None:
        if not matches_local(record, local):
            raise ValueError('Uploaded media does not match its local content checksums')
        record['sha256'] = local.sha256.hexdigest()
    return record


def verify_head(record, head):
    validate_object(record)
    if (not isinstance(head, dict) or type(head.get('ContentLength')) is not int
            or head.get('ContentLength') != record['size'] or head.get('DeleteMarker')
            or head.get('StorageClass') != record['storage_class'] or head.get('ChecksumType') != 'FULL_OBJECT'
            or head.get(ALGORITHMS[record['checksum']['algorithm']][0]) != record['checksum']['value']):
        raise ValueError('Retained media object is missing or changed')
    return head


def validate_inventory(catalog):
    videos, units, objects = catalog.get('videos'), catalog.get('units'), catalog.get('objects')
    if not isinstance(videos, list) or not isinstance(units, list) or not isinstance(objects, dict):
        raise ValueError('Media requires videos, units and object descriptors')
    keys, paths = set(), set()
    for video in videos:
        if not isinstance(video, dict) or set(video) != {'key', 'path', 'size'}:
            raise ValueError('Video requires an object key, restore path and size')
        relative_path(video['key'])
        relative_path(video['path'])
        if video['path'] in paths or type(video['size']) is not int or video['size'] < 0:
            raise ValueError('Duplicate restore path or invalid video size')
        paths.add(video['path'])
        keys.add(video['key'])
    for unit in units:
        if not isinstance(unit, dict) or not isinstance(unit.get('deltas'), list) or 'base_key' not in unit:
            raise ValueError('Invalid media archive unit')
        for key in (unit['base_key'], *unit['deltas']):
            relative_path(key)
            keys.add(key)
    if set(objects) != keys:
        raise ValueError('Media descriptors must cover exactly the selected objects')
    for key, record in objects.items():
        relative_path(key)
        validate_object(record)
        if key.startswith(PREFIX) and (record.get('sha256') is None or key != object_key(record['sha256'])):
            raise ValueError('Media key differs from its verified content SHA-256')
    for video in videos:
        record = objects[video['key']]
        if record['size'] != video['size'] or 'sha256' not in record:
            raise ValueError('Video is missing its locally verified content identity')
    validate_store(catalog.get('media_store'))
    return objects


def request(store, key):
    validate_store(store)
    relative_path(key)
    return {'Bucket': store['bucket'], 'Key': store['prefix'] + key, 'ChecksumMode': 'ENABLED'}


def restore_status(client, store, key, record):
    validate_object(record)
    head = verify_head(record, client.head_object(**request(store, key)))
    return {'key': key, 'storage_class': head['StorageClass'], 'restore': head.get('Restore')}


def request_restore(client, store, key, record, lifetime):
    if type(lifetime) is not int or lifetime < 1:
        raise ValueError('Thaw lifetime must be positive')
    restore_status(client, store, key, record)
    args = request(store, key)
    del args['ChecksumMode']
    try:
        client.restore_object(**args, RestoreRequest={'Days': lifetime, 'GlacierJobParameters': {'Tier': 'Bulk'}})
    except Exception as error:
        if getattr(error, 'response', {}).get('Error', {}).get('Code') != 'RestoreAlreadyInProgress':
            raise


def download(client, store, key, record, destination, *, reserve=50 << 30):
    validate_object(record)
    destination = Path(destination)
    destination.parent.mkdir(parents=True, exist_ok=True)
    require_space(destination.parent, record['size'], reserve)
    response = client.get_object(**request(store, key))
    with response['Body'] as body:
        verify_head(record, response)
        fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        try:
            content = Digests()
            with os.fdopen(fd, 'wb') as output:
                while block := body.read(4 << 20):
                    content.update(block)
                    if content.size > record['size']:
                        raise ValueError('Media download exceeded its expected size')
                    require_space(destination.parent, record['size'] - content.size, reserve)
                    output.write(block)
                if not matches_local(record, content):
                    raise ValueError('Downloaded media failed content verification')
                output.flush()
                os.fsync(output.fileno())
            sync_directory(destination.parent)
        except BaseException:
            destination.unlink()
            raise
