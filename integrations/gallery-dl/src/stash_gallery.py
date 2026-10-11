"""Set up and operate gallery-dl intake without installation-specific wrappers.

This command composes the existing producer APIs. Keeping onboarding outside
stash_ingest also keeps setup-only edits out of saved worker policy hashes.
"""

import argparse
from contextlib import closing, contextmanager
import fcntl
from http.client import HTTPException
import json
import os
from pathlib import Path
import sqlite3
import stat
import sys
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, Request, build_opener
import uuid

from stash_ingest.application_key import application_key
from stash_ingest.client import Client, NoRedirect, Unavailable
from stash_ingest.config_migration import publish_profile
from stash_ingest.configuration import Configuration, LAYERED_SCHEMA, runtime_identity
from stash_ingest.encoding import InvalidData, decode, encode, identifier
from stash_ingest.endpoint import origin
from stash_ingest.filesystem import Root
from stash_ingest import manual
from stash_ingest.outbox import Outbox
from stash_ingest.source_lists import publish

FORMAT = 'stash-gallery-setup-v1'
LIMIT = 1 << 20
TOKEN_ENV = 'STASH_GALLERY_SCOPED_TOKEN'


def private_directory(path):
    path = Path(path).expanduser().absolute()
    if path.is_symlink():
        raise InvalidData('The setup directory must not be a symlink')
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = path.stat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise InvalidData('The setup directory must be private (chmod 700) and owned by this user')
    return path.resolve(strict=True)


def read_private(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise InvalidData('Setup files must be private (chmod 600) and owned by this user')
        return decode(source.read(LIMIT + 1), LIMIT)


@contextmanager
def environment(name, value):
    before = os.environ.get(name)
    os.environ[name] = value
    try:
        yield
    finally:
        if before is None:
            os.environ.pop(name, None)
        else:
            os.environ[name] = before


class SetupClient:
    """Application authentication is used only for explicit setup/registration."""

    def __init__(self, endpoint, key_env):
        self.endpoint = origin(endpoint)
        self.key_env = key_env
        self.opener = build_opener(ProxyHandler({}), NoRedirect())

    def request(self, method, path, value=None):
        request = Request(self.endpoint + '/api/v3/' + path, method=method,
                          data=None if value is None else encode(value, LIMIT),
                          headers={'ApiKey': application_key(self.key_env),
                                   'Accept': 'application/json', 'Content-Type': 'application/json'})
        try:
            with self.opener.open(request, timeout=30) as response:
                if response.status == 204 and method == 'DELETE':
                    return None
                if (response.status not in (200, 201) or response.headers.get_content_type() != 'application/json'
                        or response.headers.get('Content-Encoding') is not None):
                    raise Unavailable('invalid_setup_response')
                return decode(response.read(LIMIT + 1), LIMIT)
        except HTTPError as error:
            status = error.code
            error.close()
            code = {401: 'stash_application_key_rejected', 403: 'stash_application_access_denied',
                    503: 'stash_unavailable_enable_source_tracking_and_restart'}.get(status, 'stash_setup_request_rejected')
            raise Unavailable(code, status) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable('stash_unreachable') from None

    def pages(self, path):
        after = ''
        while True:
            rows = self.request('GET', path + '?after=' + after)
            if not isinstance(rows, list) or len(rows) > 50:
                raise InvalidData('Invalid setup listing response')
            for row in rows:
                current = identifier(row['uuid'])
                if current <= after:
                    raise InvalidData('Setup listing did not advance')
                after = current
                yield row
            if len(rows) < 50:
                return


def metadata_policy():
    # Use captured text on creation; do not assign publishers as performers or
    # overwrite existing entity edits. Ownership remains a separate decision.
    rule = {'on_create': True, 'on_existing': False, 'skip_organized_on_create': True,
            'mark_organized': False, 'filename_title_fallback': True,
            'mappings': {
                'title': {'jq': '.source.metadata.title | select(type == "string" and length > 0)'},
                'details': {'jq': '.source.metadata.original_text | select(type == "string" and length > 0)'},
                'urls': {'jq': '.entity.urls as $existing | .source | select(.urls_complete) | ($existing + .urls | unique) | select(length > 0)'},
            }}
    return {'enabled': True, 'apply_to_scans': False, 'rules': {'scene': rule, 'image': rule}}


def ensure_document(path, value):
    if path.exists() or path.is_symlink():
        # Compare serialized JSON including dictionary order: conditional
        # gallery-dl configuration maps are ordered, not sets of keys.
        if json.dumps(read_private(path)) != json.dumps(value):
            raise InvalidData('Setup would replace existing configuration; use its existing directory and inputs')
    else:
        publish_profile(path, value)


def check_folder(folder, root):
    if (not isinstance(folder, dict) or folder.get('uuid') != root or folder.get('state') != 'active'
            or not isinstance(folder.get('binding'), dict) or not folder['binding'].get('directory_identity')):
        raise InvalidData('Choose an active, bound folder from Settings > Library')


def existing_queue(directory):
    try:
        info = (directory / 'outbox.sqlite').lstat()
    except FileNotFoundError:
        raise InvalidData('Saved outbox is missing; restore its mount or backup instead of creating a replacement') from None
    if not stat.S_ISREG(info.st_mode):
        raise InvalidData('Saved outbox must be a regular file, not a symlink')


def prepare(directory, endpoint, root, media, configs, key_env):
    """Finish local validation before creating server credentials. Retry in place."""
    endpoint, root = origin(endpoint), identifier(root)
    media = Path(media).expanduser().resolve(strict=True)
    root_identity = list(Root.probe(media))
    if directory.is_relative_to(media) or media.is_relative_to(directory):
        raise InvalidData('Keep worker state and the media folder in separate directories')
    files = [str(Path(path).expanduser().resolve(strict=True)) for path in configs]
    options = {'endpoint': endpoint, 'root_uuid': root, 'media_path': str(media), 'gallery_configs': files}
    path = directory / 'setup.json'
    if path.exists():
        state = read_private(path)
        if state.get('schema') != FORMAT or state.get('options') != options:
            raise InvalidData('This directory already belongs to a different setup; its queue has been preserved')
    else:
        state = {'schema': FORMAT, 'uuid': str(uuid.uuid4()), 'options': options}
        publish_profile(path, state)

    def save():
        publish(path, encode(state, LIMIT), replace=True)

    if state.get('complete'):
        existing_queue(directory)
        manual.Runtime(directory / 'manual.json')
        return state
    runtime_identity()
    app = SetupClient(endpoint, key_env)
    check_folder(app.request('GET', 'archive/media-roots/' + root), root)
    for name in ('locks', 'archives', 'requests'):
        private_directory(directory / name)
    defaults = directory / 'gallery-defaults.json'
    # No sidecar writers or detail-enrichment profiles. Explicit configs retain
    # website settings and naming; these defaults provide a confined destination.
    ensure_document(defaults, {'extractor': {
        'base-directory': str(media), 'archive': [str(directory / 'archives'), '{category}.sqlite3'],
        'skip': True, 'ytdl': {'module': 'yt_dlp'},
    }})
    profiles = {}
    for adapter, filename in (('gallery-dl', 'gallery-worker.json'), ('yt-dlp', 'ytdlp-worker.json')):
        value = {'schema': LAYERED_SCHEMA,
                 'root': {'uuid': root, 'path': str(media), 'identity': root_identity},
                 'locks': {'path': str(directory / 'locks'), 'identity': list(Root.probe(directory / 'locks'))},
                 'config_files': [str(defaults), *files], 'working_directory': str(directory),
                 'source_mode': 'traversal', 'source_adapter': adapter, 'scan_mode': 'initial'}
        # All invalid config/asset/private references fail before registration.
        Configuration.from_document(value, directory).check()
        ensure_document(directory / filename, value)
        profiles[adapter] = str(directory / filename)
    ensure_document(directory / 'manual.json', {'schema': manual.FORMAT, 'state': str(directory / 'requests'),
                                              'profiles': profiles, 'new_source_policy': metadata_policy()})
    ensure_document(directory / 'dispatch.json', {'schema': 'stash-gallery-dispatch-v1', 'uuid': state['uuid'],
        'profiles': [{'id': adapter, 'operation': 'download', 'profile': filename} for adapter, filename in profiles.items()]})

    # A stable, setup-specific label recovers a committed create whose response
    # was lost. Do not adopt a user's pre-existing producer or outbox.
    label = 'gallery-dl ' + identifier(state['uuid'])
    if 'producer_uuid' not in state:
        matches = [row for row in app.pages('ingest-admin/producers') if row.get('label') == label]
        if len(matches) > 1:
            raise InvalidData('Multiple producers share this setup identity; review before continuing')
        producer = matches[0] if matches else app.request('POST', 'ingest-admin/producers', {'label': label})
        state['producer_uuid'] = identifier(producer['uuid'])
        save()
    token_path = directory / 'credential.json'
    if not token_path.exists():
        # Tokens are returned only once. If issuance committed before a lost
        # response/local write, retire that unrecoverable setup token on retry.
        # This is our dedicated new producer, never an existing worker's token.
        route = 'ingest-admin/producers/' + state['producer_uuid'] + '/credentials'
        for row in app.pages(route):
            if not row.get('revoked'):
                app.request('DELETE', 'ingest-admin/credentials/' + identifier(row['uuid']))
        credential = app.request('POST', route, {'scopes': [], 'root_uuids': [root], 'expires_at': None})
        validate_credential(credential, state)
        publish_profile(token_path, credential)
    with credentials(directory, state):
        check_capabilities(Client(endpoint, state['producer_uuid'], token_env=TOKEN_ENV).capabilities(), state)
    with closing(Outbox(directory / 'outbox.sqlite', endpoint, state['producer_uuid'])):
        pass
    state['complete'] = True
    save()
    return state


def connection_from(state):
    # Outbox/runtime paths are derived from the selected setup directory by the
    # caller; endpoint and producer identity remain fixed in setup.json.
    return {'endpoint': state['options']['endpoint'], 'producer': identifier(state['producer_uuid'])}


def validate_credential(value, state):
    token = value.get('token') if isinstance(value, dict) else None
    credential = value.get('credential', {}) if isinstance(value, dict) else {}
    if (not isinstance(token, str) or not token or any(ord(c) <= 32 or ord(c) >= 127 for c in token)
            or credential.get('producer_uuid') != state['producer_uuid'] or credential.get('revoked') is not False
            or credential.get('root_uuids') != [state['options']['root_uuid']] or credential.get('scopes') not in ([], None)):
        raise InvalidData('Unexpected setup credential scope')
    identifier(credential['uuid'])
    return token


@contextmanager
def credentials(directory, state):
    token = validate_credential(read_private(directory / 'credential.json'), state)
    with environment(TOKEN_ENV, token):
        yield


def check_capabilities(value, state):
    if (not isinstance(value, dict) or value.get('producer_uuid') != state['producer_uuid']
            or state['options']['root_uuid'] not in (value.get('root_uuids') or [])
            or value.get('file_ingestion') is not True or value.get('source_runs') is not True
            or value.get('source_run_traversal_protocol') != 1):
        raise InvalidData('Stash must support native file intake and source runs at the selected folder')


def monitoring_profile(directory, service, recovery):
    """Use the setup's shared config; only traversal policy differs per service."""
    mode = 'recovery' if recovery else 'routine'
    name = f'monitor-{service}-{mode}'
    path = directory / (name + '.json')
    with (directory / 'monitoring.lock').open('a') as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX)
        value = read_private(directory / 'gallery-worker.json')
        value.update(source_mode='published', source_category=service, scan_mode=mode, full_history=recovery)
        Configuration.from_document(value, directory).check()
        ensure_document(path, value)
        dispatch_path = directory / 'dispatch.json'
        dispatch = read_private(dispatch_path)
        entry = {'id': name, 'operation': 'download', 'profile': str(path)}
        previous = [item for item in dispatch['profiles'] if item['id'] == name]
        if previous and previous != [entry]:
            raise InvalidData('Monitoring dispatcher entry has changed')
        if not previous:
            dispatch['profiles'].append(entry)
            publish(dispatch_path, encode(dispatch, LIMIT), replace=True)
    return path


def main(argv=None):
    default = Path(os.environ.get('XDG_STATE_HOME', str(Path.home() / '.local/state'))) / 'stash-gallery'
    parser = argparse.ArgumentParser(description='Download into Stash with gallery-dl and resume saved work.')
    parser.add_argument('--directory', default=str(default), help='Persistent setup/state directory (default: %(default)s)')
    parser.add_argument('--api-key-env', default='STASH_API_KEY', help='Application key environment variable; never saved')
    commands = parser.add_subparsers(dest='command', required=True)
    folders = commands.add_parser('folders', help='List Stash library folders and their UUIDs')
    folders.add_argument('--endpoint', required=True)
    setup = commands.add_parser('setup', help='Connect an existing library folder; rerun unchanged after interruption')
    setup.add_argument('--endpoint', required=True)
    setup.add_argument('--root', required=True, help='Folder UUID from the folders command')
    setup.add_argument('--media-path', required=True, help='The same folder mounted on this worker')
    setup.add_argument('--gallery-config', action='append', default=[], help='Existing gallery-dl JSON file; repeat for ordered overrides')
    commands.add_parser('check', help='Check local profiles and Stash permissions without accessing source websites')
    commands.add_parser('status', help='Local durable queue counts; does not contact websites')
    commands.add_parser('drain', help='Send saved events to Stash without scraping')
    commands.add_parser('worker', help='Perform one background dispatch; schedule repeatedly to drain queued work')
    monitor = commands.add_parser('monitor', help='Queue enabled Stash subscriptions for one service; schedule daily')
    monitor.add_argument('--service', required=True, choices=('reddit', 'twitter', 'instagram', 'coomer', 'kemono', 'bluesky', 'tiktok'))
    monitor.add_argument('--recovery', action='store_true', help='Lower-priority periodic historical recovery')
    monitor.add_argument('--dry-run', action='store_true', help='Preview enabled URLs without queuing source work')
    download = commands.add_parser('download', help='Register and download source URLs using the existing producer')
    download.add_argument('urls', nargs='*')
    download.add_argument('-i', '--input-file', action='append', default=[])
    download.add_argument('--queue-only', action='store_true')
    download.add_argument('--dry-run', action='store_true')
    download.add_argument('--max-wait', type=float)
    resume = commands.add_parser('resume', help='Resume the original invocation UUID printed by download')
    resume.add_argument('call')
    resume.add_argument('--queue-only', action='store_true', help='Recover registration without starting a scrape')
    resume.add_argument('--max-wait', type=float)
    args = parser.parse_args(argv)
    try:
        if args.command == 'folders':
            app = SetupClient(args.endpoint, args.api_key_env)
            print(json.dumps(list(app.pages('archive/media-roots')), indent=2))
            return 0
        directory = private_directory(args.directory) if args.command == 'setup' else Path(args.directory).expanduser().resolve(strict=True)
        if args.command == 'setup':
            fd = os.open(directory / '.setup.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                state = prepare(directory, args.endpoint, args.root, args.media_path, args.gallery_config, args.api_key_env)
            finally:
                os.close(fd)
            print(json.dumps({'state': 'ready', 'directory': str(directory), **connection_from(state),
                              'root_uuid': state['options']['root_uuid']}, indent=2))
            return 0
        state = read_private(directory / 'setup.json')
        if state.get('schema') != FORMAT or state.get('complete') is not True:
            raise InvalidData('Finish stash-gallery setup first')
        existing_queue(directory)
        connection = connection_from(state)
        common = ['--outbox', str(directory / 'outbox.sqlite'), '--endpoint', connection['endpoint'],
                  '--producer', connection['producer'], '--token-env', TOKEN_ENV]
        if args.command == 'check':
            runtime = manual.Runtime(directory / 'manual.json')
            with credentials(directory, state):
                client = Client(connection['endpoint'], connection['producer'], token_env=TOKEN_ENV)
                check_capabilities(client.capabilities(), state)
            print(json.dumps({'state': 'ready', 'directory': str(directory), **connection, 'root_uuid': runtime.root,
                              'sqlite_version': sqlite3.sqlite_version, 'runtime': runtime_identity()}, indent=2))
            return 0
        with credentials(directory, state):
            # Initialize gallery-dl log levels for both foreground and background
            # paths, just like the existing standalone producer entry point.
            import logging
            from gallery_dl import output
            output.initialize_logging(logging.WARNING)
            if args.command == 'monitor':
                from stash_ingest.launchers import main as launcher_main
                profile = monitoring_profile(directory, args.service, args.recovery)
                options = common + ['--profile', str(profile), '--subscriptions']
                if args.recovery:
                    options.append('--recovery')
                if args.dry_run:
                    options.append('--dry-run')
                return launcher_main(args.service, options)
            if args.command in ('download', 'resume'):
                options = common + ['--runtime', str(directory / 'manual.json'), '--api-key-env', args.api_key_env]
                if args.command == 'resume':
                    options += ['--call', identifier(args.call)]
                else:
                    for filename in args.input_file:
                        options += ['--input-file', filename]
                    if args.dry_run:
                        options.append('--dry-run')
                if args.queue_only:
                    options.append('--queue-only')
                if args.max_wait is not None:
                    options += ['--max-wait', str(args.max_wait)]
                if args.command == 'download':
                    options += ['--', *args.urls]
                return manual.main(options)
            from stash_ingest_bootstrap import main as ingest_main
            operation = ['dispatch-all', '--profiles', str(directory / 'dispatch.json')] if args.command == 'worker' else [args.command]
            return ingest_main(common + operation)
    except KeyboardInterrupt:
        return 130
    except (InvalidData, Unavailable) as error:
        print(json.dumps({'state': 'incomplete', 'error': str(error)}), file=sys.stderr)
    except (OSError, KeyError, TypeError, ValueError):
        print(json.dumps({'state': 'incomplete', 'error': 'Setup or local state unavailable; preserve the directory and retry'}), file=sys.stderr)
    return 1


if __name__ == '__main__':
    sys.exit(main())
