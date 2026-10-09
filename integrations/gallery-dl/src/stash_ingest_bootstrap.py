"""Initialize the command runtime before entering the native adapter."""

import logging

_initialized = False


def main(argv=None):
    global _initialized
    if not _initialized:
        try:
            from gallery_dl import output
        except ModuleNotFoundError as error:
            if error.name != "gallery_dl":
                raise
        else:
            # gallery-dl's action parser expects the lowercase log levels its
            # regular CLI registers here. Keep logs on stderr and JSON on stdout.
            output.initialize_logging(logging.WARNING)
        _initialized = True
    from stash_ingest.cli import main as command
    return command(argv)


if __name__ == "__main__":
    raise SystemExit(main())
