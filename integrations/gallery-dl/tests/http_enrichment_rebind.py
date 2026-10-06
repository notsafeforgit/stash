"""Runs against the real native API, including one committed response loss."""

from contextlib import redirect_stdout
import copy
import io
import json
from pathlib import Path
import sys

from stash_ingest.client import Unavailable
from stash_ingest.encoding import InvalidData
from stash_ingest.enrichment_rebind import Plan, inspect_plan, main, prepare
from stash_ingest.enrichment_rebind_client import EnrichmentRebindClient, validate_preview, validate_receipt


def rejects(function, exception=InvalidData):
    try:
        function()
    except exception:
        return
    raise AssertionError("invalid review was accepted")


setup = json.loads(sys.argv[1])
directory = Path(setup["directory"])
client = EnrichmentRebindClient(setup["endpoint"], "STASH_API_KEY")
first = client.candidates(setup["collection"], 2)
assert len(first) == 100
after = (first[-1]["target"]["collection_revision"], first[-1]["target"]["uuid"])
second = client.candidates(setup["collection"], 2, after=after)
assert len(second) == 5
assert client.candidates(setup["collection"], 2, after=(1, second[-1]["target"]["uuid"])) == []
assert len({row["target"]["uuid"] for row in first + second}) == 105
plans = []
for number, rows in enumerate((first, second)):
    assert all(row["disposition"] == "eligible" for row in rows)
    selection = {"collection_uuid": setup["collection"], "collection_revision": 2, "reason": "Reviewed the new collection definition",
                 "targets": [{"target_uuid": row["target"]["uuid"], "revision": row["target"]["revision"]} for row in rows]}
    path = directory / f"review-{number}.json"
    prepared = prepare(client, path, selection)
    assert prepared["rebound"] is False
    assert path.stat().st_mode & 0o777 == 0o600
    plan = Plan(path, prepared["plan_sha256"], setup["endpoint"])
    assert inspect_plan(client, plan)["pending"] is True
    rejects(lambda: prepare(client, path, selection))
    rejects(lambda: Plan(path, "0" * 64, setup["endpoint"]))
    rejects(lambda: Plan(path, prepared["plan_sha256"], "http://localhost:99"))
    preview = plan.read()["preview"]
    for mutate in (
        lambda value: value["activation"]["input"].update(uuid=value["input"]["uuid"]),
        lambda value: value["collection"].update(revision=1),
        lambda value: value["activation"]["entries"][0].update(released_target_uuid=rows[0]["target"]["uuid"]),
        lambda value: value["activation"]["entries"][0].update(priority=101),
        lambda value: value["input"].update(reason="changed after preview"),
    ):
        changed = copy.deepcopy(preview)
        mutate(changed)
        rejects(lambda: validate_preview(changed, preview["input"]))
    plans.append(plan)

rejects(lambda: inspect_plan(client, plans[0], apply=True), Unavailable)
for plan in plans:
    assert inspect_plan(client, plan, apply=True)["rebound"] is True
    assert inspect_plan(client, plan, apply=True)["rebound"] is True
    preview = plan.read()["preview"]
    receipt = client.status(preview)
    changed = copy.deepcopy(receipt)
    changed["activation"]["entries"][0]["priority"] = 99
    if changed == receipt:
        changed["activation"]["entries"][0]["priority"] = 98
    rejects(lambda: validate_receipt(changed, preview))
    args = ["--plan", str(plan.path), "--expected-sha256", plan.sha256]
    with redirect_stdout(io.StringIO()) as output:
        assert main(["show"] + args) == 0
    assert json.loads(output.getvalue())["preview"] == preview
    with redirect_stdout(io.StringIO()) as output:
        assert main(["status", "--endpoint", client.endpoint] + args) == 0
    assert json.loads(output.getvalue())["rebound"] is True
assert client.candidates(setup["collection"], 2) == []
try:
    client.preview(plans[0].read()["preview"]["input"])
except Unavailable as error:
    assert error.status == 409
else:
    raise AssertionError("a stale fresh preview was accepted")
changed_input = copy.deepcopy(plans[0].read()["preview"]["input"])
changed_input["reason"] = "changed decision using an existing operation UUID"
try:
    client.request("POST", "/enrichment-rebindings", {"input": changed_input,
                   "expected_plan_sha256": plans[0].read()["preview"]["plan_sha256"]})
except Unavailable as error:
    assert error.status == 409
else:
    raise AssertionError("a different operation reused a committed UUID")
# A Plan object must recheck disk contents before use, not trust an earlier read.
plans[1].path.write_bytes(plans[1].path.read_bytes() + b" ")
rejects(lambda: inspect_plan(client, plans[1], apply=True))
print(json.dumps({"targets": 105, "plans": 2, "lost_response_recovered": True, "same_plan_reused": True}))
