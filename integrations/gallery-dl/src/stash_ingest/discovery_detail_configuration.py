"""An explicit metadata profile for verifying inferred Reddit/Twitter post URLs."""

from .enrichment_configuration import EnrichmentConfiguration

SCHEMA = "stash-gallery-discovery-detail-v1"


class DiscoveryDetailConfiguration(EnrichmentConfiguration):
    schema = SCHEMA
    operation = "post.verify_candidate"
    categories = {"reddit", "twitter"}
