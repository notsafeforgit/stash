"""Keep incremental wheels identical to the current producer sources."""

from pathlib import Path

from setuptools import setup
from setuptools.command.build_py import build_py


class SourceOnlyBuild(build_py):
    def run(self):
        super().run()
        # setuptools reuses build/lib and otherwise packages removed modules.
        # Obsolete modules change the installed worker's policy fingerprint.
        outputs = {Path(value).resolve() for value in self.get_outputs()}
        for path in (Path(self.build_lib) / 'stash_ingest').rglob('*.py'):
            if path.resolve() not in outputs:
                path.unlink()


setup(cmdclass={'build_py': SourceOnlyBuild})
