"""Generic Stash subscriptions; website settings and scheduling stay in the worker."""

from .client import Unavailable
from .collections import target_url
from .encoding import InvalidData, encode, identifier
from .launcher_inputs import reddit_urls
from .profile_sources import PROFILE


class NoSubscriptions(Exception):
    pass


def subscribed_targets(client, root, service, *, mode='new'):
    identifier(root)
    namespace = 'mirror:' + service + ':*' if service in ('coomer', 'kemono') else 'native:' + service
    if client.capabilities().get('source_subscriptions') is not True:
        raise Unavailable('incompatible_source_subscriptions')
    after, targets, bindings = '', [], {}
    for _ in range(100):
        page = client._request('POST', '/subscriptions/list', encode({
            'root_uuid': root, 'namespace': namespace, 'after': after}), max_response_bytes=4 << 20)
        if (not isinstance(page, dict) or not isinstance(page.get('items'), list) or len(page['items']) > 100
                or type(page.get('total')) is not int or page['total'] < 0):
            raise Unavailable('invalid_source_subscriptions')
        for item in page['items']:
            try:
                collection = item['collection']
                cid = identifier(collection['uuid'])
                revision = item['revision']
                url = target_url(collection['target_url'])
                if (cid <= after or item['enabled'] is not True or type(revision) is not int or revision < 1
                        or collection['root_uuid'] != root
                        or not isinstance(collection['namespace'], str)
                        or (not collection['namespace'].startswith(namespace[:-1]) if namespace.endswith('*') else collection['namespace'] != namespace)
                        or collection['state'] != 'active'):
                    raise InvalidData('Mismatched subscription')
                urls = [url]
                if service == 'reddit' and (profile := PROFILE.fullmatch(url)):
                    urls = reddit_urls([profile[1]], [], mode=mode)
                for retrieval in urls:
                    binding = {'collection_uuid': cid, 'revision': revision}
                    if retrieval in bindings and bindings[retrieval] != binding:
                        raise InvalidData('Ambiguous subscribed source URL')
                    if retrieval not in bindings:
                        targets.append(retrieval)
                    bindings[retrieval] = binding
                after = cid
            except (InvalidData, KeyError, TypeError):
                raise Unavailable('invalid_source_subscriptions') from None
        if len(page['items']) < 100:
            return targets, bindings
    raise Unavailable('source_subscription_limit')
