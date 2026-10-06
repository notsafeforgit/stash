"""Retain old plugin settings and publish an explicitly reviewed native policy."""

import argparse
import ast
from http.client import HTTPException
import json
import os
from pathlib import Path
import re
import sys
from urllib.error import HTTPError, URLError
from urllib.request import Request

from .backfill_import import ImportClient
from .catalog_source import source_time
from .catalog_upload import read_regular
from .client import Unavailable
from .encoding import InvalidData, decode, digest, encode, identifier, native_json


LIMIT = 1 << 20
FORMAT = "legacy-metadata-policy"
BOOLEAN_RULES = ("on_create", "on_existing", "skip_organized_on_create", "mark_organized", "filename_title_fallback")


def literal_defaults(body):
    """Inspect config.py literals without importing or executing plugin code."""
    if len(body) > LIMIT:
        raise InvalidData("Legacy Python settings exceed their byte limit")
    try:
        tree = ast.parse(body.decode("utf-8"))
        result = {}
        for statement in tree.body:
            if isinstance(statement, ast.Expr) and isinstance(statement.value, ast.Constant) and isinstance(statement.value.value, str):
                continue
            if (not isinstance(statement, ast.Assign) or len(statement.targets) != 1
                    or not isinstance(statement.targets[0], ast.Name)):
                raise InvalidData("Legacy Python settings must contain literal assignments only")
            name = statement.targets[0].id
            if name in result or name.startswith("_"):
                raise InvalidData("Repeated or private legacy setting requires explicit review")
            result[name] = ast.literal_eval(statement.value)
        encode(result, LIMIT)
    except (SyntaxError, ValueError, TypeError, UnicodeError, RecursionError):
        raise InvalidData("Legacy Python settings could not be read as literals") from None
    return result


def snapshot(python_body, declared_body, saved_body, version, captured_at):
    source_time(captured_at)
    declared, saved = decode(declared_body, LIMIT, preserve_numbers=True), decode(saved_body, LIMIT, preserve_numbers=True)
    if not isinstance(declared, dict) or not isinstance(saved, dict) or not isinstance(version, str) or not version.strip():
        raise InvalidData("Supply plugin-only defaults and saved overrides as JSON objects")
    layers = {"python": literal_defaults(python_body), "declared": declared, "saved": saved}
    values = {layer + "/" + key: value for layer, settings in layers.items() for key, value in settings.items()}
    if not values or len(values) > 256:
        raise InvalidData("Legacy settings exceed the bounded migration inventory")
    result = {"format": FORMAT, "version": 1, "plugin_version": version, "captured_at": captured_at,
              "source_files": {"config.py": digest(python_body), "declared-settings.json": digest(declared_body),
                               "saved-settings.json": digest(saved_body)}, "values": values}
    encode(result, LIMIT)
    return result


def effective_settings(document):
    """Reconstruct precedence, keeping all overridden originals in the snapshot."""
    values = document["values"]
    result = {}
    for layer in ("python", "declared", "saved"):
        for key, value in values.items():
            if key.startswith(layer + "/"):
                name = key[len(layer) + 1:]
                previous = result.get(name)
                if isinstance(value, str) and (isinstance(previous, list) or name.endswith("_mappings")):
                    value = decode(value.encode(), LIMIT, preserve_numbers=True)
                result[name] = value
    return result


def canonical_binding(value):
    """Match the native typed shape before computing the shared json-v1 digest."""
    if not isinstance(value, dict) or set(value) != {"uuid", "policy", "document", "dispositions", "folder_sources"}:
        raise InvalidData("Unsupported policy migration binding shape")
    # This copy validates JSON and keeps number tokens from frozen input.
    value = decode(encode(value, LIMIT), LIMIT, preserve_numbers=True)
    identifier(value["uuid"])
    policy = value["policy"]
    if not isinstance(policy, dict) or set(policy) != {"collection_uuid", "expected_revision", "expected_collection_revision", "definition", "origin", "reason"}:
        raise InvalidData("A complete reviewed native policy input is required")
    identifier(policy["collection_uuid"])
    if (type(policy["expected_revision"]) is not int or policy["expected_revision"] < 0
            or type(policy["expected_collection_revision"]) is not int or policy["expected_collection_revision"] < 1
            or policy["origin"] != "migration" or not isinstance(policy["reason"], str) or not policy["reason"].strip()):
        raise InvalidData("Invalid reviewed policy revision or migration origin")
    definition = policy["definition"]
    if not isinstance(definition, dict) or set(definition) != {"enabled", "apply_to_scans", "rules"}:
        raise InvalidData("A complete native policy definition is required")
    if any(type(definition[key]) is not bool for key in ("enabled", "apply_to_scans")):
        raise InvalidData("Policy switches must be booleans")
    rules = definition["rules"]
    if not isinstance(rules, dict) or not set(rules) <= {"scene", "image"}:
        raise InvalidData("Policy rules support scenes and images")
    for rule in rules.values():
        if not isinstance(rule, dict) or set(rule) - {*BOOLEAN_RULES, "mappings", "organized_requires"}:
            raise InvalidData("Unknown native policy rule option")
        for key in BOOLEAN_RULES:
            rule.setdefault(key, False)
            if type(rule[key]) is not bool:
                raise InvalidData("Policy rule switches must be booleans")
        required = rule.get("organized_requires")
        if required is None or required == []:
            rule.pop("organized_requires", None)
        elif not isinstance(required, list) or any(not isinstance(field, str) for field in required):
            raise InvalidData("Organized requirements must be field names")
        mappings = rule.setdefault("mappings", None)
        if mappings is not None and not isinstance(mappings, dict):
            raise InvalidData("Native mappings must be an object")
        for field, mapping in (mappings or {}).items():
            if not isinstance(mapping, dict) or set(mapping) - {"jq", "value", "performer_names", "reference_names", "fallback"}:
                raise InvalidData("Unknown native mapping option")
            if mapping.get("jq") == "":
                mapping.pop("jq")
            for flag in ("performer_names", "reference_names"):
                if flag in mapping:
                    if type(mapping[flag]) is not bool:
                        raise InvalidData("Name matching switches must be booleans")
                    if not mapping[flag]:
                        mapping.pop(flag)
            if mapping.get("performer_names") and (field != "performers" or mapping.get("reference_names")):
                raise InvalidData("performer_names only supports performers and cannot combine with reference_names")
            if mapping.get("reference_names") and field not in {"performers", "studio", "tags", "groups"}:
                raise InvalidData("Name matching requires a relationship field")
            if ("jq" in mapping) == ("value" in mapping):
                raise InvalidData("Mappings require exactly one expression or constant")
            if "fallback" in mapping and "jq" not in mapping:
                raise InvalidData("A fallback requires a jq expression")
    document = value["document"]
    if (not isinstance(document, dict) or set(document) != {"format", "version", "plugin_version", "captured_at", "source_files", "values"}
            or document["format"] != FORMAT or type(document["version"]) is not int or document["version"] != 1):
        raise InvalidData("Unsupported legacy policy snapshot")
    source_time(document["captured_at"])
    settings, dispositions = document["values"], value["dispositions"]
    if not isinstance(settings, dict) or not settings or not isinstance(dispositions, dict) or set(settings) != set(dispositions):
        raise InvalidData("Every retained setting needs one conversion disposition")
    for disposition in dispositions.values():
        if (not isinstance(disposition, dict) or set(disposition) != {"action", "reason"}
                or disposition["action"] not in {"mapped", "replaced", "retired", "review"}
                or not isinstance(disposition["reason"], str) or not disposition["reason"].strip()):
            raise InvalidData("Invalid setting conversion disposition")
        if disposition["action"] == "review" and definition["enabled"]:
            raise InvalidData("Unresolved legacy settings require a disabled policy")
    sources = value["folder_sources"]
    if not isinstance(sources, list):
        raise InvalidData("Folder evidence must be a list of selected native documents")
    for source in sources:
        if not isinstance(source, dict) or set(source) != {"source_uuid", "head_uuid"}:
            raise InvalidData("Folder evidence requires its source and selected head UUID")
        identifier(source["source_uuid"])
        identifier(source["head_uuid"])
    encode(value, LIMIT)
    return value


class PolicyImportClient(ImportClient):
    def submit(self, binding, expected=None):
        binding = canonical_binding(binding)
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        if expected is not None and (not isinstance(expected, str) or not re.fullmatch(r"[0-9a-f]{64}", expected)):
            raise InvalidData("Apply requires the reviewed plan digest")
        value = binding if expected is None else {"binding": binding, "expected_plan_sha256": expected}
        suffix = "/preview" if expected is None else ""
        request = Request(self.endpoint + "/api/v3/archive/metadata-policy-imports" + suffix,
                          data=encode(value, LIMIT + 1024), method="POST",
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_policy_import_response")
                result = decode(response.read((512 << 10) + 1), 512 << 10, preserve_numbers=True)
        except HTTPError as error:
            status = error.code
            error.close()
            raise Unavailable("policy_import_rejected", status) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_policy_import_response") from None
        try:
            required = {"uuid", "input_sha256", "collection", "previous_policy", "definition", "reference_revisions", "folder_sources", "review_keys", "plan_sha256"}
            if set(result) != required | ({"policy_revision", "created_at"} if expected is not None else set()):
                raise InvalidData("Unexpected response shape")
            checked = {k: result[k] for k in required}
            checked["plan_sha256"] = ""
            if (result["uuid"] != binding["uuid"] or result["input_sha256"] != digest(native_json(binding, LIMIT))
                    or result["definition"] != binding["policy"]["definition"]
                    or result["collection"]["uuid"] != binding["policy"]["collection_uuid"]
                    or result["collection"]["revision"] != binding["policy"]["expected_collection_revision"]
                    or result["folder_sources"] != binding["folder_sources"]
                    or result["review_keys"] != sorted(k for k, v in binding["dispositions"].items() if v["action"] == "review")
                    or result["plan_sha256"] != digest(native_json(checked, 512 << 10))
                    or (expected is not None and (result["plan_sha256"] != expected or type(result["policy_revision"]) is not int
                                                  or result["policy_revision"] < 1))):
                raise InvalidData("Response differs from the frozen binding")
            if expected is not None:
                source_time(result["created_at"])
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_policy_import_response") from None
        return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binding", required=True, help="Frozen native policy, original settings and explicit conversion dispositions")
    parser.add_argument("--endpoint", help="Native Stash endpoint; without it only validate the binding")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    parser.add_argument("--expected-sha256", help="Reviewed server plan digest; required with --apply")
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args(argv)
    try:
        document = decode(read_regular(Path(args.binding), LIMIT + (512 << 10)), LIMIT + (512 << 10), preserve_numbers=True)
        binding = canonical_binding(document.get("binding", document))
        if args.apply and not all((args.endpoint, args.expected_sha256)):
            raise InvalidData("Apply requires an explicit native endpoint and reviewed digest")
        result = {"input_sha256": digest(native_json(binding, LIMIT))}
        if args.endpoint:
            result = PolicyImportClient(args.endpoint, args.api_key_env).submit(binding, args.expected_sha256 if args.apply else None)
        if args.expected_sha256 is not None and result.get("plan_sha256") != args.expected_sha256:
            raise InvalidData("Saved migration plan differs from the reviewed digest")
        print(encode({**result, "binding": binding, "action": "applied" if args.apply else "preview" if args.endpoint else "prepared"}, LIMIT + (512 << 10)).decode())
        return 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
    except (KeyError, TypeError, AttributeError):
        message = "Unsupported migration binding"
    except OSError:
        message = "Frozen policy binding is unavailable"
    print(json.dumps({"error": message, "acknowledged": False, "resume": "repeat_same_binding_and_plan_digest"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
