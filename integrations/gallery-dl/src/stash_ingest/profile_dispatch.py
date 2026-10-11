"""Discover both current and retained policies after shared config edits."""

from .configuration import Configuration
from .configuration_store import Configurations
from .dispatch import Dispatcher
from .encoding import InvalidData
from .outbox import Capacity


class ProfileDispatcher:
    def __init__(self, box, client, filename):
        self.box, self.client, self.filename = box, client, filename

    def once(self):
        saved = Configurations(self.box)
        current, unavailable = None, False
        try:
            current = Configuration(self.filename)
            saved.remember(current)
        except (OSError, InvalidData, Capacity):
            # Broken new settings must not strand independently valid old work.
            unavailable = True
        rows = saved.rows(self.filename)
        result = {"state": "idle"}
        for row in rows:
            is_current = (current is not None and row["policy_sha256"] == current.policy_sha256
                          and row["root_uuid"] == current.root_uuid)
            if not is_current and row["available_at"] > self.box.clock():
                continue
            saved.visited(row)  # Rotate before execution, including process death.
            try:
                profile = saved.load(row)
                result = Dispatcher(self.box, self.client, profile).once()
            except (OSError, InvalidData):
                unavailable = True
                saved.visited(row, delay=300)
                continue
            if result["state"] not in ("idle", "waiting", "backoff", "unavailable", "contended"):
                return result
            if not is_current:
                # Old policies remain eligible for queued callers and retries,
                # but an empty policy needn't add a request every worker tick.
                saved.visited(row, delay=300 if result["state"] == "idle" else 5)
        if unavailable:
            return {"state": "unavailable", "error_code": "worker_profile_unavailable"}
        return result
