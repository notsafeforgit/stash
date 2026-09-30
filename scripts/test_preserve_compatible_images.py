import importlib.util
import hashlib
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("preserve_compatible_images.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)
A = "sha256:" + "a" * 64
B = "sha256:" + "b" * 64
REPO = "ghcr.io/notsafeforgit/stash"


class PreservationTests(unittest.TestCase):
    def test_existing_tag_cannot_be_moved(self):
        with patch.object(release, "digest", return_value=B), patch.object(release, "checked") as command:
            with self.assertRaisesRegex(RuntimeError, "Refusing to move"):
                release.retain(REPO, A, "v2.5-compatible-final")
            command.assert_not_called()

    def test_retry_does_not_republish(self):
        with patch.object(release, "digest", return_value=A), patch.object(release, "checked") as command:
            self.assertEqual(release.retain(REPO, A, "v2.5-compatible-final")["digest"], A)
            command.assert_not_called()

    def test_authentication_failure_is_not_missing_image(self):
        failure = subprocess.CompletedProcess([], 1, b"", b"unauthorized: authentication required")
        with patch.object(release, "run", return_value=failure):
            with self.assertRaisesRegex(RuntimeError, "unauthorized"):
                release.digest(REPO + ":missing", missing_ok=True)

    def test_manifest_unknown_can_be_created(self):
        failure = subprocess.CompletedProcess([], 1, b"", b"reading manifest: manifest unknown")
        with patch.object(release, "run", return_value=failure):
            self.assertIsNone(release.digest(REPO + ":missing", missing_ok=True))

    def test_artifact_digest_uses_exact_manifest_bytes(self):
        body = b'{\n "artifactType": "application/vnd.docker.attestation.manifest.v1+json"\n}\n'
        result = subprocess.CompletedProcess([], 0, body, b"")
        with patch.object(release, "run", return_value=result) as command:
            self.assertEqual(release.digest(REPO + "@" + A), "sha256:" + hashlib.sha256(body).hexdigest())
            self.assertIn("--raw", command.call_args.args[0])

    def test_nested_manifest_children_are_preserved_once(self):
        values = {A: '{"manifests":[{"digest":"' + B + '"},{"digest":"' + B + '"}]}', B: '{}'}
        with patch.object(release, "checked", side_effect=lambda cmd: values[cmd[-1].split("@", 1)[1]]):
            self.assertEqual(release.descendants(REPO, A), [B])

    def test_copy_must_retain_expected_digest(self):
        with patch.object(release, "digest", side_effect=[None, B]), patch.object(release, "checked"):
            with self.assertRaisesRegex(RuntimeError, "Published digest differs"):
                release.retain(REPO, A, "v2.5-compatible-final")


if __name__ == "__main__":
    unittest.main()
