"""Bounded enrichment admission and execution with restart-safe discovery."""

from .client import Unavailable
from .encoding import InvalidData, decode, encode, identifier
from .enrichment_client import EnrichmentClient
from .enrichment_journal import EnrichmentJournal
from .enrichment_worker import execute
from .events import sha256
from .outbox import Capacity, Conflict

PAGE_SIZE = 20
DELIVERY_POLICY = "0" * 64


def migrate(db):
    db.execute("""CREATE TABLE enrichment_dispatch(
        collection_uuid TEXT NOT NULL, policy_sha256 TEXT NOT NULL,
        delivery_after TEXT NOT NULL DEFAULT '',
        local_after TEXT NOT NULL DEFAULT '', job_after INTEGER NOT NULL DEFAULT 0 CHECK(job_after>=0),
        target_cursor BLOB, revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
        failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
        available_at REAL NOT NULL DEFAULT 0, error_code TEXT,
        PRIMARY KEY(collection_uuid,policy_sha256)
    )""")


class EnrichmentDispatcher:
    table = "enrichment_dispatch"
    client_type = EnrichmentClient
    journal_type = EnrichmentJournal
    protocol = "enrichment_dispatch_protocol"
    admit_targets = True
    cursor_fields = ("delivery_after", "local_after", "job_after", "target_cursor")

    @staticmethod
    def execute_job(*args):
        return execute(*args)

    def __init__(self, box, transport, collection, configuration=None):
        if (box.endpoint, box.producer) != (transport.endpoint, transport.producer):
            raise Conflict("Enrichment dispatcher and outbox identify different producers")
        policy = configuration.policy_sha256 if configuration else DELIVERY_POLICY
        if not sha256(policy):
            raise InvalidData("Enrichment dispatch requires a reviewed policy")
        self.box, self.transport, self.configuration = box, transport, configuration
        self.client, self.journal = self.client_type(transport), self.journal_type(box)
        self.key = identifier(collection), policy
        with box.transaction():
            if self.state() is None:
                if box.db.execute(f"SELECT count(*) FROM {self.table}").fetchone()[0] >= 10000:
                    raise Capacity("Enrichment discovery capacity exhausted")
                box.db.execute(f"INSERT INTO {self.table}(collection_uuid,policy_sha256) VALUES(?,?)", self.key)

    def state(self):
        row = self.box.db.execute(f"SELECT * FROM {self.table} WHERE collection_uuid=? AND policy_sha256=?", self.key).fetchone()
        if row is None:
            return None
        state = dict(row)
        for key in ("local_after", "delivery_after"):
            if state[key]:
                identifier(state[key])
        if state.get("target_cursor") is not None:
            state["target_cursor"] = decode(state["target_cursor"], 1024)
            self.client._target_order(state["target_cursor"])
        return state

    def save(self, state, **changes):
        values = {**state, **changes}
        cursor = values.get("target_cursor")
        if cursor is not None:
            self.client._target_order(cursor)
            values["target_cursor"] = encode(cursor, 1024)
        assignments = ",".join(key + "=?" for key in self.cursor_fields)
        with self.box.transaction():
            changed = self.box.db.execute(f"""UPDATE {self.table} SET {assignments},
                failures=?,available_at=?,error_code=?,revision=revision+1
                WHERE collection_uuid=? AND policy_sha256=? AND revision=?""", (
                *(values[key] for key in self.cursor_fields), values["failures"], values["available_at"],
                values["error_code"], *self.key, state["revision"])).rowcount
        if changed:
            state.update(changes, revision=state["revision"] + 1)
        return bool(changed)

    def unavailable(self, state, error):
        code = error.code if isinstance(error, Unavailable) else "invalid_enrichment_dispatch"
        failures = state["failures"] + 1
        delay = min(86400, max(5 * 2 ** min(failures - 1, 6), error.retry_after if isinstance(error, Unavailable) else 0))
        if not self.save(state, failures=failures, available_at=self.box.clock() + delay, error_code=code):
            return {"state": "contended"}
        return {"state": "unavailable", "error_code": code, "next_attempt_at": state["available_at"]}

    def _execute(self, state, job, configuration):
        result = self.execute_job(self.box, self.transport, configuration, job)
        if result["state"] == "delivery_pending":
            result["dispatch"] = self.unavailable(state, Unavailable("native_delivery_unavailable"))
        elif not self.save(state, failures=0, available_at=0, error_code=None):
            # Execution may already have committed. Report its actual outcome;
            # a competing cursor must not erase a successful publication proof.
            result["dispatch"] = {"state": "contended"}
        return result

    def _local(self, state, *, deliveries, wrapped=False):
        # The active/review journal is bounded separately from native history.
        # Recovery ignores profile identity when a saved delivery is pending.
        key = "delivery_after" if deliveries else "local_after"
        previous = state[key]
        query = f"""SELECT job_uuid FROM {self.journal.table}
            WHERE phase='active' AND job_uuid>? AND json_extract(definition,'$.arguments.collection_uuid')=?
            AND """
        args = [previous, self.key[0]]
        if deliveries:
            query += "json_type(state,'$.pending')='object'"
        else:
            query += "json_type(state,'$.pending')='null' AND json_extract(definition,'$.arguments.policy_sha256')=?"
            args.append(self.key[1])
        rows = self.box.db.execute(query + " ORDER BY job_uuid LIMIT ?", (*args, PAGE_SIZE)).fetchall()
        for row in rows:
            value = self.journal.find(row["job_uuid"])
            if not self.save(state, **{key: value.job_uuid}):
                return {"state": "contended"}
            selected = None if deliveries else self.configuration
            result = self._execute(state, value.job_uuid, selected)
            if result["state"] == "ownership_required" and self.configuration is not None:
                if (value.definition["arguments"]["policy_sha256"] == self.configuration.policy_sha256
                        and value.definition["arguments"]["extractor_version"] == self.configuration.extractor_version):
                    result = self._execute(state, value.job_uuid, self.configuration)
                else:
                    result = {**result, "state": "profile_required"}
            if result["state"] not in ("waiting", "not_recorded"):
                return result
        if len(rows) < PAGE_SIZE and not self.save(state, **{key: ""}):
            return {"state": "contended"}
        if deliveries and previous and not rows and not wrapped:
            return self._local(state, deliveries=True, wrapped=True)
        # If more local recovery remains, finish that bounded traversal before
        # fetching unrelated source data on a later invocation.
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
            capabilities = self.client.capabilities()
            if type(capabilities.get(self.protocol)) is not int or capabilities[self.protocol] != 1:
                raise Unavailable("native_enrichment_dispatch_unavailable")
            previous = state["job_after"]
            page = self.client.ready_jobs(self.key[0], self.key[1], self.configuration.extractor_version,
                                          after=state["job_after"], limit=PAGE_SIZE)
            for candidate in page:
                if not self.save(state, job_after=candidate["sequence"]):
                    return {"state": "contended"}
                result = self._execute(state, candidate["uuid"], self.configuration)
                if result["state"] != "waiting":
                    return result
            if len(page) == PAGE_SIZE:
                return {"state": "waiting"}
            if not self.save(state, job_after=0):
                return {"state": "contended"}
            # Reaching the end after a saved cursor requires another pass from
            # zero before admitting fresh work; earlier queued retries may exist.
            if previous or page:
                # A runnable job whose claim is currently blocked must not
                # cause successive polls to fill the queue with fresh work.
                return {"state": "waiting"}
            if not self.admit_targets:
                if not self.save(state, failures=0, available_at=self.box.clock() + 30, error_code=None):
                    return {"state": "contended"}
                return {"state": "idle"}
            targets = self.client.ready(self.key[0], PAGE_SIZE, after=state["target_cursor"])
            for target in targets:
                if not self.save(state, target_cursor=self.client.target_cursor(target)):
                    return {"state": "contended"}
                if not self.configuration.accepts(target["url"]):
                    continue
                try:
                    job = self.client.admit(target["uuid"], target["revision"], self.key[1], self.configuration.extractor_version)
                except Unavailable as error:
                    if error.status == 409:
                        continue
                    raise
                return self._execute(state, job["uuid"], self.configuration)
            if len(targets) < PAGE_SIZE:
                if not self.save(state, target_cursor=None, failures=0, available_at=self.box.clock() + 30, error_code=None):
                    return {"state": "contended"}
                return {"state": "idle"}
            return {"state": "waiting"}
        except (Unavailable, InvalidData) as error:
            return self.unavailable(state, error)


def dispatch_once(box, transport, collection, configuration=None):
    result = EnrichmentDispatcher(box, transport, collection, configuration).once()
    return {**result, "collection_uuid": collection}
