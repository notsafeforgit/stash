"""Prepare only the explicitly requested, pinned external compatibility clients."""
import json
import os
from pathlib import Path
import re
import subprocess


def main():
    root = Path(__file__).resolve().parents[1]
    lock = json.loads((root / 'integration-clients.json').read_text())
    revision, repository = lock['revision'], lock['repository']
    if not re.fullmatch('[a-f0-9]{40}', revision):
        raise SystemExit('Expected a full integration release commit')
    destination = Path(os.environ.get('STASH_INTEGRATIONS_ROOT', root / '.local/integration-clients')).resolve()
    if not destination.exists():
        subprocess.run(['git', 'clone', '--filter=blob:none', repository, str(destination)], check=True)
    origin = subprocess.check_output(['git', '-C', str(destination), 'remote', 'get-url', 'origin'], text=True).strip()
    if origin != repository:
        raise SystemExit('Integration checkout has an unexpected origin; refusing to modify it')
    dirty = subprocess.check_output(['git', '-C', str(destination), 'status', '--porcelain'], text=True)
    if dirty:
        raise SystemExit('Integration checkout has local changes; preserve them before updating')
    subprocess.run(['git', '-C', str(destination), 'fetch', '--depth=1', 'origin', revision], check=True)
    subprocess.run(['git', '-C', str(destination), 'checkout', '--detach', revision], check=True)


if __name__ == '__main__':
    main()
