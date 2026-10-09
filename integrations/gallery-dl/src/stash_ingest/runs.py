"""Fenced source execution, independent of eventual outbox acknowledgement."""

from datetime import datetime
from email.utils import parsedate_to_datetime
import re
import threading
import time
import uuid

from .client import Unavailable
from .encoding import InvalidData, encode, identifier
from .events import sha256


class SourcePaused(RuntimeError):
    """The current file can finish, but no further source work may start."""


class SourceTurnComplete(RuntimeError):
    """Cooperative scheduling yield; ownership and captured progress remain valid."""


def source_scope(value):
    return (isinstance(value, str)
            and re.fullmatch(r"(?:service|mirror|host):[a-z0-9][a-z0-9.:\-]{0,252}", value) is not None)


class SourceFailure(RuntimeError):
    """A controlled service failure without website responses or private URLs."""

    def __init__(self, code, scope):
        if code not in {"source_busy", "rate_limited", "timeout", "extraction_failed", "authentication",
                        "access_denied", "challenge", "not_found"} or not source_scope(scope):
            raise InvalidData("Invalid source failure attribution")
        self.code, self.scope = code, scope
        super().__init__(code + " (" + scope + ")")


def submit(client, request):
    """Submit an already durable request; use RunQueue for offline coalescing."""
    return client._request("POST", "/runs", encode(request, 8192))


class RunLease:
    def __init__(self, client, run, owner, policy, *, seconds=180, clock=time.monotonic):
        self.client, self.run_uuid = client, identifier(run)
        self.owner, self.policy = identifier(owner), policy
        if not sha256(policy) or type(seconds) is not int or not 30 <= seconds <= 900:
            raise InvalidData("Invalid source lease configuration")
        self.seconds, self.clock = seconds, clock
        self.run, self.deadline, self.failure = None, 0.0, None
        self.turn_deadline = 0.0
        self.lock = threading.RLock()
        self.stop = threading.Event()
        self.thread = None

    @classmethod
    def claim(cls, client, run, policy, *, owner=None, seconds=180):
        lease = cls(client, run, owner or str(uuid.uuid4()), policy, seconds=seconds)
        response = client._request("POST", "/runs/" + lease.run_uuid + "/claim",
                                   encode({"owner_uuid": lease.owner, "policy_sha256": policy,
                                           "lease_seconds": seconds, "recovery_protocol": 1}), timed=True, allow_empty=True)
        if response[0] is None:
            return None
        lease._accept(response)
        return lease

    def _accept(self, response):
        run, date, started = response
        if (not isinstance(run, dict) or run.get("uuid") != self.run_uuid
                or run.get("producer_uuid") != self.client.producer
                or run.get("owner_uuid") != self.owner or run.get("state") != "running"
                or run.get("execution_policy_sha256", run.get("policy_sha256")) != self.policy
                or type(run.get("fence")) is not int or run["fence"] < 1
                or (self.run is not None and run["fence"] != self.run["fence"])):
            raise SourcePaused("Source lease response does not identify the claimed work")
        try:
            until = datetime.fromisoformat(run["lease_until"])
            turn_until = datetime.fromisoformat(run["turn_until"])
            server_time = parsedate_to_datetime(date)
            if until.tzinfo is None or turn_until.tzinfo is None or server_time.tzinfo is None:
                raise ValueError()
            # HTTP Date has one-second precision. Budget from the monotonic
            # request start, subtract another second, and cap by the requested
            # lease. A replayed claim may return an older, shorter lease.
            budget = min(self.seconds, (until - server_time).total_seconds() - 2)
            deadline = started + budget
            turn_deadline = started + min(300, (turn_until - server_time).total_seconds() - 2)
        except (ValueError, TypeError, KeyError, OverflowError):
            raise SourcePaused("Source lease response has no usable server deadline") from None
        if deadline <= self.clock():
            raise SourcePaused("Source lease expired before its response arrived")
        if self.run is not None:
            for field in ("policy_sha256", "execution_policy_sha256", "collection_uuid", "collection_revision", "target_url", "retrieval_url", "path_prefix",
                          "root_uuid", "root_revision", "window", "operation", "recovery", "turn_until"):
                if run.get(field) != self.run.get(field):
                    raise SourcePaused("Source lease definition changed")
        self.turn_deadline = turn_deadline if self.run is None else min(self.turn_deadline, turn_deadline)
        self.run, self.deadline = run, deadline

    def check(self):
        with self.lock:
            if self.failure or self.run is None or self.clock() >= self.deadline or self.stop.is_set():
                raise SourcePaused("Source execution is paused; its current file may finish queuing")

    def check_turn(self):
        self.check()
        if self.clock() >= self.turn_deadline:
            raise SourceTurnComplete("Source time budget reached; checkpointing for the next turn")

    def _change(self, **change):
        self.check()
        return self.client._request("POST", "/runs/" + self.run_uuid + "/lease",
                                    encode({"owner_uuid": self.owner, "fence": self.run["fence"], **change}, 8192), timed=True)

    def renew(self):
        with self.lock:
            try:
                self._accept(self._change(lease_seconds=self.seconds))
            except (Unavailable, InvalidData, SourcePaused) as exc:
                self.failure = type(exc).__name__
                raise SourcePaused("Cannot renew source ownership; further source work is paused") from None

    def progress(self, items_seen, files_completed, cursor):
        with self.lock:
            try:
                self._accept(self._change(progress={"items_seen": items_seen,
                             "files_completed": files_completed, "cursor": cursor}))
            except (Unavailable, InvalidData, SourcePaused) as exc:
                self.failure = type(exc).__name__
                raise SourcePaused("Cannot checkpoint source progress; evidence remains queued") from None

    def reserve_source(self, url):
        with self.lock:
            try:
                self.check()
                result = self.client._request("POST", "/runs/" + self.run_uuid + "/source",
                                              encode({"owner_uuid": self.owner, "fence": self.run["fence"],
                                                      "url": url}, 16384))
                self.check()
                if (not isinstance(result, dict) or result.get("run_uuid") != self.run_uuid
                        or type(result.get("fence")) is not int or result["fence"] != self.run["fence"]
                        or type(result.get("ready")) is not bool or not source_scope(result.get("source_scope"))):
                    raise SourcePaused("Source reservation does not identify the current attempt")
            except (Unavailable, InvalidData, SourcePaused):
                self.failure = "source_reservation_unavailable"
                raise SourcePaused("Cannot confirm source service ownership") from None
            if not result["ready"]:
                raise SourceFailure("source_busy", result["source_scope"])
            return result["source_scope"]

    def start(self):
        self.check()
        if self.thread is not None:
            raise InvalidData("Source heartbeat already started")

        def heartbeat():
            while not self.stop.wait(self.seconds / 3):
                try:
                    self.renew()
                except SourcePaused:
                    return

        self.thread = threading.Thread(target=heartbeat, name="stash-source-lease", daemon=True)
        self.thread.start()
        return self

    def finish(self, state, *, error_code="", error_scope="", retry_after_seconds=0):
        with self.lock:
            try:
                expected = {"succeeded": {"queued", "succeeded"}, "retry": {"queued", "deferred"},
                            "deferred": {"deferred"}}.get(state)
                if expected is None:
                    raise InvalidData("Invalid source attempt outcome")
                outcome = {"state": state, "error_code": error_code, "retry_after_seconds": retry_after_seconds}
                if error_scope:
                    SourceFailure(error_code, error_scope)
                    outcome["error_scope"] = error_scope
                result, _, _ = self._change(outcome=outcome)
                if (not isinstance(result, dict) or result.get("uuid") != self.run_uuid
                        or result.get("fence") != self.run["fence"] or result.get("state") not in expected
                        or any(result.get(field) != self.run.get(field) for field in (
                            "policy_sha256", "execution_policy_sha256", "collection_uuid", "collection_revision", "root_uuid", "root_revision",
                            "target_url", "retrieval_url", "path_prefix", "operation"))):
                    raise SourcePaused("Run completion was not acknowledged")
                return result
            finally:
                self.stop.set()

    def close(self):
        self.stop.set()
        if self.thread is not None:
            self.thread.join(self.client.timeout + 1)
            if self.thread.is_alive():
                raise SourcePaused("Source heartbeat has not stopped")
