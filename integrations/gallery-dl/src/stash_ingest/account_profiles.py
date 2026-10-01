"""Persist profile content, not site preferences, counters or viewer state."""
import copy

ROLES = frozenset({'user', 'author', 'source_user', 'retweeted_by', 'quoted_by', 'quote_by'})
TWITTER_FIELDS = frozenset({
    'id', 'id_str', 'rest_id', 'name', 'screen_name', 'nick', 'description',
    'location', 'based_in', 'url', 'date', 'created_at', 'profile_image',
    'profile_banner', 'profile_image_url', 'profile_image_url_https', 'profile_banner_url',
})
REDDIT_FIELDS = frozenset({'id', 'name', 'created', 'created_utc', 'icon_img', 'snoovatar_img'})
REDDIT_PAGE_FIELDS = frozenset({
    'id', 'name', 'display_name', 'display_name_prefixed', 'title', 'url',
    'description', 'public_description', 'icon_img', 'banner_img', 'header_img', 'community_icon',
})


def profile(value, platform):
    if platform == 'reddit':
        result = {k: copy.deepcopy(v) for k, v in value.items() if k in REDDIT_FIELDS}
        if 'created_utc' in result:
            result.pop('created', None)
        if isinstance(value.get('subreddit'), dict):
            result['subreddit'] = {k: copy.deepcopy(v) for k, v in value['subreddit'].items() if k in REDDIT_PAGE_FIELDS}
        return result
    result = {k: copy.deepcopy(v) for k, v in value.items() if k in TWITTER_FIELDS}
    # metadata-user-original can expose the untransformed GraphQL user object.
    for section, fields in (('legacy', TWITTER_FIELDS), ('core', {'name', 'screen_name', 'created_at'}),
                            ('avatar', {'image_url'})):
        if isinstance(value.get(section), dict):
            result[section] = {k: copy.deepcopy(v) for k, v in value[section].items() if k in fields}
            if section == 'legacy' and isinstance(value[section].get('entities'), dict):
                result[section]['entities'] = {k: copy.deepcopy(v) for k, v in value[section]['entities'].items() if k in {'url', 'description'}}
    if isinstance(value.get('entities'), dict):
        result['entities'] = {k: copy.deepcopy(v) for k, v in value['entities'].items() if k in {'url', 'description'}}
    return result


def retain(value, platform=None):
    """Filter known account objects on a copy before observation IDs are made."""
    if isinstance(value, list):
        return [retain(v, platform) for v in value]
    if not isinstance(value, dict):
        return value
    platform = value.get('category', platform)
    result = {}
    for key, child in value.items():
        site = 'reddit' if key == '_reddit' else platform
        if site in {'reddit', 'twitter'} and key in ROLES and isinstance(child, dict):
            result[key] = profile(child, site)
        else:
            result[key] = retain(child, site)
    return result
