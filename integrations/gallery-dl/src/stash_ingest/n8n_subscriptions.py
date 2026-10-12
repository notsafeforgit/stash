"""n8n source operations using Stash subscriptions as the sole target list."""

from . import source_accounts, source_management, source_policy, subscription_management as subscriptions
from .catalog_upload import read_regular
from .encoding import InvalidData, digest, identifier


def prepare(app, policies, producer, runtime, value, directory, call):
    from .n8n_sources import load, save, source_target, target_urls
    platform, action = value['platform'], value['action']
    guards, source_plan, policy_entries = [], None, []
    if action == 'remove-performer':
        entity = app.request('GET', '/entity-identities/performer/' + value['identity'])
        if entity.get('kind') != 'performer':
            raise InvalidData('Performer identity changed')
        performer = identifier(entity['uuid'])
        items = subscriptions.listing(app, root=runtime.root, namespace='native:' + platform, performer=performer)
        for cid in sorted({item['collection']['account_uuid'] for item in items if item['enabled']}):
            row = source_accounts.account(app.request('GET', '/source-accounts/' + cid))
            guards.append(source_accounts.owner(row, performer))
    else:
        ref = ('id' if platform == 'twitter' else 'handle', value['identity'])
        account = source_accounts.lookup(app, platform, ref)
        items = subscriptions.listing(app, root=runtime.root, namespace='native:' + platform,
                                      account=account['uuid']) if account else []
        if not items:
            target = source_target(target_urls(platform, [ref])[0], platform, runtime.root, None if account is None else account['uuid'])
            spec = {'operation': 'ensure' if action == 'add' else 'disable', 'root_uuid': runtime.root,
                    'reason': 'n8n subscription ' + call, 'targets': [target]}
            path = directory / 'sources.json'
            if not path.exists():
                source_management.prepare(app, producer, spec, path)
            source_plan = source_management.validate_plan(load(path))
            if source_plan['intent'] != spec:
                raise source_management.conflict()
            entry = source_plan['entries'][0]
            if entry['before'] is not None:
                items = subscriptions.listing(app, collection=entry['before']['uuid'])
            elif action == 'add':
                items = [{'collection': {**target, 'uuid': entry['input']['uuid'], 'revision': 1,
                           'created_at': '1970-01-01T00:00:00Z'}, 'revision': 0, 'enabled': False}]
            if action == 'add':
                policy_entries = source_policy.prepare(policies, source_plan, runtime.policy)
            else:
                # Lookup only: stopping monitoring never disables a definition.
                source_plan = None
        elif action == 'add':
            # One existing account may have stable ID and handle URLs. Subscribe
            # its existing sources, without creating another URL definition.
            if any(item['collection']['state'] != 'active' for item in items):
                raise source_management.conflict()
    changes = [subscriptions.change(item, action == 'add') for item in items if item['enabled'] != (action == 'add')]
    plan = {'request': value, 'source_plan': source_plan, 'policies': policy_entries, 'ownership': guards, 'changes': changes}
    save(directory / 'plan.json', plan)
    return plan


def apply(app, policies, producer, runtime, value, directory, plan):
    from .n8n_sources import MAX_STATE_BYTES, save
    if (not isinstance(plan, dict) or set(plan) != {'request', 'source_plan', 'policies', 'ownership', 'changes'}
            or plan['request'] != value or not isinstance(plan['changes'], list) or len(plan['changes']) > 10000):
        raise InvalidData('Subscription plan does not match this workflow request')
    source_accounts.verify(app, plan['ownership'])
    if plan['source_plan'] is not None:
        if not source_management.inspect(app, producer, plan['source_plan'], apply=True)['complete']:
            raise source_management.conflict()
        if not source_policy.inspect(policies, plan['policies'], plan['source_plan'], apply=True)['complete']:
            raise source_management.conflict()
    for change in plan['changes']:
        subscriptions.apply(app, change)
    result = {'complete': True, 'status': 'registered' if value['action'] == 'add' else 'removed',
              'source_targets': len(plan['changes']), 'removedCount': len(plan['changes']) if value['action'] != 'add' else 0,
              'added': value['action'] == 'add' and bool(plan['changes']), 'scrape_completion': 'not_checked',
              'removedUsernames': [], 'removedIds': [], 'resolvedId': value['identity'] if value['platform'] == 'twitter' and value['action'] != 'remove-performer' else '',
              'message': 'Updated automatic monitoring in Stash; existing media and manual imports are preserved.'}
    save(directory / 'receipt.json', {'request': value, 'plan_sha256': digest(read_regular(directory / 'plan.json', MAX_STATE_BYTES)), 'result': result})
    return result
