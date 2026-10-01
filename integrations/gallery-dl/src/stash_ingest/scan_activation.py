"""Preview or activate a reviewed native binding for retained scan requests."""

import argparse
from datetime import datetime
from http.client import HTTPException
import json
import os
from pathlib import Path
import re
import sys
from urllib.error import HTTPError, URLError
from urllib.request import Request

from .backfill_import import ImportClient
from .client import Unavailable
from .encoding import InvalidData, decode, encode, identifier


class ScanActivationClient(ImportClient):
    def submit(self, binding, expected=None):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        if expected is not None and not re.fullmatch(r"[0-9a-f]{64}", expected):
            raise InvalidData("Apply requires the reviewed plan digest")
        value = binding if expected is None else {"binding": binding, "expected_plan_sha256": expected}
        suffix = "/preview" if expected is None else ""
        request = Request(self.endpoint + "/api/v3/archive/scan-journal-activations" + suffix,
                          data=encode(value, 16384), method="POST",
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_scan_activation_response")
                result = decode(response.read((1 << 20) + 1), 1 << 20)
        except HTTPError as error:
            code = error.code
            error.close()
            raise Unavailable("scan_activation_rejected", code) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_scan_activation_response") from None
        # The server returns the normalized reviewed input; callers keep that
        # binding for apply/retry rather than reconstructing dates or identities.
        if (not isinstance(result, dict) or not isinstance(result.get("binding"), dict)
                or result["binding"].get("uuid") != binding.get("uuid")
                or not isinstance(result.get("plan_sha256"), str)
                or not re.fullmatch(r"[0-9a-f]{64}", result["plan_sha256"])
                or (expected is not None and result["plan_sha256"] != expected)):
            raise Unavailable("invalid_scan_activation_response")
        for name, value in binding.items():
            returned = result["binding"].get(name)
            if name == "cutoff":
                try:
                    equal = datetime.fromisoformat(value) == datetime.fromisoformat(returned)
                except (ValueError, TypeError):
                    equal = False
            elif name == "checkpoint_record_uuid" and not value:
                equal = not returned
            else:
                equal = returned == value
            if not equal:
                raise Unavailable("invalid_scan_activation_response")
        if expected is not None:
            identifier(result.get("run_uuid"))
        return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binding", required=True, help="Frozen activation binding JSON, or a saved preview containing binding")
    parser.add_argument("--endpoint", required=True, help="Explicit native application endpoint")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    parser.add_argument("--expected-sha256", help="Reviewed server plan digest; required with --apply")
    parser.add_argument("--apply", action="store_true", help="Create one native run, preserving deferrals and retry delay")
    args = parser.parse_args(argv)
    try:
        if args.apply and not args.expected_sha256:
            raise InvalidData("Apply requires the reviewed plan digest")
        with Path(args.binding).open("rb") as source:
            value = decode(source.read((1 << 20) + 1), 1 << 20)
        if not isinstance(value, dict):
            raise InvalidData("Activation binding must be an object")
        binding = value.get("binding", value)
        if not isinstance(binding, dict):
            raise InvalidData("Activation binding must be an object")
        result = ScanActivationClient(args.endpoint, args.api_key_env).submit(binding, args.expected_sha256 if args.apply else None)
        if args.expected_sha256 is not None and result["plan_sha256"] != args.expected_sha256:
            raise InvalidData("Activation plan differs from the reviewed digest")
        print(json.dumps({**result, "action": "activated" if args.apply else "preview"}, sort_keys=True))
        return 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
    except OSError:
        message = "Frozen activation binding is unavailable"
    print(json.dumps({"error": message, "acknowledged": False, "resume": "repeat_same_binding_and_plan_digest"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
