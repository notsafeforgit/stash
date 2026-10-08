"""Run manual gallery-dl targets through native registration and fenced intake."""

import argparse
from contextlib import closing
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import sys
import time
import uuid

from . import source_management, source_policy, windows
from .catalog_upload import read_regular
from .cli import worker_output
from .client import Client, Unavailable, drain_once
from .collections import lookup_collections, target_url
from .completion import inspect_call, inspect_ticket
from .configuration import Configuration
from .encoding import InvalidData, decode, digest, encode, identifier
from .outbox import Capacity, Conflict, Outbox
from .run_queue import RunQueue, submit_once
from .source_calls import SourceCalls, resolve_once
from .source_lists import publish
from .worker import execute

FORMAT = 'stash-manual-gallery-v1'
LIMIT = 2 << 20
ADAPTERS = ('gallery-dl', 'yt-dlp')


def read(path):
    return decode(read_regular(path, LIMIT), LIMIT)


def save(path, value):
    publish(path, encode(value, LIMIT))


def directory(value):
    if not isinstance(value, str) or not value or any(ord(c) < 32 for c in value):
        raise InvalidData('Invalid manual state directory')
    path = Path(value)
    if not path.is_absolute() or path.is_symlink() or str(path.resolve()) != value or not path.is_dir():
        raise InvalidData('Manual state requires an existing canonical directory')
    return path


class Runtime:
    def __init__(self, path):
        value = read(path)
        if (not isinstance(value, dict) or set(value) != {'schema', 'state', 'profiles', 'new_source_policy'}
                or value['schema'] != FORMAT or not isinstance(value['profiles'], dict)
                or set(value['profiles']) != set(ADAPTERS)):
            raise InvalidData('Configure the native manual gallery-dl runtime')
        self.state = directory(value['state'])
        self.profile_paths, self.profiles = {}, {}
        for adapter, filename in value['profiles'].items():
            if not isinstance(filename, str) or not Path(filename).is_absolute():
                raise InvalidData('Manual worker profiles require absolute paths')
            profile = Configuration(filename)
            profile.check()
            if (profile.source_adapter != adapter or profile.source_mode != 'traversal'
                    or profile.source_category is not None):
                raise InvalidData('Manual profiles require the matching adapter and an unrestricted configured scan')
            self.profile_paths[adapter], self.profiles[adapter] = filename, profile
        first = self.profiles['gallery-dl']
        self.root, self.locks = first.root_uuid, first.locks.path
        for profile in self.profiles.values():
            if (profile.root_uuid, profile.root.path, profile.root.identity, profile.locks.path, profile.locks.identity) != (
                    first.root_uuid, first.root.path, first.root.identity, first.locks.path, first.locks.identity):
                raise InvalidData('Manual profiles must share one media root and lock directory')
        self.policy = source_policy.definition(value['new_source_policy'])
        if self.policy['enabled'] is not True or self.policy['apply_to_scans'] is not False:
            raise InvalidData('Manual source defaults must apply only to source intake')

    def snapshot(self):
        return {adapter: {'path': self.profile_paths[adapter], 'policy_sha256': profile.policy_sha256}
                for adapter, profile in self.profiles.items()}


def targets(urls, files=()):
    values = list(urls)
    for filename in files:
        try:
            body = read_regular(filename, LIMIT).decode('utf-8-sig')
        except UnicodeError:
            raise InvalidData('Manual URL lists must be UTF-8') from None
        for line in body.splitlines():
            line = line.strip()
            if line and not line.startswith('#'):
                values.append(line)
    found = {}
    for value in values:
        if any(character.isspace() for character in value):
            raise InvalidData('Each manual target must be a single URL without extra options or comments')
        adapter = 'yt-dlp' if value.startswith('ytdl:') else 'gallery-dl'
        url = target_url(value[5:] if adapter == 'yt-dlp' else value)
        if url in found and found[url] != adapter:
            raise InvalidData('One manual request cannot select two adapters for the same URL')
        found[url] = adapter
    if not 1 <= len(found) <= 50:
        raise InvalidData('Provide one to fifty distinct HTTP URLs or ytdl: URLs')
    return [{'url': url, 'adapter': adapter} for url, adapter in found.items()]


def options(urls, files, runtime_path):
    return {'urls': list(urls), 'input_files': [str(Path(name).absolute()) for name in files],
            'runtime': str(Path(runtime_path).absolute())}


def request(runtime, client, call, directory, supplied, now):
    path = directory / 'request.json'
    if path.exists():
        value = read(path)
        if not isinstance(value, dict):
            raise InvalidData('Invalid saved manual request')
        if supplied is not None and value.get('options') != supplied:
            raise Conflict('Manual call UUID already identifies different inputs')
    else:
        if supplied is None:
            raise InvalidData('This manual call has no saved request to resume')
        value = {'format': FORMAT, 'call_uuid': call, 'endpoint': client.endpoint, 'producer_uuid': client.producer,
                 'root_uuid': runtime.root, 'options': supplied, 'targets': targets(supplied['urls'], supplied['input_files']),
                 'profiles': runtime.snapshot(), 'new_source_policy': runtime.policy,
                 'window': windows.normalize({'basis': 'traversal', 'since': None,
                            'until': datetime.fromtimestamp(now, timezone.utc).isoformat(timespec='milliseconds')})}
        save(path, value)
    if (not isinstance(value, dict) or set(value) != {'format', 'call_uuid', 'endpoint', 'producer_uuid', 'root_uuid',
            'options', 'targets', 'profiles', 'new_source_policy', 'window'} or value['format'] != FORMAT
            or (value['call_uuid'], value['endpoint'], value['producer_uuid'], value['root_uuid']) != (
                call, client.endpoint, client.producer, runtime.root)
            or value['profiles'] != runtime.snapshot() or value['new_source_policy'] != runtime.policy):
        raise Conflict('Manual request or reviewed worker configuration changed; retain the original profile to resume')
    rows = value['targets']
    if (not isinstance(rows, list) or any(not isinstance(row, dict) or set(row) != {'url', 'adapter'}
                                       or row['adapter'] not in ADAPTERS for row in rows)
            or targets([('ytdl:' if row['adapter'] == 'yt-dlp' else '') + row['url'] for row in rows]) != rows
            or windows.normalize(value['window']) != value['window'] or value['window'].get('basis') != 'traversal'):
        raise InvalidData('Invalid saved manual targets or scan window')
    return value


def source_definition(url, root):
    # Publishers/performers are established by captured evidence or explicit
    # review. A new manual target may contain leaves from more than one site.
    return {'label': url.encode('utf-8')[:1024].decode('utf-8', errors='ignore'), 'kind': 'manual_batch', 'namespace': '', 'state': 'active',
            'target_url': url, 'account_uuid': None, 'root_uuid': root, 'path_prefix': '.'}


def new_sources(source):
    indexes = [index for index, entry in enumerate(source['entries']) if entry['before'] is None]
    if not indexes:
        return None
    return {**source, 'intent': {**source['intent'], 'targets': [source['intent']['targets'][index] for index in indexes]},
            'entries': [source['entries'][index] for index in indexes]}


def registration(app, policies, client, runtime, value, path):
    source_path, plan_path = path / 'sources.json', path / 'plan.json'
    if not source_path.exists():
        definitions = [source_definition(row['url'], runtime.root) for row in value['targets']]
        spec = {'operation': 'ensure', 'root_uuid': runtime.root, 'reason': 'Manual gallery-dl request', 'targets': definitions}
        matches = source_management.management_lookup(client, spec)
        for index, match in enumerate(matches['targets']):
            if match['state'] == 'ambiguous':
                raise source_management.conflict()
            if match['candidates']:
                candidate = match['candidates'][0]
                current = app.collection(candidate['collection_uuid'])
                if current['revision'] != candidate['collection_revision'] or current['state'] != 'active':
                    raise source_management.conflict()
                spec['targets'][index] = {key: current[key] for key in source_management.DEFINITION}
        source_management.prepare(app, client, spec, source_path)
    source = source_management.validate_plan(read(source_path))
    if (source['endpoint'] != client.endpoint or source['producer_uuid'] != client.producer
            or source['intent']['root_uuid'] != runtime.root or source['intent']['operation'] != 'ensure'
            or [item['target_url'] for item in source['intent']['targets']] != [row['url'] for row in value['targets']]):
        raise InvalidData('Saved source plan belongs to another manual request')
    sha = digest(read_regular(source_path, LIMIT))
    policy_source = new_sources(source)
    if not plan_path.exists():
        save(plan_path, {'request_sha256': digest(encode(value, LIMIT)), 'sources_sha256': sha,
                         'policies': source_policy.prepare(policies, policy_source, runtime.policy) if policy_source else []})
    plan = read(plan_path)
    if (not isinstance(plan, dict) or set(plan) != {'request_sha256', 'sources_sha256', 'policies'}
            or plan['request_sha256'] != digest(encode(value, LIMIT)) or plan['sources_sha256'] != sha
            or policy_source is None and plan['policies'] != []):
        raise InvalidData('Manual source plan differs from its saved request')
    receipt = {'request_sha256': plan['request_sha256'], 'plan_sha256': digest(read_regular(plan_path, LIMIT))}
    receipt_path = path / 'registered.json'
    if receipt_path.exists():
        if read(receipt_path) != receipt:
            raise InvalidData('Manual source registration receipt changed')
        return
    if (source_management.inspect(app, client, source)['needs_review']
            or policy_source and source_policy.inspect(policies, plan['policies'], policy_source)['needs_review']):
        raise source_management.conflict()
    if not source_management.inspect(app, client, source, apply=True)['complete']:
        raise source_management.conflict()
    if policy_source and not source_policy.inspect(policies, plan['policies'], policy_source, apply=True)['complete']:
        raise source_management.conflict()
    # The receipt is published only after all prerequisites. Native callers are
    # recorded afterward, so a dispatcher cannot resolve an unregistered target.
    save(receipt_path, receipt)


def admit(app, policies, client, runtime, box, call, supplied):
    identifier(call)
    if (app.endpoint, policies.endpoint, box.endpoint, box.producer) != (
            client.endpoint, client.endpoint, client.endpoint, client.producer):
        raise InvalidData('Manual clients and outbox identify different Stash endpoints or producers')
    with source_management.management_lock(runtime.locks, runtime.root):
        path = runtime.state / call
        if path.is_symlink():
            raise InvalidData('Manual request directory must not be a symlink')
        path.mkdir(mode=0o700, exist_ok=True)
        parent = os.open(runtime.state, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(parent)
        finally:
            os.close(parent)
        value = request(runtime, client, call, path, supplied, box.clock())
        registration(app, policies, client, runtime, value, path)
        groups = []
        calls = SourceCalls(box)
        with box.transaction():
            for adapter in ADAPTERS:
                urls = [row['url'] for row in value['targets'] if row['adapter'] == adapter]
                if not urls:
                    continue
                identity = str(uuid.uuid5(uuid.UUID(call), 'adapter/' + adapter))
                calls.record_in_transaction(identity, digest(encode({'call_uuid': call, 'adapter': adapter})), {
                    'targets': urls, 'root_uuid': runtime.root, 'operation': 'download',
                    'policy_sha256': value['profiles'][adapter]['policy_sha256'], 'cooldown_seconds': 0, 'window': value['window']})
                groups.append({'call_uuid': identity, 'adapter': adapter})
        return groups


def status(box, client, groups):
    states = [inspect_call(SourceCalls(box), client, group['call_uuid']) for group in groups]
    if all(item['state'] == 'source_succeeded' for item in states):
        state = 'source_succeeded'
    else:
        state = next((name for name in ('review', 'cancelled', 'deferred', 'unavailable')
                      if any(item['state'] == name for item in states)), 'pending')
    return {'state': state, 'calls': states, 'intake_completion': 'inspect_native_receipts'}


def step(box, client, runtime, groups):
    drain_once(box, client)
    calls = SourceCalls(box)
    resolve_once(calls, client)
    submit_once(RunQueue(box), client)
    for group in groups:
        # Each manual group has at most fifty targets. Only this invocation's
        # admitted run IDs can launch a worker; other callers may be resolved
        # or submitted but are never downloaded by this foreground command.
        for row in calls.page(group['call_uuid']):
            if row['state'] != 'queued':
                continue
            ticket = inspect_ticket(box, client, row['ticket_uuid'])
            for part in ticket['submissions']:
                if part['remaining'] and part['state'] == 'queued' and part['run_uuid']:
                    result = execute(box, client, runtime.profiles[group['adapter']], part['run_uuid'])
                    if result.get('error_code') == 'worker_interrupted':
                        raise KeyboardInterrupt
                    if result['state'] != 'waiting':
                        return status(box, client, groups)
    return status(box, client, groups)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('urls', nargs='*', help='HTTP source URLs; prefix with ytdl: to select the yt-dlp bridge')
    parser.add_argument('-i', '--input-file', action='append', default=[], help='UTF-8 file containing one full URL per line')
    parser.add_argument('--runtime', default=os.environ.get('STASH_INGEST_MANUAL_CONFIG'))
    parser.add_argument('--outbox', default=os.environ.get('STASH_INGEST_OUTBOX'))
    parser.add_argument('--endpoint', default=os.environ.get('STASH_INGEST_ENDPOINT'))
    parser.add_argument('--producer', default=os.environ.get('STASH_INGEST_PRODUCER'))
    parser.add_argument('--token-env', default='STASH_INGEST_TOKEN')
    parser.add_argument('--api-key-env', default='STASH_API_KEY')
    parser.add_argument('--call', help='Resume the saved invocation UUID; inputs may be omitted')
    parser.add_argument('--queue-only', action='store_true', help='Register and save work without waiting for downloads')
    parser.add_argument('--max-wait', type=float, help='Stop waiting after this many seconds; queued work remains durable')
    parser.add_argument('--dry-run', action='store_true', help='Inspect source URLs and reviewed profile selection without writes')
    args = parser.parse_args(argv)
    call = args.call or str(uuid.uuid4())
    try:
        identifier(call)
        if not args.runtime or (args.max_wait is not None and not 0 < args.max_wait <= 86400):
            raise InvalidData('Configure the manual runtime and a positive optional wait limit of at most one day')
        runtime = Runtime(args.runtime)
        supplied = options(args.urls, args.input_file, args.runtime) if args.urls or args.input_file else None
        if args.dry_run:
            print(json.dumps({'state': 'preview', 'targets': targets(args.urls, args.input_file), 'root_uuid': runtime.root,
                              'profiles': runtime.snapshot(), 'new_source_policy': runtime.policy}, sort_keys=True))
            return 0
        if not all((args.outbox, args.endpoint, args.producer)):
            raise InvalidData('Configure the native outbox, endpoint and producer')
        client = Client(args.endpoint, args.producer, token_env=args.token_env)
        app = source_management.SourceManagementClient(args.endpoint, args.api_key_env)
        policies = source_policy.SourcePolicyClient(args.endpoint, args.api_key_env)
        with closing(Outbox(args.outbox, args.endpoint, args.producer)) as box:
            groups = admit(app, policies, client, runtime, box, call, supplied)
            if args.queue_only:
                print(json.dumps({'call_uuid': call, 'state': 'recorded', 'groups': groups,
                                  'intake_completion': 'inspect_native_receipts'}, sort_keys=True))
                return 0
            print('Manual invocation ' + call, file=sys.stderr)
            started = time.monotonic()
            while True:
                with worker_output():
                    result = step(box, client, runtime, groups)
                if result['state'] != 'pending' or (args.max_wait is not None and time.monotonic() - started >= args.max_wait):
                    print(json.dumps({'call_uuid': call, **result}, sort_keys=True))
                    return 0 if result['state'] == 'source_succeeded' else 2 if result['state'] in ('review', 'cancelled', 'deferred') else 3
                time.sleep(5)
    except KeyboardInterrupt:
        message, code = 'Manual invocation interrupted; saved work remains resumable', 130
    except (InvalidData, Unavailable, Capacity, Conflict) as error:
        message, code = str(error), 2 if isinstance(error, Conflict) or isinstance(error, Unavailable) and error.status == 409 else 1
    except (OSError, ValueError, TypeError, KeyError):
        message, code = 'Manual configuration or saved state is unavailable', 1
    print(json.dumps({'call_uuid': call, 'state': 'incomplete', 'error': message}, sort_keys=True), file=sys.stderr)
    return code


if __name__ == '__main__':
    sys.exit(main())
