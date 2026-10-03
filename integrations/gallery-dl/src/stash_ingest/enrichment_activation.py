"""Prepare, review and resume activation of imported enrichment holds."""

import sys

from . import activation_plan
from .activation_plan import inspect_plan
from .enrichment_activation_client import (DISPOSITIONS, MAX_BATCH, MAX_CANDIDATES, MAX_HTTP_BYTES,
                                        EnrichmentActivationClient, validate_candidate, validate_preview, activation_input)

RULES = activation_plan.ActivationRules("enrichment", (2 * MAX_HTTP_BYTES) + (64 << 10), MAX_BATCH, MAX_CANDIDATES,
                                       frozenset(DISPOSITIONS), validate_candidate, validate_preview, activation_input,
                                       frozenset(DISPOSITIONS - {"eligible", "completed"}))


def prepare(client, output, snapshot, manifest_sha256):
    return activation_plan.prepare(client, output, snapshot, manifest_sha256, RULES)


class Plan(activation_plan.Plan):
    def __init__(self, directory, expected_sha256, endpoint=None, *, verify_pages=True):
        super().__init__(directory, expected_sha256, endpoint, rules=RULES, verify_pages=verify_pages)


def main(argv=None):
    return activation_plan.main(argv, RULES, EnrichmentActivationClient)


if __name__ == "__main__":
    sys.exit(main())
