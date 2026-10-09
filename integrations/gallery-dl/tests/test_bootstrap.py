"""Exercise action parsing through a fresh native command process."""

from pathlib import Path
import subprocess
import sys
import unittest


class BootstrapTests(unittest.TestCase):
    def test_lowercase_gallery_actions_and_repeated_invocation(self):
        source = Path(__file__).resolve().parents[1] / "src"
        code = r'''
import logging, sys
from unittest.mock import patch
sys.path.insert(0, sys.argv[1])
import stash_ingest_bootstrap
from gallery_dl import actions, exception

def command(argv):
    parsed = actions.parse_logging({"error:network security": "abort"})
    assert parsed[logging.ERROR] or parsed[-logging.ERROR]
    for group in (parsed[-logging.ERROR], parsed[logging.ERROR]):
        for matches, action in group:
            assert matches("blocked by network security")
            try:
                action({})
            except exception.StopExtraction:
                return 0
    raise AssertionError("The configured abort action was not executed")

with patch("stash_ingest.cli.main", side_effect=command):
    assert stash_ingest_bootstrap.main([]) == 0
    handlers = list(logging.getLogger().handlers)
    assert stash_ingest_bootstrap.main([]) == 0
    assert list(logging.getLogger().handlers) == handlers
print("ok")
'''
        result = subprocess.run([sys.executable, "-I", "-c", code, str(source)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "ok\n")
