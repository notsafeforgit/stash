import copy
from pathlib import Path
import subprocess
import tempfile
import unittest

from ci_changes import frontend_path, gate_failures, select_checks


class CheckSelectionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        self.git("init", "-q")
        self.git("config", "user.email", "ci@example.invalid")
        self.git("config", "user.name", "CI test")
        self.git("config", "commit.gpgsign", "false")
        self.base = self.commit("internal/server.go")
        self.runs = [self.run_record(self.base)]

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo).decode().strip()

    def commit(self, path):
        target = self.repo / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text("changed\n")
        self.git("add", "--all")
        self.git("commit", "-qm", "fixture")
        return self.git("rev-parse", "HEAD")

    @staticmethod
    def run_record(sha, **overrides):
        return {"head_sha": sha, "event": "push", "conclusion": "success", "head_branch": "v3-rewrite", **overrides}

    def select(self, event="push", runs=None):
        return select_checks(event, "v3-rewrite", self.git("rev-parse", "HEAD"), self.runs if runs is None else runs, self.repo)

    def test_frontend_changes_skip_backend_only_after_success(self):
        self.commit("ui/v3/src/routes/index.tsx")
        self.assertEqual(self.select()["backend"], "false")
        self.assertEqual(self.select("pull_request")["backend"], "false")
        self.assertEqual(self.select()["baseline"], self.base)
        self.assertEqual(self.select(runs=[])["backend"], "true")

    def test_failed_or_cancelled_backend_commit_is_not_hidden_by_ui_push(self):
        backend = self.commit("pkg/sqlite/migrations/1000108.sql")
        self.commit("ui/v3/src/routes/index.tsx")
        for conclusion in ("failure", "cancelled", "skipped", None):
            with self.subTest(conclusion=conclusion):
                runs = [self.run_record(backend, conclusion=conclusion), *self.runs]
                self.assertEqual(self.select(runs=runs)["backend"], "true")

    def test_multiple_unpublished_ui_commits_can_use_last_success(self):
        self.commit("ui/v3/src/routes/index.tsx")
        self.commit("ui/v3/src/routes/another.tsx")
        self.assertEqual(self.select()["backend"], "false")

    def test_manual_runs_and_no_changes_always_run_all_checks(self):
        self.assertEqual(self.select()["backend"], "true")
        self.commit("ui/v3/src/app.tsx")
        self.assertEqual(self.select("workflow_dispatch")["backend"], "true")

    def test_other_branch_and_preservation_dispatch_are_not_baselines(self):
        self.commit("ui/v3/src/app.tsx")
        for overrides in ({"head_branch": "develop"}, {"event": "workflow_dispatch"}, {"head_sha": "missing"}):
            self.assertEqual(self.select(runs=[self.run_record(self.base, **overrides)])["backend"], "true")

    def test_unrelated_history_and_unavailable_objects_run_full_suite(self):
        self.git("checkout", "-q", "--orphan", "unrelated")
        unrelated = self.commit("unrelated.txt")
        self.git("checkout", "-q", "--detach", self.base)
        self.commit("ui/v3/src/app.tsx")
        for sha in (unrelated, "a" * 40):
            self.assertEqual(self.select(runs=[self.run_record(sha)])["backend"], "true")

    def test_removing_or_renaming_backend_files_cannot_skip_checks(self):
        (self.repo / "internal/server.go").unlink()
        self.commit("ui/v3/src/server.ts")
        self.assertEqual(self.select()["backend"], "true")

    def test_deleted_frontend_file_and_unusual_filename(self):
        self.base = self.commit("ui/v3/src/deleted.ts")
        self.runs = [self.run_record(self.base)]
        (self.repo / "ui/v3/src/deleted.ts").unlink()
        self.commit("ui/v3/src/name with\nnewline.tsx")
        self.assertEqual(self.select()["backend"], "false")

    def test_allowlist_does_not_skip_shared_or_unknown_files(self):
        for path in ("ui/v3/src/app.tsx", "ui/v3/graphql/data.graphql", "ui/login/login.html", "ui/v3/pnpm-lock.yaml"):
            self.assertTrue(frontend_path(path), path)
        for path in ("ui/ui.go", "ui/v3/backend.go", "ui/v3/tool.py", "ui/v3/.npmrc", "graphql/schema/type.graphql", "go.sum", "Makefile", "scripts/ci_changes.py", ".github/workflows/native-ci.yml", "docs/guide.md", "new-component/file.ts"):
            self.assertFalse(frontend_path(path), path)


class PublishGateTest(unittest.TestCase):
    def needs(self, backend):
        result = {job: {"result": "success"} for job in ("changes", "assets", "binary", "backend-tests", "backend-lint", "python")}
        result["changes"]["outputs"] = {"backend": backend}
        if backend == "false":
            for job in ("backend-tests", "backend-lint", "python"):
                result[job]["result"] = "skipped"
        return result

    def test_only_selected_successes_pass(self):
        for backend in ("true", "false"):
            self.assertEqual(gate_failures(self.needs(backend)), [])

    def test_failure_cancellation_or_unexpected_skip_blocks_publish(self):
        for backend in ("true", "false"):
            original = self.needs(backend)
            for job in original:
                for result in ("failure", "cancelled", "skipped", "success"):
                    if original[job]["result"] == result:
                        continue
                    with self.subTest(backend=backend, job=job, result=result):
                        needs = copy.deepcopy(original)
                        needs[job]["result"] = result
                        self.assertTrue(gate_failures(needs))

    def test_missing_selection_or_job_blocks_publish(self):
        for backend in ("", "yes", None):
            self.assertTrue(gate_failures(self.needs(backend)))
        for job in self.needs("true"):
            needs = self.needs("true")
            del needs[job]
            self.assertTrue(gate_failures(needs))


if __name__ == "__main__":
    unittest.main()
