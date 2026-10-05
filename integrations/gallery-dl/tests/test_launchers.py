from contextlib import closing, redirect_stderr, redirect_stdout
from datetime import datetime, timezone
import io
import json
import os
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch
import uuid

from gallery_dl import config

from stash_ingest.configuration import Configuration
from stash_ingest.encoding import InvalidData
from stash_ingest.launcher_inputs import date_min, instagram_list, instagram_target, mirror_list, mirror_target, reddit_list, reddit_name, reddit_urls, twitter_list, twitter_target
from stash_ingest.launchers import caller_uuid, coomer_main, instagram_main, kemono_main, reddit_main, twitter_main
from stash_ingest.outbox import Outbox
from stash_ingest.source_calls import SourceCalls
from helpers import PRODUCER
from test_configuration import profile_fixture
from test_gallery import Fixture


class LauncherTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.profile, self.value = profile_fixture(self.directory)
        self.value["source_category"] = "twitter"
        self.value["gallery"]["extractor"]["twitter"]["skip"] = "abort:4"
        self.profile.write_text(json.dumps(self.value))
        self.full = self.directory / "full.json"
        self.full.write_text(json.dumps({**self.value, "gallery": {**self.value["gallery"], "skip": True}}))
        self.outbox = self.directory / "producer.sqlite"
        self.call = str(uuid.uuid4())
        self.base = ["--outbox", str(self.outbox), "--endpoint", "http://fixture.invalid", "--producer", PRODUCER,
                     "--call", self.call, "--until", "2026-10-01T12:00:00Z"]
        config.clear()
        self.addCleanup(config.clear)

    def invoke(self, main, args):
        output, errors = io.StringIO(), io.StringIO()
        with redirect_stdout(output), redirect_stderr(errors):
            status = main(args)
        return status, json.loads(output.getvalue()) if output.getvalue() else None, errors.getvalue()

    def test_twitter_handles_ids_and_profile_urls_keep_list_order_and_report_ignored_lines(self):
        source = self.directory / "twitter.conf"
        source.write_text("@Example\n123\nhttps://twitter.com/Example # duplicate\nnot/valid\nhttps://x.com/i/user/456\n")
        urls, ignored = twitter_list(source)
        self.assertEqual(urls, ["https://x.com/Example", "https://x.com/i/user/123", "https://x.com/i/user/456"])
        self.assertEqual(ignored, [4])
        self.assertEqual(twitter_target("123", "user"), "https://x.com/123")
        self.assertEqual(twitter_target("https://twitter.com/i/user/456", "id"), urls[-1])
        with self.assertRaises(InvalidData):
            twitter_target("https://example.invalid/someone")

    def test_reddit_list_and_new_top_saved_expansion_preserve_exact_query_order(self):
        source = self.directory / "reddit.conf"
        source.write_text("https://www.reddit.com/user/zeta/\nhttps://reddit.com/u/Alpha/\nhttps://reddit.com/r/Pictures/new/\n"
                          "https://reddit.com/r/Pictures/comments/abc/post/\nhttps://reddit.com/user/me/\nhttps://reddit.com/u/Alpha/\n")
        users, subs, ignored = reddit_list(source)
        self.assertEqual((users, subs, ignored), (["Alpha", "zeta"], ["Pictures"], [4]))
        expected_new = ["https://www.reddit.com/user/Alpha/submitted/?sort=new",
                        "https://www.reddit.com/search?q=author%3AAlpha+nsfw%3Ayes&include_over_18=on&sort=new&t=all",
                        "https://www.reddit.com/r/Pictures/?sort=new", "https://www.reddit.com/user/me/saved/?sort=new"]
        self.assertEqual(reddit_urls(["Alpha"], subs, saved=True), expected_new)
        self.assertEqual(reddit_urls(["Alpha"], [], mode="top"), [
            "https://www.reddit.com/user/Alpha/submitted/?sort=top&t=all",
            "https://www.reddit.com/search?q=author%3AAlpha+nsfw%3Ayes&include_over_18=on&sort=top&t=all",
            "https://www.reddit.com/user/Alpha/submitted/?sort=top&t=year",
            "https://www.reddit.com/search?q=author%3AAlpha+nsfw%3Ayes&include_over_18=on&sort=top&t=year"])
        self.assertEqual(len(reddit_urls(users, subs, mode="top", saved=True)), 12)
        with self.assertRaises(InvalidData):
            reddit_name("me")

    def test_instagram_list_preserves_explicit_extractors_and_fails_on_invalid_lines(self):
        source = self.directory / 'instagram.conf'
        urls = ['https://www.instagram.com/example/', 'https://www.instagram.com/p/Example/?img_index=2',
                'https://www.instagram.com/stories/highlights/123/']
        source.write_text('# profiles and selected posts\n@example\n' + '\n'.join(urls) + '\n')
        self.assertEqual(instagram_list(source), (urls, []))
        for invalid in ('..', '.', 'https://www.instagram.com/../example/', 'https://x.com/example',
                        'https://private@www.instagram.com/example/', 'https://www.instagram.com:443/example/',
                        'https://www.instagram.com/example/ # ignored?'):
            with self.subTest(value=invalid), self.assertRaises(InvalidData):
                instagram_target(invalid)
        source.write_text(urls[0] + '\nhttps://x.com/example\n')
        with self.assertRaisesRegex(InvalidData, 'line 2'):
            instagram_list(source)

    def test_instagram_dry_run_and_durable_request_replay_do_not_claim_source_completion(self):
        source = self.directory / 'instagram.conf'
        source.write_text('https://www.instagram.com/example/\n')
        status, preview, errors = self.invoke(instagram_main, ['--config-file', str(source), '--dry-run',
                                                              '--outbox', str(self.outbox)])
        self.assertEqual((status, errors), (0, ''))
        self.assertEqual(preview['targets'], ['https://www.instagram.com/example/'])
        self.assertFalse(self.outbox.exists())
        args = [*self.base, '--config-file', str(source), '--profile', str(self.profile)]
        status, _, error = self.invoke(instagram_main, args)
        self.assertEqual(status, 1)
        self.assertIn('source category', error)
        self.value['source_category'] = 'instagram'
        self.profile.write_text(json.dumps(self.value))
        status, original, errors = self.invoke(instagram_main, args)
        self.assertEqual((status, errors), (0, ''))
        self.assertEqual(original['state'], 'recorded')
        source.unlink()
        self.profile.unlink()
        status, replay, errors = self.invoke(instagram_main, args)
        self.assertEqual((status, replay, errors), (0, original, ''))
        status, unfinished, _ = self.invoke(instagram_main, [*args, '--strict-errors'])
        self.assertEqual((status, unfinished['state']), (2, 'pending'))
        with closing(Outbox(self.outbox, 'http://fixture.invalid', PRODUCER)) as box:
            self.assertEqual(SourceCalls(box).summary(self.call)['target_count'], 1)

    def test_mirror_targets_preserve_service_ids_and_reject_other_collection_kinds(self):
        for category in ('coomer', 'kemono'):
            account = f'https://{category}.su/onlyfans/user/123'
            detail = f'https://{category}.st/patreon/user/456/post/789'
            listing = f'https://{category}.cr/posts?q=example'
            source = self.directory / (category + '.conf')
            source.write_text('# selected sources\n' + '\n'.join([account, detail, account, listing]))
            self.assertEqual(mirror_list(source, category), ([account, detail, listing], []))
            for invalid in ('https://example.invalid/onlyfans/user/123', f'https://{category}.st/discord/server/1/2',
                            f'https://{category}.st/favorites', f'https://private@{category}.st/onlyfans/user/123'):
                with self.subTest(url=invalid), self.assertRaises(InvalidData):
                    mirror_target(invalid, category)
        with self.assertRaises(InvalidData):
            mirror_target('https://coomer.st/onlyfans/user/123', 'kemono')

    def test_mirror_launchers_keep_pending_calls_and_replay_without_the_source_list(self):
        for category, main in (('coomer', coomer_main), ('kemono', kemono_main)):
            source = self.directory / (category + '.conf')
            source.write_text(f'https://{category}.st/onlyfans/user/123\n')
            self.value['source_category'] = category
            self.value['gallery']['extractor'][category] = {'original': True}
            self.profile.write_text(json.dumps(self.value))
            args = [*self.base, '--call', str(uuid.uuid4()), '--config-file', str(source), '--profile', str(self.profile)]
            status, original, error = self.invoke(main, args)
            self.assertEqual((status, error, original['state']), (0, '', 'recorded'))
            source.unlink()
            self.profile.unlink()
            self.assertEqual(self.invoke(main, args), (0, original, ''))
            status, pending, _ = self.invoke(main, [*args, '--strict-errors'])
            self.assertEqual((status, pending['state']), (2, 'pending'))
            status, preview, error = self.invoke(main, ['--url', f'https://{category}.st/onlyfans/user/123', '--dry-run'])
            self.assertEqual((status, error, preview['targets']), (0, '', [f'https://{category}.st/onlyfans/user/123']))

    @unittest.skipUnless(hasattr(time, "tzset"), "host launcher timezone fixture requires tzset")
    def test_date_filters_keep_gallery_dl_boundaries_absolute_precedence_and_frozen_relative_time(self):
        now = datetime(2026, 10, 1, 12, 34, 56, 789000, tzinfo=timezone.utc).timestamp()
        try:
            with patch.dict(os.environ, {"TZ": "America/Los_Angeles"}):
                time.tzset()
                self.assertEqual(date_min(None, "1 week ago", None, now), "2026-09-24T05:34:56.000Z")
                self.assertEqual(date_min(None, "today", None, now), "2026-10-01T00:00:00.000Z")
                self.assertEqual(date_min(None, "yesterday", None, now), "2026-09-30T00:00:00.000Z")
                self.assertEqual(date_min(None, "now", None, now), "2026-10-01T05:34:56.000Z")
                self.assertEqual(date_min(None, "60 mins ago", None, now), "2026-10-01T04:34:56.000Z")
                expected = date_min("2026-09-01T03:04:05.999-07:00", "not relative", -1, now)
                self.assertEqual(expected, "2026-09-01T10:04:05.000Z")
                config.set((), "date-min", "2026-09-01T03:04:05.999-07:00")
                self.assertEqual(Fixture.from_url("https://fixture.invalid/account")._get_date_min_max()[0],
                                 datetime.fromisoformat(expected).timestamp())
        finally:
            time.tzset()
        for args in ((None, "1 day ago", 1, now), (None, None, -1, now), ("not a date", None, None, now)):
            with self.assertRaises(InvalidData):
                date_min(*args)

    def test_dry_run_needs_no_native_connection_and_never_creates_an_outbox(self):
        status, result, _ = self.invoke(reddit_main, ["--username", "Example", "--mode", "top", "--saved", "--dry-run",
                                                    "--outbox", str(self.outbox)])
        self.assertEqual(status, 0)
        self.assertEqual(len(result["targets"]), 6)
        self.assertTrue(result["full_history"])
        self.assertFalse(self.outbox.exists())

    def test_saved_list_request_survives_missing_inputs_without_becoming_completed(self):
        source = self.directory / "twitter.conf"
        source.write_text("123\n@Example\n")
        args = [*self.base, "--config-file", str(source), "--profile", str(self.profile)]
        status, original, errors = self.invoke(twitter_main, args)
        self.assertEqual((status, errors), (0, ""))
        self.assertEqual(original["state"], "recorded")
        source.unlink()
        self.profile.unlink()
        status, replay, _ = self.invoke(twitter_main, args)
        self.assertEqual((status, replay), (0, original))
        status, unfinished, _ = self.invoke(twitter_main, [*args, "--strict-errors"])
        self.assertEqual(status, 2)
        self.assertEqual(unfinished["state"], "pending")
        self.assertNotIn("private-first-value", json.dumps(unfinished))
        with closing(Outbox(self.outbox, "http://fixture.invalid", PRODUCER)) as box:
            self.assertEqual(SourceCalls(box).summary(self.call)["target_count"], 2)

    def test_full_history_requires_matching_reviewed_profile_and_keeps_the_date_minimum(self):
        self.value["source_category"] = "reddit"
        self.value["gallery"]["extractor"]["reddit"] = {"skip": "abort:4"}
        self.profile.write_text(json.dumps(self.value))
        self.full.write_text(json.dumps({**self.value, "gallery": {**self.value["gallery"], "skip": True}}))
        options = [*self.base, "--username", "Example", "--full-history", "--date-min", "2026-09-01"]
        status, _, error = self.invoke(reddit_main, [*options, "--profile", str(self.profile)])
        self.assertEqual(status, 1)
        self.assertIn("skip=true", error)
        status, result, error = self.invoke(reddit_main, [*options, "--profile", str(self.full)])
        self.assertEqual((status, error), (0, ""))
        definition = result["call"]["definition"]
        self.assertEqual(definition["window"]["since"], "2026-09-01T00:00:00.000Z")
        normal, full = Configuration(self.profile), Configuration(self.full)
        self.assertNotEqual(normal.policy_sha256, full.policy_sha256)
        self.assertEqual(definition["policy_sha256"], full.policy_sha256)
        with full.activate():
            self.assertIs(config.interpolate(("extractor", "reddit", "user"), "skip"), True)
            self.assertEqual(config.get(("extractor", "reddit"), "skip"), "abort:4")

    def test_top_mode_selects_full_history_profile_without_exposing_website_settings(self):
        self.value["source_category"] = "reddit"
        self.full.write_text(json.dumps({**self.value, "gallery": {**self.value["gallery"], "skip": True}}))
        with patch.dict(os.environ, {"STASH_INGEST_REDDIT_FULL_HISTORY_PROFILE": str(self.full),
                                    "STASH_INGEST_REDDIT_PROFILE": "/unavailable/normal.json"}):
            status, result, error = self.invoke(reddit_main, [*self.base, "--username", "Example", "--mode", "top"])
        self.assertEqual((status, error), (0, ""))
        self.assertEqual(result["call"]["target_count"], 4)
        self.assertNotIn("private-first-value", json.dumps(result))

    def test_systemd_identity_repeats_within_invocation_and_changes_for_new_work(self):
        with patch.dict(os.environ, {"INVOCATION_ID": uuid.uuid4().hex}):
            first = caller_uuid("reddit", PRODUCER, None, "new")
            self.assertEqual(first, caller_uuid("reddit", PRODUCER, None, "new"))
            self.assertNotEqual(first, caller_uuid("reddit", PRODUCER, None, "top"))
        with patch.dict(os.environ, {"INVOCATION_ID": uuid.uuid4().hex}):
            self.assertNotEqual(first, caller_uuid("reddit", PRODUCER, None, "new"))
        self.assertEqual(caller_uuid("reddit", PRODUCER, first), first)

    def test_raw_downloader_overrides_are_rejected_before_creating_a_queue(self):
        with redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as raised:
            twitter_main([*self.base, "--username", "Example", "--profile", str(self.profile), "--", "-o", "skip=false"])
        self.assertEqual(raised.exception.code, 2)
        self.assertFalse(self.outbox.exists())


if __name__ == "__main__":
    unittest.main()
