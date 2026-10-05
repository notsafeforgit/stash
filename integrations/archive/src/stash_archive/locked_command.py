"""Hold inherited backup exclusion until an indirect command has exited.

The supervisor retains the descriptor even when sudo closes descriptors before
launching ZFS. A caller timeout stops waiting, not the supervisor or its command.
Closing the caller's copy preserves exclusion until the command is drained.
"""

import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import threading


def validate_lock(lock_fd):
    if type(lock_fd) is not int or lock_fd < 0 or not stat.S_ISREG(os.fstat(lock_fd).st_mode):
        raise ValueError("Backup command exclusion must be an open regular descriptor")


def run_locked(command, lock_fd, timeout):
    validate_lock(lock_fd)
    process = subprocess.Popen([sys.executable, str(Path(__file__).absolute()), str(lock_fd), *command],
                               pass_fds=(lock_fd,), start_new_session=True, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               encoding="utf-8", errors="surrogateescape", env={**os.environ, "LC_ALL": "C"})
    try:
        stdout, stderr = process.communicate(timeout=timeout)
    except BaseException:
        # Reap if this host process stays alive. If it dies, the detached
        # supervisor still retains the lock and drains its own command first.
        threading.Thread(target=process.communicate, daemon=True).start()
        raise
    return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)


def main():
    descriptor = int(sys.argv[1])
    validate_lock(descriptor)
    # A terminal cancellation cannot release the lock while privileged work
    # remains alive. The command has its own normal signal dispositions.
    signal.signal(signal.SIGTERM, lambda *_: None)
    signal.signal(signal.SIGINT, lambda *_: None)
    try:
        result = subprocess.run(sys.argv[2:], stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, check=False, close_fds=True)
        try:
            sys.stdout.buffer.write(result.stdout)
            sys.stdout.buffer.flush()
            sys.stderr.buffer.write(result.stderr)
            sys.stderr.buffer.flush()
        except BrokenPipeError:
            pass
        return result.returncode
    except OSError:
        return 127


if __name__ == "__main__":
    raise SystemExit(main())
