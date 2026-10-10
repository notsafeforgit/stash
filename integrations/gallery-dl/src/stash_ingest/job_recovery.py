"""Keep worker ownership generations separate from actual failure budgets."""


def valid_job_retries(job):
    if not isinstance(job, dict):
        return False
    fence = job.get("fence")
    maximum = job.get("max_attempts")
    if (type(fence) is not int or not 0 <= fence <= 2**53 - 1
            or type(maximum) is not int or maximum != 8):
        return False
    # Pending producer journals can retain an earlier native response. Keep
    # those original bytes readable; new responses include the failure count.
    if "failures" not in job:
        return fence <= maximum and not (job.get("state") == "queued" and fence == maximum)
    failures = job["failures"]
    return (type(failures) is int and 0 <= failures <= min(fence, maximum)
            and not (job.get("state") in ("queued", "running") and failures == maximum))
