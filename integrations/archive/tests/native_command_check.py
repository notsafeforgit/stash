"""Go invokes the portable CLI against its actual native database/main command."""

from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys

from stash_archive.bundle import export_archive, iter_artifacts
from stash_archive.storage import json_bytes


inputs = json.load(sys.stdin)
database = Path(inputs['database'])
root = database.parent
archive = root / 'valid-archive'
manifest = export_archive(database, archive, reserve=0)
library, = [entry for entry in iter_artifacts(archive, manifest) if entry['role'] == 'library']


def command(bundle):
    return subprocess.run([sys.executable, '-m', 'stash_archive.cli', 'verify', str(bundle),
                           '--native-validator', inputs['validator'],
                           '--producer-origin', 'https://stash.example',
                           '--temp-parent', str(root), '--reserve-bytes', '0'],
                          stdin=subprocess.DEVNULL, capture_output=True, timeout=120)


result = command(archive)
assert result.returncode == 0, result.stderr.decode(errors='replace')
proof = json.loads(result.stdout)
native = proof['native_snapshot']
assert native['archive_uuid'] == manifest['uuid']
assert native['manifest_sha256'] == hashlib.sha256(json_bytes(manifest)).hexdigest()
assert native['sha256'] == library['sha256']
assert native['bytes'] == library['size']
assert native['database_verified'] is True
assert native['filesystem_recovery_verified'] is False
assert proof['ingestion_receipts']['registered_producers_complete'] is True
assert proof['ingestion_receipts']['components'][0]['sha256'] == native['sha256']
assert not list(root.glob('stash-archive-verify-*'))

# Transport/integrity/FKs still succeed when a required native guard is absent.
# The real main command must reject it without printing a success receipt or
# creating application configuration, and the temporary restore is discarded.
with closing(sqlite3.connect(database)) as db:
    db.execute('DROP TRIGGER archive_scene_deleted')
    db.commit()
invalid = root / 'invalid-archive'
export_archive(database, invalid, reserve=0)
result = command(invalid)
assert result.returncode == 1, result.stderr.decode(errors='replace')
assert not result.stdout
assert b'Native snapshot validation failed' in result.stderr
assert not list(root.glob('stash-archive-verify-*'))
assert not Path(os.environ['STASH_CONFIG_FILE']).exists()
print(json.dumps({'verified': True}))
