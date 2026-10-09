"""Verify native workflow source operations against a real Go/SQLite server."""
import json
from pathlib import Path
import subprocess
import sys

from test_n8n_sources import policy_definition

setup = json.load(sys.stdin)
directory = Path(setup["directory"])
runtime_path = directory / "runtime.json"
reddit = directory / "reddit-list.txt"
original = b"# preserve\r\nhttps://reddit.com/r/example/\r\nhttps://reddit.com/user/unrelated/submitted/\r\n"
if not runtime_path.exists():
    for name in ("locks", "state"):
        (directory / name).mkdir()
    reddit.write_bytes(original)
    (directory / "twitter-list.txt").write_bytes(b"# preserve Twitter\n")
    runtime_path.write_text(json.dumps({"version": 1, "root_uuid": setup["root"],
        "locks": str(directory / "locks"), "state": str(directory / "state"),
        "lists": {"reddit": str(reddit), "twitter": str(directory / "twitter-list.txt")},
        "new_source_policy": policy_definition()}))


def run(action, identity, execution, expected=0):
    result = subprocess.run([sys.executable, "-m", "stash_ingest.n8n_sources", "--action", action,
        "--platform", "reddit", "--identity", identity, "--workflow", "source-parent-fixture",
        "--execution", execution, "--node", "11111111-1111-4111-8111-111111111111", "--item", "0",
        "--runtime", str(runtime_path), "--endpoint", setup["endpoint"], "--producer", setup["producer"]],
        capture_output=True, text=True, timeout=45)
    assert result.returncode == expected, (result.returncode, result.stdout, result.stderr)
    assert "fixture-application-key" not in result.stdout + result.stderr
    return json.loads(result.stdout)


if setup["phase"] == "lost_policy_reply":
    result = run("add", "example", "100", expected=1)
    assert result["complete"] is False and result["error"] == "network_unavailable"
    assert reddit.read_bytes() == original
elif setup["phase"] == "resume":
    result = run("add", "example", "100")
    assert result["complete"] and result["added"] and result["source_targets"] == 1
    assert result["scrape_completion"] == "not_checked"
    assert reddit.read_bytes() == original + b"https://reddit.com/user/example/submitted/\r\n"
elif setup["phase"] == "linked_removal":
    result = run("remove-performer", "1", "101")
    assert result["complete"] and result["status"] == "removed" and result["removedCount"] == 1
    assert result["performerName"] == "A different display name"
    assert result["removedUsernames"] == ["example"]
    assert reddit.read_bytes() == original
    assert run("remove-performer", "1", "101") == result
    assert run("add", "example", "100")["complete"]
    assert reddit.read_bytes() == original, "Completed older registration cannot undo the later removal"
else:
    raise AssertionError("Unknown native workflow test phase")
print(json.dumps({"verified": True, "phase": setup["phase"]}))
