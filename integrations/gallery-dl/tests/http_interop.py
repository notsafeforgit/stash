"""Run by the Go HTTP integration test against its real router and SQLite DB."""

import json
import sys
from pathlib import Path

from stash_ingest.client import Client, drain_once
from stash_ingest.encoding import encode
from stash_ingest.outbox import Outbox
from helpers import capture


def main():
    setup = json.load(sys.stdin)
    now = [1000.0]
    box = Outbox(Path(setup["directory"]) / "producer.sqlite", setup["endpoint"],
                 setup["producer"], clock=lambda: now[0])
    client = Client(setup["endpoint"], setup["producer"])
    source = capture(producer_uuid=setup["producer"], collection_uuid=setup["collection"],
                     collection_revision=setup["revision"], root_uuid=None)
    # Well-formed local envelope, but the claimed post contradicts raw evidence.
    rejected = capture(producer_uuid=setup["producer"], collection_uuid=setup["collection"],
                       collection_revision=setup["revision"], root_uuid=None,
                       post={"namespace": "native:twitter", "value": "different"})
    try:
        box.enqueue(encode(source))
        box.enqueue(encode(rejected))
        first = drain_once(box, client)
        assert first["retried"] == 2, first
        assert box.receipt(source["event_uuid"]) is None
        now[0] += 10
        second = drain_once(box, client)
        assert second["acknowledged"] == 1 and second["review"] == 1, second
        ack = box.receipt(source["event_uuid"])
        assert client.receipt(source["event_uuid"]) == ack
        box.enqueue(encode(source))
        assert box.status()["counts"] == {"pending": 0, "sending": 0, "review": 1, "acknowledged": 1}
        print(json.dumps({"accepted": source["event_uuid"], "rejected": rejected["event_uuid"],
                          "sha256": ack["sha256"]}))
    finally:
        box.close()


if __name__ == "__main__":
    main()
