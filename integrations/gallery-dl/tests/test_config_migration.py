"""Effective configuration migration, including real path/archive semantics."""

from contextlib import closing, redirect_stderr, redirect_stdout
import copy
import io
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

from gallery_dl import config, util
from gallery_dl.path import PathFormat

from stash_ingest.config_migration import Converter, main, publish_profile
from stash_ingest.configuration import Configuration, profile_bytes
from stash_ingest.encoding import InvalidData, decode
from stash_ingest.filesystem import Root
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.outbox import Outbox
from stash_ingest.producer import Producer
from helpers import ROOT, PRODUCER
from test_gallery import Fixture
from test_producer import LeaseFixture, reddit_data


def migration_fixture(directory):
    directory = Path(directory)
    directory.mkdir(exist_ok=True)
    media, locks, archives = (directory / name for name in ("media", "locks", "archives"))
    for path in (media, locks, archives):
        path.mkdir()
    helper = directory / "migration_helper.py"
    helper.write_text("def prepare(data):\n    data['helper_ran'] = True\n")
    root = {"uuid": ROOT, "path": str(media), "identity": list(Root.probe(media))}
    lock = {"path": str(locks), "identity": list(Root.probe(locks))}
    archive = [str(archives), "{category}.sqlite3"]
    value = {"extractor": {"base-directory": str(media), "directory": ["Account"],
             "filename": {"count > 1": "album_{id}_{filename}.{extension}",
                          "count > 0": "{id}_{filename}.{extension}", "": "fallback.{extension}"},
             "archive": archive, "archive-event": "after", "skip": "abort:4",
             "sleep": 0, "sleep-request": 0, "sleep-extractor": 0,
             "headers": {"X-Base": "base-private", "X-Shared": "old-private"},
             "reddit": {"cookies": "base-cookie-private", "parent-metadata": "_reddit", "postprocessors": ["helper"]},
             "coomer": {"original": True}, "kemono": {"original": True},
             "postprocessors": ["old-prepare", "old-complete"]},
             "postprocessor": {
                "old-prepare": {"name": "python", "function": "/old/gallery_catalog_hook.py:prepare", "event": "prepare"},
                "old-complete": {"name": "python", "function": "/old/gallery_catalog_hook.py:complete", "event": ["after", "skip"]},
                "helper": {"name": "python", "event": "prepare", "function": str(helper) + ":prepare"},
                "convert": {"name": "exec", "event": "after", "command": ["/usr/bin/python3", str(helper), "{_path}"], "async": False}}}
    path = directory / "gallery.json"
    path.write_text(json.dumps(value))
    return path, value, root, lock


class ConfigMigrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.path, self.value, self.root, self.locks = migration_fixture(self.directory)
        config.clear()
        self.addCleanup(config.clear)

    def converter(self, *extra):
        return Converter([self.path, *extra], self.root, self.locks, working_directory=self.directory)

    def profile(self, converter=None):
        value = (converter or self.converter()).convert()
        return value, Configuration.from_document(value, self.directory)

    def test_merged_private_objects_and_array_replacement_match_gallery_dl(self):
        override = {"extractor": {"headers": {"X-Shared": "replacement-private", "X-Added": "added-private"},
                    "reddit": {"cookies": "override-private", "postprocessors": []}}}
        path = self.directory / "override.json"
        path.write_text(json.dumps(override))
        expected = util.combine_dict(copy.deepcopy(self.value), copy.deepcopy(override))
        converter = self.converter(path)
        value, profile = self.profile(converter)
        self.assertEqual(converter.settings.plain(), expected)
        self.assertEqual(value["gallery"]["extractor"]["postprocessors"], [])
        self.assertEqual(value["gallery"]["extractor"]["reddit"]["postprocessors"], [])
        self.assertEqual(set(value["gallery"]["postprocessor"]), {"helper", "convert"})
        self.assertEqual(len(converter.report()["removed_catalog_processors"]), 4)
        raw = profile_bytes(value)
        for secret in ("base-private", "old-private", "replacement-private", "added-private", "override-private"):
            self.assertNotIn(secret.encode(), raw)
        with profile.activate():
            self.assertEqual(config.get(("extractor",), "headers"), expected["extractor"]["headers"])
            self.assertEqual(config.get(("extractor", "reddit"), "cookies"), "override-private")
            self.assertEqual(config.get(("extractor", "reddit"), "parent-metadata"), "_reddit")
            self.assertEqual(config.get(("extractor",), "skip"), "abort:4")
            for site in ("coomer", "kemono"):
                self.assertIs(config.get(("extractor", site), "original"), True)

    def test_ancestor_replacement_does_not_resurrect_old_private_keys(self):
        paths = []
        for index, value in enumerate(({"extractor": None}, {"extractor": {"base-directory": self.root["path"],
                                      "headers": {"X-Only": "new-private"}}})):
            path = self.directory / f"layer-{index}.json"
            path.write_text(json.dumps(value))
            paths.append(path)
        _, profile = self.profile(self.converter(*paths))
        with profile.activate():
            self.assertEqual(config.get(("extractor",), "headers"), {"X-Only": "new-private"})

    def test_host_and_container_bindings_share_policy_but_changed_behavior_does_not(self):
        host_value, host = self.profile()
        other, value, root, locks = migration_fixture(self.directory / "container")
        value["extractor"]["archive"] = [str(other.parent), "archives", "{category}.sqlite3"]
        value["extractor"]["reddit"]["cookies"] = "container-private"
        other.write_text(json.dumps(value))
        converter = Converter([other], root, locks)
        container_value = converter.convert()
        container = Configuration.from_document(container_value, other.parent)
        self.assertEqual(host_value["gallery"], container_value["gallery"])
        self.assertEqual(host.policy_sha256, container.policy_sha256)
        value["extractor"]["coomer"]["original"] = False
        other.write_text(json.dumps(value))
        changed = Configuration.from_document(Converter([other], root, locks).convert(), other.parent)
        self.assertNotEqual(changed.policy_sha256, host.policy_sha256)

    def test_scoped_policy_retains_reddit_children_and_ignores_unrelated_thisvid_actions(self):
        self.value["extractor"]["reddit"]["whitelist"] = ["imgur", "redgifs", "directlink"]
        self.value["extractor"]["redgifs"] = {"timeout": 42}
        self.value["extractor"]["reddit>redgifs"] = {"directory": [], "skip": "abort:4"}
        self.value["extractor"]["ytdl"] = {"actions": {"error:private": ["exit 75"]}}
        self.path.write_text(json.dumps(self.value))
        original = Converter([self.path], self.root, self.locks, category="reddit").convert()
        self.assertEqual(original["source_category"], "reddit")
        self.assertEqual(original["gallery"]["extractor"]["redgifs"]["timeout"], 42)
        self.assertEqual(original["gallery"]["extractor"]["reddit>redgifs"]["skip"], "abort:4")
        self.assertNotIn("ytdl", original["gallery"]["extractor"])
        self.assertEqual(set(original["gallery"]["postprocessor"]), {"helper"})
        self.value["extractor"]["ytdl"]["actions"] = {"error:private": ["print refresh elsewhere", "exit 75"]}
        self.path.write_text(json.dumps(self.value))
        changed = Converter([self.path], self.root, self.locks, category="reddit").convert()
        self.assertEqual(Configuration.from_document(original, self.directory).policy_sha256,
                         Configuration.from_document(changed, self.directory).policy_sha256)
        # An unknown child graph must retain its settings rather than guessing
        # which dependencies can safely be removed.
        del self.value["extractor"]["reddit"]["whitelist"]
        self.path.write_text(json.dumps(self.value))
        broad = Converter([self.path], self.root, self.locks, category="reddit").convert()
        self.assertIn("ytdl", broad["gallery"]["extractor"])

    def test_credential_arguments_are_referenced_without_hiding_format_selection(self):
        args = ["--cookies", "/private/cookies.txt", "--password", "hidden-password", "--format", "bv+ba"]
        self.value["extractor"]["ytdl"] = {"cmdline-args": args}
        self.path.write_text(json.dumps(self.value))
        value, profile = self.profile()
        self.assertNotIn(b"hidden-password", profile_bytes(value))
        with profile.activate():
            self.assertEqual(config.get(("extractor", "ytdl"), "cmdline-args"), args)
        changed = copy.deepcopy(value)
        changed["gallery"]["extractor"]["ytdl"]["cmdline-args"][-1] = "worst"
        self.assertNotEqual(profile.policy_sha256, Configuration.from_document(changed, self.directory).policy_sha256)
        changed["gallery"]["extractor"]["ytdl"]["cmdline-args"][3] = "inline-password"
        with self.assertRaises(InvalidData):
            Configuration.from_document(changed, self.directory)
        structured = copy.deepcopy(value)
        reference = structured["gallery"]["extractor"]["ytdl"]["cmdline-args"][1]
        structured["bindings"][reference[len("${stash:"):-1]]["pointer"] = "/extractor/headers"
        with self.assertRaisesRegex(InvalidData, "arguments must be strings"):
            Configuration.from_document(structured, self.directory)
        self.value["extractor"]["ytdl"]["cmdline-args"] = ["--password=hidden-password"]
        self.path.write_text(json.dumps(self.value))
        with self.assertRaises(InvalidData) as failure:
            self.converter().convert()
        self.assertNotIn("hidden-password", str(failure.exception))

    def test_instagram_profile_keeps_child_options_without_unrelated_private_bindings(self):
        self.value['extractor']['instagram'] = {'include': 'stories,highlights,posts', 'videos': True,
                                               'cookies': 'instagram-private', 'stories': {'skip': True}}
        self.value['extractor']['instagram>instagram'] = {'sleep': 12}
        self.path.write_text(json.dumps(self.value))
        converter = Converter([self.path], self.root, self.locks, category='instagram')
        value = converter.convert()
        profile = Configuration.from_document(value, self.directory)
        for site in ('reddit', 'coomer', 'kemono'):
            self.assertNotIn(site, value['gallery']['extractor'])
        self.assertEqual(value['gallery']['postprocessor'], {})
        self.assertNotIn(b'instagram-private', profile_bytes(value))
        with profile.activate():
            self.assertEqual(config.get(('extractor', 'instagram'), 'cookies'), 'instagram-private')
            self.assertEqual(config.get(('extractor', 'instagram'), 'include'), 'stories,highlights,posts')
            self.assertTrue(config.get(('extractor', 'instagram', 'stories'), 'skip'))
            self.assertEqual(config.get(('extractor', 'instagram>instagram'), 'sleep'), 12)

    def test_mirror_profiles_keep_originals_and_their_helpers_without_other_services(self):
        for category, other in (('coomer', 'kemono'), ('kemono', 'coomer')):
            self.value['extractor'][category]['postprocessors'] = ['helper']
            self.path.write_text(json.dumps(self.value))
            value = Converter([self.path], self.root, self.locks, category=category).convert()
            self.assertNotIn(other, value['gallery']['extractor'])
            self.assertNotIn('reddit', value['gallery']['extractor'])
            self.assertEqual(set(value['gallery']['postprocessor']), {'helper'})
            with Configuration.from_document(value, self.directory).activate():
                self.assertIs(config.interpolate(('extractor', category), 'original'), True)
                self.assertEqual(config.interpolate(('extractor', category), 'postprocessors'), ['helper'])

    def test_social_profiles_preserve_explicit_collection_and_media_choices(self):
        for category in ('bluesky', 'tiktok'):
            options = {'audio': False, 'videos': True, 'include': ['posts', 'stories'] if category == 'tiktok' else ['media'],
                       'postprocessors': ['helper']}
            self.value['extractor'][category] = options
            self.path.write_text(json.dumps(self.value))
            value = Converter([self.path], self.root, self.locks, category=category).convert()
            self.assertNotIn('reddit', value['gallery']['extractor'])
            self.assertNotIn('coomer', value['gallery']['extractor'])
            with Configuration.from_document(value, self.directory).activate():
                for key in ('audio', 'videos', 'include', 'postprocessors'):
                    self.assertEqual(config.interpolate(('extractor', category), key), options[key])

    def test_conditional_order_survives_writing_and_changes_policy_and_filename(self):
        value, original = self.profile()
        changed = copy.deepcopy(value)
        rules = changed["gallery"]["extractor"]["filename"]
        rules["count > 1"] = rules.pop("count > 1")
        reordered = Configuration.from_document(changed, self.directory)
        self.assertNotEqual(reordered.policy_sha256, original.policy_sha256)
        target = self.directory / "native.json"
        publish_profile(target, value)
        restored = Configuration(target)
        self.assertEqual(restored.policy_sha256, original.policy_sha256)
        filenames = []
        for profile in (restored, reordered):
            with profile.activate():
                extractor = Fixture.from_url("https://fixture.invalid/account")
                formatter = PathFormat(extractor)
                filenames.append(formatter.build_filename(reddit_data(count=2)))
        self.assertEqual(filenames, ["album_postabc123_abc123.jpg", "postabc123_abc123.jpg"])
        self.assertEqual(target.stat().st_mode & 0o777, 0o600)

    def test_archive_format_and_existing_download_ids_survive_conversion(self):
        value, profile = self.profile()
        media = Path(self.root["path"])
        (media / "Account").mkdir()
        (media / "Account/postabc123_abc123.jpg").write_bytes(b"existing completed file")
        archive = self.directory / "archives/reddit.sqlite3"
        with closing(sqlite3.connect(archive)) as database, database:
            database.execute("CREATE TABLE archive(entry TEXT PRIMARY KEY) WITHOUT ROWID")
            database.execute("INSERT INTO archive VALUES ('redditpostabc123_abc123')")
        lease = LeaseFixture()
        lease.run["path_prefix"] = "Account"
        with closing(Outbox(self.directory / "outbox.sqlite", lease.client.endpoint, PRODUCER)) as box:
            with profile.activate():
                extractor = Fixture.from_url(lease.run["target_url"])
                extractor.records, extractor.visited = [reddit_data(count=1)], []
                producer = Producer(box, lease, profile.root, extractor_version="fixture", configuration_check=profile.check)
                task = NativeDownloadJob(extractor, producer=producer, lock_directory=profile.locks.path)
                task.download = lambda _: self.fail("Existing archive row caused a download")
                self.assertEqual(task.run(), 0)
            events = [decode(row[0]) for row in box.db.execute("SELECT body FROM events ORDER BY seq")]
            self.assertEqual([event["kind"] for event in events], ["source.capture", "file.completed"])
            self.assertEqual(events[1]["relative_path"], "Account/postabc123_abc123.jpg")
        with closing(sqlite3.connect(archive)) as database:
            self.assertEqual(database.execute("SELECT entry FROM archive").fetchall(), [("redditpostabc123_abc123",)])

    def test_inline_and_named_type_legacy_hooks_are_removed_but_unknown_hooks_stop(self):
        self.value["extractor"]["postprocessors"] = [
            {"type": "old-prepare", "event": "prepare"},
            {"name": "python", "function": "/other/gallery_catalog_hook.py:complete", "event": "after"},
            "helper"]
        self.path.write_text(json.dumps(self.value))
        value, _ = self.profile()
        self.assertEqual(value["gallery"]["extractor"]["postprocessors"], ["helper"])
        self.value["postprocessor"]["old-prepare"]["function"] = "/old/gallery_catalog_hook.py:custom_write"
        self.path.write_text(json.dumps(self.value))
        with self.assertRaisesRegex(InvalidData, "unknown legacy"):
            self.converter().convert()

    def test_cli_publishes_once_and_keeps_live_sources_and_private_values_out_of_output(self):
        original = self.path.read_bytes()
        destination = self.directory / "native-worker.json"
        args = ["--config", str(self.path), "--root", ROOT, "--root-path", self.root["path"], "--root-identity",
                *map(str, self.root["identity"]), "--locks", self.locks["path"], "--lock-identity",
                *map(str, self.locks["identity"]), "--output", str(destination)]
        output, errors = io.StringIO(), io.StringIO()
        with redirect_stdout(output), redirect_stderr(errors):
            self.assertEqual(main(args), 0, errors.getvalue())
        report = json.loads(output.getvalue())
        self.assertEqual(report["state"], "converted")
        self.assertEqual(report["policy_sha256"], Configuration(destination).policy_sha256)
        saved = destination.read_bytes()
        with redirect_stdout(output), redirect_stderr(errors):
            self.assertEqual(main(args), 1)
        self.assertEqual(destination.read_bytes(), saved)
        self.assertEqual(self.path.read_bytes(), original)
        self.assertNotIn("base-private", output.getvalue() + errors.getvalue())
        self.assertEqual(list(self.directory.glob(".native-worker-*")), [])

    def test_replaced_mount_and_unreviewed_path_forms_are_not_converted(self):
        for change in ({"archive": "postgresql://hidden-password@database/archive"},
                       {"base-directory": str(self.directory / "outside")},
                       {"archive": ["/tmp", "..", "{category}.sqlite3"]}):
            with self.subTest(change=change):
                value = copy.deepcopy(self.value)
                value["extractor"].update(change)
                self.path.write_text(json.dumps(value))
                with self.assertRaises(InvalidData) as failure:
                    self.converter().convert()
                self.assertNotIn("hidden-password", str(failure.exception))
        self.path.write_text(json.dumps(self.value))
        bad_root = {**self.root, "identity": [0, 0]}
        with self.assertRaises(InvalidData):
            Converter([self.path], bad_root, self.locks)

    def test_full_history_cli_changes_global_skip_without_rewriting_inputs_or_private_values(self):
        self.value["extractor"]["reddit"]["skip"] = "abort:4"
        self.value["extractor"]["reddit>redgifs"] = {"skip": "abort:4"}
        self.path.write_text(json.dumps(self.value))
        original = self.path.read_bytes()
        _, normal = self.profile()
        destination = self.directory / "full-history.json"
        args = ["--config", str(self.path), "--root", ROOT, "--root-path", self.root["path"], "--root-identity",
                *map(str, self.root["identity"]), "--locks", self.locks["path"], "--lock-identity",
                *map(str, self.locks["identity"]), "--full-history", "--output", str(destination)]
        output, errors = io.StringIO(), io.StringIO()
        with redirect_stdout(output), redirect_stderr(errors):
            self.assertEqual(main(args), 0, errors.getvalue())
        full = Configuration(destination)
        self.assertNotEqual(full.policy_sha256, normal.policy_sha256)
        self.assertEqual(json.loads(output.getvalue())["policy_sha256"], full.policy_sha256)
        self.assertEqual(self.path.read_bytes(), original)
        self.assertNotIn(b"base-private", destination.read_bytes())
        self.assertNotIn("base-private", output.getvalue() + errors.getvalue())
        with full.activate():
            for category in ("reddit", "reddit>redgifs"):
                self.assertIs(config.interpolate(("extractor", category), "skip"), True)
                self.assertEqual(config.get(("extractor", category), "skip"), "abort:4")
            for category in ("coomer", "kemono"):
                self.assertIs(config.get(("extractor", category), "original"), True)
            self.assertEqual(config.get(("extractor",), "headers")["X-Base"], "base-private")


if __name__ == "__main__":
    unittest.main()
