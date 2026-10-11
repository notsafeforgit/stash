"""Onboarding through real HTTP, filesystem pins and the installed producer."""

from contextlib import redirect_stderr, redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io
import json
import os
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import patch
import uuid

import stash_gallery as setup
from stash_ingest.configuration import Configuration, runtime_identity
from stash_ingest import manual
from helpers import ROOT, PRODUCER


class Remote:
    def __init__(self, media):
        self.root = {'uuid': ROOT, 'label': 'Library', 'state': 'active',
                     'binding': {'path': str(media), 'directory_identity': 'bound'}}
        self.producers, self.credentials, self.requests, self.drops = [], [], [], set()
        self.enabled = True
        self.redirect = None
        self.secret = 'scoped-test-token'
        remote = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def handle_request(self):
                body = json.loads(self.rfile.read(int(self.headers['Content-Length']))) if 'Content-Length' in self.headers else None
                remote.requests.append((self.command, self.path, dict(self.headers), body))
                path = self.path.split('?', 1)[0]
                if remote.redirect:
                    self.send_response(302)
                    self.send_header('Location', remote.redirect)
                    self.end_headers()
                    return
                if not remote.enabled:
                    return self.reply(503, {'error': 'source_tracking_disabled'})
                if path == '/api/v3/ingest/capabilities':
                    if self.headers.get('Authorization') != 'Bearer ' + remote.secret:
                        return self.reply(401, {})
                    return self.reply(200, {'protocol': 1, 'retention_policy': 'gallery-dl-retained-v1',
                        'producer_uuid': PRODUCER, 'root_uuids': [ROOT],
                        'kinds': ['source.capture', 'attachment.download', 'file.completed'],
                        'max_event_bytes': 5 << 20, 'max_batch_bytes': 16 << 20, 'max_batch_events': 8,
                        'file_ingestion': True, 'source_runs': True, 'source_run_traversal_protocol': 1})
                if self.headers.get('ApiKey') != 'application-test-key':
                    return self.reply(401, {})
                if path == '/api/v3/archive/media-roots':
                    return self.reply(200, [remote.root])
                if path == '/api/v3/archive/media-roots/' + ROOT:
                    return self.reply(200, remote.root)
                if path == '/api/v3/ingest-admin/producers':
                    if self.command == 'GET':
                        return self.reply(200, remote.producers)
                    value = {'uuid': PRODUCER, 'label': body['label']}
                    remote.producers.append(value)
                    return self.commit_reply('producer', value)
                if path == '/api/v3/ingest-admin/producers/' + PRODUCER + '/credentials':
                    if self.command == 'GET':
                        return self.reply(200, sorted(remote.credentials, key=lambda row: row['uuid']))
                    value = {'uuid': str(uuid.uuid4()), 'producer_uuid': PRODUCER, 'revoked': False, **body}
                    remote.credentials.append(value)
                    return self.commit_reply('credential', {'credential': value, 'token': remote.secret})
                if path.startswith('/api/v3/ingest-admin/credentials/') and self.command == 'DELETE':
                    for row in remote.credentials:
                        if row['uuid'] == path.rsplit('/', 1)[1]:
                            row['revoked'] = True
                    return self.reply(204, None)
                return self.reply(404, {})

            def commit_reply(self, operation, value):
                if operation in remote.drops:
                    remote.drops.remove(operation)
                    self.close_connection = True  # Server committed; client receives no response.
                    return
                self.reply(201, value)

            def reply(self, status, value):
                body = json.dumps(value).encode()
                self.send_response(status)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            do_GET = do_POST = do_DELETE = handle_request

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.endpoint = 'http://127.0.0.1:' + str(self.server.server_port)

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()


class GallerySetupTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.path = Path(temporary.name)
        self.media, self.directory = self.path / 'media', self.path / 'setup'
        self.media.mkdir()
        self.remote = Remote(self.media)
        self.addCleanup(self.remote.close)
        env = patch.dict(os.environ, {'STASH_API_KEY': 'application-test-key'})
        env.start()
        self.addCleanup(env.stop)

    def command(self, *args):
        stdout, stderr = io.StringIO(), io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = setup.main(['--directory', str(self.directory), *args])
        # No request/setup secret may be printed, even on failure.
        for secret in ('application-test-key', self.remote.secret, 'website-private-cookie'):
            self.assertNotIn(secret, stdout.getvalue() + stderr.getvalue())
        return code, stdout.getvalue(), stderr.getvalue()

    def install(self, *args):
        return self.command('setup', '--endpoint', self.remote.endpoint, '--root', ROOT,
                            '--media-path', str(self.media), *args)

    def test_monitoring_profiles_share_config_and_are_registered_for_dispatch(self):
        self.assertEqual(self.install()[0], 0)
        original = json.loads((self.directory / 'gallery-worker.json').read_text())
        routine = setup.monitoring_profile(self.directory, 'reddit', False)
        recovery = setup.monitoring_profile(self.directory, 'reddit', True)
        setup.monitoring_profile(self.directory, 'reddit', False)
        for path, mode in ((routine, 'routine'), (recovery, 'recovery')):
            document = json.loads(path.read_text())
            self.assertEqual(document['config_files'], original['config_files'])
            self.assertEqual(document['scan_mode'], mode)
            self.assertEqual(document['source_mode'], 'published')
            self.assertEqual(document['source_category'], 'reddit')
        dispatch = json.loads((self.directory / 'dispatch.json').read_text())
        self.assertEqual(len(dispatch['profiles']), 4)

    def test_setup_repeat_check_and_foreground_use_existing_contracts(self):
        before_runtime = runtime_identity()
        code, output, error = self.install()
        self.assertEqual(code, 0, error)
        self.assertEqual(json.loads(output)['root_uuid'], ROOT)
        self.assertEqual(len(self.remote.producers), 1)
        self.assertEqual(len(self.remote.credentials), 1)
        token = self.remote.credentials[0]
        self.assertEqual(token['root_uuids'], [ROOT])
        self.assertEqual(token['scopes'], [])
        original = {path.name: path.read_bytes() for path in self.directory.glob('*.json')}
        self.assertEqual(self.install()[0], 0)
        self.assertEqual(original, {path.name: path.read_bytes() for path in self.directory.glob('*.json')})
        self.assertEqual(len(self.remote.credentials), 1)
        runtime = manual.Runtime(self.directory / 'manual.json')
        self.assertEqual(runtime.root, ROOT)
        self.assertFalse(runtime.policy['rules']['scene']['on_existing'])
        self.assertNotIn('performer_ids', runtime.policy['rules']['scene']['mappings'])
        self.assertEqual(self.command('check')[0], 0)
        code, output, error = self.command('download', '--dry-run', 'https://www.reddit.com/user/example/')
        self.assertEqual(code, 0, error)
        self.assertEqual(json.loads(output)['targets'][0]['adapter'], 'gallery-dl')
        self.assertEqual(before_runtime, runtime_identity())
        self.assertNotIn(setup.TOKEN_ENV, os.environ)
        for path in self.directory.rglob('*'):
            if path.is_file() and path.suffix == '.json':
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                if path.name != 'credential.json':
                    self.assertNotIn(self.remote.secret, path.read_text())

    def test_setup_recovers_both_lost_create_responses_without_replacing_queue(self):
        self.remote.drops = {'producer', 'credential'}
        self.assertEqual(self.install()[0], 1)
        self.assertEqual(len(self.remote.producers), 1)
        self.assertEqual(self.install()[0], 1)
        self.assertEqual(len(self.remote.producers), 1)
        self.assertEqual(len(self.remote.credentials), 1)
        self.assertEqual(self.install()[0], 0)
        self.assertEqual(len(self.remote.credentials), 2)
        self.assertTrue(self.remote.credentials[0]['revoked'])
        self.assertFalse(self.remote.credentials[1]['revoked'])
        box_path = self.directory / 'outbox.sqlite'
        inode = box_path.stat().st_ino
        self.assertEqual(self.install()[0], 0)
        self.assertEqual(box_path.stat().st_ino, inode)

    def test_invalid_local_config_and_disabled_server_do_not_register_a_producer(self):
        config = self.path / 'invalid.json'
        config.write_text(json.dumps({'extractor': {'base-directory': '/outside'}}))
        code, _, error = self.install('--gallery-config', str(config))
        self.assertEqual(code, 1)
        self.assertIn('outside', error)
        self.assertEqual(self.remote.producers, [])
        self.remote.enabled = False
        code, _, error = self.install('--gallery-config', str(config))
        self.assertEqual(code, 1)
        self.assertIn('enable_source_tracking', error)
        self.assertEqual(self.remote.producers, [])

    def test_profiles_reference_shared_private_config_and_reject_changed_mount(self):
        config = self.path / 'gallery.json'
        config.write_text(json.dumps({'extractor': {'reddit': {'cookies': 'website-private-cookie'},
                                        'filename': {'num > 1': 'a_{filename}.{extension}', '': 'b_{filename}.{extension}'}}}))
        code, _, error = self.install('--gallery-config', str(config))
        self.assertEqual(code, 0, error)
        profile = Configuration(self.directory / 'gallery-worker.json')
        self.assertEqual(profile._gallery['extractor']['reddit']['cookies'], 'website-private-cookie')
        self.assertNotIn('website-private-cookie', json.dumps(profile.document))
        old = profile.policy_sha256
        config.write_text(json.dumps({'extractor': {'reddit': {'cookies': 'rotated-private-cookie'},
                                        'filename': {'num > 1': 'a_{filename}.{extension}', '': 'b_{filename}.{extension}'}}}))
        self.assertEqual(Configuration(self.directory / 'gallery-worker.json').policy_sha256, old)
        self.media.rename(self.path / 'original-media')
        self.media.mkdir()
        self.assertEqual(self.command('check')[0], 1)
        self.assertEqual(self.install('--gallery-config', str(config))[0], 1)

    def test_existing_setup_cannot_be_rebound_or_disclose_credentials(self):
        self.assertEqual(self.install()[0], 0)
        before = (self.directory / 'setup.json').read_bytes()
        other = self.path / 'other'
        other.mkdir()
        code, _, _ = self.command('setup', '--endpoint', self.remote.endpoint, '--root', ROOT, '--media-path', str(other))
        self.assertEqual(code, 1)
        self.assertEqual(before, (self.directory / 'setup.json').read_bytes())
        path = self.directory / 'credential.json'
        path.chmod(0o644)
        self.assertEqual(self.command('check')[0], 1)
        path.chmod(0o600)
        original = path.read_bytes()
        path.unlink()
        shared = self.path / 'secret.json'
        shared.write_bytes(original)
        path.symlink_to(shared)
        self.assertEqual(self.command('check')[0], 1)

    def test_http_authentication_does_not_follow_redirect_or_inherit_proxy(self):
        self.remote.redirect = 'https://foreign.invalid/steal-key'
        with patch.dict(os.environ, {'HTTPS_PROXY': 'http://bad.invalid', 'HTTP_PROXY': 'http://bad.invalid'}):
            code, _, error = self.command('folders', '--endpoint', self.remote.endpoint)
        self.assertEqual(code, 1)
        self.assertIn('stash_setup_request_rejected', error)
        self.assertEqual(len(self.remote.requests), 1)
        self.assertEqual(self.remote.requests[0][2]['Apikey'], 'application-test-key')

    def test_dispatch_and_delivery_use_existing_queue_without_application_auth(self):
        self.assertEqual(self.install()[0], 0)
        with patch.dict(os.environ, {'STASH_API_KEY': ''}), patch('stash_ingest_bootstrap.main', return_value=0) as command:
            self.assertEqual(self.command('worker')[0], 0)
            arguments = command.call_args.args[0]
            self.assertEqual(arguments[-3:], ['dispatch-all', '--profiles', str(self.directory / 'dispatch.json')])
            self.assertEqual(arguments[1], str(self.directory / 'outbox.sqlite'))
            self.assertEqual(self.command('drain')[0], 0)
            self.assertEqual(command.call_args.args[0][-1], 'drain')
            self.assertEqual(self.command('status')[0], 0)
        with patch('stash_ingest.manual.main', return_value=0) as command:
            call = str(uuid.uuid4())
            self.assertEqual(self.command('resume', call)[0], 0)
            self.assertEqual(command.call_args.args[0][-2:], ['--call', call])

    def test_no_media_or_credentials_touched_when_folder_is_unbound(self):
        self.remote.root['binding'] = None
        self.assertEqual(self.install()[0], 1)
        self.assertEqual(list(self.media.iterdir()), [])
        self.assertEqual(self.remote.producers, [])

    def test_missing_saved_queue_is_not_replaced_by_setup_or_commands(self):
        self.assertEqual(self.install()[0], 0)
        box = self.directory / 'outbox.sqlite'
        box.rename(self.directory / 'retained.sqlite')
        for arguments in (('status',), ('worker',), ('check',)):
            code, _, error = self.command(*arguments)
            self.assertEqual(code, 1)
            self.assertIn('outbox is missing', error)
            self.assertFalse(box.exists())
        self.assertEqual(self.install()[0], 1)
        self.assertFalse(box.exists())


if __name__ == '__main__':
    unittest.main()
