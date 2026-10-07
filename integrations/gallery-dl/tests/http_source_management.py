"""Exercise the real native HTTP contract across separate command processes."""
import json
from pathlib import Path
import subprocess
import sys

setup = json.load(sys.stdin)
directory = Path(setup["directory"])
locks = directory / "source-locks"
locks.mkdir(exist_ok=True)


def run(command, plan, *extra, expected=0):
    result = subprocess.run([sys.executable, "-m", "stash_ingest.source_management", command,
                             "--endpoint", setup["endpoint"], "--producer", setup["producer"],
                             "--plan", str(plan), "--locks", str(locks), *extra], capture_output=True, text=True, timeout=45)
    assert result.returncode == expected, (result.returncode, result.stderr)
    assert "fixture-application-key" not in result.stdout + result.stderr
    return json.loads(result.stdout if result.returncode in (0, 2, 3) else result.stderr)


def prepare(operation):
    value = {"operation": operation, "root_uuid": setup["root"], "reason": "Source management HTTP fixture", "targets": [
        {"label": "New source label", "kind": "account", "namespace": "native:reddit", "state": "active",
         "target_url": target, "root_uuid": setup["root"], "path_prefix": ".", "account_uuid": None}
        for target in setup["targets"]]}
    source, plan = directory / (operation + "-input.json"), directory / (operation + "-plan.json")
    source.write_text(json.dumps(value))
    result = run("prepare", plan, "--input", str(source))
    assert result["applied"] is False
    (directory / (operation + "-sha256.txt")).write_text(result["plan_sha256"])
    return plan, result["plan_sha256"]


if setup["phase"] == "create_lost_reply":
    plan, sha = prepare("ensure")
    before = plan.read_bytes()
    result = run("apply", plan, "--expected-sha256", sha, expected=1)
    assert result["complete"] is False and result["error"] == "network_unavailable"
    assert plan.read_bytes() == before
elif setup["phase"] == "resume_and_disable":
    plan = directory / "ensure-plan.json"
    sha = (directory / "ensure-sha256.txt").read_text()
    assert run("status", plan, "--expected-sha256", sha)["complete"]
    result = run("apply", plan, "--expected-sha256", sha)
    assert result["states"] == ["unchanged", "completed"]
    assert result["scrape_completion"] == "not_checked"
    plan, sha = prepare("disable")
    result = run("apply", plan, "--expected-sha256", sha)
    assert result["complete"] and result["states"] == ["completed", "completed"]
elif setup["phase"] == "preserve_later_edit":
    for operation in ("ensure", "disable"):
        result = run("apply", directory / (operation + "-plan.json"), "--expected-sha256",
                     (directory / (operation + "-sha256.txt")).read_text(), expected=2)
        assert result["needs_review"] and not result["complete"]
else:
    raise AssertionError("Unknown source-management test phase")
print(json.dumps({"phase": setup["phase"], "verified": True}))
