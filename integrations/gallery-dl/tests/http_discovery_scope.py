"""Exercise saved collection reviews against native HTTP, including restart."""

from contextlib import redirect_stdout
import copy
import io
import json
from pathlib import Path
import sys

from stash_ingest.client import Unavailable
from stash_ingest.discovery_scope import Plan, inspect_plan, main, prepare
from stash_ingest.discovery_scope_client import DiscoveryScopeClient, validate_preview, validate_receipt
from stash_ingest.encoding import InvalidData


def rejects(function, exception=InvalidData):
    try:
        function()
    except exception:
        return
    raise AssertionError("invalid discovery review was accepted")


setup = json.loads(sys.argv[1])
client = DiscoveryScopeClient(setup["endpoint"])
directory = Path(setup["directory"])
requests = []
request = client.request


def tracked(method, path, *args, **kwargs):
    requests.append(method)
    return request(method, path, *args, **kwargs)


client.request = tracked
if setup["phase"] == "apply":
    rows, after = [], None
    while True:
        page = client.candidates(setup["collection"], 2, after=after, limit=1)
        if not page:
            break
        rows += page
        after = page[-1]["listing"]["uuid"]
    assert len(rows) == 3 and all(row["disposition"] == "eligible" for row in rows)
    plans = []
    for index, row in enumerate(rows):
        path = directory / f"review-{index}.json"
        selection = {"listing_uuid": row["listing"]["uuid"], "expected_definition_sha256": row["listing"]["sha256"],
                     "collection_revision": 2, "reason": "Reviewed collection association"}
        prepared = prepare(client, path, selection)
        assert path.stat().st_mode & 0o777 == 0o600
        plan = Plan(path, prepared["plan_sha256"], client.endpoint)
        assert inspect_plan(client, plan)["pending"]
        rejects(lambda: prepare(client, path, selection))
        rejects(lambda: Plan(path, prepared["plan_sha256"], "http://localhost:9876"))
        saved = plan.read()
        for mutate in (
            lambda value: value["previous"].update(profile_url="https://www.reddit.com/user/unrelated/"),
            lambda value: value["collection"].update(revision=3),
            lambda value: value.update(definition_sha256="0" * 64),
        ):
            changed = copy.deepcopy(saved["preview"])
            mutate(changed)
            rejects(lambda: validate_preview(changed))
        body = path.read_bytes()
        path.write_bytes(body + b" ")
        rejects(plan.read)
        path.write_bytes(body)
        plans.append({"path": str(path), "sha256": plan.sha256})
    lost = 0
    for item in plans:
        plan = Plan(item["path"], item["sha256"], client.endpoint)
        try:
            result = inspect_plan(client, plan, True)
        except Unavailable as error:
            assert error.code == "network_unavailable" and lost == 0
            lost += 1
            result = inspect_plan(client, plan, True)
        assert result["rebound"] and not result["pending"]
        receipt = client.status(plan.read()["preview"])
        changed = copy.deepcopy(receipt)
        changed["previous"]["historical_pages"] += 1
        rejects(lambda: validate_receipt(changed, plan.read()["preview"]))
    assert lost == 1
    with (directory / "saved-reviews.json").open("x") as stream:
        json.dump(plans, stream)
    for path, expected in (
        (f"/collections/{setup['collection']}/discovery-scope-candidates?collection_revision={'9' * 40}&limit=1", 400),
        (f"/collections/{setup['collection']}/discovery-scope-candidates?collection_revision=1", 409),
    ):
        try:
            client.request("GET", path)
        except Unavailable as error:
            assert error.status == expected
        else:
            raise AssertionError("invalid or stale candidate request was accepted")
else:
    plans = json.loads((directory / "saved-reviews.json").read_text())
    for item in plans:
        plan = Plan(item["path"], item["sha256"], client.endpoint)
        assert inspect_plan(client, plan, True)["rebound"]
        output = io.StringIO()
        with redirect_stdout(output):
            assert main(["status", "--endpoint", client.endpoint, "--plan", item["path"], "--expected-sha256", item["sha256"]]) == 0
        assert json.loads(output.getvalue())["rebound"]
    assert requests == ["GET"] * 3
print(json.dumps({"phase": setup["phase"], "reviews": len(plans)}))
