"""Strict local single-part S3 transport; no network or SDK credentials."""

import base64
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
                             "ETag": '"' + hashlib.md5(body, usedforsecurity=False).hexdigest() + '"'}
        if Key in self.lost_reply:
            raise OSError("lost upload reply")
        return {}

    def get_object(self, *, Bucket, Key, ChecksumMode):
        assert ChecksumMode == "ENABLED"
        self.operations.append(("get", Key))
        body = (self.root / Key).read_bytes()
        return {"Body": io.BytesIO(body + b"changed" if self.corrupt_read else body)}
