from copy import deepcopy
import os
import tempfile
import unittest
from unittest.mock import patch

from stash_ingest.configuration import CONFIG_LIMIT, profile_bytes, runtime_identity
from stash_ingest.discovery_configuration import DiscoveryConfiguration, SCHEMA
from stash_ingest.encoding import InvalidData, digest, encode
from stash_ingest.enrichment_configuration import EnrichmentConfiguration, SCHEMA as ENRICHMENT_SCHEMA
from stash_ingest.metadata_fetch import safe_config


class DiscoveryConfigurationTests(unittest.TestCase):
    def test_listing_profiles_are_service_scoped_and_private_access_does_not_change_policy(self):
        value = {"schema": SCHEMA, "source_category": "reddit", "bindings": {
            "login": {"kind": "private", "env": "DISCOVERY_FIXTURE_LOGIN"}},
            "gallery": {"extractor": {"reddit": {"cookies": "${stash:login}"}, "sleep-request": 0,
                "archive": "/unused/archive", "filename": "unused"}, "postprocessor": {"exec": {"command": "unused"}}}}
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict(os.environ, {"DISCOVERY_FIXTURE_LOGIN": "first-private-token"}):
                first = DiscoveryConfiguration.from_document(value, directory)
            with patch.dict(os.environ, {"DISCOVERY_FIXTURE_LOGIN": "rotated-private-token"}):
                second = DiscoveryConfiguration.from_document(value, directory)
            self.assertEqual(first.policy_sha256, second.policy_sha256)
            self.assertTrue(first.accepts("https://www.reddit.com/user/example/submitted/?sort=new"))
            for url in ("https://www.reddit.com/comments/abc123", "https://www.reddit.com/user/example/submitted/?after=t3_saved",
                        "https://x.com/example/timeline", "https://www.instagram.com/example/"):
                self.assertFalse(first.accepts(url))
            self.assertIsNone(first.settings()["extractor"]["archive"])
            self.assertNotIn("postprocessor", first.settings())
            self.assertFalse(hasattr(first, "root"))
            self.assertEqual(second.settings()["extractor"]["reddit"]["cookies"], "rotated-private-token")
            changed = deepcopy(value)
            changed["gallery"]["extractor"]["sleep-request"] = 1
            with patch.dict(os.environ, {"DISCOVERY_FIXTURE_LOGIN": "rotated-private-token"}):
                self.assertNotEqual(first.policy_sha256, DiscoveryConfiguration.from_document(changed, directory).policy_sha256)
                changed["gallery"]["extractor"]["reddit"]["cookies"] = "inline-private-token"
                with self.assertRaises(InvalidData):
                    DiscoveryConfiguration.from_document(changed, directory)

    def test_discovery_and_enrichment_have_distinct_policy_operations_and_enrichment_digest_is_preserved(self):
        value = {"schema": SCHEMA, "source_category": "twitter", "gallery": {"extractor": {"sleep-request": 0}}, "bindings": {}}
        with tempfile.TemporaryDirectory() as directory:
            discovery = DiscoveryConfiguration.from_document(value, directory)
            enrichment = EnrichmentConfiguration.from_document({**value, "schema": ENRICHMENT_SCHEMA}, directory)
            self.assertTrue(discovery.accepts("https://x.com/example/timeline"))
            self.assertFalse(discovery.accepts("https://x.com/example/status/1234567890123456789"))
            self.assertTrue(enrichment.accepts("https://x.com/example/status/1234567890123456789"))
            self.assertFalse(enrichment.accepts("https://x.com/example/timeline"))
            self.assertNotEqual(discovery.policy_sha256, enrichment.policy_sha256)
            expected = digest(encode({"version": ENRICHMENT_SCHEMA, "operation": "post.enrich", "source_category": "twitter",
                "runtime": runtime_identity(), "gallery_sha256": digest(profile_bytes(safe_config(value["gallery"]))), "assets": {}}, CONFIG_LIMIT))
            self.assertEqual(enrichment.policy_sha256, expected)
            with self.assertRaises(InvalidData):
                DiscoveryConfiguration.from_document({**value, "source_category": "instagram"}, directory)
            with self.assertRaises(InvalidData):
                DiscoveryConfiguration.from_document({**value, "schema": ENRICHMENT_SCHEMA}, directory)
