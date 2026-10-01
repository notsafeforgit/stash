import copy
from datetime import datetime, timezone
import json
from pathlib import Path
import unittest

from stash_ingest.encoding import InvalidData, decode, encode
from stash_ingest.events import validate
from stash_ingest.retention import POLICY, retain
from helpers import capture, file_event


class RetentionTests(unittest.TestCase):
    def test_shared_server_policy_corpus(self):
        path = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/source-retention-v1.json"
        fixture = json.loads(path.read_text())
        self.assertEqual(fixture["policy"], POLICY)
        for case in fixture["cases"]:
            with self.subTest(case=case["name"]):
                original = copy.deepcopy(case["input"])
                retained = retain(case["input"])
                self.assertEqual(retained, case["retained"])
                self.assertEqual(case["input"], original)
                self.assertEqual(retain(retained), retained)

    def test_runtime_handles_and_secrets_never_reach_serialization(self):
        data = {"category": "twitter", "_runtime": object(), "headers": object(),
                "nested": {"API-KEY": object(), "safe": "value"},
                "date": datetime(2026, 10, 1, tzinfo=timezone.utc)}
        self.assertEqual(retain(data), {"category": "twitter", "nested": {"safe": "value"},
                                      "date": "2026-10-01T00:00:00+00:00"})
        with self.assertRaises(InvalidData):
            retain({"public": object()})

    def test_strict_json_and_event_boundary(self):
        for body in (b'{"a":1,"a":2}', b'{"a":NaN}', b'{"a":1e999}', b'"\\ud800"'):
            with self.subTest(body=body), self.assertRaises(InvalidData):
                decode(body)
        for value in (float("nan"), float("inf"), {1: "bad"}, "\udfff"):
            with self.assertRaises(InvalidData):
                encode(value)
        self.assertEqual(decode(encode(9007199254740993)), 9007199254740993)
        event = capture()
        self.assertEqual(validate(encode(event)), event)
        for change in ({"source": {"cookie": "secret"}}, {"settings": {}}, {"protocol": True},
                       {"metadata": {"settings": {}}}, {"observed_at": "2026-10-01"}):
            with self.subTest(change=change), self.assertRaises(InvalidData):
                validate(encode(dict(event, **change)))
        for path in ("../escape.mp4", "/absolute.mp4", "pending.mp4.part", "a//b.mp4", "a\\b.mp4"):
            with self.subTest(path=path), self.assertRaises(InvalidData):
                validate(encode(file_event(relative_path=path)))

    def test_missing_reddit_id_does_not_drop_unrelated_previews(self):
        data = {"category": "reddit", "url": "https://i.redd.it/",
                "preview": {"images": [{"source": {"url": "https://elsewhere.test/a.jpg"}}]}}
        self.assertIn("preview", retain(data))


if __name__ == "__main__":
    unittest.main()
