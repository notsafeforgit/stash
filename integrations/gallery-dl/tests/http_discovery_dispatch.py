"""A separate producer process per dispatch cycle, using real native transport."""

from contextlib import closing, redirect_stdout
from copy import deepcopy
import io
import json
from pathlib import Path
import sys
from unittest.mock import patch

from stash_ingest.cli import main
from stash_ingest.client import Client
from stash_ingest.discovery_configuration import DiscoveryConfiguration, SCHEMA
from stash_ingest.discovery_dispatch import dispatch_once
from stash_ingest.discovery_worker import execute
from stash_ingest.encoding import decode, encode
from stash_ingest.outbox import Outbox


setup = json.load(sys.stdin)
directory = Path(setup["directory"])
profile = None
profile_document = None
if not setup.get("delivery_only"):
    profile_document = {"schema": SCHEMA, "source_category": "reddit", "bindings": {
        "login": {"kind": "private", "env": "DISCOVERY_FIXTURE_LOGIN"}}, "gallery": {"extractor": {
        "sleep-request": 0, "sleep-extractor": 0, "reddit": {"cookies": "${stash:login}"}}}}
    profile = DiscoveryConfiguration.from_document(profile_document, directory)
if setup.get("policy_only"):
    print(json.dumps({"policy_sha256": profile.policy_sha256}))
    raise SystemExit(0)

transport = Client(setup["endpoint"], setup["producer"], timeout=30)
page = decode(Path(setup["fixture"]).read_bytes(), preserve_numbers=True)["pages"][1]["page"]
count_path = directory / "fetches.txt"


def fetched(url, settings, cursor, *, check, reserve_source):
    assert not setup.get("delivery_only"), "Saved deliveries cannot access the website"
    check()
    assert reserve_source(url)
    assert settings["extractor"]["reddit"]["cookies"] == "fixture-private-site-token"
    assert url == page["url"]
    count_path.write_text(str((int(count_path.read_text()) if count_path.exists() else 0) + 1))
    result = deepcopy(page)
    if cursor is not None:
        assert cursor == page["next_cursor"]
        result.update(cursor=cursor, next_cursor=None, complete=True, records=[])
    return result


with closing(Outbox(directory / "outbox.sqlite", transport.endpoint, transport.producer,
                    clock=lambda: setup["clock"])) as box:
    with (patch("stash_ingest.discovery_dispatch.execute", side_effect=lambda b, t, c, j: execute(b, t, c, j, fetcher=fetched)),
          patch("requests.sessions.Session.request", side_effect=AssertionError("No website access in dispatcher fixture"))):
        if setup.get("delivery_only"):
            output = io.StringIO()
            with redirect_stdout(output):
                status = main(["--outbox", str(box.path), "--endpoint", transport.endpoint, "--producer", transport.producer,
                               "dispatch-discovery", "--collection", setup["collection"]])
            result = json.loads(output.getvalue())
            assert status == (0 if result["state"] in ("page_delivered", "idle") else 2)
        elif setup.get("global"):
            profile_path = directory / "discovery.json"
            # Gallery configuration order is meaningful and part of the reviewed
            # policy identity. Event encoding sorts maps and is unsuitable here.
            profile_path.write_text(json.dumps(profile_document), encoding="utf-8")
            assert DiscoveryConfiguration(profile_path).policy_sha256 == profile.policy_sha256
            profiles = directory / "profiles.json"
            profiles.write_bytes(encode({"schema": "stash-gallery-dispatch-v1", "uuid": setup["worker_uuid"], "profiles": [
                {"id": "discovery", "operation": "account.list_page", "profile": "discovery.json"}]}))
            # Use the supplied clock in the CLI-opened outbox, too. Each cycle
            # is a separate process; advancing time never edits saved cursors.
            with patch("stash_ingest.cli.Outbox", side_effect=lambda *a, **kw: Outbox(*a, **kw, clock=lambda: setup["clock"])):
                output = io.StringIO()
                with redirect_stdout(output):
                    status = main(["--outbox", str(box.path), "--endpoint", transport.endpoint, "--producer", transport.producer,
                                   "dispatch-all", "--profiles", str(profiles)])
            result = json.loads(output.getvalue())
            assert status == (0 if result["state"] in ("page_delivered", "idle") else 2)
            assert result["intake_completion"] == "inspect_native_receipts"
            assert "discovery" in result and "discovery_delivery" in result
        else:
            result = dispatch_once(box, transport, setup["collection"], profile)
    assert "fixture-private-site-token" not in "\n".join(box.db.iterdump())
    print(json.dumps({"result": result, "fetches": int(count_path.read_text()) if count_path.exists() else 0}))
