"""Exercise the supported CLI in separate processes against native Go handlers."""

from contextlib import redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import sys

from stash_ingest.album_backfill import main


def invoke(arguments, expected):
    output, errors = io.StringIO(), io.StringIO()
    with redirect_stdout(output), redirect_stderr(errors):
        code = main(arguments)
    assert code == expected, (arguments[0], code, output.getvalue(), errors.getvalue())
    return json.loads(errors.getvalue() if code == 1 else output.getvalue())


def run(setup):
    directory = Path(setup["directory"])
    endpoint = ["--endpoint", setup["endpoint"]]
    post = setup["post_uuid"]
    original, retry = directory / "plan", directory / "retry"
    state_path = directory / "client-state.json"
    if setup["phase"] == "queued":
        prepared = invoke(["prepare", *endpoint, "--all-selected", "--policy", "source-identifiers-v1",
                           "--output", str(original)], 0)
        assert prepared["posts"] == 1 and not prepared["submitted"], prepared
        assert prepared["matches"] == {"unavailable": 2}, prepared
        explicit = invoke(["prepare", *endpoint, "--post", post, "--policy", "source-identifiers-v1",
                           "--output", str(directory / "explicit")], 0)
        assert explicit["posts"] == 1 and explicit["matches"] == prepared["matches"] and not explicit["submitted"], explicit
        base = [*endpoint, "--plan", str(original), "--expected-sha256", prepared["plan_sha256"]]
        show = invoke(["show", "--plan", str(original), "--expected-sha256", prepared["plan_sha256"], "--post", post], 0)
        assert show["preview"]["initial_metadata"]["title"] == "Album title"
        assert not invoke(["status", *base], 3)["complete"]
        # The server commits admission, then disconnects before acknowledging it.
        assert not invoke(["apply", *base], 1)["complete"]
        accepted = invoke(["apply", *base], 3)["records"][0]["job"]
        assert accepted["state"] == "queued" and not accepted["publication_committed"]
        cancel = ["cancel", *base, "--post", post, "--expected-revision", str(accepted["revision"])]
        assert not invoke(cancel, 1)["complete"]
        assert invoke(cancel, 0)["state"] == "cancelled"
        assert invoke(["status", *base], 2)["needs_review"]
        prepared_retry = invoke(["prepare-retry", *base, "--post", post, "--output", str(retry)], 0)
        retry_base = [*endpoint, "--plan", str(retry), "--expected-sha256", prepared_retry["plan_sha256"]]
        assert invoke(["status", *retry_base], 3)["counts"] == {"not_submitted": 1}
        assert not invoke(["apply", *retry_base], 1)["complete"]
        resumed = invoke(["apply", *retry_base], 3)["records"][0]["job"]
        assert resumed["resume_from_job_uuid"] == accepted["job_uuid"]
        assert resumed["job_uuid"] != accepted["job_uuid"]
        state_path.write_text(json.dumps({"original": prepared["plan_sha256"], "retry": prepared_retry["plan_sha256"],
                                          "original_job": accepted["job_uuid"], "retry_job": resumed["job_uuid"]}))
    else:
        saved = json.loads(state_path.read_text())
        base = [*endpoint, "--plan", str(retry), "--expected-sha256", saved["retry"]]
        final = invoke(["status", *base], 0)
        assert final["complete"] and final["counts"] == {"succeeded": 1}
        job = final["records"][0]["job"]
        assert job["job_uuid"] == saved["retry_job"]
        assert job["resume_from_job_uuid"] == saved["original_job"]
        assert job["publication_committed"] and job["hooks_finished"]
        assert job["publication"]["created"] and job["publication"]["unavailable"] == 2
        assert invoke(["apply", *base], 0) == final, "Replay must inspect the original job even after its preview changed"
        assert invoke(["status", *endpoint, "--plan", str(original), "--expected-sha256", saved["original"]], 2)["counts"] == {"cancelled": 1}
    print(json.dumps({"phase": setup["phase"], "verified": True}))


if __name__ == "__main__":
    run(json.loads(sys.argv[1]))
