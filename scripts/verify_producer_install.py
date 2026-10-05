"""Verify a real installed producer, with Python isolated from workspace imports."""

import hashlib
from pathlib import Path

import stash_ingest


def modules(directory):
    return {path.relative_to(directory).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in directory.rglob('*.py')}


source = Path(__file__).resolve().parents[1] / 'integrations/gallery-dl/src/stash_ingest'
installed = Path(stash_ingest.__file__).resolve().parent
if source == installed:
    raise SystemExit('Installation verification loaded workspace sources; run Python with -I')
expected, actual = modules(source), modules(installed)
if expected != actual:
    extra = sorted(actual.keys() - expected.keys())
    missing = sorted(expected.keys() - actual.keys())
    changed = sorted(key for key in expected.keys() & actual.keys() if expected[key] != actual[key])
    raise SystemExit(f'Installed producer differs: extra={extra}, missing={missing}, changed={changed}')
print(f'Installed producer matches all {len(expected)} current source modules')
