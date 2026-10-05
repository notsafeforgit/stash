#!/usr/bin/env python3
"""Read-only backup coverage audit. Never requests Glacier restores or payloads."""
from __future__ import annotations

import argparse
from collections import Counter
from datetime import datetime, timezone
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import shutil
import sqlite3
import sys
import tempfile

sys.dont_write_bytecode = True
BIN_DIR = Path(__file__).resolve().parent
ARCHIVE_BUCKET = 'video-backup-andrew'
METADATA_BUCKET = 'metadata-backup-andrew'


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class MetadataReader:
    """Restrict S3 access to LISTs and small manifests in Standard storage."""
    def __init__(self, client):
        self.client = client

    def inventory(self):
        result = {}
        request = {'Bucket': ARCHIVE_BUCKET, 'MaxKeys': 1000}
        pages = 0
        while True:
            page = self.client.list_objects_v2(**request)
            for entry in page.get('Contents', []):
                result[entry['Key']] = {'size': entry['Size'], 'storage_class': entry.get('StorageClass')}
            pages += 1
            if pages % 50 == 0:
                print(f'Listed {len(result):,} current S3 objects...', flush=True)
            if not page.get('IsTruncated'):
                break
            request['ContinuationToken'] = page['NextContinuationToken']
        return result

    def manifest(self, key):
        if key not in ('current_manifest.json', 'current_manifest.txt'):
            raise ValueError('Audit payload reads are restricted to Standard-storage manifests')
        try:
            response = self.client.get_object(Bucket=METADATA_BUCKET, Key=key)
        except Exception as error:
            code = getattr(error, 'response', {}).get('Error', {}).get('Code')
            if code in ('NoSuchKey', '404', 'NotFound'):
                return None, None
            raise
        with response['Body'] as body:
            payload = body.read()
        modified = response.get('LastModified')
        return payload, modified.isoformat() if modified else None


def snapshot_ledgers(ledger, output):
    directory = output / 'ledger-snapshot'
    directory.mkdir()
    with (ledger / '.backup_run.lock').open('rb') as lock:
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        for name in ('tar_delta_state.sqlite3', 'tar_delta_state.sqlite3-wal',
                     'tarball_footprints.jsonl', 'video_manifest_previous.nul'):
            source = ledger / name
            if source.exists():
                shutil.copyfile(source, directory / name)
    return directory


def fetch_catalog(reader, snapshot, output, restorer):
    payload, modified = reader.manifest('current_manifest.json')
    if payload is not None:
        (output / 'published-manifest.json').write_bytes(payload)
        return restorer.validate_manifest(json.loads(payload)), modified, 'json'
    payload, modified = reader.manifest('current_manifest.txt')
    if payload is None:
        raise RuntimeError('No published backup manifest exists in Standard storage')
    legacy = output / 'published-manifest.txt'
    legacy.write_bytes(payload)
    catalog = restorer.load_legacy_manifest(legacy, snapshot / 'tar_delta_state.sqlite3', snapshot / 'tarball_footprints.jsonl')
    (output / 'published-manifest.json').write_text(json.dumps(catalog), encoding='utf-8')
    return catalog, modified, 'legacy text + local ledger snapshot'


def audit_local(backup, snapshot, catalog, inventory, issues_file, hash_nfo=False):
    """Compare local files with committed indices; archive contents stay cold."""
    counts = Counter()
    connection = sqlite3.connect(snapshot / 'tar_delta_state.sqlite3')
    connection.execute('PRAGMA query_only=ON')
    state = {row[0]: {'base': row[1], 'last_delta': row[2]} for row in connection.execute(
        'SELECT unit_id,base_key,last_delta_key FROM unit_state'
    )}
    footprints = {}
    for line in (snapshot / 'tarball_footprints.jsonl').read_text().splitlines():
        if line:
            value = json.loads(line)
            footprints[value.get('unit') or 'R:' + value['rel_dir']] = value.get('fp')
    published = {unit['unit_id']: unit for unit in catalog['units']} if catalog else {}
    published_videos = {video['key'] for video in catalog['videos']} if catalog else set()
    local_units = backup.collect_partitioned_tar_units(backup.BASE_DIR)
    local_ids = {unit['unit_id'] for unit in local_units}
    counts['local_tar_units'] = len(local_units)

    with issues_file.open('w', encoding='utf-8') as details:
        def issue(kind, **fields):
            counts[kind] += 1
            details.write(json.dumps({'kind': kind, **fields}, ensure_ascii=True) + '\n')

        for number, unit in enumerate(local_units, 1):
            uid = unit['unit_id']
            previous = {row[0]: (row[1], row[2]) for row in connection.execute(
                'SELECT relpath,size,nfohash FROM file_index WHERE unit_id=?', (uid,)
            )}
            selected = published.get(uid)
            generation = state.get(uid, {})
            trusted = bool(previous) and (catalog is None or (
                selected and generation.get('base') == selected['base_key']
                and (not generation.get('last_delta') or generation['last_delta'] in selected['deltas'])
            ))
            if previous and not trusted:
                issue('unpublished_or_mismatched_index_units', unit=uid)
            entries = backup.iter_tar_entries_direct(unit['abs_dir']) if unit['kind'] == 'rootfiles' else backup.iter_tar_entries_recursive(unit['abs_dir'])
            present, paths = set(), []
            for path, relative, st in entries:
                present.add(relative)
                paths.append(path)
                nfo = relative.lower().endswith('.nfo')
                counts['local_nfo_files' if nfo else 'local_image_files'] += 1
                key = str(Path(unit['rel_dir']) / relative)
                if trusted:
                    old = previous.get(relative)
                    if old is None:
                        issue('pending_nfo_additions' if nfo else 'pending_image_additions', path=key, unit=uid, size=st.st_size)
                    elif old[0] != st.st_size:
                        issue('pending_nfo_changes' if nfo else 'pending_image_size_changes', path=key, unit=uid)
                    elif nfo and hash_nfo and backup.compute_nfo_hash(path) != old[1]:
                        issue('pending_nfo_changes', path=key, unit=uid)
                    elif nfo and not hash_nfo:
                        counts['indexed_current_nfo_files_by_size_only'] += 1
                    else:
                        counts['indexed_current_nfo_files' if nfo else 'indexed_current_image_files'] += 1
                else:
                    counts['unindexed_or_unpublished_current_nfo_files' if nfo else 'unindexed_or_unpublished_current_image_files'] += 1
            if trusted:
                counts['units_with_published_file_index'] += 1
                for relative in sorted(set(previous) - present):
                    nfo = relative.lower().endswith('.nfo')
                    issue('pending_nfo_deletions' if nfo else 'pending_image_deletions', path=str(Path(unit['rel_dir']) / relative), unit=uid)
            elif selected and not previous:
                footprint = backup.compute_v2_footprint(paths, unit['abs_dir'])
                if footprint == footprints.get(uid):
                    counts['legacy_units_matching_fingerprint'] += 1
                else:
                    issue('legacy_units_needing_full_baseline', unit=uid, current_files=len(present))
            elif not selected and catalog is not None:
                issue('local_units_not_in_published_manifest', unit=uid, current_files=len(present))
            if number % 100 == 0:
                print(f'Compared {number}/{len(local_units)} metadata units...', flush=True)

        if catalog:
            for uid in sorted(set(published) - local_ids):
                issue('published_units_pending_retirement', unit=uid)
                for (relative,) in connection.execute('SELECT relpath FROM file_index WHERE unit_id=?', (uid,)):
                    kind = 'nfo_files_in_retired_units' if relative.lower().endswith('.nfo') else 'image_files_in_retired_units'
                    counts[kind] += 1

        local_videos = set()
        stack = [Path(backup.BASE_DIR)]
        base = Path(backup.BASE_DIR)
        while stack:
            directory = stack.pop()
            with os.scandir(directory) as entries:
                for entry in entries:
                    if entry.is_symlink():
                        counts['local_symlinks_excluded_from_backup'] += 1
                        continue
                    if entry.is_dir(follow_symlinks=False):
                        stack.append(Path(entry.path))
                    elif entry.is_file(follow_symlinks=False):
                        extension = os.path.splitext(entry.name)[1].lower()
                        if directory == base and extension in backup.TAR_INCLUDE_EXTENSIONS:
                            issue('tar_eligible_files_at_uncovered_source_root', path=entry.name)
                        if extension not in backup.VIDEO_EXTENSIONS:
                            continue
                        key = str(Path(entry.path).relative_to(base))
                        local_videos.add(key)
                        size = entry.stat(follow_symlinks=False).st_size
                        if inventory is not None:
                            remote = inventory.get(key)
                            if remote is None:
                                issue('local_videos_missing_remote', path=key, size=size, in_published_manifest=key in published_videos)
                            elif remote['size'] != size:
                                issue('local_videos_remote_size_mismatch', path=key, local_size=size, remote_size=remote['size'])
                            else:
                                counts['local_videos_matching_remote_size'] += 1
                        if catalog is not None and key not in published_videos:
                            issue('local_videos_not_in_published_manifest', path=key)
        counts['local_videos'] = len(local_videos)
        if catalog:
            for key in sorted(published_videos - local_videos):
                issue('published_videos_pending_deletion', path=key)
        if inventory is not None:
            referenced = set(published_videos)
            for unit in published.values():
                referenced.update([unit['base_key'], *unit['deltas']])
            counts['published_object_references'] = len(referenced)
            counts['remote_visible_objects'] = len(inventory)
            for key in sorted(referenced):
                if key not in inventory:
                    issue('published_objects_missing_remote', path=key)
                else:
                    counts['published_objects_present_remote'] += 1
            counts['remote_tar_archives_not_in_current_manifest'] = sum(key.startswith('tarballs/') and key not in referenced for key in inventory)
            for key in inventory:
                if not key.startswith('tarballs/') and key.lower().endswith(backup.VIDEO_EXTENSIONS) and key not in local_videos:
                    counts['remote_videos_without_current_local_path'] += 1
    connection.close()
    return dict(counts)


def write_report(output, result):
    (output / 'summary.json').write_text(json.dumps(result, indent=2) + '\n', encoding='utf-8')
    lines = [
        '# S3 backup audit', '', f"Started: {result['started_utc']}",
        f"Finished: {result['finished_utc']}", f"Published manifest: {result['manifest_format']}",
        f"Manifest last modified: {result.get('manifest_last_modified') or 'not checked'}", '',
        f"Native archive verification: {result.get('native_archive', {}).get('coverage', 'not checked')}", '',
        f"Rehashed indexed NFO content: {result.get('indexed_nfo_content_hashed', False)}", '',
        'Read-only: no source deletions, S3 writes, Glacier restores, or archive payload reads.', '',
        '| Check | Count |', '| --- | ---: |',
    ]
    lines += [f'| {key.replace("_", " ")} | {value:,} |' for key, value in sorted(result['counts'].items())]
    lines += [
        '', '## Interpretation', '',
        '- Pending image/NFO deletions are missing local paths still present in the published generation\'s file index. The next successful backup must encode deletion records or replace/exclude the unit with an accurate full snapshot.',
        '- A full replacement baseline represents the current file set without needing a per-file tombstone for each file absent from an unindexed legacy archive.',
        '- A matching legacy fingerprint checks names, sizes, and NFO content. It does not verify image bytes inside Glacier.',
        '- Indexed NFO files are checked by path and size unless --hash-nfo is used. Image and NFO deletions are always checked. Unindexed legacy fingerprints still require NFO content reads.',
        '- Existing image members and embedded deletion records cannot be inspected from S3 LIST/HEAD metadata. This audit validates object presence and local index coverage, not a physical cold-archive restore.',
        '- Image deletion records change reconstructed files. Old bytes remain in archive objects until compaction and lifecycle cleanup; individual archived images are not separate S3 objects to delete.',
        '- Archives absent from the current manifest can be retained history or pending lifecycle cleanup; they are not automatically classified as lost or safe to delete.',
        '- The local filesystem is scanned live. Scrapers can create files during the audit; new pending files since the published manifest are expected.',
        '', 'Detailed findings: `issues.jsonl`. No repair is performed by this command.',
    ]
    (output / 'report.md').write_text('\n'.join(lines) + '\n', encoding='utf-8')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=Path)
    parser.add_argument('--remote', action='store_true', help='List current S3 objects and read the Standard-storage manifest; never fetch archived data.')
    parser.add_argument('--native-checksums', action='store_true', help='With --remote, verify SHA-256 metadata for every native Standard-storage object.')
    parser.add_argument('--hash-nfo', action='store_true', help='Also rehash indexed NFO contents; all image paths and deletions are checked regardless.')
    parser.add_argument('--base-dir', type=Path, default=Path('/tank/media/porn'))
    parser.add_argument('--ledger-dir', type=Path, default=Path('/tank/media/backup_ledgers'))
    args = parser.parse_args(argv)
    if args.native_checksums and not args.remote:
        parser.error('--native-checksums requires --remote')
    output = args.output_dir or Path(tempfile.mkdtemp(prefix='s3-backup-audit-'))
    output.mkdir(mode=0o700, parents=True, exist_ok=True)
    result = {'started_utc': datetime.now(timezone.utc).isoformat(), 'manifest_format': 'local state only', 'indexed_nfo_content_hashed': args.hash_nfo}
    backup = load_module('s3_backup_audit_subject', BIN_DIR / 's3_log_backup.py')
    restorer = load_module('s3_backup_audit_restorer', BIN_DIR / 's3_restore_performer.py')
    backup.BASE_DIR = str(args.base_dir)
    if not args.base_dir.is_dir():
        raise RuntimeError('Source directory is unavailable; audit aborted')
    # Only the requested reports persist. Ledger copies and fetched manifests
    # are scratch data; /tmp is also cleared on reboot on the installed host.
    with tempfile.TemporaryDirectory(prefix='s3-audit-work-') as temporary:
        work = Path(temporary)
        snapshot = snapshot_ledgers(args.ledger_dir, work)
        catalog, inventory = None, None
        if args.remote:
            import boto3
            from botocore.config import Config
            reader = MetadataReader(boto3.client('s3', config=Config(connect_timeout=10, read_timeout=60,
                                                                   max_pool_connections=20, retries={'max_attempts': 3})))
            catalog, modified, manifest_format = fetch_catalog(reader, snapshot, work, restorer)
            result.update(manifest_last_modified=modified, manifest_format=manifest_format)
            if catalog['version'] == 3:
                if args.native_checksums:
                    from native_store import NativeStore
                    result['native_archive'] = NativeStore(reader.client, METADATA_BUCKET).audit(catalog['native_archive'])
                else:
                    result['native_archive'] = {'archive_uuid': catalog['native_archive']['archive_uuid'],
                                                'coverage': 'manifest-reference-only'}
            elif args.native_checksums:
                raise ValueError('Historical backups have no native archive to verify')
            print('Reading current S3 object metadata; archived payloads remain untouched.', flush=True)
            inventory = reader.inventory()
        print('Comparing local files with the ledger snapshot...', flush=True)
        result['counts'] = audit_local(backup, snapshot, catalog, inventory, output / 'issues.jsonl', hash_nfo=args.hash_nfo)
    result['finished_utc'] = datetime.now(timezone.utc).isoformat()
    write_report(output, result)
    print(json.dumps(result, indent=2))
    print(f'Report: {output / "report.md"}')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
