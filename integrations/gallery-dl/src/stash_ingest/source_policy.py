"""Initialize collection metadata policies from saved source-registration plans."""

from copy import deepcopy

from .activation_client import ActivationClient, integer
from .catalog_source import source_time
from .client import Unavailable
from .encoding import InvalidData, encode, identifier
from .source_management import conflict, text, validate_plan

MAX_BYTES = 2 << 20


def definition(value):
    # The native server owns the field/rule schema. Keep transport validation
    # here and let its revision-guarded PUT validate the complete definition.
    if (not isinstance(value, dict) or set(value) != {"enabled", "apply_to_scans", "rules"}
            or type(value["enabled"]) is not bool or type(value["apply_to_scans"]) is not bool
            or not isinstance(value["rules"], dict) or not value["rules"]
            or not set(value["rules"]) <= {"scene", "image"}):
        raise InvalidData("Expected a complete native metadata policy definition")
    value = deepcopy(value)
    flags = {"on_create", "on_existing", "skip_organized_on_create", "mark_organized", "filename_title_fallback"}
    for rule in value["rules"].values():
        if (not isinstance(rule, dict) or not flags | {"mappings"} <= set(rule)
                or set(rule) - flags - {"mappings", "organized_requires"}
                or any(type(rule[key]) is not bool for key in flags) or not isinstance(rule["mappings"], dict)):
            raise InvalidData("Source metadata rules require explicit event and fallback choices")
        if "organized_requires" in rule:
            fields = rule["organized_requires"]
            if not isinstance(fields, list) or any(not isinstance(field, str) for field in fields):
                raise InvalidData("Invalid metadata completeness requirements")
            if not fields:
                del rule["organized_requires"]
        for mapping in rule["mappings"].values():
            if (not isinstance(mapping, dict) or set(mapping) - {"jq", "value", "performer_names", "reference_names", "fallback"}
                    or ("jq" in mapping and not isinstance(mapping["jq"], str))
                    or any(type(mapping[key]) is not bool for key in ("performer_names", "reference_names") if key in mapping)):
                raise InvalidData("Invalid source metadata mapping")
            for key in ("jq", "performer_names", "reference_names"):
                if key in mapping and not mapping[key]:
                    del mapping[key]
    encode(value, 131072)
    return value


def policy(value, collection_uuid):
    if value is None:
        return None
    if (not isinstance(value, dict) or set(value) != {"collection_uuid", "revision", "collection_revision",
                                                        "definition", "origin", "reason", "created_at"}
            or value["collection_uuid"] != collection_uuid or value["origin"] not in {"review", "migration"}):
        raise InvalidData("Invalid collection metadata policy response")
    identifier(value["collection_uuid"])
    integer(value["revision"], 1, 2147483647)
    integer(value["collection_revision"], 1, 2147483647)
    if definition(value["definition"]) != value["definition"]:
        raise InvalidData("Metadata policy response is not in its canonical form")
    text(value["reason"], 4096)
    source_time(value["created_at"])
    return value


def mutation(value):
    if (not isinstance(value, dict) or set(value) != {"collection_uuid", "expected_revision",
            "expected_collection_revision", "definition", "reason"} or value["expected_revision"] != 0
            or type(value["expected_revision"]) is not int):
        raise InvalidData("Source automation may only initialize a missing metadata policy")
    identifier(value["collection_uuid"])
    integer(value["expected_collection_revision"], 1, 2147483647)
    if definition(value["definition"]) != value["definition"]:
        raise InvalidData("Save canonical metadata defaults before applying them")
    if value["definition"]["enabled"] is not True or value["definition"]["apply_to_scans"] is not False:
        raise InvalidData("New source policies must be enabled and confined to source intake")
    text(value["reason"], 4096)
    return value


class SourcePolicyClient(ActivationClient):
    kind = "source_policy"
    max_http_bytes = MAX_BYTES

    def policy(self, collection_uuid):
        identifier(collection_uuid)
        try:
            return policy(self.request("GET", f"/collections/{collection_uuid}/metadata-policy"), collection_uuid)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_source_policy") from None

    def recover(self, value):
        value = mutation(value)
        identity = value["collection_uuid"]
        rows = self.request("GET", f"/collections/{identity}/metadata-policy/history?after=0&limit=1")
        try:
            if not isinstance(rows, list) or len(rows) > 1:
                raise InvalidData("Invalid metadata policy history")
            if not rows:
                if self.policy(identity) is not None:
                    raise conflict()
                return None
            found = policy(rows[0], identity)
            if (found is None or found["revision"] != 1 or found["origin"] != "review"
                    or found["collection_revision"] != value["expected_collection_revision"]
                    or found["definition"] != value["definition"] or found["reason"] != value["reason"]):
                raise conflict()
            return found
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_source_policy_history") from None

    def apply(self, value):
        value = mutation(value)
        previous = self.recover(value)
        if previous is not None:
            return previous
        try:
            result = self.request("PUT", f"/collections/{value['collection_uuid']}/metadata-policy", value)
        except Unavailable as error:
            if error.status != 409:
                raise
            previous = self.recover(value)
            if previous is None:
                raise
            return previous
        try:
            found = policy(result, value["collection_uuid"])
            if (found is None or found["revision"] != 1 or found["origin"] != "review"
                    or found["collection_revision"] != value["expected_collection_revision"]
                    or found["definition"] != value["definition"] or found["reason"] != value["reason"]):
                raise InvalidData("Metadata policy result differs from its saved initialization")
            return found
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_source_policy_result") from None


def prepare(client, source_plan, new_definition):
    source_plan = validate_plan(source_plan)
    if source_plan["intent"]["operation"] != "ensure" or source_plan["endpoint"] != client.endpoint:
        raise InvalidData("Metadata initialization requires this endpoint's registration plan")
    new_definition = definition(new_definition)
    entries = []
    for source in source_plan["entries"]:
        before, create = source["before"], source["input"]
        identity = before["uuid"] if before else create["uuid"]
        current = client.policy(identity)
        value = None
        if before is None:
            if current is not None:
                raise conflict()
            value = mutation({"collection_uuid": identity, "expected_revision": 0,
                "expected_collection_revision": 1, "definition": deepcopy(new_definition),
                "reason": source_plan["intent"]["reason"]})
        elif (current is None or current["collection_revision"] != before["revision"]
              or current["definition"]["enabled"] is not True):
            # Existing disabled/missing policies require a deliberate choice.
            raise conflict()
        entries.append({"before": current, "input": value})
    return validate(entries, source_plan)


def validate(entries, source_plan):
    validate_plan(source_plan)
    if source_plan["intent"]["operation"] != "ensure" or not isinstance(entries, list) or len(entries) != len(source_plan["entries"]):
        raise InvalidData("Metadata policy plan does not cover the registration plan")
    for entry, source in zip(entries, source_plan["entries"], strict=True):
        if not isinstance(entry, dict) or set(entry) != {"before", "input"}:
            raise InvalidData("Invalid saved source metadata policy")
        current, value = entry["before"], entry["input"]
        before, create = source["before"], source["input"]
        if before is None:
            mutation(value)
            if (current is not None or value["collection_uuid"] != create["uuid"]
                    or value["expected_collection_revision"] != 1
                    or value["reason"] != source_plan["intent"]["reason"]):
                raise InvalidData("New metadata policy changed its source binding")
        else:
            policy(current, before["uuid"])
            if (value is not None or current is None or current["collection_revision"] != before["revision"]
                    or current["definition"]["enabled"] is not True):
                raise InvalidData("Existing metadata policy must retain its reviewed definition")
    return entries


def inspect(client, entries, source_plan, *, apply=False):
    validate(entries, source_plan)
    if client.endpoint != source_plan["endpoint"]:
        raise InvalidData("Metadata policy plan belongs to another endpoint")
    states, pending = [], []
    for index, entry in enumerate(entries):
        before, value = entry["before"], entry["input"]
        identity = before["collection_uuid"] if before else value["collection_uuid"]
        try:
            receipt = client.recover(value) if value is not None else before
            current = client.policy(identity)
            if current != receipt:
                raise conflict()
        except Unavailable as error:
            if error.status != 409:
                raise
            states.append("changed")
            continue
        states.append("pending" if receipt is None else "completed" if value is not None else "unchanged")
        if receipt is None:
            pending.append(index)
    if apply and pending and "changed" not in states:
        for index in pending:
            client.apply(entries[index]["input"])
        return inspect(client, entries, source_plan)
    return {"states": states, "needs_review": "changed" in states, "pending": bool(pending),
            "complete": not (pending or "changed" in states)}
