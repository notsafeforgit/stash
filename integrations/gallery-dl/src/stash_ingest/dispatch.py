"""Bounded worker discovery with durable pagination and outage backoff."""

from .client import Unavailable, drain_once
from .encoding import InvalidData, encode, identifier
from .events import sha256
from .outbox import Capacity, Conflict
from .run_queue import RunQueue, submit_once
from .worker import execute


class Dispatcher:
    """A cursor is a scheduling hint; only a server claim permits source work."""

    def __init__(self, box, client, configuration):
        if (box.endpoint, box.producer) != (client.endpoint, client.producer):
            raise Conflict("Dispatcher and outbox identify different Stash producers")
        identifier(configuration.root_uuid)
        if not sha256(configuration.policy_sha256):
            raise InvalidData("Dispatcher requires a reviewed worker policy")
        self.box, self.client, self.configuration = box, client, configuration
        self.key = configuration.root_uuid, configuration.policy_sha256
        with box.transaction():
            if not self.state():
                if box.db.execute("SELECT count(*) FROM dispatch_cursors").fetchone()[0] >= 10000:
                    raise Capacity("Worker policy cursor capacity exhausted")
                box.db.execute("INSERT INTO dispatch_cursors(root_uuid,policy_sha256) VALUES(?,?)", self.key)

    def state(self):
        row = self.box.db.execute("SELECT * FROM dispatch_cursors WHERE root_uuid=? AND policy_sha256=?", self.key).fetchone()
        return dict(row) if row else None

    def save(self, state, after, *, failures=0, available=0, error=None):
        # Competing dispatchers can discover the same page. Only one advances
        # this cursor revision; server fencing still owns the actual work.
        with self.box.transaction():
            changed = self.box.db.execute("""UPDATE dispatch_cursors SET after_sequence=?,revision=revision+1,
                failures=?,available_at=?,error_code=? WHERE root_uuid=? AND policy_sha256=? AND revision=?""",
                (after, failures, available, error, *self.key, state["revision"])).rowcount
        if changed:
            state.update(after_sequence=after, revision=state["revision"] + 1,
                         failures=failures, available_at=available, error_code=error)
        return bool(changed)

    def unavailable(self, state, error):
        code = error.code if isinstance(error, Unavailable) else "invalid_dispatch_response"
        failures = state["failures"] + 1
        delay = min(86400, max(5 * 2 ** min(failures - 1, 6),
                               error.retry_after if isinstance(error, Unavailable) else 0))
        if not self.save(state, state["after_sequence"], failures=failures,
                         available=self.box.clock() + delay, error=code):
            return {"state": "contended"}
        return {"state": "unavailable", "error_code": code, "next_attempt_at": state["available_at"]}

    def page(self, after):
        capabilities = self.client.capabilities()
        if (capabilities.get("source_runs") is not True or capabilities.get("source_run_protocol") != 1
                or capabilities.get("source_run_dispatch") is not True or capabilities.get("file_ingestion") is not True):
            raise Unavailable("native_dispatch_unavailable")
        page = self.client._request("POST", "/runs/ready", encode({"root_uuid": self.key[0],
                    "policy_sha256": self.key[1], "after": after}))
        if not isinstance(page, list) or len(page) > 50:
            raise InvalidData("Invalid worker discovery page")
        previous, seen = after, set()
        for item in page:
            if (not isinstance(item, dict) or set(item) != {"sequence", "uuid"}
                    or type(item["sequence"]) is not int or not previous < item["sequence"] <= 9223372036854775807):
                raise InvalidData("Invalid worker discovery cursor")
            identifier(item["uuid"])
            if item["uuid"] in seen:
                raise InvalidData("Repeated worker discovery candidate")
            previous = item["sequence"]
            seen.add(item["uuid"])
        return page

    def once(self):
        state = self.state()
        if state["available_at"] > self.box.clock():
            return {"state": "backoff", "error_code": state["error_code"], "next_attempt_at": state["available_at"]}
        self.configuration.check()
        try:
            page = self.page(state["after_sequence"])
        except (Unavailable, InvalidData) as error:
            return self.unavailable(state, error)
        for item in page:
            # Persist before invoking the downloader. A process crash cannot
            # make an unavailable first page starve all subsequent work.
            if not self.save(state, item["sequence"]):
                return {"state": "contended"}
            try:
                result = execute(self.box, self.client, self.configuration, item["uuid"])
            except (Unavailable, InvalidData) as error:
                return self.unavailable(state, error)
            if result["state"] != "waiting":
                return result
        if len(page) < 50 and not self.save(state, 0):
            return {"state": "contended"}
        return {"state": "waiting" if page else "idle", "candidates_checked": len(page),
                "after_sequence": state["after_sequence"]}


def dispatch_once(box, client, configuration):
    """Deliver, admit and attempt work once; no outcome certifies media intake."""
    dispatcher = Dispatcher(box, client, configuration)
    requests = RunQueue(box)
    delivery = drain_once(box, client)
    submission = submit_once(requests, client)
    result = dispatcher.once()
    return {**result, "delivery": delivery, "submission": submission, "outbox": box.status(),
            "source_requests": requests.status(), "intake_completion": "inspect_native_receipts"}
