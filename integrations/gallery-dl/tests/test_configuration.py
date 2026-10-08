import copy
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch

from gallery_dl import config

from stash_ingest.cli import main
from stash_ingest.configuration import Configuration, SCHEMA
from stash_ingest.encoding import InvalidData, digest
from stash_ingest.filesystem import Root
from helpers import ROOT, PRODUCER


def profile_fixture(directory):
    directory = Path(directory)
    directory.mkdir(exist_ok=True)
    media, locks = directory / "media", directory / "locks"
    media.mkdir(exist_ok=True)
    locks.mkdir(exist_ok=True)
    asset = directory / "converter.py"
    asset.write_text("def prepare(data):\n    return None\n")
    private = directory / "website.json"
    private.write_text(json.dumps({"extractor": {"twitter": {"cookies": {"auth_token": "private-first-value"}}}}))
    value = {"schema": SCHEMA, "root": {"uuid": ROOT, "path": str(media), "identity": list(Root.probe(media))},
             "locks": {"path": str(locks), "identity": list(Root.probe(locks))},
             "bindings": {"archive": {"kind": "path", "path": str(directory / "archive.sqlite")},
                          "converter": {"kind": "asset", "path": str(asset), "sha256": digest(asset.read_bytes())},
                          "login": {"kind": "private", "file": str(private), "pointer": "/extractor/twitter/cookies"}},
             "gallery": {"extractor": {"base-directory": "${stash:media_root}", "directory": ["Account"],
                          "filename": "{id}_{filename}.{extension}", "archive": "${stash:archive}",
                          "skip": True, "sleep": 0, "sleep-request": 0, "sleep-extractor": 0,
                          "twitter": {"cookies": "${stash:login}"},
                          "postprocessors": ["reviewed-converter"]},
                          "postprocessor": {"reviewed-converter": {"name": "python", "event": "prepare",
                                               "function": "${stash:converter}:prepare"}}}}
    path = directory / "worker.json"
    path.write_text(json.dumps(value))
    return path, value


class ConfigurationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.path, self.value = profile_fixture(self.directory)
        config.clear()
        self.addCleanup(config.clear)

    def write(self):
        self.path.write_text(json.dumps(self.value))

    def test_reviewed_source_mode_has_a_distinct_policy(self):
        original = Configuration(self.path)
        self.assertEqual(original.source_mode, 'published')
        self.value['source_mode'] = 'published'
        self.write()
        self.assertEqual(original.policy_sha256, Configuration(self.path).policy_sha256)
        self.value['source_mode'] = 'traversal'
        self.write()
        self.assertNotEqual(original.policy_sha256, Configuration(self.path).policy_sha256)
        for mode in ('unknown', None, [], True):
            self.value['source_mode'] = mode
            self.write()
            with self.subTest(mode=mode), self.assertRaises(InvalidData):
                Configuration(self.path)

    def test_equivalent_host_container_paths_and_rotated_access_keep_one_policy(self):
        host = Configuration(self.path)
        container_path, _ = profile_fixture(self.directory / "container")
        container = Configuration(container_path)
        self.assertEqual(host.policy_sha256, container.policy_sha256)
        private = self.directory / "website.json"
        private.write_text(json.dumps({"extractor": {"twitter": {"cookies": {"auth_token": "rotated-secret"}}}}))
        rotated = Configuration(self.path)
        self.assertEqual(host.policy_sha256, rotated.policy_sha256)
        config.set(("extractor",), "skip", "abort:4")
        with rotated.activate():
            self.assertTrue(config.interpolate(("extractor",), "skip"))
            self.assertEqual(config.interpolate(("extractor", "twitter"), "cookies"), {"auth_token": "rotated-secret"})
            self.assertEqual(config.interpolate(("extractor",), "base-directory"), str(host.root.path))
            config.set(("extractor",), "skip", False)
        self.assertEqual(config.interpolate(("extractor",), "skip"), "abort:4")
        with rotated.activate():
            self.assertIs(config.interpolate(("extractor",), "skip"), True)
            with self.assertRaises(InvalidData):
                with host.activate():
                    self.fail("Two workers shared gallery-dl's global configuration")

    def test_changed_behavior_or_reviewed_asset_changes_policy_and_unreviewed_asset_stops(self):
        original = Configuration(self.path)
        self.value["gallery"]["extractor"]["skip"] = "abort:4"
        self.write()
        self.assertNotEqual(original.policy_sha256, Configuration(self.path).policy_sha256)
        asset = self.directory / "converter.py"
        asset.write_text("def prepare(data):\n    return 'changed'\n")
        with self.assertRaises(InvalidData):
            original.check()
        with self.assertRaises(InvalidData):
            Configuration(self.path)
        self.value["gallery"]["extractor"]["skip"] = True
        self.value["bindings"]["converter"]["sha256"] = digest(asset.read_bytes())
        self.write()
        self.assertNotEqual(original.policy_sha256, Configuration(self.path).policy_sha256)

    def test_mount_or_lock_replacement_is_not_silently_accepted(self):
        profile = Configuration(self.path)
        for name in ("media", "locks"):
            with self.subTest(directory=name):
                path = self.directory / name
                path.rename(self.directory / (name + "-saved"))
                path.mkdir()
                with self.assertRaises(InvalidData):
                    profile.check()
                with self.assertRaises(InvalidData):
                    Configuration(self.path)
                path.rmdir()
                (self.directory / (name + "-saved")).rename(path)

    def test_inline_access_values_and_private_interpolation_are_rejected_without_echoing_them(self):
        original = copy.deepcopy(self.value)
        changes = [lambda: self.value["gallery"]["extractor"]["twitter"].update(password="hidden-password"),
                   lambda: self.value["gallery"]["extractor"].update(filename="${stash:login}"),
                   lambda: self.value["gallery"]["extractor"]["twitter"].update(cookies="Bearer ${stash:login}"),
                   lambda: self.value["gallery"]["extractor"].update(filename="${stash:missing}"),
                   lambda: self.value["gallery"]["extractor"].update(filename="${stash:not-valid}"),
                   lambda: self.value["gallery"]["postprocessor"]["reviewed-converter"].update(function="${stash:converter}.bak:run"),
                   lambda: self.value["gallery"]["postprocessor"]["reviewed-converter"].update(function="/unreviewed/script.py:run")]
        for change in changes:
            self.value = copy.deepcopy(original)
            change()
            self.write()
            with self.assertRaises(InvalidData) as caught:
                Configuration(self.path)
            self.assertNotIn("hidden-password", str(caught.exception))
            self.assertNotIn("private-first-value", str(caught.exception))

    def test_environment_and_json_pointer_private_references_preserve_structured_values(self):
        self.value["bindings"]["login"] = {"kind": "private", "env": "WORKER_FIXTURE_ACCESS"}
        self.write()
        with patch.dict(os.environ, {}, clear=True), self.assertRaises(InvalidData):
            Configuration(self.path)
        with patch.dict(os.environ, {"WORKER_FIXTURE_ACCESS": "first"}):
            first = Configuration(self.path)
        with patch.dict(os.environ, {"WORKER_FIXTURE_ACCESS": "${stash:literal-not-a-reference}"}):
            second = Configuration(self.path)
            self.assertEqual(first.policy_sha256, second.policy_sha256)
            with second.activate():
                self.assertEqual(config.interpolate(("extractor", "twitter"), "cookies"), "${stash:literal-not-a-reference}")
        private = self.directory / "website.json"
        private.write_text(json.dumps({"a/b": {"~key": [{"auth_token": "nested"}]}}))
        self.value["bindings"]["login"] = {"kind": "private", "file": str(private), "pointer": "/a~1b/~0key/0"}
        self.write()
        with Configuration(self.path).activate():
            self.assertEqual(config.interpolate(("extractor", "twitter"), "cookies"), {"auth_token": "nested"})

    def test_legacy_catalog_writer_is_rejected_even_when_aliased_as_an_asset(self):
        asset = self.directory / "gallery_catalog_hook.py"
        asset.write_text("def prepare(data):\n    return None\n")
        self.value["bindings"]["converter"]["path"] = str(asset)
        self.write()
        with self.assertRaisesRegex(InvalidData, "legacy catalog"):
            Configuration(self.path)

    def test_cli_reports_only_local_policy_identity(self):
        output = io.StringIO()
        args = ["--endpoint", "http://fixture.invalid", "--producer", PRODUCER, "--outbox", str(self.directory / "outbox.sqlite"),
                "worker-policy", "--profile", str(self.path)]
        with redirect_stdout(output):
            self.assertEqual(main(args), 0)
        value = json.loads(output.getvalue())
        self.assertEqual(value["policy_sha256"], Configuration(self.path).policy_sha256)
        self.assertNotIn("private-first-value", output.getvalue())
        self.assertNotIn(str(self.directory), output.getvalue())


if __name__ == "__main__":
    unittest.main()
