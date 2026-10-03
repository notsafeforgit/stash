"""Round-robin metadata discovery across the producer's permitted collections."""

from .client import Unavailable
from .encoding import InvalidData, identifier
from .enrichment_client import EnrichmentClient
from .enrichment_dispatch import EnrichmentDispatcher, PAGE_SIZE
from .events import sha256
from .outbox import Capacity, Conflict


def migrate(db):
    db.execute("""CREATE TABLE enrichment_collection_dispatch(
        policy_sha256 TEXT PRIMARY KEY NOT NULL, after_collection TEXT NOT NULL DEFAULT '',
        revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0), failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
        available_at REAL NOT NULL DEFAULT 0, error_code TEXT
    )""")
    db.execute("""CREATE TABLE worker_dispatch(
        uuid TEXT PRIMARY KEY NOT NULL, after_entry TEXT NOT NULL DEFAULT '', delivery_after TEXT NOT NULL DEFAULT '',
        revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0)
    )""")


class CollectionDispatcher:
    def __init__(self, box, transport, configuration):
        if (box.endpoint, box.producer) != (transport.endpoint, transport.producer):
            raise Conflict("Collection dispatcher and outbox identify different producers")
        self.box, self.transport, self.configuration = box, transport, configuration
        self.client, self.policy = EnrichmentClient(transport), configuration.policy_sha256
        if not sha256(self.policy):
            raise InvalidData("Collection discovery requires a reviewed policy")
        with box.transaction():
            if self.state() is None:
                if box.db.execute("SELECT count(*) FROM enrichment_collection_dispatch").fetchone()[0] >= 10000:
                    raise Capacity("Collection discovery capacity exhausted")
                box.db.execute("INSERT INTO enrichment_collection_dispatch(policy_sha256) VALUES(?)", (self.policy,))

    def state(self):
        row = self.box.db.execute("SELECT * FROM enrichment_collection_dispatch WHERE policy_sha256=?", (self.policy,)).fetchone()
        return dict(row) if row else None

    def save(self, state, **changes):
        value = {**state, **changes}
        with self.box.transaction():
            changed = self.box.db.execute("""UPDATE enrichment_collection_dispatch SET after_collection=?,failures=?,available_at=?,error_code=?,revision=revision+1
                WHERE policy_sha256=? AND revision=?""", (value["after_collection"], value["failures"], value["available_at"],
                    value["error_code"], self.policy, state["revision"])).rowcount
        if changed:
            state.update(changes, revision=state["revision"] + 1)
        return bool(changed)

    def once(self):
        state = self.state()
        if state["after_collection"]:
            identifier(state["after_collection"])
        if state["available_at"] > self.box.clock():
            return {"state": "backoff", "error_code": state["error_code"], "next_attempt_at": state["available_at"]}
        try:
            self.configuration.check()
            capabilities = self.client.capabilities()
            if type(capabilities.get("enrichment_collections_protocol")) is not int or capabilities["enrichment_collections_protocol"] != 1:
                raise Unavailable("native_enrichment_collections_unavailable")
            previous = state["after_collection"]
            page = self.client.ready_collections(self.policy, self.configuration.extractor_version, after=previous, limit=PAGE_SIZE)
            waiting = False
            for item in page:
                # Persist before execution, including on process death or a
                # continuously busy collection. The next pass visits its peers.
                if not self.save(state, after_collection=item["uuid"], failures=0, available_at=0, error_code=None):
                    return {"state": "contended"}
                result = EnrichmentDispatcher(self.box, self.transport, item["uuid"], self.configuration).once()
                if result["state"] not in ("waiting", "idle", "backoff"):
                    return {**result, "collection_uuid": item["uuid"]}
                waiting |= result["state"] != "idle"
            if len(page) == PAGE_SIZE:
                return {"state": "waiting"}
            # Wrapping a partial traversal is not proof that earlier work is
            # absent. Start again without an idle delay on the next pass.
            waiting |= bool(previous)
            if not self.save(state, after_collection="", failures=0, error_code=None,
                             available_at=0 if waiting else self.box.clock() + 30):
                return {"state": "contended"}
            return {"state": "waiting" if waiting else "idle"}
        except (Unavailable, InvalidData) as error:
            failures = state["failures"] + 1
            delay = min(86400, max(5 * 2 ** min(failures - 1, 6), error.retry_after if isinstance(error, Unavailable) else 0))
            code = error.code if isinstance(error, Unavailable) else "invalid_collection_dispatch"
            if not self.save(state, failures=failures, available_at=self.box.clock() + delay, error_code=code):
                return {"state": "contended"}
            return {"state": "unavailable", "error_code": code, "next_attempt_at": state["available_at"]}
