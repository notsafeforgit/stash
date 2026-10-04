"""Fail-closed native job ownership measured against the server's clock."""

from datetime import datetime
from email.utils import parsedate_to_datetime
import threading
import time
import uuid

from .client import Unavailable
from .encoding import InvalidData, identifier
from .runs import SourcePaused


class JobLease:
    def __init__(self, client, owner, seconds=180, *, clock=time.monotonic):
        client._seconds(seconds)
        self.client, self.owner, self.seconds = client, identifier(owner), seconds
        self.clock, self.job, self.deadline, self.failed = clock, None, 0.0, False
        self.lock, self.stop, self.thread = threading.RLock(), threading.Event(), None

    @classmethod
    def claim(cls, client, job, *, owner=None, seconds=180):
        lease = cls(client, owner or str(uuid.uuid4()), seconds)
        response = client.claim(job, lease.owner, seconds)
        if response[0] is None:
            return None
        lease._accept(response)
        return lease

    def _accept(self, response):
        job, date, started = response
        # Transport validates job/owner/fence and immutable arguments before
        # this deadline becomes usable. Never trust the worker's wall clock.
        try:
            until = datetime.fromisoformat(job["lease_until"])
            server_time = parsedate_to_datetime(date)
            if until.tzinfo is None or server_time.tzinfo is None:
                raise ValueError()
            budget = min(self.seconds, (until - server_time).total_seconds() - 2)
            deadline = started + budget
        except (ValueError, TypeError, KeyError, OverflowError):
            raise SourcePaused("Native job lease has no usable server deadline") from None
        if deadline <= self.clock():
            raise SourcePaused("Native job lease expired before its response arrived")
        self.job, self.deadline = job, deadline

    def check(self):
        with self.lock:
            if self.failed or self.job is None or self.stop.is_set() or self.clock() >= self.deadline:
                raise SourcePaused("Native job ownership is no longer confirmed")

    def renew(self):
        with self.lock:
            try:
                self.check()
                self._accept(self.client.renew(self.job, self.seconds))
            except (Unavailable, InvalidData, SourcePaused):
                self.failed = True
                raise SourcePaused("Cannot renew native job ownership") from None

    def reserve_source(self, url):
        with self.lock:
            self.check()
            ready = self.client.reserve_source(self.job, url)
            self.check()
            return ready

    def start(self):
        self.check()
        if self.thread is not None:
            raise InvalidData("Native job heartbeat already started")

        def heartbeat():
            while not self.stop.wait(self.seconds / 3):
                try:
                    self.renew()
                except SourcePaused:
                    return

        self.thread = threading.Thread(target=heartbeat, name="stash-job-lease", daemon=True)
        self.thread.start()
        return self

    def close(self):
        self.stop.set()
        if self.thread is not None:
            self.thread.join(self.client.client.timeout + 1)
            if self.thread.is_alive():
                raise SourcePaused("Native job heartbeat has not stopped")
