"""Separate producer processes recover acknowledgements from the actual API."""

from contextlib import closing, redirect_stdout
from copy import deepcopy
import io
import json
from pathlib import Path
import sys
from unittest.mock import patch

from stash_ingest.cli import main
from stash_ingest.client import Client
from stash_ingest.discovery_detail_client import DiscoveryDetailClient
from stash_ingest.discovery_detail_configuration import DiscoveryDetailConfiguration, SCHEMA
from stash_ingest.discovery_detail_journal import DiscoveryDetailJournal
from stash_ingest.discovery_detail_worker import execute
from stash_ingest.encoding import decode, encode
from stash_ingest.outbox import Outbox

setup = decode(sys.stdin.buffer.read(), preserve_numbers=True)
directory = Path(setup["directory"])
transport = Client(setup["endpoint"], setup["producer"], timeout=30)
prefix = ["--outbox", str(directory / "outbox.sqlite"), "--endpoint", transport.endpoint, "--producer", transport.producer]


def cli(*args):
    output = io.StringIO()
    with redirect_stdout(output):
        code = main([*prefix, *args])
    return code, json.loads(output.getvalue())


profile = None
job_path = directory / "job.json"
if not setup["deliver_only"]:
    document = {"schema": SCHEMA, "source_category": "reddit", "gallery": {
        "extractor": {"sleep-request": 0, "sleep-extractor": 0, "reddit": {"cookies": "${stash:login}"}}},
        "bindings": {"login": {"kind": "private", "env": "DETAIL_FIXTURE_LOGIN"}}}
    profile_path = directory / "detail-profile.json"
    profile_path.write_bytes(encode(document))
    profile = DiscoveryDetailConfiguration(profile_path)
    status, policy = cli("detail-policy", "--profile", str(profile_path))
    assert status == 0 and policy["policy_sha256"] == profile.policy_sha256
    if not job_path.exists():
        status, job = cli("admit-detail", setup["target"], "--revision", str(setup["revision"]),
                          "--candidate", str(setup["candidate"]), "--profile", str(profile_path))
        assert status == 0, job
        job_path.write_bytes(encode(job))
job = decode(job_path.read_bytes())
client = DiscoveryDetailClient(transport)
client._job(job)
count_path = directory / "fetches.txt"


def fetched(url, settings, *, resume, check, reserve_source):
    assert not setup["deliver_only"], "saved delivery must never fetch again"
    assert resume is None and url == setup["body"]["url"]
    check()
    assert reserve_source(url)
    assert settings["extractor"]["reddit"]["cookies"] == "fixture-private-site-token"
    count_path.write_text(str(int(count_path.read_text()) + 1 if count_path.exists() else 1))
    return deepcopy(setup["body"])


with closing(Outbox(directory / "outbox.sqlite", transport.endpoint, transport.producer)) as box:
    with patch("requests.sessions.Session.request", side_effect=AssertionError("unexpected source access")):
        if setup["deliver_only"]:
            status, result = cli("deliver-detail", job["uuid"])
            assert status == (0 if result["state"] == "completed" else 2)
        else:
            client.capabilities()
            collections = client.ready_collections(profile.policy_sha256, profile.extractor_version)
            assert {"uuid": job["arguments"]["collection_uuid"]} in collections
            result = execute(box, transport, profile, job["uuid"], fetcher=fetched)
    assert result["state"] == setup["expected"], result
    status, summary = cli("detail-status", "--job", job["uuid"])
    assert status == 0 and summary == DiscoveryDetailJournal(box).summary(job["uuid"])
    assert "publication" not in summary
    assert "fixture-private-site-token" not in "\n".join(box.db.iterdump())
    print(json.dumps({"job_uuid": job["uuid"], "state": result["state"], "journal": summary,
                      "fetches": int(count_path.read_text()) if count_path.exists() else 0}))
