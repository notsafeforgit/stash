"""Source HTTP permits and counters; media sessions do not use this adapter."""

from datetime import datetime, timezone
from email.utils import parsedate_to_datetime
import math
from urllib.parse import urlsplit
import uuid

from .encoding import InvalidData, encode
from .runs import SourcePaused, source_scope


def operation(url):
    path = urlsplit(url).path.lower()
    if any(value in path for value in ('usermedia', 'usertweets', 'searchtimeline', '/submitted', '/search', '/timeline', '/.json')) and '/comments/' not in path:
        return 'listing'
    if any(value in path for value in ('userbyscreenname', 'userbyrestid', 'about.json', '/users/show')):
        return 'profile'
    if any(value in urlsplit(url).hostname for value in ('redgifs', 'imgur')):
        return 'media-resolution'
    return 'other'


def retry_after(response):
    headers = response.headers
    delay = 0
    now = datetime.now(timezone.utc)
    value = headers.get('Retry-After', '')
    if value:
        try:
            delay = float(value)
        except (ValueError, TypeError):
            try:
                delay = (parsedate_to_datetime(value) - now).total_seconds()
            except (ValueError, TypeError, OverflowError):
                pass
    for prefix in ('x-rate-limit-', 'x-ratelimit-'):
        try:
            if float(headers.get(prefix + 'remaining', '1')) <= 0:
                reset = float(headers[prefix + 'reset'])
                # Twitter uses an epoch; Reddit returns remaining seconds.
                delay = max(delay, reset - now.timestamp() if prefix == 'x-rate-limit-' else reset)
        except (ValueError, TypeError, KeyError, OverflowError):
            pass
    if response.status_code == 429:
        delay = max(60, delay)
    return min(86400, max(0, math.ceil(delay))) if math.isfinite(delay) else 60


def request(producer, url):
    lease = producer.lease
    request_id = str(uuid.uuid4())
    while True:
        producer.check()
        with lease.lock:
            result = lease.client._request('POST', '/runs/' + lease.run_uuid + '/requests', encode({
                'owner_uuid': lease.owner, 'fence': lease.run['fence'], 'request_uuid': request_id,
                'url': url, 'class': producer.scan_mode, 'operation': operation(url)}, 16384))
        if (not isinstance(result, dict) or result.get('request_uuid') != request_id
                or not source_scope(result.get('source_scope')) or type(result.get('ready')) is not bool
                or type(result.get('wait_ms')) is not int or not 0 <= result['wait_ms'] <= 30000):
            raise InvalidData('Invalid shared source request permit')
        if result['ready']:
            producer.check()
            return result
        # Keep heartbeats, cancellation and the useful-work turn responsive.
        if lease.stop.wait(max(.05, result['wait_ms'] / 1000)):
            raise SourcePaused('Source worker stopped while awaiting request budget')


def report(producer, permit, response=None):
    lease = producer.lease
    lease.check()
    with lease.lock:
        result = lease.client._request('POST', '/runs/' + lease.run_uuid + '/requests/result', encode({
            'owner_uuid': lease.owner, 'fence': lease.run['fence'], 'request_uuid': permit['request_uuid'],
            'status': 0 if response is None else response.status_code,
            'retry_after_seconds': 0 if response is None else retry_after(response)}, 4096))
    if result != {'recorded': True}:
        raise InvalidData('Invalid source request acknowledgement')


class MetadataBudget:
    """A source reservation with HTTP callbacks for the isolated metadata child."""
    def __init__(self, lease):
        self.lease = lease

    def __call__(self, url):
        return self.lease.reserve_source(url)

    def request(self, url):
        lease, request_id = self.lease, str(uuid.uuid4())
        while True:
            with lease.lock:
                lease.check()
                result = lease.client.request_source(lease.job, request_id, url)
                lease.check()
            if result['ready']:
                return result
            if result['wait_ms'] == 0:
                return None
            if lease.stop.wait(result['wait_ms'] / 1000):
                raise SourcePaused('Metadata worker stopped while awaiting idle budget')

    def report(self, result):
        with self.lease.lock:
            self.lease.check()
            self.lease.client.report_source(self.lease.job, result)
