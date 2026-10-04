"""One bounded scheduling pass over local download and metadata profiles."""

from pathlib import Path
import re

from .backfill_calls import BackfillCalls, advance_once
from .client import Unavailable, drain_once
from .collection_dispatch import CollectionDispatcher
from .configuration import Configuration, _json_file, _path
from .dispatch import Dispatcher
from .discovery_collection_dispatch import DiscoveryCollectionDispatcher
from .discovery_configuration import DiscoveryConfiguration
from .discovery_journal import DiscoveryJournal
from .discovery_worker import execute as deliver_discovery
from .discovery_detail_configuration import DiscoveryDetailConfiguration
from .discovery_detail_dispatch import DiscoveryDetailCollectionDispatcher
from .discovery_detail_journal import DiscoveryDetailJournal
from .discovery_detail_worker import execute as deliver_detail
from .encoding import InvalidData, identifier
from .enrichment_configuration import EnrichmentConfiguration
from .enrichment_journal import EnrichmentJournal
from .enrichment_worker import execute as deliver_enrichment
from .outbox import Capacity, Conflict
from .run_queue import RunQueue, submit_once
from .source_calls import SourceCalls, resolve_once

SCHEMA = "stash-gallery-dispatch-v1"


class Profiles:
    def __init__(self, filename):
        filename = Path(filename).resolve(strict=True)
        value = _json_file(filename)
        if (not isinstance(value, dict) or set(value) != {"schema", "uuid", "profiles"} or value["schema"] != SCHEMA
                or not isinstance(value["profiles"], list) or not 1 <= len(value["profiles"]) <= 32):
            raise InvalidData("Invalid worker dispatch configuration")
        self.uuid, self.entries = identifier(value["uuid"]), []
        seen = set()
        for entry in value["profiles"]:
            if (not isinstance(entry, dict) or set(entry) != {"id", "operation", "profile"}
                    or not isinstance(entry["id"], str) or not re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,63}", entry["id"])
                    or entry["id"] in seen or entry["operation"] not in ("download", "post.enrich", "account.list_page", "post.verify_candidate")):
                raise InvalidData("Invalid or repeated worker profile entry")
            seen.add(entry["id"])
            self.entries.append({**entry, "profile": _path(entry["profile"], filename.parent)})


class WorkerDispatcher:
    def __init__(self, box, transport, profiles):
        if (box.endpoint, box.producer) != (transport.endpoint, transport.producer):
            raise Conflict("Worker dispatcher and outbox identify different producers")
        self.box, self.transport, self.profiles = box, transport, profiles
        with box.transaction():
            if self.state() is None:
                if box.db.execute("SELECT count(*) FROM worker_dispatch").fetchone()[0] >= 10000:
                    raise Capacity("Worker dispatch capacity exhausted")
                box.db.execute("INSERT INTO worker_dispatch(uuid) VALUES(?)", (profiles.uuid,))

    def state(self):
        row = self.box.db.execute("SELECT * FROM worker_dispatch WHERE uuid=?", (self.profiles.uuid,)).fetchone()
        return dict(row) if row else None

    def save(self, state, **changes):
        value = {**state, **changes}
        with self.box.transaction():
            changed = self.box.db.execute("""UPDATE worker_dispatch SET after_entry=?,enrichment_delivery_after=?,discovery_delivery_after=?,discovery_detail_delivery_after=?,revision=revision+1
                WHERE uuid=? AND revision=?""", (value["after_entry"], value["enrichment_delivery_after"], value["discovery_delivery_after"],
                    value["discovery_detail_delivery_after"], self.profiles.uuid, state["revision"])).rowcount
        if changed:
            state.update(changes, revision=state["revision"] + 1)
        return bool(changed)

    def recover_delivery(self, state, kind):
        # Delivery does not require a current website profile, active collection
        # definition or new discovery grant. The server authenticates the saved
        # job's historical scope when receiving its exact pending operation.
        cursor = kind + "_delivery_after"
        table, deliver = {"enrichment": ("enrichment_executions", deliver_enrichment),
                          "discovery": ("discovery_executions", deliver_discovery),
                          "discovery_detail": ("discovery_detail_executions", deliver_detail)}[kind]
        if state[cursor]:
            identifier(state[cursor])
        query = f"""SELECT job_uuid FROM {table} WHERE phase='active'
            AND json_type(state,'$.pending')='object' AND job_uuid>? ORDER BY job_uuid LIMIT 1"""
        row = self.box.db.execute(query, (state[cursor],)).fetchone()
        if row is None and state[cursor]:
            row = self.box.db.execute(query, ("",)).fetchone()
        if not self.save(state, **{cursor: row["job_uuid"] if row else ""}):
            return {"state": "contended"}
        return deliver(self.box, self.transport, None, row["job_uuid"]) if row else {"state": "idle"}

    def once(self):
        state = self.state()
        deliveries = {}
        for kind in ("enrichment", "discovery", "discovery_detail"):
            delivery = self.recover_delivery(state, kind)
            deliveries[kind + "_delivery"] = delivery
            if delivery["state"] == "contended":
                return {"state": "contended", **deliveries}
        entries = self.profiles.entries
        names = [entry["id"] for entry in entries]
        start = names.index(state["after_entry"]) + 1 if state["after_entry"] in names else 0
        ordered = entries[start:] + entries[:start]
        skipped = []
        for entry in ordered:
            if not self.save(state, after_entry=entry["id"]):
                return {"state": "contended", **deliveries, "profiles": skipped}
            try:
                if entry["operation"] == "download":
                    result = Dispatcher(self.box, self.transport, Configuration(entry["profile"])).once()
                elif entry["operation"] == "post.enrich":
                    result = CollectionDispatcher(self.box, self.transport, EnrichmentConfiguration(entry["profile"])).once()
                elif entry["operation"] == "account.list_page":
                    result = DiscoveryCollectionDispatcher(self.box, self.transport, DiscoveryConfiguration(entry["profile"])).once()
                else:
                    result = DiscoveryDetailCollectionDispatcher(self.box, self.transport, DiscoveryDetailConfiguration(entry["profile"])).once()
            except (OSError, InvalidData, Capacity, Unavailable):
                # An unavailable local profile cannot hold every other source.
                # Do not return paths, access values or raw exception strings.
                result = {"state": "unavailable", "error_code": "worker_profile_unavailable"}
            result = {**result, "profile_id": entry["id"]}
            if result["state"] not in ("waiting", "idle", "backoff", "unavailable", "profile_required", "contended"):
                return {**result, "profiles": skipped, **deliveries}
            skipped.append(result)
        idle = all(r["state"] == "idle" for r in [*skipped, *deliveries.values()])
        return {"state": "idle" if idle else "waiting", "profiles": skipped, **deliveries}


def dispatch_all(box, client, profiles):
    worker = WorkerDispatcher(box, client, profiles)
    requests, calls, backfills = RunQueue(box), SourceCalls(box), BackfillCalls(box)
    delivery = drain_once(box, client)
    backfill = advance_once(backfills, client)
    resolution = resolve_once(calls, client)
    submission = submit_once(requests, client)
    result = worker.once()
    return {**result, "delivery": delivery, "backfill": backfill, "resolution": resolution, "submission": submission,
            "outbox": box.status(), "source_requests": requests.status(), "source_calls": calls.summary(),
            "backfill_calls": backfills.summary(), "enrichment": EnrichmentJournal(box).summary(),
            "discovery": DiscoveryJournal(box).summary(),
            "discovery_detail": DiscoveryDetailJournal(box).summary(),
            "intake_completion": "inspect_native_receipts"}
