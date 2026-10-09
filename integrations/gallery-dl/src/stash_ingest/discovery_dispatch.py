"""Restart-safe traversal of existing, reviewed account listing definitions."""

from .client import Unavailable
from .discovery_client import DiscoveryClient
from .discovery_journal import DiscoveryJournal
from .discovery_worker import execute
from .encoding import InvalidData, identifier
from .events import sha256
from .outbox import Capacity, Conflict
from .worker_policy import execution_policy

PAGE_SIZE = 20
DELIVERY_POLICY = "0" * 64


def migrate(db):
    db.execute("""CREATE TABLE discovery_dispatch(
        collection_uuid TEXT NOT NULL, policy_sha256 TEXT NOT NULL,
        delivery_after TEXT NOT NULL DEFAULT '', local_after TEXT NOT NULL DEFAULT '',
        job_after INTEGER NOT NULL DEFAULT 0 CHECK(job_after>=0),
        listing_after TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
        failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
        available_at REAL NOT NULL DEFAULT 0, error_code TEXT,
        PRIMARY KEY(collection_uuid,policy_sha256)
    )""")


class DiscoveryDispatcher:
    def __init__(self, box, transport, collection, configuration=None):
        if (box.endpoint, box.producer) != (transport.endpoint, transport.producer):
            raise Conflict("Discovery dispatcher and outbox identify different producers")
        policy = configuration.policy_sha256 if configuration else DELIVERY_POLICY
        if not sha256(policy) or (configuration is not None and configuration.operation != "account.list_page"):
            raise InvalidData("Discovery dispatch requires a reviewed account-listing profile")
        self.box, self.transport, self.configuration = box, transport, configuration
        self.client, self.journal = DiscoveryClient(transport), DiscoveryJournal(box)
        self.key = identifier(collection), policy
        with box.transaction():
            if self.state() is None:
                if box.db.execute("SELECT count(*) FROM discovery_dispatch").fetchone()[0] >= 10000:
                    raise Capacity("Discovery dispatch capacity exhausted")
                box.db.execute("INSERT INTO discovery_dispatch(collection_uuid,policy_sha256) VALUES(?,?)", self.key)

    def state(self):
        row = self.box.db.execute("SELECT * FROM discovery_dispatch WHERE collection_uuid=? AND policy_sha256=?", self.key).fetchone()
        if row is None:
            return None
        state = dict(row)
        for key in ("local_after", "delivery_after", "listing_after"):
            if state[key] != "":
                identifier(state[key])
        return state

    def save(self, state, **changes):
        values = {**state, **changes}
        with self.box.transaction():
            changed = self.box.db.execute("""UPDATE discovery_dispatch SET delivery_after=?,local_after=?,job_after=?,listing_after=?,
                failures=?,available_at=?,error_code=?,revision=revision+1
                WHERE collection_uuid=? AND policy_sha256=? AND revision=?""", (
                values["delivery_after"], values["local_after"], values["job_after"], values["listing_after"],
                values["failures"], values["available_at"], values["error_code"], *self.key, state["revision"])).rowcount
        if changed:
            state.update(changes, revision=state["revision"] + 1)
        return bool(changed)

    def unavailable(self, state, error):
        code = error.code if isinstance(error, Unavailable) else "invalid_discovery_dispatch"
        failures = state["failures"] + 1
        delay = min(86400, max(5 * 2 ** min(failures - 1, 6), error.retry_after if isinstance(error, Unavailable) else 0))
        if not self.save(state, failures=failures, available_at=self.box.clock() + delay, error_code=code):
            return {"state": "contended"}
        return {"state": "unavailable", "error_code": code, "next_attempt_at": state["available_at"]}

    def _execute(self, state, job, configuration):
        result = execute(self.box, self.transport, configuration, job)
        if result["state"] == "delivery_pending":
            result["dispatch"] = self.unavailable(state, Unavailable("native_delivery_unavailable"))
        elif not self.save(state, failures=0, available_at=0, error_code=None):
            result["dispatch"] = {"state": "contended"}
        return result

    def _local(self, state, *, deliveries, wrapped=False):
        key = "delivery_after" if deliveries else "local_after"
        previous = state[key]
        query = """SELECT job_uuid FROM discovery_executions
            WHERE phase='active' AND job_uuid>? AND json_extract(definition,'$.arguments.collection_uuid')=?
            AND """
        args = [previous, self.key[0]]
        if deliveries:
            query += "json_type(state,'$.pending')='object'"
        else:
            query += "json_type(state,'$.pending')='null' AND json_extract(definition,'$.listing.policy_sha256')=?"
            args.append(self.key[1])
        rows = self.box.db.execute(query + " ORDER BY job_uuid LIMIT ?", (*args, PAGE_SIZE)).fetchall()
        for row in rows:
            value = self.journal.find(row["job_uuid"])
            if not self.save(state, **{key: value.job_uuid}):
                return {"state": "contended"}
            selected = None if deliveries else self.configuration
            result = self._execute(state, value.job_uuid, selected)
            if result["state"] == "ownership_required" and self.configuration is not None:
                listing = value.definition["listing"]
                current = self.client.describe(value.job_uuid)
                if (execution_policy(current["job"], listing["policy_sha256"]) == self.configuration.policy_sha256
                        and listing["extractor_version"] == self.configuration.extractor_version):
                    result = self._execute(state, value.job_uuid, self.configuration)
                else:
                    result = {**result, "state": "profile_required"}
            if result["state"] not in ("waiting", "not_recorded"):
                return result
        if len(rows) < PAGE_SIZE and not self.save(state, **{key: ""}):
            return {"state": "contended"}
        if deliveries and previous and not rows and not wrapped:
            return self._local(state, deliveries=True, wrapped=True)
        return {"state": "waiting"} if len(rows) == PAGE_SIZE or (deliveries and (previous or rows)) else None

    def once(self):
        state = self.state()
        if state["available_at"] > self.box.clock():
            return {"state": "backoff", "error_code": state["error_code"], "next_attempt_at": state["available_at"]}
        try:
            local = self._local(state, deliveries=True)
            if local is not None:
                return local
            if self.configuration is None:
                return {"state": "idle", "mode": "delivery_only"}
            local = self._local(state, deliveries=False)
            if local is not None:
                return local
            self.configuration.check()
            capabilities = self.client.capabilities(readiness=True)
            if type(capabilities.get("discovery_dispatch_protocol")) is not int or capabilities["discovery_dispatch_protocol"] != 1:
                raise Unavailable("native_discovery_dispatch_unavailable")
            previous = state["job_after"]
            jobs = self.client.ready_jobs(self.key[0], self.key[1], self.configuration.extractor_version,
                                          after=previous, limit=PAGE_SIZE)
            for candidate in jobs:
                if not self.save(state, job_after=candidate["sequence"]):
                    return {"state": "contended"}
                result = self._execute(state, candidate["uuid"], self.configuration)
                if result["state"] != "waiting":
                    return result
            if len(jobs) == PAGE_SIZE:
                return {"state": "waiting"}
            if not self.save(state, job_after=0):
                return {"state": "contended"}
            if previous or jobs:
                # Wrap admitted retry discovery before admitting fresh work.
                return {"state": "waiting"}
            page = self.client.ready_listings(self.key[0], self.key[1], self.configuration.extractor_version,
                                              after=state["listing_after"], limit=PAGE_SIZE)
            for candidate in page["listings"]:
                if not self.save(state, listing_after=candidate["uuid"]):
                    return {"state": "contended"}
                try:
                    job = self.client.admit(candidate["uuid"], candidate["definition_sha256"],
                                            self.key[1], self.configuration.extractor_version)
                except Unavailable as error:
                    if error.status == 409:
                        continue
                    raise
                return self._execute(state, job["uuid"], self.configuration)
            # Empty filtered pages still advance. An inspection cursor alone
            # cannot establish listing completion or a published source match.
            changes = {"listing_after": page["after"] if page["has_more"] else ""}
            if not page["has_more"]:
                changes.update(failures=0, available_at=self.box.clock() + 30, error_code=None)
            if not self.save(state, **changes):
                return {"state": "contended"}
            return {"state": "waiting" if page["has_more"] else "idle"}
        except (Unavailable, InvalidData) as error:
            return self.unavailable(state, error)


def dispatch_once(box, transport, collection, configuration=None):
    result = DiscoveryDispatcher(box, transport, collection, configuration).once()
    return {**result, "collection_uuid": collection}
