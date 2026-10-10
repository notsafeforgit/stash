import fcntl
import multiprocessing
import os
from pathlib import Path
import tempfile
import time
import unittest

from stash_ingest.encoding import InvalidData
from stash_ingest.publication_lock import ACTIVE, GATE, PublicationBarrier, PublicationBusy, publication_lock


def lock_process(directory, exclusive, entered, leave):
    context = PublicationBarrier([directory], timeout=5) if exclusive else publication_lock(directory, timeout=5)
    with context:
        entered.set()
        if not leave.wait(5):
            raise RuntimeError("test did not release child lock")


class PublicationLockTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.processes = multiprocessing.get_context("spawn")

    def child(self, exclusive):
        entered, leave = self.processes.Event(), self.processes.Event()
        process = self.processes.Process(target=lock_process, args=(self.root, exclusive, entered, leave))
        process.start()

        def cleanup():
            leave.set()
            process.join(3)
            if process.is_alive():
                process.kill()
                process.join()
        self.addCleanup(cleanup)
        return entered, leave, process

    def gate_blocked(self):
        fd = os.open(self.root / GATE, os.O_RDWR)
        try:
            try:
                fcntl.flock(fd, fcntl.LOCK_SH | fcntl.LOCK_NB)
                return False
            except BlockingIOError:
                return True
        finally:
            os.close(fd)

    def test_waiting_backup_drains_current_mutation_and_blocks_new_mutations(self):
        with publication_lock(self.root):
            backup_entered, release_backup, backup = self.child(True)
            deadline = time.monotonic() + 3
            while not self.gate_blocked() and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertTrue(self.gate_blocked())
            self.assertFalse(backup_entered.is_set())
            new_entered, release_new, new = self.child(False)
            self.assertFalse(new_entered.wait(0.1))
            # A nested callback on the already-running file can still finish
            # even after the backup takes its gate. No lock/lease reacquisition.
            with publication_lock(self.root, check=lambda: self.fail("nested lease check")):
                pass
        self.assertTrue(backup_entered.wait(3))
        self.assertFalse(new_entered.is_set())
        release_backup.set()
        self.assertTrue(new_entered.wait(3))
        release_new.set()
        for process in (backup, new):
            process.join(3)
            self.assertEqual(process.exitcode, 0)
        self.assertTrue((self.root / GATE).is_file())
        self.assertTrue((self.root / ACTIVE).is_file())

    def test_early_release_and_failed_multi_root_acquisition_free_all_locks(self):
        with PublicationBarrier([self.root]) as barrier:
            entered, leave, process = self.child(False)
            self.assertFalse(entered.wait(0.1))
            barrier.release()
            self.assertTrue(entered.wait(3))
            leave.set()
            process.join(3)
            self.assertEqual(process.exitcode, 0)
        with self.assertRaises(InvalidData): barrier.__enter__()
        other = self.root / "second"
        other.mkdir()
        with publication_lock(other):
            with self.assertRaises(InvalidData):
                with PublicationBarrier([self.root, other], timeout=0.05):
                    self.fail("barrier passed an active writer")
        with PublicationBarrier([self.root, other]):
            pass

    def test_refuses_symlinks_timeout_and_cancelled_worker_wait(self):
        outside = self.root / "outside"
        outside.write_bytes(b"keep")
        (self.root / ACTIVE).symlink_to(outside)
        with self.assertRaises(OSError):
            with publication_lock(self.root):
                self.fail("accepted redirected lock")
        (self.root / ACTIVE).unlink()
        with PublicationBarrier([self.root]):
            with self.assertRaises(PublicationBusy):
                with publication_lock(self.root, timeout=0.05):
                    self.fail("worker ignored backup exclusion")

            def cancelled():
                raise InvalidData("lease cancelled")
            with self.assertRaisesRegex(InvalidData, "lease cancelled"):
                with publication_lock(self.root, cancelled):
                    self.fail("cancelled worker entered publication")
        self.assertEqual(outside.read_bytes(), b"keep")
        with publication_lock(self.root):
            pass
