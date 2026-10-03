from datetime import datetime, timedelta, timezone
from email.utils import format_datetime
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from stash_ingest.client import Unavailable
from stash_ingest.runs import RunLease, SourceFailure, SourcePaused, SourceTurnComplete
from stash_ingest.encoding import decode
from helpers import PRODUCER, COLLECTION, ROOT, RUN

OWNER = "3569ad7a-4e4a-4f4f-9ecf-a59ed6f59df1"


class RunLeaseTests(unittest.TestCase):
    def setUp(self):
        self.now = [1000.0]
        self.client = SimpleNamespace(producer=PRODUCER, timeout=1, _request=Mock())
        self.lease = RunLease(self.client, RUN, OWNER, "a" * 64, seconds=180, clock=lambda: self.now[0])
        self.wall = datetime(2026, 10, 1, tzinfo=timezone.utc)
        self.run = {"uuid": RUN, "owner_uuid": OWNER, "producer_uuid": PRODUCER,
                    "state": "running", "fence": 3, "policy_sha256": "a" * 64,
                    "collection_uuid": COLLECTION, "collection_revision": 1, "root_uuid": ROOT,
                    "target_url": "https://fixture.invalid/account", "path_prefix": ".",
                    "root_revision": 1, "operation": "download", "window": {"since": None, "until": self.wall.isoformat()},
                    "lease_until": (self.wall + timedelta(seconds=180)).isoformat()}
        self.run["turn_until"] = (self.wall + timedelta(seconds=300)).isoformat()

    def response(self, **changes):
        return dict(self.run, **changes), format_datetime(self.wall, usegmt=True), self.now[0]

    def test_server_date_avoids_host_clock_skew_and_claim_replay_does_not_extend_lease(self):
        self.lease._accept(self.response())
        self.assertEqual(self.lease.deadline, 1178)
        self.now[0] += 100
        self.wall += timedelta(seconds=100)
        self.lease._accept(self.response())
        self.assertEqual(self.lease.deadline, 1178)
        self.now[0] = 1179
        with self.assertRaises(SourcePaused):
            self.lease.check()

    def test_renewal_progress_and_terminal_finish(self):
        self.lease._accept(self.response())
        self.now[0] += 50
        self.wall += timedelta(seconds=50)
        renewed = self.response(lease_until=(self.wall + timedelta(seconds=180)).isoformat())
        self.client._request.return_value = renewed
        self.lease.renew()
        self.assertEqual(self.lease.deadline, 1228)
        self.lease.progress(4, 3, "cursor")
        self.client._request.return_value = (dict(self.run, state="succeeded"), None, 0)
        result = self.lease.finish("succeeded")
        self.assertEqual(result["state"], "succeeded")
        with self.assertRaises(SourcePaused):
            self.lease.check()

    def test_outage_fence_or_definition_change_pauses_source_work(self):
        for changed in ({"fence": 4}, {"collection_revision": 2}, {"owner_uuid": PRODUCER}, {"state": "queued"},
                        {"target_url": "https://fixture.invalid/other"}, {"path_prefix": "Other"}):
            with self.subTest(changed=changed):
                lease = RunLease(self.client, RUN, OWNER, "a" * 64, seconds=180, clock=lambda: self.now[0])
                lease._accept(self.response())
                self.client._request.return_value = self.response(**changed)
                with self.assertRaises(SourcePaused):
                    lease.renew()
                with self.assertRaises(SourcePaused):
                    lease.check()
        self.lease._accept(self.response())
        self.client._request.side_effect = Unavailable("network_unavailable")
        with self.assertRaises(SourcePaused):
            self.lease.progress(1, 1, "first")
        with self.assertRaises(SourcePaused):
            self.lease.check()

    def test_missing_or_expired_server_deadline_is_not_guessed(self):
        for response in ((self.run, None, 1000), self.response(lease_until=self.wall.isoformat())):
            with self.assertRaises(SourcePaused):
                self.lease._accept(response)

    def test_finish_cannot_acknowledge_a_different_definition_or_invalid_outcome(self):
        for changes in ({"state": "cancelled"}, {"state": None}, {"policy_sha256": "b" * 64},
                        {"root_uuid": PRODUCER}, {"fence": 2}):
            with self.subTest(changes=changes):
                lease = RunLease(self.client, RUN, OWNER, "a" * 64, seconds=180, clock=lambda: self.now[0])
                lease._accept(self.response())
                self.client._request.return_value = (dict(self.run, **({"state": "succeeded"} | changes)), None, 0)
                with self.assertRaises(SourcePaused):
                    lease.finish("succeeded")
                with self.assertRaises(SourcePaused):
                    lease.check()


    def test_source_reservation_is_fenced_and_busy_can_be_reported_with_live_lease(self):
        self.lease._accept(self.response())
        permit = {"run_uuid": RUN, "fence": 3, "ready": True, "source_scope": "service:redgifs"}
        self.client._request.return_value = permit
        self.assertEqual(self.lease.reserve_source("https://redgifs.com/watch/example"), "service:redgifs")
        request = self.client._request.call_args.args
        self.assertEqual(request[:2], ("POST", "/runs/" + RUN + "/source"))
        self.assertEqual(decode(request[2]), {"owner_uuid": OWNER, "fence": 3, "url": "https://redgifs.com/watch/example"})
        self.client._request.return_value = {**permit, "ready": False}
        with self.assertRaises(SourceFailure) as failure:
            self.lease.reserve_source("https://redgifs.com/watch/example")
        self.assertEqual((failure.exception.code, failure.exception.scope), ("source_busy", "service:redgifs"))
        self.lease.check()
        self.client._request.return_value = (dict(self.run, state="queued"), None, 0)
        self.lease.finish("retry", error_code="source_busy", error_scope="service:redgifs")
        self.assertEqual(decode(self.client._request.call_args.args[2])["outcome"]["error_scope"], "service:redgifs")

    def test_invalid_or_late_source_permit_stops_ownership(self):
        permit = {"run_uuid": RUN, "fence": 3, "ready": True, "source_scope": "service:redgifs"}
        for change in ({"run_uuid": ROOT}, {"fence": 2}, {"fence": True}, {"ready": "true"},
                       {"source_scope": "https://redgifs.com/private?token=secret"}, {"source_scope": None}):
            with self.subTest(change=change):
                lease = RunLease(self.client, RUN, OWNER, "a" * 64, clock=lambda: self.now[0])
                lease._accept(self.response())
                self.client._request.return_value = {**permit, **change}
                with self.assertRaises(SourcePaused):
                    lease.reserve_source("https://redgifs.com/watch/example")
                with self.assertRaises(SourcePaused):
                    lease.check()
        self.lease._accept(self.response())
        def late(*args, **kwargs):
            self.now[0] += 180
            return permit
        self.client._request.side_effect = late
        with self.assertRaises(SourcePaused):
            self.lease.reserve_source("https://redgifs.com/watch/example")

    def test_heartbeat_does_not_extend_turn_and_yield_can_still_checkpoint(self):
        self.lease._accept(self.response())
        first = self.lease.turn_deadline
        self.now[0] += 160
        self.wall += timedelta(seconds=160)
        self.client._request.return_value = self.response(lease_until=(self.wall + timedelta(seconds=180)).isoformat())
        self.lease.renew()
        self.assertEqual(self.lease.turn_deadline, first)
        self.now[0] = first
        with self.assertRaises(SourceTurnComplete):
            self.lease.check_turn()
        self.lease.check()
        self.lease.progress(2, 1, "saved")
        self.client._request.return_value = (dict(self.run, state="queued"), None, 0)
        self.lease.finish("retry", error_code="source_turn_complete")

    def test_missing_or_changed_turn_deadline_cannot_extend_source_work(self):
        missing = dict(self.run)
        missing.pop("turn_until")
        with self.assertRaises(SourcePaused):
            self.lease._accept((missing, format_datetime(self.wall, usegmt=True), self.now[0]))
        self.lease._accept(self.response())
        self.client._request.return_value = self.response(turn_until=(self.wall + timedelta(seconds=600)).isoformat())
        with self.assertRaises(SourcePaused):
            self.lease.renew()


if __name__ == "__main__":
    unittest.main()
