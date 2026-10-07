"""Resolve source-list removals through current native account ownership."""

from urllib.parse import urlencode

from .activation_client import integer
from .client import Unavailable
from .encoding import InvalidData, identifier
from .source_lists import account as list_account, key
from .source_management import conflict


def account(value, namespace=None):
    if not isinstance(value, dict):
        raise InvalidData("Invalid native source account")
    identity = identifier(value.get("uuid"))
    if value.get("canonical_uuid") != identity or value.get("redirect_to") is not None:
        raise InvalidData("Source lookup must return canonical accounts")
    integer(value.get("revision"), 1, 2147483647)
    if not isinstance(value.get("namespace"), str) or (namespace is not None and value["namespace"] != namespace):
        raise InvalidData("Source account belongs to another service")
    if type(value.get("more_identifiers")) is not bool or not isinstance(value.get("identifiers"), list):
        raise InvalidData("Invalid source identifier summary")
    return value


def lookup(client, platform, reference):
    namespace = "native:" + platform
    kind, value = reference
    query = urlencode({"namespace": namespace, "kind": kind, "value": value, "limit": 2})
    result = client.request("GET", "/source-accounts/lookup?" + query)
    if not isinstance(result, list) or len(result) > 2:
        raise Unavailable("invalid_source_account_lookup")
    rows = [account(row, namespace) for row in result]
    if len(rows) > 1:
        raise conflict()
    return rows[0] if rows else None


def identifiers(client, row):
    if row["more_identifiers"]:
        rows, after = [], ""
        for _ in range(11):
            page = client.request("GET", f"/source-accounts/{row['uuid']}/identifiers?after={after}&limit=100")
            if not isinstance(page, list) or len(page) > 100:
                raise Unavailable("invalid_source_identifier_page")
            for item in page:
                if not isinstance(item, dict):
                    raise Unavailable("invalid_source_identifier")
                identity = identifier(item.get("uuid"))
                if identity <= after:
                    raise Unavailable("invalid_source_identifier_cursor")
                after = identity
            rows.extend(page)
            if len(rows) > 1000:
                raise InvalidData("Source account has too many identifiers for this operation")
            if len(page) < 100:
                break
        else:
            raise InvalidData("Source identifier pagination did not finish")
    else:
        rows = row["identifiers"]
    result = []
    for item in rows:
        if not isinstance(item, dict):
            raise Unavailable("invalid_source_identifier")
        identifier(item.get("uuid"))
        identifier(item.get("account_uuid"))
        ref = item.get("reference")
        if (not isinstance(ref, dict) or set(ref) != {"namespace", "kind", "value"}
                or ref["namespace"] != row["namespace"] or not isinstance(ref["value"], str)):
            raise Unavailable("invalid_source_identifier")
        if ref["kind"] in {"id", "handle"}:
            result.append((ref["kind"], ref["value"]))
    return result


def owner(row, performer_uuid):
    ownership = row.get("ownership")
    if (not isinstance(ownership, dict) or ownership.get("state") != "linked"
            or not isinstance(ownership.get("performer"), dict)
            or ownership["performer"].get("uuid") != performer_uuid):
        raise conflict()
    return {"uuid": row["uuid"], "revision": row["revision"], "namespace": row["namespace"],
            "decision_uuid": identifier(ownership.get("decision_uuid")), "performer_uuid": performer_uuid}


def owned_list_accounts(client, platform, local_id, body):
    if not isinstance(local_id, str) or not local_id.isascii() or not local_id.isdigit() or not 1 <= int(local_id) <= 2147483647:
        raise InvalidData("Choose a valid Stash performer ID")
    entity = client.request("GET", f"/entity-identities/performer/{int(local_id)}")
    if (not isinstance(entity, dict) or entity.get("kind") != "performer"
            or entity.get("local_id") != int(local_id)):
        raise Unavailable("invalid_performer_identity")
    requested = identifier(entity.get("uuid"))
    rows, after, performer = [], "", None
    for _ in range(11):
        page = client.request("GET", f"/entities/{requested}/source-accounts?after={after}&limit=100")
        if (not isinstance(page, dict) or page.get("requested_uuid") != requested
                or not isinstance(page.get("accounts"), list) or len(page["accounts"]) > 100
                or not isinstance(page.get("performer"), dict)):
            raise Unavailable("invalid_performer_source_accounts")
        current = page["performer"]
        identifier(current.get("uuid"))
        integer(current.get("revision"), 1, 2147483647)
        if current.get("state") != "active" or (performer is not None and current != performer):
            raise conflict()
        performer = current
        for value in page["accounts"]:
            value = account(value)
            if value["uuid"] <= after:
                raise Unavailable("invalid_performer_account_cursor")
            after = value["uuid"]
            owner(value, performer["uuid"])
            rows.append(value)
        if len(rows) > 1000:
            raise InvalidData("Performer has too many accounts for one source operation")
        if len(page["accounts"]) < 100:
            break
    else:
        raise InvalidData("Performer account pagination did not finish")
    relevant = [row for row in rows if row["namespace"] == "native:" + platform]
    if not relevant:
        raise Unavailable("performer_has_no_linked_source_accounts", 409)
    candidates = {}
    for row in relevant:
        for ref in identifiers(client, row):
            candidates.setdefault(key(ref), []).append(row)
    try:
        lines = body.decode("utf-8").splitlines()
    except UnicodeError:
        raise InvalidData("Saved source list must be UTF-8") from None
    selected, guards, checked = [], {}, {}
    for line in lines:
        ref = list_account(platform, line)
        if ref is None or key(ref) not in candidates:
            continue
        possible = {row["uuid"]: row for row in candidates[key(ref)]}
        if len(possible) != 1:
            raise conflict()
        if key(ref) not in checked:
            checked[key(ref)] = lookup(client, platform, ref)
        found = checked[key(ref)]
        if found is None or found["uuid"] not in possible:
            raise conflict()
        guard = owner(found, performer["uuid"])
        if guard != owner(possible[found["uuid"]], performer["uuid"]):
            raise conflict()
        guards[found["uuid"]] = guard
        selected.append(ref)
    return {"performer": performer, "guards": list(guards.values()), "references": selected}


def verify(client, guards):
    for guard in guards:
        row = account(client.request("GET", "/source-accounts/" + identifier(guard["uuid"])), guard["namespace"])
        if owner(row, guard["performer_uuid"]) != guard:
            raise conflict()
