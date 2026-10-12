"""Exercise startup dependency policy without network or system modifications."""
from pathlib import Path
import os
import subprocess
import tempfile
import unittest


class PythonRequirementsTest(unittest.TestCase):
    helper = Path(__file__).resolve().parents[1] / "root/opt/python-requirements.sh"

    def run_policy(self, flag=None, contents=None, installer_status=0):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            requirements = root / "requirements.txt"
            if contents is not None:
                requirements.write_text(contents)
            env = dict(os.environ, PYTHON_REQS=str(requirements),
                       UV_TARGET=str(root / "install"), UV_CACHE_DIR=str(root / "cache"),
                       TEST_TRACE=str(root / "trace"), TEST_STATUS=str(installer_status))
            env.pop("INSTALL_PYTHON_REQUIREMENTS", None)
            if flag is not None:
                env["INSTALL_PYTHON_REQUIREMENTS"] = flag
            result = subprocess.run(["bash", "-c", '''
source "$1"
info() { :; }
error() { printf '%s\n' "$*" >&2; }
try_reown_r() { printf 'directory:%s\n' "$1" >> "$TEST_TRACE"; }
runas() {
  printf 'install:%s:%s:%s\n' "$1" "$2" "$3" >> "$TEST_TRACE"
  return "$TEST_STATUS"
}
install_python_deps
''', "test", str(self.helper)], env=env, capture_output=True, text=True)
            trace = (root / "trace").read_text() if (root / "trace").exists() else ""
            self.assertEqual(requirements.read_text() if requirements.exists() else None, contents)
            return result, trace

    def test_default_does_not_install_even_with_old_requirements(self):
        for flag in (None, "false", "FALSE", "0", ""):
            with self.subTest(flag=flag):
                result, trace = self.run_policy(flag, "legacy-scraper-package\n")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(trace, "")

    def test_explicit_install_preserves_pinned_requirements(self):
        for flag in ("true", "TRUE", "1"):
            with self.subTest(flag=flag):
                result, trace = self.run_policy(flag, "requests==2.32.5\n")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("install:/usr/local/bin/uv-pip:requirements:", trace)

    def test_missing_or_empty_explicit_file_fails_without_install(self):
        for contents in (None, ""):
            with self.subTest(contents=contents):
                result, trace = self.run_policy("true", contents)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("explicit nonempty", result.stderr)
                self.assertEqual(trace, "")

    def test_invalid_flag_fails_without_install(self):
        result, trace = self.run_policy("yes", "requests==2.32.5\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(trace, "")

    def test_installer_failure_is_propagated(self):
        result, trace = self.run_policy("true", "requests==2.32.5\n", 17)
        self.assertEqual(result.returncode, 17)
        self.assertIn("install:/usr/local/bin/uv-pip:requirements:", trace)


if __name__ == "__main__":
    unittest.main()
