"""Run by the Go HTTP integration test against its real router and SQLite DB."""

import json
import sys
import uuid
from datetime import datetime, timezone
from pathlib import Path

from stash_ingest.client import Client, drain_once
from stash_ingest.completion import inspect_ticket
from stash_ingest.encoding import encode
from stash_ingest.outbox import Outbox
from stash_ingest.runs import RunLease
from stash_ingest.run_queue import RunQueue, submit_once
from helpers import capture


def main():
    setup = json.load(sys.stdin)
    now = [1000.0]
    box = Outbox(Path(setup["directory"]) / "producer.sqlite", setup["endpoint"],
                 setup["producer"], clock=lambda: now[0])
    client = Client(setup["endpoint"], setup["producer"])
    policy = "b" * 64
    request = {"collection_uuid": setup["collection"],
               "collection_revision": setup["revision"], "operation": "enrich", "policy_sha256": policy,
               "window": {"since": None, "until": datetime.now(timezone.utc).isoformat(timespec="milliseconds")},
               "cooldown_seconds": 0}
    queue = RunQueue(box)
    ticket = str(uuid.uuid4())
    intent = queue.enqueue(request, ticket_uuid=ticket)
    failed = submit_once(queue, client)
    assert failed["state"] == "pending", failed
    box.close()
    box = Outbox(Path(setup["directory"]) / "producer.sqlite", setup["endpoint"],
                 setup["producer"], clock=lambda: now[0])
    queue = RunQueue(box)
    now[0] += 10
    admitted = submit_once(queue, client)
    assert admitted["state"] == "admitted", admitted
    assert admitted["request_uuid"] == failed["request_uuid"]
    assert queue.enqueue(request, ticket_uuid=ticket) == intent
    assert queue.claim(str(uuid.uuid4())) is None
    completion = inspect_ticket(box, client, ticket)
    assert completion["state"] == "queued", completion
    assert completion["remaining"], completion
    run_id = admitted["run_uuid"]
    lease = RunLease.claim(client, run_id, policy)
    assert lease is not None
    try:
        assert lease.run["target_url"] == "https://x.com/fixture/media"
        assert lease.run["path_prefix"] == ""
        assert RunLease.claim(client, run_id, policy) is None
        lease.renew()
        lease.progress(1, 0, "source-key")
        assert lease.run["progress"]["items_seen"] == 1
        assert inspect_ticket(box, client, ticket)["state"] == "running"
        assert lease.finish("succeeded")["state"] == "succeeded"
    finally:
        lease.close()
    completion = inspect_ticket(box, client, ticket)
    assert completion["state"] == "source_succeeded" and not completion["remaining"], completion
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
                          "sha256": ack["sha256"], "run_uuid": run_id}))
    finally:
        box.close()


if __name__ == "__main__":
    main()
