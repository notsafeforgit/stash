"""Discovery profile rotation over currently permitted collection containers."""

from .collection_dispatch import CollectionDispatcher
from .discovery_client import DiscoveryClient
from .discovery_dispatch import DiscoveryDispatcher, PAGE_SIZE


def migrate(db):
    db.execute("""CREATE TABLE discovery_collection_dispatch(
        policy_sha256 TEXT PRIMARY KEY NOT NULL, after_collection TEXT NOT NULL DEFAULT '',
        revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0), failures INTEGER NOT NULL DEFAULT 0 CHECK(failures>=0),
        available_at REAL NOT NULL DEFAULT 0, error_code TEXT
    )""")
    db.execute("ALTER TABLE worker_dispatch RENAME COLUMN delivery_after TO enrichment_delivery_after")
    db.execute("ALTER TABLE worker_dispatch ADD COLUMN discovery_delivery_after TEXT NOT NULL DEFAULT ''")


class DiscoveryCollectionDispatcher(CollectionDispatcher):
    table = "discovery_collection_dispatch"
    protocol = "discovery_collections_protocol"
    protocol_error = "native_discovery_collections_unavailable"

    @staticmethod
    def make_client(transport):
        return DiscoveryClient(transport)

    def ready(self, after):
        return self.client.ready_collections(after=after, limit=PAGE_SIZE)

    def selected(self, collection):
        return DiscoveryDispatcher(self.box, self.transport, collection, self.configuration).once()
