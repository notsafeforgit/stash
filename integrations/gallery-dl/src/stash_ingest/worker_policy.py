"""Keep an approved executable repair separate from original admission data."""

from .client import Unavailable
from .events import sha256


def execution_policy(job, original):
    # Saved pre-upgrade jobs have only their original policy. Newly described
    # jobs include the server-approved policy, never a caller-supplied alias.
    policy = job.get("execution_policy_sha256", original)
    if not sha256(policy):
        raise Unavailable("invalid_response")
    return policy
