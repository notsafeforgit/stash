"""Manage generic monitoring intent without editing downloader target files."""

from urllib.parse import urlencode

from .activation_client import integer
from .encoding import InvalidData, identifier
from .source_management import collection, conflict


def validate(value):
    if not isinstance(value, dict) or set(value) != {'collection', 'revision', 'enabled'}:
        raise InvalidData('Invalid source subscription')
    collection(value['collection'])
    integer(value['revision'], 0, 2147483647)
    if type(value['enabled']) is not bool:
        raise InvalidData('Invalid monitoring state')
    return value


def listing(app, **filters):
    after, result = '', []
    for _ in range(100):
        page = app.request('GET', '/subscriptions?' + urlencode({**filters, 'after': after, 'limit': 100}))
        if (not isinstance(page, dict) or not isinstance(page.get('items'), list) or len(page['items']) > 100
                or type(page.get('total')) is not int or page['total'] < 0):
            raise InvalidData('Invalid source subscription page')
        for item in page['items']:
            validate(item)
            current = item['collection']
            if current['uuid'] <= after or any(current[key] != filters[query]
                    for query, key in (('root', 'root_uuid'), ('namespace', 'namespace'), ('collection', 'uuid')) if query in filters):
                raise InvalidData('Subscription listing changed scope or cursor')
            after = current['uuid']
            result.append(item)
        if len(page['items']) < 100:
            return result
    raise InvalidData('Too many source subscriptions for one operation')


def change(item, enabled):
    validate(item)
    return {'collection_uuid': item['collection']['uuid'], 'expected_revision': item['revision'],
            'before_enabled': item['enabled'], 'enabled': enabled}


def apply(app, value):
    if not isinstance(value, dict) or set(value) != {'collection_uuid', 'expected_revision', 'before_enabled', 'enabled'}:
        raise InvalidData('Invalid subscription change')
    cid = identifier(value['collection_uuid'])
    revision = integer(value['expected_revision'], 0, 2147483646)
    if any(type(value[k]) is not bool for k in ('enabled', 'before_enabled')):
        raise InvalidData('Invalid subscription change')
    rows = listing(app, collection=cid)
    if len(rows) != 1:
        raise conflict()
    current = rows[0]
    completed_revision = revision + (value['before_enabled'] != value['enabled'])
    if current['revision'] == completed_revision and current['enabled'] == value['enabled']:
        return current  # Lost response; no repeated change after a later owner edit.
    if current['revision'] != revision or current['enabled'] != value['before_enabled']:
        raise conflict()
    result = validate(app.request('PUT', f'/collections/{cid}/subscription',
        {'expected_revision': revision, 'enabled': value['enabled']}))
    if result['collection']['uuid'] != cid or result['revision'] != completed_revision or result['enabled'] != value['enabled']:
        raise InvalidData('Subscription response differs from the request')
    return result
