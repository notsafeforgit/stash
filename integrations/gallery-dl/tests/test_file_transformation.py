from copy import deepcopy
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock
import uuid

from stash_ingest.client import drain_once
from stash_ingest.encoding import InvalidData, encode
from stash_ingest.events import validate
from stash_ingest.outbox import Outbox
from helpers import PRODUCER, capture, file_event, receipt


class FileTransformationTests(unittest.TestCase):
    def event(self):
        return file_event(capture(), transformation={"kind": "gif-to-video",
                          "original_relative_path": "Fixture/actual-final.gif"})

    def test_only_same_stem_sourced_gif_to_video_claims_are_accepted(self):
        event = self.event()
        self.assertEqual(validate(encode(event)), event)
        upper = deepcopy(event)
        upper["transformation"]["original_relative_path"] = "Fixture/actual-final.GIF"
        self.assertEqual(validate(encode(upper)), upper)
        for changes in ({"transformation": None}, {"transformation": {}},
                        {"transformation": {"kind": "rename", "original_relative_path": "Fixture/actual-final.gif"}},
                        {"transformation": {"kind": "gif-to-video", "original_relative_path": "../actual-final.gif"}},
                        {"transformation": {"kind": "gif-to-video", "original_relative_path": "Fixture/different.gif"}},
                        {"transformation": {"kind": "gif-to-video", "original_relative_path": "Fixture/actual-final.jpg"}},
                        {"transformation": {**event["transformation"], "image_uuid": str(uuid.uuid4())}},
                        {"source": None}, {"media_kind": "image"}, {"relative_path": "Fixture/actual-final.mp4"}):
            with self.subTest(changes=changes), self.assertRaises(InvalidData):
                validate(encode(dict(event, **changes)))

    def test_unsupported_server_defers_exact_event_without_blocking_other_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            now = [1000.0]
            path = Path(temporary) / "outbox.sqlite"
            box = Outbox(path, "http://localhost:8009", PRODUCER, clock=lambda: now[0])
            parent = capture()
            transformed = file_event(parent, transformation=self.event()["transformation"])
            raw = encode(transformed)
            ordinary = file_event(parent, relative_path="Fixture/ordinary.mkv")
            box.enqueue(encode(parent))
            box.enqueue(raw)
            box.enqueue(encode(ordinary))
            pending, = box.claim(str(uuid.uuid4()))
            box.acknowledge(pending, receipt(parent))
            sent = []

            def batch(deliveries):
                sent.extend(d.body for d in deliveries)
                return [{"status": 202, "receipt": receipt(ordinary if d.event_uuid == ordinary["event_uuid"] else transformed)}
                        for d in deliveries]

            client = SimpleNamespace(endpoint=box.endpoint, producer=PRODUCER,
                                     capabilities=Mock(return_value={"file_ingestion": True}), batch=Mock(side_effect=batch))
            result = drain_once(box, client)
            self.assertEqual((result["retried"], result["acknowledged"], result["review"]), (1, 1, 0))
            self.assertEqual(sent, [encode(ordinary)])
            box.close()
            box = Outbox(path, client.endpoint, PRODUCER, clock=lambda: now[0])
            try:
                self.assertEqual(drain_once(box, client)["retried"], 0, "deferred events respect backoff")
                now[0] += 61
                client.capabilities.return_value["file_transformation_protocol"] = 1
                result = drain_once(box, client)
                self.assertEqual(result["acknowledged"], 1)
                self.assertEqual(sent, [encode(ordinary), raw], "restart and capability negotiation retain exact bytes")
                self.assertIsNone(box.db.execute("SELECT body FROM events WHERE event_uuid=?", (transformed["event_uuid"],)).fetchone()[0])
            finally:
                box.close()
