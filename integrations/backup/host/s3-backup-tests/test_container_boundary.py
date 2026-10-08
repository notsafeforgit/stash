"""External writer coordination, expiry, and exact-ID recovery contracts."""

import copy
from pathlib import Path
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from container_boundary import (ContainerBoundary, PODMAN, SYSTEMCTL, SYSTEMD_RUN,
                                validate_options)
from stash_archive.storage import InvalidArchive


class ContainerBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.containers = {'n8n': ['1' * 64, 'running', False],
                           'second': ['2' * 64, 'running', False]}
        self.units, self.calls = {}, []
        self.failure = None
        patched = patch('container_boundary.command', side_effect=self.command)
        patched.start()
        self.addCleanup(patched.stop)

    def by_id(self, identity):
        return next(record for record in self.containers.values() if record[0] == identity)

    def command(self, arguments, *, check=True):
        self.calls.append(arguments)
        if arguments[0] == PODMAN and arguments[1] == 'inspect':
            key = arguments[-1]
            record = self.containers[key] if key in self.containers else self.by_id(key)
            return SimpleNamespace(returncode=0, stdout=f'{record[0]} {record[1]} {str(record[2]).lower()}\n')
        if arguments[0] == SYSTEMD_RUN:
            unit = next(arg.removeprefix('--unit=') for arg in arguments if arg.startswith('--unit='))
            pause = next(arg.removeprefix('--property=ExecStartPre=') for arg in arguments if arg.startswith('--property=ExecStartPre='))
            identity = pause.split()[-1]
            self.assertEqual(pause, PODMAN + ' pause ' + identity)
            self.assertIn('--property=ExecStopPost=' + PODMAN + ' unpause ' + identity, arguments)
            self.assertIn('--property=RuntimeMaxSec=300', arguments)
            self.units[unit] = {'id': identity, 'active': True}
            self.by_id(identity)[2] = True
            if self.failure == identity:
                raise InvalidArchive('uncertain start response')
            return SimpleNamespace(returncode=0, stdout='')
        if arguments[:3] == [SYSTEMCTL, '--user', 'show']:
            active = self.units[arguments[3]]['active']
            return SimpleNamespace(returncode=0, stdout=('ActiveState=active\nSubState=running\n' if active
                                                         else 'ActiveState=failed\nSubState=failed\n'))
        if arguments[:3] == [SYSTEMCTL, '--user', 'stop']:
            unit = self.units[arguments[3]]
            unit['active'] = False
            self.by_id(unit['id'])[2] = False
            return SimpleNamespace(returncode=0, stdout='')
        raise AssertionError(arguments)

    def test_capture_resumes_exact_container_and_retains_boundary(self):
        with ContainerBoundary(['n8n']) as capture:
            self.assertTrue(self.containers['n8n'][2])
            proof = capture.binding()
            capture.validate_boundary({'details': {'external_containers': proof}})
        self.assertFalse(self.containers['n8n'][2])
        self.assertFalse(capture.pending)
        capture.release()
        self.assertEqual(len([call for call in self.calls if call[:3] == [SYSTEMCTL, '--user', 'stop']]), 1)
        self.assertEqual(proof['containers'][0]['id'], '1' * 64)

    def test_preexisting_pause_is_never_resumed(self):
        self.containers['second'][2] = True
        with self.assertRaisesRegex(InvalidArchive, 'already paused'):
            with ContainerBoundary(['n8n', 'second']):
                self.fail('must refuse before changing either container')
        self.assertTrue(self.containers['second'][2])
        self.assertFalse(self.containers['n8n'][2])
        self.assertFalse(self.units)

    def test_partial_start_and_lost_response_resume_every_owned_pause(self):
        self.failure = '2' * 64
        with self.assertRaisesRegex(InvalidArchive, 'uncertain'):
            with ContainerBoundary(['n8n', 'second']):
                self.fail('uncertain startup must not capture')
        self.assertTrue(all(not record[2] for record in self.containers.values()))
        self.assertTrue(all(not unit['active'] for unit in self.units.values()))

    def test_capture_exception_still_resumes(self):
        with self.assertRaisesRegex(RuntimeError, 'copy failed'):
            with ContainerBoundary(['n8n']):
                raise RuntimeError('copy failed')
        self.assertFalse(self.containers['n8n'][2])

    def test_expired_guard_cannot_supply_fresh_boundary(self):
        with ContainerBoundary(['n8n']) as capture:
            next(iter(self.units.values()))['active'] = False
            self.containers['n8n'][2] = False
            with self.assertRaisesRegex(InvalidArchive, 'deadline expired'):
                capture.binding()
        self.assertIsNone(capture.proof)

    def test_replaced_container_name_is_rejected_and_replacement_not_touched(self):
        with ContainerBoundary(['n8n']) as capture:
            self.containers['old-n8n'] = self.containers.pop('n8n')
            self.containers['n8n'] = ['3' * 64, 'running', False]
            with self.assertRaisesRegex(InvalidArchive, 'changed during capture'):
                capture.binding()
        self.assertFalse(self.containers['old-n8n'][2])
        self.assertEqual(self.containers['n8n'], ['3' * 64, 'running', False])

    def test_inactive_container_stays_inactive_and_unexpected_start_is_refused(self):
        self.containers['n8n'][1] = 'exited'
        with ContainerBoundary(['n8n']) as capture:
            proof = capture.binding()
            self.assertEqual(proof['containers'][0]['guard_unit'], '')
            self.containers['n8n'][1] = 'running'
            with self.assertRaisesRegex(InvalidArchive, 'changed during capture'):
                capture.binding()
        self.assertFalse(self.units)

    def test_sealed_retry_neither_inspects_nor_pauses_current_containers(self):
        with ContainerBoundary(['n8n']) as original:
            proof = original.binding()
        self.calls.clear()
        with ContainerBoundary(['n8n'], existing_only=True) as retry:
            retry.validate_boundary({'details': {'external_containers': proof}})
            bad = copy.deepcopy(proof)
            bad['containers'][0]['name'] = 'second'
            with self.assertRaises(InvalidArchive):
                retry.validate_boundary({'details': {'external_containers': bad}})
            with self.assertRaises(InvalidArchive):
                retry.binding()
        self.assertEqual(self.calls, [])

    def test_invalid_configuration_rejected_before_commands(self):
        for names, timeout in [(['--all'], 300), (['n8n', 'n8n'], 300), ('n8n', 300),
                               (['n8n'], 0), (['n8n'], 901), (['n8n'], True)]:
            with self.subTest(names=names, timeout=timeout), self.assertRaises(InvalidArchive):
                validate_options(names, timeout)
        self.assertEqual(self.calls, [])
