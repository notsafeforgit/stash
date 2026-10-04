"""Reviewed account-listing policies with private website access kept local."""

from .discovery_fetch import profile_platform
from .encoding import InvalidData
from .enrichment_configuration import EnrichmentConfiguration

SCHEMA = "stash-gallery-discovery-v1"


class DiscoveryConfiguration(EnrichmentConfiguration):
    schema = SCHEMA
    operation = "account.list_page"
    categories = frozenset({"reddit", "twitter"})

    def accepts(self, url):
        self.check()
        try:
            return profile_platform(url) == self.source_category
        except InvalidData:
            return False
