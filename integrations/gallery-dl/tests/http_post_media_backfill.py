"""Exercise the supported matching CLI against actual Go HTTP handlers."""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import sys

from stash_ingest.post_media_backfill import main


def invoke(arguments, expected=0):
    output, errors = io.StringIO(), io.StringIO()
    with redirect_stdout(output), redirect_stderr(errors):
        code = main(arguments)
    assert code == expected, (arguments[0], code, output.getvalue(), errors.getvalue())
    return json.loads(errors.getvalue() if code == 1 else output.getvalue())


def run(setup):
    directory = Path(setup["directory"])
    endpoint = ["--endpoint", setup["endpoint"]]
    plan, saved = directory / "plan", directory / "client-state.json"
    if setup["phase"] == "lost-response":
        prepared = invoke(["prepare", *endpoint, "--output", str(plan)])
        assert prepared["posts"] == 1 and prepared["matches"] == {"matched": 1} and not prepared["submitted"], prepared
        selected = directory / "posts.json"
        selected.write_text(json.dumps([setup["post_uuid"]]))
        explicit = invoke(["prepare", *endpoint, "--posts-file", str(selected), "--output", str(directory / "explicit")])
        assert explicit["posts"] == 1 and explicit["matches"] == prepared["matches"] and not explicit["submitted"], explicit
        review = invoke(["show", "--plan", str(plan), "--expected-sha256", prepared["plan_sha256"], "--post", setup["post_uuid"]])
        assert review["preview"]["candidates"][0]["proofs"][0]["basis"] == "catalog-file"
        base = [*endpoint, "--plan", str(plan), "--expected-sha256", prepared["plan_sha256"]]
        assert invoke(["status", *base], 3)["counts"] == {"not_submitted": 1}
        assert invoke(["apply", *base], 1)["error"] == "network_unavailable"
        saved.write_text(json.dumps({"plan_sha256": prepared["plan_sha256"]}))
    else:
        state = json.loads(saved.read_text())
        base = [*endpoint, "--plan", str(plan), "--expected-sha256", state["plan_sha256"]]
        status = invoke(["status", *base])
        assert status["processed"] and status["counts"] == {"committed": 1}, status
        assert status["outcomes"]["selected"] == 1 and not status["needs_review"], status
        assert invoke(["apply", *base]) == status, "Resume must retain the original request after later review"
    print(json.dumps({"phase": setup["phase"], "verified": True}))


if __name__ == "__main__":
    run(json.loads(sys.argv[1]))
