"""Brief host-owned pauses for external database/payload writers.

Each pause belongs to a separate bounded systemd service. Its stop action resumes
the exact container ID even if the backup process disappears. The server never
executes these commands; sealed retries validate evidence without pausing again.
"""

import copy
import re
import subprocess
import uuid

from stash_archive.storage import InvalidArchive

FORMAT = "org.notsafeforgit.stash.container-boundary"
NAME = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}")
ID = re.compile(r"[0-9a-f]{64}")
UNIT = re.compile(r"stash-backup-freeze-[0-9a-f]{32}-[0-9]{1,2}\.service")
PODMAN = "/usr/bin/podman"
SYSTEMCTL = "/usr/bin/systemctl"
SYSTEMD_RUN = "/usr/bin/systemd-run"
INACTIVE = {"created", "exited", "stopped"}


def validate_options(names, timeout):
    if (not isinstance(names, list) or len(names) > 16
            or any(not isinstance(name, str) or not NAME.fullmatch(name) for name in names)
            or len(set(names)) != len(names)
            or type(timeout) is not int or not 1 <= timeout <= 900):
        raise InvalidArchive("Container capture requires unique names and a 1–900 second pause deadline")


def command(arguments, *, check=True):
    try:
        result = subprocess.run(arguments, stdin=subprocess.DEVNULL, capture_output=True,
                                text=True, timeout=45, check=False)
    except (OSError, subprocess.SubprocessError):
        raise InvalidArchive("External container capture command did not complete") from None
    if check and result.returncode:
        # Never include command output: host diagnostics can contain private paths.
        raise InvalidArchive("External container capture command failed")
    return result


def inspect_container(name):
    result = command([PODMAN, "inspect", "--type", "container", "--format",
                      "{{.Id}} {{.State.Status}} {{.State.Paused}}", name])
    fields = result.stdout.strip().split()
    if len(fields) != 3 or not ID.fullmatch(fields[0]) or fields[2] not in {"true", "false"}:
        raise InvalidArchive("Invalid external container identity or state")
    return fields[0], fields[1], fields[2] == "true"


class ContainerBoundary:
    def __init__(self, names, *, timeout=300, existing_only=False):
        validate_options(names, timeout)
        self.names, self.timeout = list(names), timeout
        self.existing_only = existing_only
        self.records, self.pending, self.proof = [], [], None

    def __enter__(self):
        if self.existing_only:
            return self
        invocation = uuid.uuid4().hex
        try:
            # Inspect all before changing any. Do not resume a user-paused container.
            for index, name in enumerate(self.names):
                identity, state, paused = inspect_container(name)
                if paused or state not in INACTIVE | {"running"}:
                    raise InvalidArchive("External container is already paused or changing state")
                self.records.append({"name": name, "id": identity, "initial_state": state,
                                     "guard_unit": (f"stash-backup-freeze-{invocation}-{index}.service"
                                                    if state == "running" else "")})
            for record in self.records:
                if not record["guard_unit"]:
                    continue
                # Retain intent before a command with an uncertain response. The
                # service itself owns both pause and resume, avoiding a timer/pause race.
                self.pending.append(record)
                command([SYSTEMD_RUN, "--user", "--quiet", "--collect", "--unit=" + record["guard_unit"],
                         "--property=Type=exec", "--property=RuntimeMaxSec=" + str(self.timeout),
                         "--property=TimeoutStartSec=30", "--property=TimeoutStopSec=30",
                         "--property=ExecStartPre=" + PODMAN + " pause " + record["id"],
                         "--property=ExecStopPost=" + PODMAN + " unpause " + record["id"],
                         "/usr/bin/sleep", "infinity"])
            self.verify_active()
        except BaseException:
            self.release()
            raise
        return self

    def verify_active(self):
        if self.existing_only or len(self.records) != len(self.names):
            raise InvalidArchive("No fresh external container capture is active")
        for record in self.records:
            if record["guard_unit"]:
                state = command([SYSTEMCTL, "--user", "show", record["guard_unit"],
                                 "--property=ActiveState,SubState"]).stdout.splitlines()
                if set(state) != {"ActiveState=active", "SubState=running"}:
                    raise InvalidArchive("External container capture deadline expired")
            identity, state, paused = inspect_container(record["name"])
            if (identity != record["id"] or (record["guard_unit"] and not paused)
                    or (not record["guard_unit"] and (paused or state != record["initial_state"]))):
                raise InvalidArchive("External container changed during capture")

    def binding(self):
        self.verify_active()
        self.proof = {"format": FORMAT, "version": 1, "timeout_seconds": self.timeout,
                      "containers": copy.deepcopy(self.records)}
        return copy.deepcopy(self.proof)

    def validate_boundary(self, boundary):
        proof = boundary.get("details", {}).get("external_containers")
        if (not isinstance(proof, dict) or set(proof) != {"format", "version", "timeout_seconds", "containers"}
                or proof["format"] != FORMAT or type(proof["version"]) is not int or proof["version"] != 1
                or type(proof["timeout_seconds"]) is not int or proof["timeout_seconds"] != self.timeout
                or not isinstance(proof["containers"], list) or len(proof["containers"]) != len(self.names)):
            raise InvalidArchive("Invalid retained external container boundary")
        for name, record in zip(self.names, proof["containers"]):
            if (not isinstance(record, dict) or set(record) != {"name", "id", "initial_state", "guard_unit"}
                    or record["name"] != name or not isinstance(record["id"], str) or not ID.fullmatch(record["id"])
                    or not isinstance(record["initial_state"], str) or record["initial_state"] not in INACTIVE | {"running"}
                    or not isinstance(record["guard_unit"], str)
                    or (record["initial_state"] == "running" and not UNIT.fullmatch(record["guard_unit"]))
                    or (record["initial_state"] != "running" and record["guard_unit"] != "")):
                raise InvalidArchive("Retained external container identity differs")
        if not self.existing_only and proof != self.proof:
            raise InvalidArchive("External container boundary differs from this capture")

    def release(self):
        failed = []
        for record in list(reversed(self.pending)):
            try:
                command([SYSTEMCTL, "--user", "stop", record["guard_unit"]], check=False)
                # A timed-out unit may already have been collected. Always check
                # the exact captured ID; never resume a replacement under its name.
                _, _, paused = inspect_container(record["id"])
                if paused:
                    command([PODMAN, "unpause", record["id"]])
                    if inspect_container(record["id"])[2]:
                        raise InvalidArchive("External container remains paused")
                self.pending.remove(record)
            except InvalidArchive:
                failed.append(record["name"])
        if failed:
            raise InvalidArchive("Could not confirm external container resume: " + ", ".join(failed))

    def __exit__(self, *_):
        self.release()
