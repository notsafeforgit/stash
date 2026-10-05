from datetime import datetime, timedelta, timezone
from email.message import Message
import io
import json
import unittest
from unittest.mock import Mock, patch
import uuid

from stash_archive.filesystem_boundary import FORMAT, read_checkpoint_response
from stash_archive.server_checkpoint import ServerCheckpoint
from stash_archive.storage import InvalidArchive


def response(kind, data):
    result = io.BytesIO(data)
    result.headers = Message()
    result.headers["Content-Type"] = kind
    return result


def line(value):
    return json.dumps(value).encode() + b"\n"


class FilesystemBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.provider = Mock(return_value={"snapshot": "view", "guid": 18446744073709551615})
        self.client = ServerCheckpoint("https://stash.example", "test-key", str(uuid.uuid4()), boundary=self.provider)
        self.request_hash = "a" * 64
        self.ready = {"uuid": self.client.request_id, "token": str(uuid.uuid4()), "request_sha256": self.request_hash,
                      "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=30)).isoformat()}
        self.record = {"format": FORMAT, "version": 1, "uuid": self.client.request_id,
                       "token": self.ready["token"], "request_sha256": self.request_hash,
                       "confirmed_at": datetime.now(timezone.utc).isoformat(), "details": self.provider.return_value}

    def ready_line(self, ready=None):
        return line({"event": "boundary_ready", "ready": ready or self.ready})

    def test_challenge_is_validated_before_provider_or_confirmation(self):
        for field, value in (("uuid", str(uuid.uuid4())), ("token", "invalid"),
                             ("request_sha256", "b" * 64), ("expires_at", "invalid"),
                             ("expires_at", "2000-01-01T00:00:00Z")):
            with self.subTest(field=field, value=value):
                ready = dict(self.ready, **{field: value})
                with patch.object(self.client, "open") as transport, self.assertRaises(InvalidArchive):
                    read_checkpoint_response(self.client, response("application/x-ndjson", self.ready_line(ready)), self.request_hash)
                transport.assert_not_called()
        self.provider.assert_not_called()

    def test_accepted_boundary_requires_one_terminal_sealed_event(self):
        for tail in (b"", line({"event": "error", "error": "deadline"}),
                     line({"event": "unexpected"}), line({"event": "sealed", "checkpoint": {}}) + b"extra"):
            with self.subTest(tail=tail):
                with patch.object(self.client, "open", return_value=response("application/json", line(self.record))), \
                        self.assertRaises(InvalidArchive):
                    read_checkpoint_response(self.client, response("application/x-ndjson", self.ready_line() + tail), self.request_hash)
        with patch.object(self.client, "open", return_value=response("application/json", line(self.record))) as transport:
            result = read_checkpoint_response(self.client,
                                              response("application/x-ndjson", self.ready_line() + line({"event": "sealed", "checkpoint": {"fixture": True}})),
                                              self.request_hash)
        self.assertEqual(json.loads(result), {"fixture": True})
        self.assertEqual(self.client.boundary_receipt, self.record)
        suffix, body = transport.call_args.args
        self.assertEqual(suffix, f"/{self.client.request_id}/boundary")
        self.assertEqual(json.loads(body), {"token": self.ready["token"], "details": self.provider.return_value})

    def test_changed_confirmation_or_provider_failure_never_produces_manifest(self):
        wrong = dict(self.record, details={"snapshot": "different"})
        with patch.object(self.client, "open", return_value=response("application/json", line(wrong))), self.assertRaises(InvalidArchive):
            read_checkpoint_response(self.client, response("application/x-ndjson", self.ready_line()), self.request_hash)
        self.provider.side_effect = RuntimeError("snapshot provider failed")
        with patch.object(self.client, "open") as transport, self.assertRaisesRegex(RuntimeError, "provider failed"):
            read_checkpoint_response(self.client, response("application/x-ndjson", self.ready_line()), self.request_hash)
        transport.assert_not_called()

    def test_sealed_replay_never_runs_provider(self):
        body = b'{"fixture":true}\n'
        self.assertEqual(read_checkpoint_response(self.client, response("application/json", body), self.request_hash), body)
        self.provider.assert_not_called()
