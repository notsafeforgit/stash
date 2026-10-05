"""Strict local single-part S3 transport; no network or SDK credentials."""

import base64
from datetime import datetime, timedelta, timezone
import hashlib
import io



class S3Error(RuntimeError):
    def __init__(self, code):
        super().__init__(code)
        self.response = {"Error": {"Code": code}}


class FakeS3:
    def __init__(self, root):
        self.root = root
        self.headers = {}
        self.operations = []
        self.before_put = lambda key: None
        self.lost_reply = set()
        self.head_failure = None
        self.corrupt_read = False
        self.list_failure = None
        self.page_size = 1000

    def list_objects_v2(self, *, Bucket, Prefix, MaxKeys, ContinuationToken=None):
        self.operations.append(("list", Prefix))
        if self.list_failure:
            raise S3Error(self.list_failure)
        keys = sorted(key for key in self.headers if key.startswith(Prefix) and (self.root / key).is_file())
        start = int(ContinuationToken or 0)
        selected = keys[start:start + min(MaxKeys, self.page_size)]
        end = start + len(selected)
        result = {"IsTruncated": end < len(keys), "Contents": [
            {"Key": key, "Size": self.headers[key]["ContentLength"],
             "ETag": self.headers[key]["ETag"], "LastModified": self.headers[key]["LastModified"],
             "StorageClass": self.headers[key]["StorageClass"]} for key in selected]}
        if result["IsTruncated"]:
            result["NextContinuationToken"] = str(end)
        return result

    def head_object(self, *, Bucket, Key, ChecksumMode):
        assert ChecksumMode == "ENABLED"
        self.operations.append(("head", Key))
        if self.head_failure:
            raise S3Error(self.head_failure)
        if not (self.root / Key).is_file():
            raise S3Error("404")
        return dict(self.headers[Key])

    def put_object(self, *, Bucket, Key, Body, ContentLength, StorageClass, ChecksumSHA256, IfNoneMatch=None, IfMatch=None):
        self.operations.append(("put", Key))
        self.before_put(Key)
        path = self.root / Key
        if IfNoneMatch == "*" and path.exists():
            raise S3Error("PreconditionFailed")
        if IfMatch is not None and (not path.exists() or self.headers[Key]['ETag'] != IfMatch):
            raise S3Error("PreconditionFailed")
        body = Body.read() if hasattr(Body, "read") else Body
        assert ContentLength == len(body)
        assert ChecksumSHA256 == base64.b64encode(hashlib.sha256(body).digest()).decode("ascii")
        assert StorageClass == "STANDARD"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)
        self.headers[Key] = {"ContentLength": len(body), "ChecksumSHA256": ChecksumSHA256,
                             "ChecksumType": "FULL_OBJECT", "StorageClass": StorageClass,
                             "LastModified": datetime(2026, 10, 5, tzinfo=timezone.utc) + timedelta(seconds=len(self.operations)),
                             "ETag": '"' + hashlib.md5(body, usedforsecurity=False).hexdigest() + '"'}
        if Key in self.lost_reply:
            raise OSError("lost upload reply")
        return {}

    def get_object(self, *, Bucket, Key, ChecksumMode):
        assert ChecksumMode == "ENABLED"
        self.operations.append(("get", Key))
        body = (self.root / Key).read_bytes()
        return {"Body": io.BytesIO(body + b"changed" if self.corrupt_read else body)}


class FakeColdS3(FakeS3):
    """rclone writes local files; S3 exposes independently computed CRC metadata."""
    def __init__(self, root):
        super().__init__(root)
        self.signatures = {}

    def reconcile(self, key):
        from awscrt.checksums import crc64nvme
        path = self.root / key
        if not path.is_file():
            return
        info = path.stat()
        signature = (info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)
        if self.signatures.get(key) == signature:
            return
        body = path.read_bytes()
        self.headers[key] = {'ContentLength': len(body), 'ChecksumType': 'FULL_OBJECT',
                             'ChecksumCRC64NVME': base64.b64encode(crc64nvme(body).to_bytes(8, 'big')).decode(),
                             'ETag': '"' + hashlib.md5(body, usedforsecurity=False).hexdigest() + '-2"',
                             'LastModified': datetime.fromtimestamp(info.st_mtime, timezone.utc),
                             'StorageClass': 'DEEP_ARCHIVE'}
        self.signatures[key] = signature

    def list_objects_v2(self, **args):
        for path in self.root.rglob('*'):
            if path.is_file():
                self.reconcile(path.relative_to(self.root).as_posix())
        return super().list_objects_v2(**args)

    def head_object(self, **args):
        self.reconcile(args['Key'])
        return super().head_object(**args)

    def get_object(self, **args):
        raise AssertionError('Backup must not download cold objects')

    def restore_object(self, **args):
        raise AssertionError('Backup must not thaw cold objects')

    def put_object(self, **args):
        raise AssertionError('Bulk uploads must use the exercised rclone transport')
