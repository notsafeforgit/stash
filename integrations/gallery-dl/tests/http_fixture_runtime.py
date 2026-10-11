"""Use the recorded extractor version when replaying saved HTTP fixtures."""

import json
from pathlib import Path

from stash_ingest import enrichment_configuration


def use_recorded_metadata_version():
    # These tests inject captured responses instead of calling gallery-dl.
    # Keep their jobs and checkpoints on the version that produced that evidence;
    # dependency upgrades must not relabel the shared Go/Python fixtures.
    fixture = Path(__file__).resolve().parents[3] / "pkg/archive/testdata/enrichment-transcript-v1.json"
    enrichment_configuration.SUPPORTED_VERSION = json.loads(fixture.read_bytes())["complete"]["extractor_version"]
