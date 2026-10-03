"""Actual CLI discovery/admission/delivery against the native HTTP fixture."""

from contextlib import redirect_stdout
from copy import deepcopy
import io
import json
from pathlib import Path
import sys
from unittest.mock import patch

from stash_ingest.cli import main
from stash_ingest.encoding import decode, encode
from stash_ingest.enrichment_worker import execute
from stash_ingest.outbox import Outbox

setup = json.load(sys.stdin)
directory = Path(setup["directory"])
profile = directory / "profile.json"
if not profile.exists():
    profile.write_bytes(encode({"schema": "stash-gallery-enrichment-v1", "source_category": "reddit",
        "gallery": {"extractor": {"sleep-request": 0, "reddit": {"cookies": "${stash:login}"}}},
        "bindings": {"login": {"kind": "private", "env": "ENRICHMENT_DISPATCH_LOGIN"}}}))
body = decode(Path(setup["fixture"]).read_bytes(), preserve_numbers=True)["complete"]
count_path = directory / "fetches.txt"


def fetched(url, settings, *, resume, check, reserve_source):
    assert reserve_source(url)
    assert not setup["forbid_fetch"], "saved evidence must not be replaced by a new lookup"
    check()
    assert url == body["url"] and resume is None
    assert settings["extractor"]["reddit"]["cookies"] == "fixture-private-login"
    count = int(count_path.read_text()) if count_path.exists() else 0
    count_path.write_text(str(count + 1))
    return deepcopy(body)


def execute_fixture(box, transport, configuration, job):
    return execute(box, transport, configuration, job, fetcher=fetched)


args = ["--outbox", str(directory / "outbox.sqlite"), "--endpoint", setup["endpoint"], "--producer", setup["producer"],
        "dispatch-enrichment", "--collection", setup["collection"]]
if not setup["delivery_only"]:
    args.extend(["--profile", str(profile)])
output = io.StringIO()
with (patch("stash_ingest.cli.Outbox", side_effect=lambda *a, **k: Outbox(*a, **k, clock=lambda: setup["clock"])),
      patch("stash_ingest.enrichment_dispatch.execute", side_effect=execute_fixture),
      patch("requests.sessions.Session.request", side_effect=AssertionError("No website network in fixture")),
      redirect_stdout(output)):
    status = main(args)
result = json.loads(output.getvalue())
assert status == (0 if result["state"] in ("completed", "idle") else 2), (status, result)
assert result["state"] == setup["expected"], result
with_box = Outbox(directory / "outbox.sqlite", setup["endpoint"], setup["producer"])
try:
    assert "fixture-private-login" not in "\n".join(with_box.db.iterdump())
finally:
    with_box.close()
print(json.dumps({**result, "fetches": int(count_path.read_text()) if count_path.exists() else 0}))
