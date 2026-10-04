"""Resume saved detail work before bounded admission using a selected profile."""

from .client import Unavailable
from .collection_dispatch import CollectionDispatcher
from .discovery_detail_client import DiscoveryDetailClient
from .discovery_detail_journal import DiscoveryDetailJournal
from .discovery_detail_worker import execute
from .encoding import InvalidData
from .enrichment_dispatch import EnrichmentDispatcher, PAGE_SIZE


def migrate(db):
    db.execute("""CREATE TABLE discovery_detail_dispatch(
        collection_uuid TEXT NOT NULL, policy_sha256 TEXT NOT NULL,
        delivery_after TEXT NOT NULL DEFAULT '', local_after TEXT NOT NULL DEFAULT '',
        job_after INTEGER NOT NULL DEFAULT 0 CHECK(job_after>=0),
        revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0), failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
        available_at REAL NOT NULL DEFAULT 0, error_code TEXT,
        PRIMARY KEY(collection_uuid,policy_sha256)
    )""")
    db.execute("""CREATE TABLE discovery_detail_collection_dispatch(
        policy_sha256 TEXT PRIMARY KEY NOT NULL, after_collection TEXT NOT NULL DEFAULT '',
        revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0), failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
        available_at REAL NOT NULL DEFAULT 0, error_code TEXT
    )""")
    db.execute("ALTER TABLE worker_dispatch ADD COLUMN discovery_detail_delivery_after TEXT NOT NULL DEFAULT ''")


def migrate_candidates(db):
    db.execute("ALTER TABLE discovery_detail_dispatch ADD COLUMN target_cursor BLOB")


class DiscoveryDetailDispatcher(EnrichmentDispatcher):
    table = "discovery_detail_dispatch"
    client_type = DiscoveryDetailClient
    journal_type = DiscoveryDetailJournal
    protocol = "discovery_detail_protocol"
    cursor_fields = ("delivery_after", "local_after", "job_after", "target_cursor")

    def __init__(self, box, transport, collection, configuration=None):
        if configuration is not None and configuration.operation != "post.verify_candidate":
            raise InvalidData("Detail execution requires its reviewed metadata profile")
        super().__init__(box, transport, collection, configuration)

    @staticmethod
    def execute_job(*args):
        return execute(*args)

    def admit_next(self, state, capabilities):
        if type(capabilities.get("discovery_detail_admission_protocol")) is not int or capabilities["discovery_detail_admission_protocol"] != 1:
            raise Unavailable("native_discovery_detail_admission_unavailable")
        page = self.client.candidates(self.key[0], PAGE_SIZE, after=state["target_cursor"])
        for candidate in page["candidates"]:
            # Save progress before sending. An uncertain admission is recovered
            # by the ready-job pass, and a later traversal revisits an unsent one.
            if not self.save(state, target_cursor=candidate["cursor"]):
                return {"state": "contended"}
            if not self.configuration.accepts(candidate["url"]):
                continue
            try:
                job = self.client.admit(candidate["target_uuid"], candidate["target_revision"], candidate["candidate_sequence"],
                                        self.key[1], self.configuration.extractor_version, automatic=True)
            except Unavailable as error:
                if error.status == 409:
                    continue
                raise
            return self._execute(state, job["uuid"], self.configuration)
        if not self.save(state, target_cursor=page["after"] if page["has_more"] else None,
                         failures=0, available_at=0 if page["has_more"] else self.box.clock() + 30, error_code=None):
            return {"state": "contended"}
        return {"state": "waiting" if page["has_more"] else "idle"}


class DiscoveryDetailCollectionDispatcher(CollectionDispatcher):
    # Inspection includes blocked/empty containers. A completed traversal must
    # pause even when more than one collection page was needed to reach its end.
    pause_after_traversal = True
    table = "discovery_detail_collection_dispatch"
    protocol = "discovery_detail_admission_protocol"
    protocol_error = "native_discovery_detail_admission_unavailable"

    @staticmethod
    def make_client(transport):
        return DiscoveryDetailClient(transport)

    def selected(self, collection):
        return DiscoveryDetailDispatcher(self.box, self.transport, collection, self.configuration).once()


def dispatch_once(box, transport, collection, configuration=None):
    result = DiscoveryDetailDispatcher(box, transport, collection, configuration).once()
    return {**result, "collection_uuid": collection}
