from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import patch

from stash_ingest.client import Client, Unavailable, drain_once
from stash_ingest.encoding import InvalidData, digest, encode
from stash_ingest.endpoint import origin
from stash_ingest.outbox import Outbox
from stash_ingest.retention import POLICY
from helpers import PRODUCER, capture, file_event, receipt


class ClientTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.now, self.requests, self.mode = [1000.0], [], "normal"
        self.receipts = {}
        test = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def respond(self, status, body):
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(encode(body))

            def do_GET(self):
                test.requests.append((self.path, self.headers.get("Authorization")))
                if test.mode == "unavailable":
                    self.respond(503, {"error": "temporarily_unavailable"})
                    return
                self.respond(200, {"protocol": 1, "producer_uuid": PRODUCER, "retention_policy": POLICY,
                                  "kinds": ["source.capture", "file.completed"], "file_ingestion": test.mode != "disabled",
                                  "max_event_bytes": 5 << 20, "max_batch_bytes": 16 << 20, "max_batch_events": 8})

            def do_POST(self):
                test.requests.append((self.path, self.headers.get("Authorization")))
                if test.mode == "redirect":
                    self.send_response(302)
                    self.send_header("Location", "/leaked-token")
                    self.end_headers()
                    return
                if test.mode == "unauthorized":
                    self.respond(401, {"error": "private server response never persisted"})
                    return
                raw = self.rfile.read(int(self.headers["Content-Length"]))
                body = json.loads(raw)
                results = []
                for i, item in enumerate(body["events"]):
                    event = item["event"]
                    assert item["sha256"] == digest(encode(event))
                    assert encode(event) in raw
                    if event.get("metadata", {}).get("title") == "reject":
                        results.append({"index": i, "status": 409, "error": "conflict"})
                        continue
                    if test.mode == "disabled" and event["event_uuid"] not in test.receipts:
                        results.append({"index": i, "status": 422, "error": "unsupported"})
                        continue
                    if event["event_uuid"] not in test.receipts:
                        test.receipts[event["event_uuid"]] = receipt(event)
                    ack = test.receipts[event["event_uuid"]]
                    if test.mode == "mismatch":
                        ack = dict(ack, sha256="f" * 64)
                    results.append({"index": i, "status": 200 if event["kind"] == "source.capture" else 202, "receipt": ack})
                if test.mode == "lost_response":
                    self.respond(503, {"error": "temporarily_unavailable"})
                elif test.mode == "duplicate_index":
                    self.respond(200, {"results": [results[0], results[0]]})
                else:
                    self.respond(200, {"results": list(reversed(results))})

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.stop_server)
        self.endpoint = "http://127.0.0.1:" + str(self.server.server_port)
        self.box = Outbox(Path(self.temp.name) / "outbox.sqlite", self.endpoint, PRODUCER, clock=lambda: self.now[0])
        self.addCleanup(self.box.close)
        self.client = Client(self.endpoint, PRODUCER)
        env = patch.dict(os.environ, {"STASH_INGEST_TOKEN": "fixture-secret"})
        env.start()
        self.addCleanup(env.stop)

    def stop_server(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(5)

    def queue(self, event=None):
        event = event or capture()
        self.box.enqueue(encode(event))
        return event

    def test_partial_success_and_explicit_conflict(self):
        self.queue()
        self.queue(capture(metadata={"title": "reject"}))
        self.assertEqual(drain_once(self.box, self.client), {"acknowledged": 1, "review": 1, "retried": 0, "lease_lost": 0})
        self.assertEqual(self.box.status()["counts"]["review"], 1)

    def test_capability_outage_backs_off_without_repeated_network_requests(self):
        self.queue()
        self.mode = "unavailable"
        self.assertEqual(drain_once(self.box, self.client)["retried"], 1)
        self.assertEqual(len(self.requests), 1)
        self.assertEqual(drain_once(self.box, self.client)["retried"], 0)
        self.assertEqual(len(self.requests), 1)
        self.now[0] += 10
        self.mode = "normal"
        self.assertEqual(drain_once(self.box, self.client)["acknowledged"], 1)

    def test_lost_response_replays_exact_bytes_and_token_rotation(self):
        event = self.queue()
        self.mode = "lost_response"
        self.assertEqual(drain_once(self.box, self.client)["retried"], 1)
        self.assertIsNone(self.box.receipt(event["event_uuid"]))
        first_receipt = self.receipts[event["event_uuid"]]
        self.now[0] += 10
        self.mode = "normal"
        os.environ["STASH_INGEST_TOKEN"] = "replacement-token"
        self.assertEqual(drain_once(self.box, self.client)["acknowledged"], 1)
        self.assertEqual(self.box.receipt(event["event_uuid"]), first_receipt)
        self.assertEqual(self.requests[-1][1], "Bearer replacement-token")
        self.assertEqual(len(self.receipts), 1)

    def test_unknown_or_mismatched_ack_never_discards_events(self):
        self.queue()
        self.queue()
        self.mode = "duplicate_index"
        self.assertEqual(drain_once(self.box, self.client)["retried"], 2)
        self.now[0] += 10
        self.mode = "mismatch"
        self.assertEqual(drain_once(self.box, self.client)["review"], 2)
        self.assertEqual(self.box.status()["counts"]["acknowledged"], 0)

    def test_redirect_and_unauthorized_keep_queue_and_do_not_leak_token(self):
        self.queue()
        self.mode = "redirect"
        self.assertEqual(drain_once(self.box, self.client)["retried"], 1)
        self.assertFalse(any(path == "/leaked-token" for path, _ in self.requests))
        self.now[0] += 10
        self.mode = "unauthorized"
        self.assertEqual(drain_once(self.box, self.client)["retried"], 1)
        row = self.box.db.execute("SELECT error_code,body FROM events").fetchone()
        self.assertEqual(row[0], "stash_token_rejected")
        self.assertNotIn(b"private server response", row[1])

    def test_disabled_file_processor_recovers_old_receipt_and_retries_new_file(self):
        old = self.queue(file_event())
        self.receipts[old["event_uuid"]] = receipt(old)
        self.queue(file_event())
        self.mode = "disabled"
        result = drain_once(self.box, self.client)
        self.assertEqual((result["acknowledged"], result["retried"], result["review"]), (1, 1, 0))
        self.assertEqual(self.box.receipt(old["event_uuid"])["result"]["status"], "queued")

    def test_endpoint_and_token_configuration_rejected_before_io(self):
        for value in ("https://user:secret@example.test", "https://example.test/?token=x", "https://example.test/#",
                      "https://example.test/other", "https://example.test:bad", "https://example.test\n"):
            with self.subTest(value=value), self.assertRaises(InvalidData):
                origin(value)
        self.assertEqual(origin("HTTP://LOCALHOST:80/"), "http://localhost")
        os.environ["STASH_INGEST_TOKEN"] = "invalid\r\nheader"
        with self.assertRaises(Unavailable):
            self.client.capabilities()
        self.assertEqual(self.requests, [])


if __name__ == "__main__":
    unittest.main()
