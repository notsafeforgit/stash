"""Resume saved detail delivery and rotate through explicitly admitted jobs."""

from .collection_dispatch import CollectionDispatcher
from .discovery_detail_client import DiscoveryDetailClient
from .discovery_detail_journal import DiscoveryDetailJournal
from .discovery_detail_worker import execute
from .encoding import InvalidData
from .enrichment_dispatch import EnrichmentDispatcher


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


class DiscoveryDetailDispatcher(EnrichmentDispatcher):
    table = "discovery_detail_dispatch"
    client_type = DiscoveryDetailClient
    journal_type = DiscoveryDetailJournal
    protocol = "discovery_detail_protocol"
    admit_targets = False
    cursor_fields = ("delivery_after", "local_after", "job_after")

    def __init__(self, box, transport, collection, configuration=None):
        if configuration is not None and configuration.operation != "post.verify_candidate":
            raise InvalidData("Detail execution requires its reviewed metadata profile")
        super().__init__(box, transport, collection, configuration)

    @staticmethod
    def execute_job(*args):
        return execute(*args)


class DiscoveryDetailCollectionDispatcher(CollectionDispatcher):
    table = "discovery_detail_collection_dispatch"
    protocol = "discovery_detail_collections_protocol"
    protocol_error = "native_discovery_detail_collections_unavailable"

    @staticmethod
    def make_client(transport):
        return DiscoveryDetailClient(transport)

    def selected(self, collection):
        return DiscoveryDetailDispatcher(self.box, self.transport, collection, self.configuration).once()


def dispatch_once(box, transport, collection, configuration=None):
    result = DiscoveryDetailDispatcher(box, transport, collection, configuration).once()
    return {**result, "collection_uuid": collection}
