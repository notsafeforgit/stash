"""Restarted selected-page execution against native HTTP, with no source access."""

from contextlib import redirect_stdout
from copy import deepcopy
import io
import json
from pathlib import Path
import sys
from unittest.mock import patch

from stash_ingest.cli import main
from stash_ingest.client import Client
from stash_ingest.discovery_configuration import DiscoveryConfiguration, SCHEMA
from stash_ingest.discovery_journal import DiscoveryJournal
from stash_ingest.discovery_worker import execute
from stash_ingest.encoding import decode
from stash_ingest.job_lease import JobLease
from stash_ingest.outbox import Conflict, Outbox
from stash_ingest.runs import SourcePaused


setup = json.load(sys.stdin)
directory = Path(setup["directory"])
profile = None
if not setup.get("deliver_only", False):
    profile = DiscoveryConfiguration.from_document({"schema": SCHEMA, "source_category": "reddit", "bindings": {
        "login": {"kind": "private", "env": "DISCOVERY_FIXTURE_LOGIN"}}, "gallery": {"extractor": {
        "sleep-request": 1 if setup.get("mismatch") or setup.get("upgraded") else 0, "sleep-extractor": 0,
        "reddit": {"cookies": "${stash:login}"}}}}, directory)
if setup.get("policy_only"):
    print(json.dumps({"policy_sha256": profile.policy_sha256, "extractor_version": profile.extractor_version}))
    raise SystemExit(0)

transport = Client(setup["endpoint"], setup["producer"], timeout=30)
page = decode(Path(setup["fixture"]).read_bytes(), preserve_numbers=True)["pages"][1]["page"]
page["records"][0]["patch"]["source_number"] = decode(b'{"n":0.123456789012345678901234567890}', preserve_numbers=True)["n"]
if setup["fetch"] == "empty_final":
    page.update(records=[], complete=True, next_cursor=None)
count_path = directory / "fetches.txt"
paused = False
original_check = JobLease.check


def check_lease(lease):
    if paused:
        raise SourcePaused("Fixture ownership lost after source returned")
    original_check(lease)


def fetched(url, settings, cursor, *, check, reserve_source):
    global paused
    assert setup["fetch"] != "forbidden", "saved discovery observations cannot be fetched again"
    check()
    assert reserve_source(url)
    assert cursor == page["cursor"] and url == page["url"]
    assert settings["extractor"]["reddit"]["cookies"] == "fixture-private-site-token"
    count_path.write_text(str((int(count_path.read_text()) if count_path.exists() else 0) + 1))
    paused = setup.get("pause_after_fetch", False)
    return {"error": "timeout"} if setup["fetch"] == "source_failure" else deepcopy(page)


box = Outbox(directory / "outbox.sqlite", transport.endpoint, transport.producer, max_bytes=setup.get("max_bytes", 512 << 20))
try:
    with (patch("requests.sessions.Session.request", side_effect=AssertionError("No source website requests in discovery fixture")),
          patch.object(JobLease, "check", check_lease)):
        if setup["deliver_only"]:
            output = io.StringIO()
            with redirect_stdout(output):
                status = main(["--outbox", str(box.path), "--endpoint", transport.endpoint, "--producer", transport.producer,
                               "deliver-discovery", setup["job_uuid"]])
            result = json.loads(output.getvalue())
            assert status == (0 if result["state"] == "page_delivered" else 2)
        elif setup.get("mismatch"):
            try:
                execute(box, transport, profile, setup["job_uuid"], fetcher=fetched)
            except Conflict:
                result = {"state": "profile_mismatch"}
            else:
                raise AssertionError("Changed profile claimed a pinned listing")
        else:
            result = execute(box, transport, profile, setup["job_uuid"], fetcher=fetched)
    journal = DiscoveryJournal(box)
    assert "fixture-private-site-token" not in "\n".join(box.db.iterdump())
    assert result["state"] == setup["expected"], result
    print(json.dumps({"state": result["state"], "journal": journal.summary(setup["job_uuid"]),
        "fetches": int(count_path.read_text()) if count_path.exists() else 0}))
finally:
    box.close()
