"""Verify exact retained migration input, lost responses and native receipts."""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
import os
from pathlib import Path
import sys
from urllib.request import Request, urlopen

from stash_ingest.encoding import encode
from stash_ingest.metadata_policy_import import main, snapshot


def execute(args, wanted=0):
    output, errors = io.StringIO(), io.StringIO()
    with redirect_stdout(output), redirect_stderr(errors):
        assert main(args) == wanted, errors.getvalue()
    return json.loads(output.getvalue()) if wanted == 0 else None


def run():
    setup = json.loads(sys.argv[1])
    directory = Path(setup["directory"])
    source = snapshot(b'set_organized_only_if = ["title", "cover_image"]\n', b'{}', b'{}', "1.14.1", "2026-09-29T00:00:00Z")
    source["values"]["saved/private_notes"] = "Unicode <value> & café\u2028preserved"
    binding = {"uuid": setup["uuid"], "document": source, "folder_sources": [],
               "policy": {"collection_uuid": setup["collection"], "expected_collection_revision": 1, "expected_revision": 0,
                          "origin": "migration", "reason": "Reviewed retained settings", "definition": {
                              "enabled": False, "apply_to_scans": True, "rules": {"scene": {
                                  "on_create": True, "filename_title_fallback": True,
                                  "mappings": {
                                      "title": {"jq": ".source.metadata.title // empty"},
                                      "performers": {"value": ["Known alias"], "reference_names": True, "performer_names": False},
                                      "studio": {"value": "Studio alias", "reference_names": True},
                                      "tags": {"jq": ".source.payload.tags // empty", "reference_names": True},
                                      "groups": {"value": [{"name": "Album", "scene_index": 3}], "reference_names": True}
                                  }}}}},
               "dispositions": {"python/set_organized_only_if": {"action": "review", "reason": "Artwork condition needs explicit conversion"},
                                "saved/private_notes": {"action": "retired", "reason": "Retained source note"}}}
    frozen = directory / "binding.json"
    frozen.write_bytes(encode(binding))
    args = ["--binding", str(frozen), "--endpoint", setup["endpoint"]]
    plan = execute(args)
    assert plan["action"] == "preview"
    mappings = plan["definition"]["rules"]["scene"]["mappings"]
    for field in ("performers", "studio", "tags", "groups"):
        assert mappings[field]["reference_names"] is True
    assert "performer_names" not in mappings["performers"]
    frozen.write_bytes(encode(plan))
    reviewed = frozen.read_bytes()
    args += ["--apply", "--expected-sha256", plan["plan_sha256"]]
    execute(args, 1)
    first = execute(args)
    assert first == execute(args)
    assert first["policy_revision"] == 1
    assert frozen.read_bytes() == reviewed
    request = Request(setup["endpoint"] + "/api/v3/archive/metadata-policy-imports/" + setup["uuid"],
                      headers={"ApiKey": os.environ["STASH_API_KEY"]})
    with urlopen(request) as response:
        assert response.headers["Cache-Control"] == "no-store"
        details = json.load(response)
    assert details["binding"] == plan["binding"]
    assert details["review_keys"] == ["python/set_organized_only_if"]
    print(json.dumps({"source_unchanged": True, "replayed": True, "retained_original_values": True}))


if __name__ == "__main__":
    run()
