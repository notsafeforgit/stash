"""Reviewed metadata-only policies without media roots or download writers."""

import copy

from .configuration import Configuration, CONFIG_LIMIT, _snapshot, profile_bytes, runtime_identity
from .encoding import InvalidData, digest, encode
from .gallery import SUPPORTED_VERSION
from .metadata_fetch import POSTS, is_post, public_url, safe_config

SCHEMA = "stash-gallery-enrichment-v1"


class EnrichmentConfiguration(Configuration):
    def _initialize(self, value, base):
        if (not isinstance(value, dict) or set(value) != {"schema", "source_category", "gallery", "bindings"}
                or value["schema"] != SCHEMA or not isinstance(value["gallery"], dict)
                or not isinstance(value["source_category"], str) or value["source_category"] not in POSTS):
            raise InvalidData("Invalid metadata-only worker configuration")
        self.source_category = value["source_category"]
        assets = self._bindings(value["bindings"], base)
        # Validate references on the reviewed template before projection. Access
        # values never enter the policy digest, native job or local journal.
        template = safe_config(value["gallery"])
        expanded = self._expand(template)
        self._gallery = safe_config(expanded)
        self.extractor_version = SUPPORTED_VERSION
        self.policy_sha256 = digest(encode({"version": SCHEMA, "operation": "post.enrich",
            "source_category": self.source_category, "runtime": runtime_identity(),
            "gallery_sha256": digest(profile_bytes(template)), "assets": assets}, CONFIG_LIMIT))

    def check(self):
        for path, expected in self.assets.items():
            if _snapshot(path.stat()) != expected:
                raise InvalidData("A reviewed metadata worker asset changed")

    def accepts(self, url):
        from gallery_dl import extractor
        self.check()
        target = extractor.find(public_url(url))
        return target is not None and target.category == self.source_category and is_post(target)

    def settings(self):
        self.check()
        return copy.deepcopy(self._gallery)
