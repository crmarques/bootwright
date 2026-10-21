"""The object bucket worker: one bucket of one object user this operation created, created and given its policy through S3 signed with
Signature Version 4 under the user's key pair, which never leaves the worker's memory, and proved with radosgw-admin.

The signer is held to the AWS Signature Version 4 test suite (Apache-2.0, as botocore vendors it in tests/unit/auth/aws4_testsuite: key
AKIDEXAMPLE, secret wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY, region us-east-1, service `service`, 20150830T123600Z). The scripted RGW is a
loopback HTTP server that verifies each signature independently, as src/rgw/rgw_auth_s3.cc:941-990 does, and answers the v20.2.1 shapes:
- CreateBucket stores the bucket for its owner and answers 409 BucketAlreadyExists for another owner's (src/rgw/rgw_op.cc:3873-3935);
- PutBucketPolicy stores the bytes sent or answers 400 MalformedPolicy (src/rgw/rgw_op.cc:8803-8840); GetBucketPolicy answers them or
  404 NoSuchBucketPolicy (:8878-8905); a suspended bucket answers 403 UserSuspended (:387-400);
- `bucket list` prints a JSON array of names and `bucket stats` the bucket, tenant, zonegroup ID, id and owner
  (src/rgw/driver/rados/rgw_bucket.cc:1514-1583, :1868-1887).
"""

from __future__ import annotations

import hashlib
import hmac
import http.server
import json
import re
import threading

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_object, ceph_tunnel_staging
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_users as users

native_boundaries = gateway.native_boundaries
root = users.root
ACCESS, SECRET = users.ACCESS, users.SECRET
POLICY = '{"Statement":[{"Action":["s3:GetObject"],"Effect":"Allow","Resource":["arn:aws:s3:::app-data/*"]}],"Version":"2012-10-17"}'
BUCKET = {"backend": "rgw.objects", "certificateRef": "", "endpoint": "", "gateway": "objects", "name": "app-data", "owner": "app-writer",
          "policy": POLICY, "port": 8080, "realm": "objects", "zone": "objects", "zoneGroup": "objects"}

SUITE = {
    "get-vanilla": ("GET", "/", "", {"Host": "example.amazonaws.com", "X-Amz-Date": "20150830T123600Z"}, b"",
                    "host;x-amz-date", "5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"),
    "get-vanilla-empty-query-key": ("GET", "/", "Param1=value1", {"Host": "example.amazonaws.com", "X-Amz-Date": "20150830T123600Z"}, b"",
                                    "host;x-amz-date", "a67d582fa61cc504c4bae71f336f98b97f1ea3c7a6bfe1b6e45aec72011b9aeb"),
    "get-vanilla-query-order-key-case": ("GET", "/", "Param2=value2&Param1=value1", {"Host": "example.amazonaws.com", "X-Amz-Date": "20150830T123600Z"},
                                         b"", "host;x-amz-date", "b97d918cfa904a5beff61c982a1b6f458b799221646efd99d3219ec94cdf2500"),
    "get-header-value-trim": ("GET", "/", "", {"Host": "example.amazonaws.com", "My-Header1": " value1", "My-Header2": ' "a   b   c"',
                                               "X-Amz-Date": "20150830T123600Z"}, b"",
                              "host;my-header1;my-header2;x-amz-date", "acc3ed3afb60bb290fc8d2dd0098b9911fcaa05412b367055dee359757a9c736"),
    "post-vanilla": ("POST", "/", "", {"Host": "example.amazonaws.com", "X-Amz-Date": "20150830T123600Z"}, b"",
                     "host;x-amz-date", "5da7c1a2acd57cee7505fc6676e4e544621c30862966e37dddb68e92efbe5d6b"),
    "post-header-key-sort": ("POST", "/", "", {"Host": "example.amazonaws.com", "My-Header1": "value1", "X-Amz-Date": "20150830T123600Z"}, b"",
                             "host;my-header1;x-amz-date", "c5410059b04c1ee005303aed430f6e6645f61f4dc9e1461ec8f8916fdf18852c"),
    "post-x-www-form-urlencoded": ("POST", "/", "", {"Content-Type": "application/x-www-form-urlencoded", "Host": "example.amazonaws.com",
                                                     "X-Amz-Date": "20150830T123600Z"}, b"Param1=value1",
                                   "content-type;host;x-amz-date", "ff11897932ad3f4e8b18135d722051e5ac45fc38421b1da7b9d196a0fe09473a"),
}
SUITE_PAIR = {"accessKey": "AKIDEXAMPLE", "secretKey": "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}
GET_VANILLA_CREQ = "GET\n/\n\nhost:example.amazonaws.com\nx-amz-date:20150830T123600Z\n\nhost;x-amz-date\n" + hashlib.sha256(b"").hexdigest()


@pytest.mark.parametrize("name", sorted(SUITE))
def test_signer_matches_the_aws_signature_v4_test_suite(name):
    method, path, query, headers, body, signed, signature = SUITE[name]
    value = ceph_native_object.sigv4(method, path, query, headers, hashlib.sha256(body).hexdigest(), SUITE_PAIR, "us-east-1", "service", "20150830T123600Z")
    assert value == ("AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, SignedHeaders=" + signed
                     + ", Signature=" + signature)


def test_canonical_request_is_the_suites():
    request, signed = ceph_native_object.canonical_request("GET", "/", "", SUITE["get-vanilla"][3], hashlib.sha256(b"").hexdigest())
    assert request == GET_VANILLA_CREQ and signed == "host;x-amz-date"
    assert ceph_native_object.canonical_query("policy") == "policy="


def verify(handler, body, secrets):
    """RGW's check, written apart from the worker: the canonical request from the signed headers as received, the payload hash, then the
    signature under the key the credential names."""
    found = re.fullmatch(r"AWS4-HMAC-SHA256 Credential=([^/]+)/(\d{8})/([^/]+)/s3/aws4_request, SignedHeaders=([a-z0-9;-]+), Signature=([0-9a-f]{64})",
                         handler.headers.get("Authorization", ""))
    if found is None or found[1] not in secrets or handler.headers.get("x-amz-content-sha256") != hashlib.sha256(body).hexdigest():
        return False
    signed = found[4].split(";")
    if not {"host", "x-amz-date", "x-amz-content-sha256"} <= set(signed) or signed != sorted(signed):
        return False
    path, unused, query = handler.path.partition("?")
    lines = [handler.command, path, "&".join(sorted(part if "=" in part else part + "=" for part in query.split("&") if part))]
    lines += [name + ":" + " ".join(handler.headers[name].split()) for name in signed]
    stamp = handler.headers["x-amz-date"]
    request = "\n".join(lines + ["", found[4], handler.headers["x-amz-content-sha256"]])
    scope = found[2] + "/" + found[3] + "/s3/aws4_request"
    text = "\n".join(("AWS4-HMAC-SHA256", stamp, scope, hashlib.sha256(request.encode()).hexdigest()))
    key = ("AWS4" + secrets[found[1]]).encode()
    for part in (found[2], found[3], "s3", "aws4_request"):
        key = hmac.new(key, part.encode(), hashlib.sha256).digest()
    return stamp[:8] == found[2] and found[3] == "objects" and hmac.compare_digest(hmac.new(key, text.encode(), hashlib.sha256).hexdigest(), found[5])


class Gateway:
    """The scripted RGW behind one loopback rgw daemon, sharing its bucket table with the cluster's radosgw-admin."""

    def __init__(self, cluster, address=("127.0.0.1", 0)):
        self.cluster, self.requests, self.raw, self.suspended, self.drop = cluster, [], [], False, False
        outer = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def handle_one_request(self):
                self.raw_requestline = self.rfile.readline(65537)
                outer.raw.append(self.raw_requestline)
                if not self.raw_requestline or not self.parse_request():
                    return
                body = self.rfile.read(int(self.headers.get("content-length", "0")))
                outer.raw.append(bytes(self.headers) + body)
                outer.answer(self, body)

            def log_message(self, *args):
                return

        self.server = http.server.HTTPServer(address, Handler)
        self.port = self.server.server_address[1]
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def answer(self, handler, body):
        path, unused, query = handler.path.partition("?")
        self.requests.append((handler.command, path, query, body))
        secrets = {entry["access_key"]: entry["secret_key"] for user in self.cluster.users.values() for entry in user["keys"]}
        owners = {entry["access_key"]: user["user_id"] for user in self.cluster.users.values() for entry in user["keys"]}
        if not verify(handler, body, secrets):
            return self.reply(handler, 403, "SignatureDoesNotMatch")
        if self.drop:
            self.cluster.buckets.setdefault(path[1:], self.cluster.bucket(path[1:], owners[ACCESS]))
            return None
        name, owner = path[1:], owners[re.search(r"Credential=([^/]+)/", handler.headers["Authorization"])[1]]
        stored = self.cluster.buckets.get(name)
        if handler.command == "PUT" and query == "":
            if stored is not None and stored["owner"] != owner:
                return self.reply(handler, 409, "BucketAlreadyExists")
            self.cluster.buckets.setdefault(name, self.cluster.bucket(name, owner))
            return self.reply(handler, 200)
        if stored is None:
            return self.reply(handler, 404, "NoSuchBucket")
        if self.suspended:
            return self.reply(handler, 403, "UserSuspended")
        if handler.command == "PUT" and query == "policy":
            try:
                valid = isinstance(json.loads(body), dict)
            except ValueError:
                valid = False
            if not valid:
                return self.reply(handler, 400, "MalformedPolicy", "Policy has invalid principal")
            stored["policy"] = body
            return self.reply(handler, 204)
        if handler.command == "GET" and query == "policy":
            if not stored["policy"]:
                return self.reply(handler, 404, "NoSuchBucketPolicy", "The bucket policy does not exist")
            return self.reply(handler, 200, data=stored["policy"])
        raise AssertionError((handler.command, path, query))

    @staticmethod
    def reply(handler, status, code="", message="", data=None):
        if data is None:
            data = ("<?xml version=\"1.0\" encoding=\"UTF-8\"?><Error><Code>" + code + "</Code><Message>" + message
                    + "</Message><RequestId>tx0</RequestId></Error>").encode() if code else b""
        handler.send_response(status)
        handler.send_header("Content-Length", str(len(data)))
        handler.end_headers()
        handler.wfile.write(data)


class Cluster(users.Cluster):
    """The user's scripted cluster that also answers `radosgw-admin bucket list` and `bucket stats`."""

    def __init__(self, request):
        super().__init__(request)
        self.buckets, self.counter = {}, 0

    def bucket(self, name, owner):
        self.counter += 1
        group = self.zonegroups["objects"]["id"]
        return {"bucket": name, "tenant": "", "versioning": "off", "zonegroup": group, "placement_rule": "default-placement",
                "id": self.zones["objects"]["id"] + ".4137." + str(self.counter), "marker": self.zones["objects"]["id"] + ".4137." + str(self.counter),
                "owner": owner, "num_shards": 11, "usage": {}, "policy": b""}

    def radosgw(self, command):
        found, values, flags = gateway.words(command)
        if found[:1] != ("bucket",):
            return super().radosgw(command)
        assert (values["--rgw-realm"], values["--rgw-zonegroup"], values["--rgw-zone"]) == ("objects", "objects", "objects") and not flags
        if found == ("bucket", "list"):
            assert set(values) == {"--rgw-realm", "--rgw-zonegroup", "--rgw-zone"}
            return 0, json.dumps(sorted(self.buckets)).encode()
        assert found == ("bucket", "stats") and values["--bucket"] in self.buckets
        return 0, json.dumps({key: value for key, value in self.buckets[values["--bucket"]].items() if key != "policy"}).encode()


def bucket_request(**members):
    request = {key: value for key, value in users.user_request().items() if key != "objectUser"}
    request.update(action="object-bucket", objectBucket=dict(BUCKET, **members))
    return request


@pytest.fixture
def ready(root):
    """The gateway's zone, the owner created by this operation, and one running rgw daemon on loopback."""
    del root
    request = gateway.gateway_request()
    markers = users.Markers(request)
    markers.write("owner.json", ceph_native.marker_identity(request))
    markers.write("bootstrap-scope.json", gateway.scope(request))
    cluster = Cluster(request)
    for phase in ("gateway_zone", "gateway_placement"):
        getattr(gateway.worker(cluster, markers), phase)()
    cluster.request = users.user_request()
    users.user_worker(cluster, markers).object_user()
    rgw = Gateway(cluster)
    cluster.request = bucket_request(port=rgw.port)
    cluster.running = [gateway.daemon(request, "node-1", ip="127.0.0.1", ports=[rgw.port])]
    cluster.mutations.clear()
    cluster.log.clear()
    yield cluster, markers, rgw
    rgw.close()


def bucket_worker(cluster, markers, tls=None):
    native = ceph_native_object.BucketWorker(cluster.request, "node-1", run=cluster.run, markers=markers)
    native.pause = lambda seconds: None
    native.tls = tls
    return native


def observe(cluster, markers, tls=None):
    native = bucket_worker(cluster, markers, tls)
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), native.blocking, evidence


def creates(rgw):
    return [entry for entry in rgw.requests if entry[0] == "PUT" and entry[2] == ""]


def intent():
    return {"owner": gateway.OWNER, "fsid": gateway.FSID, "gateway": "objects", "bucket": "app-data", "user": "app-writer",
            "realm": "objects", "zoneGroup": "objects", "zone": "objects"}


def test_bucket_is_created_once_by_its_owner_and_given_its_policy(ready):
    cluster, markers, rgw = ready
    assert bucket_worker(cluster, markers).object_bucket() == {"changed": True}
    assert [(method, path, query) for method, path, query, unused in rgw.requests] == [
        ("PUT", "/app-data", ""), ("GET", "/app-data", "policy"), ("PUT", "/app-data", "policy"), ("GET", "/app-data", "policy")]
    assert rgw.requests[2][3] == POLICY.encode() and cluster.buckets["app-data"]["owner"] == "app-writer"
    for suffix in ("intent", "attempt", "complete"):
        assert markers.read("object-bucket-" + suffix + ".app-data.json") == intent()
    assert markers.read("object-bucket-id.app-data.json") == {"id": cluster.buckets["app-data"]["id"]}
    assert bucket_worker(cluster, markers).object_bucket() == {"changed": False}
    assert len(creates(rgw)) == 1 and len([entry for entry in rgw.requests if entry[:3] == ("PUT", "/app-data", "policy")]) == 1


def test_a_bucket_without_a_policy_never_puts_one(ready):
    cluster, markers, rgw = ready
    cluster.request = bucket_request(port=rgw.port, policy="")
    assert bucket_worker(cluster, markers).object_bucket() == {"changed": True}
    assert [(method, query) for method, unused, query, unused_body in rgw.requests] == [("PUT", ""), ("GET", "policy")]
    proved, unused, evidence = observe(cluster, markers)
    assert proved and evidence["resource"] == {"id": "app-data", "path": ""}


def test_the_attempt_record_precedes_the_create(ready):
    cluster, markers, rgw = ready
    seen = []
    original = ceph_native_object.s3_exchange

    def watched(*args, **options):
        seen.append(sorted(markers.records))
        return original(*args, **options)

    ceph_native_object.s3_exchange = watched
    try:
        bucket_worker(cluster, markers).object_bucket()
    finally:
        ceph_native_object.s3_exchange = original
    assert "object-bucket-attempt.app-data.json" in seen[0] and "object-bucket-id.app-data.json" not in seen[0]


def test_a_lost_reply_refuses_and_is_recovered_under_the_attempt_record_without_a_second_create(ready):
    cluster, markers, rgw = ready
    rgw.drop = True
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-s3$") as caught:
        bucket_worker(cluster, markers).object_bucket()
    assert caught.value.native_error["cause"] == "PUT /app-data: no complete answer from the daemon at 127.0.0.1 after the request was sent"
    assert "app-data" in cluster.buckets and markers.read("object-bucket-attempt.app-data.json") == intent()
    rgw.drop = False
    assert bucket_worker(cluster, markers).object_bucket() == {"changed": True}
    assert len(creates(rgw)) == 1


def stalled(cluster, release):
    """A daemon that takes the request, creates the bucket and then never answers."""
    daemon = Gateway(cluster, ("127.0.0.2", cluster.request["objectBucket"]["port"]))

    def answer(handler, body):
        daemon.requests.append((handler.command, handler.path, "", body))
        cluster.buckets.setdefault("app-data", cluster.bucket("app-data", "app-writer"))
        release.wait(10)

    daemon.answer = answer
    return daemon


def test_a_request_the_first_daemon_took_is_never_sent_to_a_second_daemon(ready, monkeypatch):
    cluster, markers, rgw = ready
    monkeypatch.setattr(ceph_native_object, "S3_TIMEOUT", 0.5)
    release = threading.Event()
    first = stalled(cluster, release)
    try:
        cluster.running = [gateway.daemon(cluster.request, "node-1", ip="127.0.0.2", ports=[rgw.port], daemon_id="objects.node-1.aaaaaa"),
                           gateway.daemon(cluster.request, "node-2", ip="127.0.0.1", ports=[rgw.port], daemon_id="objects.node-2.bbbbbb")]
        with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-s3$") as caught:
            bucket_worker(cluster, markers).object_bucket()
        assert caught.value.native_error["cause"] == "PUT /app-data: no complete answer from the daemon at 127.0.0.2 after the request was sent"
        assert len(first.requests) == 1 and rgw.requests == [] and markers.read("object-bucket-attempt.app-data.json") == intent()
        cluster.running = cluster.running[1:]
        assert bucket_worker(cluster, markers).object_bucket() == {"changed": True}
        assert creates(rgw) == []
    finally:
        release.set()
        first.close()


def test_a_daemon_that_cannot_be_connected_to_is_passed_over(ready):
    cluster, markers, rgw = ready
    cluster.running = [gateway.daemon(cluster.request, "node-1", ip="127.0.0.2", ports=[rgw.port], daemon_id="objects.node-1.aaaaaa"),
                       gateway.daemon(cluster.request, "node-2", ip="127.0.0.1", ports=[rgw.port], daemon_id="objects.node-2.bbbbbb")]
    assert bucket_worker(cluster, markers).object_bucket() == {"changed": True}
    assert len(creates(rgw)) == 1


def test_a_foreign_bucket_refuses_before_any_write(ready):
    cluster, markers, rgw = ready
    cluster.buckets["app-data"] = cluster.bucket("app-data", "app-writer")
    before = dict(markers.records)
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-foreign$") as caught:
        bucket_worker(cluster, markers).object_bucket()
    assert caught.value.native_error["cause"] == "bucket app-data in realm objects has no creation record of this operation"
    assert markers.records == before and rgw.requests == []
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-foreign$"):
        observe(cluster, markers)


def test_a_bucket_another_user_holds_after_the_attempt_is_never_adopted(ready):
    cluster, markers, rgw = ready
    markers.write("object-bucket-attempt.app-data.json", intent())
    cluster.buckets["app-data"] = cluster.bucket("app-data", "someone")
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-foreign$") as caught:
        bucket_worker(cluster, markers).object_bucket()
    assert caught.value.native_error["cause"] == "bucket app-data in realm objects is owned by $someone, not app-writer"
    assert "object-bucket-id.app-data.json" not in markers.records and rgw.requests == []


def test_a_bucket_another_user_creates_before_the_create_refuses(ready):
    cluster, markers, rgw = ready
    original = rgw.answer

    def race(handler, body):
        cluster.buckets.setdefault("app-data", cluster.bucket("app-data", "someone"))
        return original(handler, body)

    rgw.answer = race
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-s3$") as caught:
        bucket_worker(cluster, markers).object_bucket()
    assert caught.value.native_error["cause"] == "PUT /app-data: HTTP 409 BucketAlreadyExists"
    rgw.answer = original
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-foreign$"):
        bucket_worker(cluster, markers).object_bucket()
    assert len(creates(rgw)) == 1 and "object-bucket-id.app-data.json" not in markers.records


def test_a_created_bucket_that_is_gone_refuses_and_is_never_recreated(ready):
    cluster, markers, rgw = ready
    bucket_worker(cluster, markers).object_bucket()
    del cluster.buckets["app-data"]
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-foreign$") as caught:
        bucket_worker(cluster, markers).object_bucket()
    assert caught.value.native_error["cause"] == "bucket app-data in realm objects that this operation created is gone"
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-foreign$"):
        observe(cluster, markers)
    assert len(creates(rgw)) == 1


@pytest.mark.parametrize("drift", [{"owner": "someone"}, {"tenant": "other"}, {"id": "changed.4137.9"}, {"zonegroup": "another-group"},
                                   {"policy": b'{"Version":"2012-10-17","Statement":[]}'}, {"policy": b""}])
def test_a_changed_bucket_refuses_identity(ready, drift):
    cluster, markers, rgw = ready
    bucket_worker(cluster, markers).object_bucket()
    cluster.buckets["app-data"].update(drift)
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-identity$"):
        bucket_worker(cluster, markers).object_bucket()
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-identity$"):
        observe(cluster, markers)
    assert len(creates(rgw)) == 1


def test_a_policy_the_bucket_does_not_declare_refuses_and_is_never_removed(ready):
    cluster, markers, rgw = ready
    cluster.request = bucket_request(port=rgw.port, policy="")
    markers.write("object-bucket-attempt.app-data.json", dict(intent()))
    cluster.buckets["app-data"] = dict(cluster.bucket("app-data", "app-writer"), policy=POLICY.encode())
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-identity$") as caught:
        bucket_worker(cluster, markers).object_bucket()
    assert caught.value.native_error["cause"] == "bucket app-data holds a bucket policy other than its declared one"
    assert cluster.buckets["app-data"]["policy"] == POLICY.encode() and not [entry for entry in rgw.requests if entry[0] != "GET"]


def test_rgw_refusals_name_the_request_and_its_error_without_a_key(ready):
    cluster, markers, rgw = ready
    original = rgw.answer
    rgw.answer = lambda handler, body: (rgw.reply(handler, 400, "MalformedPolicy", "Policy has invalid principal")
                                        if handler.path.endswith("?policy") and handler.command == "PUT" else original(handler, body))
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-s3$") as caught:
        bucket_worker(cluster, markers).object_bucket()
    assert caught.value.native_error["cause"] == "PUT /app-data?policy: HTTP 400 MalformedPolicy Policy has invalid principal"
    rgw.answer = original
    cluster.request = bucket_request(port=rgw.port)
    markers.records.pop("object-bucket-intent.app-data.json")
    markers.records.pop("object-bucket-attempt.app-data.json")
    markers.records.pop("object-bucket-id.app-data.json")
    del cluster.buckets["app-data"]
    bucket_worker(cluster, markers).object_bucket()
    rgw.suspended = True
    with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-s3$") as caught:
        observe(cluster, markers)
    assert caught.value.native_error["cause"].startswith("GET /app-data?policy: HTTP 403 UserSuspended")
    for key in (ACCESS, SECRET):
        assert key not in json.dumps(caught.value.native_error)


def test_the_owner_key_never_leaves_the_worker(ready):
    cluster, markers, rgw = ready
    bucket_worker(cluster, markers).object_bucket()
    unused, unused_blocking, evidence = observe(cluster, markers)
    for key in (ACCESS, SECRET):
        assert key not in json.dumps(markers.records) and key not in json.dumps(evidence)
        assert all(key not in " ".join(command) for unused_tool, command, unused_data in cluster.log)
    assert all(SECRET.encode() not in raw for raw in rgw.raw)
    assert any(ACCESS.encode() in raw for raw in rgw.raw)


def test_the_owner_must_be_a_user_this_operation_created_with_one_active_key(ready):
    cluster, markers, rgw = ready
    record = markers.records.pop("object-user-complete.app-writer.json")
    before = dict(markers.records)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        bucket_worker(cluster, markers).object_bucket()
    assert markers.records == before and rgw.requests == []
    markers.records["object-user-complete.app-writer.json"] = record
    cluster.users["app-writer"]["keys"][0]["active"] = False
    with pytest.raises(ceph_native.Refusal, match="^object-user-identity$"):
        bucket_worker(cluster, markers).object_bucket()
    assert markers.records == {**before, "object-user-complete.app-writer.json": record} and rgw.requests == []


def test_no_running_daemon_leaves_the_bucket_unready_and_names_its_backend(ready):
    cluster, markers, rgw = ready
    cluster.running = [gateway.daemon(cluster.request, "node-1", status=0, ip="127.0.0.1", ports=[rgw.port])]
    native = bucket_worker(cluster, markers)
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        native.object_bucket()
    assert native.blocking == ["http@rgw.objects"] and rgw.requests == []
    cluster.running = [gateway.daemon(cluster.request, "node-1", ip="127.0.0.1", ports=[rgw.port])]
    bucket_worker(cluster, markers).object_bucket()
    rgw.close()
    proved, blocking, evidence = observe(cluster, markers)
    assert not proved and blocking == ["http@rgw.objects"] and evidence["resource"] == {"id": "", "path": ""}
    rgw.__init__(cluster)


def test_a_daemon_answering_a_server_error_refuses_and_the_request_goes_to_no_second_daemon(ready):
    cluster, markers, rgw = ready
    failing = Gateway(cluster, ("127.0.0.2", rgw.port))
    failing.answer = lambda handler, body: failing.requests.append(handler.command) or failing.reply(handler, 503, "ServiceUnavailable")
    first = gateway.daemon(cluster.request, "node-1", ip="127.0.0.2", ports=[rgw.port], daemon_id="objects.node-1.aaaaaa")
    try:
        cluster.running = [first, gateway.daemon(cluster.request, "node-2", ip="127.0.0.1", ports=[rgw.port], daemon_id="objects.node-2.bbbbbb")]
        with pytest.raises(ceph_native.NativeFailure, match="^object-bucket-s3$") as caught:
            bucket_worker(cluster, markers).object_bucket()
        assert caught.value.native_error["cause"] == "PUT /app-data: HTTP 503 ServiceUnavailable"
        assert failing.requests == ["PUT"] and rgw.requests == [] and "app-data" not in cluster.buckets
        cluster.running = cluster.running[1:]
        assert bucket_worker(cluster, markers).object_bucket() == {"changed": True}
        assert len(creates(rgw)) == 1
    finally:
        failing.close()


def test_observation_is_read_only_and_names_only_the_bucket(ready):
    cluster, markers, rgw = ready
    proved, unused, evidence = observe(cluster, markers)
    assert not proved and not evidence["owned"] and not evidence["exists"] and rgw.requests == []
    bucket_worker(cluster, markers).object_bucket()
    before, mutations, sent = dict(markers.records), list(cluster.mutations), len(rgw.requests)
    proved, unused, evidence = observe(cluster, markers)
    assert proved and evidence["owned"] and evidence["exists"] and evidence["resource"] == {"id": "app-data", "path": ""}
    assert markers.records == before and cluster.mutations == mutations and [entry[0] for entry in rgw.requests[sent:]] == ["GET"]


def test_recovery_scope_is_the_clusters_bootstrap_scope(ready):
    cluster, markers, unused = ready
    assert bucket_worker(cluster, markers).recovery_scope() == gateway.scope(gateway.gateway_request())
    assert "objectBucket" not in json.dumps(ceph_tunnel_staging.recovery_scope({k: v for k, v in cluster.request.items() if k != "objectBucket"}))


@pytest.mark.parametrize("change", [
    {"name": "ab"}, {"name": "App-Data"}, {"owner": "dashboard"}, {"owner": "-v"}, {"backend": "ingress.objects"}, {"port": 0},
    {"certificateRef": "gateway-tls"}, {"endpoint": "s3.example.test"}, {"policy": "[]"}, {"policy": "{"}, {"policy": "x" * 65537},
    {"realm": "Objects"}, {"gateway": ""},
])
def test_request_admits_only_its_closed_bucket(change):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(bucket_request(**change))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(dict(bucket_request(), objectBucket=dict(BUCKET, accessKey=ACCESS)))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(dict(bucket_request(), objectUser=dict(users.USER)))
    assert ceph_native.validate(bucket_request(policy=""))
    assert ceph_native.validate(bucket_request(certificateRef="gateway-tls", endpoint="s3.example.test", port=443))


def policy_of(statement='[{"Effect":"Allow"}]', version='"2012-10-17"'):
    return '{"Statement":' + statement + ',"Version":' + version + "}"


@pytest.mark.parametrize("policy", [
    "{}", '{"Version":"2012-10-17"}', '{"Statement":[{"Effect":"Allow"}]}', policy_of("[]"), policy_of("{}"), policy_of('"Allow"'), policy_of("1"),
    policy_of(version="2012"), policy_of(version='""'), policy_of(version="null"),
    policy_of('[{"Sid":null}]'), policy_of('[{"Condition":{"NumericLessThan":{"s3:max-keys":1.5}}}]'), policy_of('[{"Sid":1e3}]'),
    policy_of('[{"Sid":9223372036854775808}]'), policy_of('[{"Sid":NaN}]'), policy_of('[{"Sid":-0}]'), policy_of('[{"Sid":[[null]]}]'),
    '{"Statement":[{"Effect":"Allow"}], "Version":"2012-10-17"}', '{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}',
    '{"Statement":[{"Effect":"Allow"}],"Version":"2012-10-17","Version":"2012-10-17"}', policy_of('[{"Sid":"<"}]'), policy_of('[{"Sid":"\\u00e9"}]'),
    policy_of('[{"Sid":"\\u0041"}]'), policy_of('[{"Sid":"\\/"}]'), policy_of('[{"Effect":"Allow"}]') + "\n", "[" + policy_of() + "]", '"x"', "null",
    policy_of("[" * 5000 + "]" * 5000),
])
def test_request_refuses_a_policy_the_planner_never_freezes(policy):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(bucket_request(policy=policy))


GO_TEXT = ('{"Statement":[{"K\u00e9":true,"N":-9223372036854775808,"Sid":"\\u003c\\u003e\\u0026 \u00e9\\u2028\\u2029\\t\\"\\\\\\u0001\x7f\\b\\f\\n\\r\u20ac",'
           '"Z":"z"}],"Version":"2012-10-17"}')


@pytest.mark.parametrize("policy", [
    POLICY, policy_of('{"Effect":"Allow"}'), GO_TEXT,
    policy_of('[{"Condition":{"NumericLessThan":{"s3:max-keys":9223372036854775807}},"NotBefore":-9223372036854775808,"Sid":true}]'),
])
def test_request_admits_the_policy_text_the_planner_freezes(policy):
    assert ceph_native.validate(bucket_request(policy=policy))


def test_request_admits_a_bucket_name_of_at_most_44_characters():
    assert ceph_native.validate(bucket_request(name="b" * 44))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(bucket_request(name="b" * 45))


def test_dispatch_lends_a_tls_bucket_only_its_certificate(ready, monkeypatch):
    cluster, unused, unused_rgw = ready
    seen = []
    monkeypatch.setattr(ceph_native_object.BucketWorker, "object_bucket", lambda self: seen.append((type(self), self.tls)) or {"changed": False})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    envelope = {"request": cluster.request, "host": "node-1", "phase": "object_bucket", "private": {}}
    assert ceph_native.dispatch(envelope) == {"changed": False} and seen == [(ceph_native_object.BucketWorker, {})]
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.dispatch(dict(envelope, private={"gatewayCertificate": gateway.CERT}))
    tls = dict(envelope, request=bucket_request(certificateRef="gateway-tls", endpoint="s3.example.test", port=443))
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.dispatch(tls)
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.dispatch(dict(tls, private={"gatewayCertificate": gateway.CERT, "gatewayKey": gateway.KEY}))
    assert ceph_native.dispatch(dict(tls, private={"gatewayCertificate": gateway.CERT})) == {"changed": False}
    monkeypatch.setattr(ceph_native_object.BucketWorker, "prerequisites", lambda self: seen.append(self.tls) or {"changed": False})
    assert ceph_native.dispatch(dict(tls, phase="prerequisites")) == {"changed": False} and seen[-1] == {}
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.dispatch(dict(tls, phase="prerequisites", private={"gatewayCertificate": gateway.CERT}))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch(dict(envelope, phase="object_user"))


def test_a_tls_backend_is_never_sent_a_request_unless_it_serves_the_bound_leaf(ready, monkeypatch):
    cluster, markers, rgw = ready
    cluster.request = bucket_request(port=rgw.port, certificateRef="gateway-tls", endpoint="s3.example.test")
    exchanges = []
    monkeypatch.setattr(ceph_native_object, "s3_exchange", lambda *args, **options: exchanges.append(options["pinned"]) or None)
    native = bucket_worker(cluster, markers, {"gatewayCertificate": gateway.CERT})
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        native.object_bucket()
    leaf = hashlib.sha256(ceph_native_object.leaf(gateway.CERT)).hexdigest()
    assert exchanges == [("s3.example.test", leaf)] and native.blocking == ["tls@rgw.objects"]
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        bucket_worker(cluster, markers).object_bucket()


def core_bucket_request(request, **members):
    return dict(request, action="object-bucket", objectBucket=dict(BUCKET, **members))


class Transport:
    def __init__(self, request, ready_after=0):
        self.request, self.calls, self.ready_after = request, [], ready_after

    def gateway_material(self, key):
        assert key is False
        return {"gatewayCertificate": gateway.CERT}

    def call(self, host, phase, private=None, timeout=None):
        del timeout
        self.calls.append((host["name"], phase, private))
        if phase == "observe":
            done = len([call for call in self.calls if call[1] == "observe"]) > self.ready_after
            return dict(core.observation(self.request, ready=done), request="", removed=False, exists=True, partial=not done,
                        resource={"id": "app-data" if done else "", "path": ""})
        return {"changed": True}


@pytest.mark.parametrize("tls", [False, True])
def test_apply_runs_one_bucket_phase_then_waits_lending_only_the_certificate(core_request, monkeypatch, tls):
    request = core_bucket_request(core_request, **({"certificateRef": "gateway-tls", "endpoint": "s3.example.test", "port": 443} if tls else {}))
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    transport = Transport(request, ready_after=1)
    result = ceph_core.execute(request, "apply", "d" * 64, transport, {}, pause=lambda seconds: None)
    private = {"gatewayCertificate": gateway.CERT} if tls else None
    assert result["changed"] and result["evidence"]["resource"] == {"id": "app-data", "path": ""}
    assert transport.calls == [("node-1", "prerequisites", None), ("node-1", "object_bucket", private), ("node-1", "observe", private),
                               ("node-1", "observe", private)]
    with pytest.raises(ceph_native.Refusal):
        ceph_core.execute(request, "apply", "d" * 64, Transport(request), {"object-user": "/dev/null"}, pause=lambda seconds: None)


def test_object_bucket_evidence_admits_only_its_bucket_name(core_request):
    request = core_bucket_request(core_request)
    value = dict(core.observation(request, ready=True), request="d" * 64, resource={"id": "app-data", "path": ""})
    assert ceph_core.evidence(value, "d" * 64, request) is value
    assert ceph_core.evidence(dict(value, ready=False, partial=True, resource={"id": "", "path": ""}), "d" * 64, request)
    for refused in (dict(value, resource={"id": "other", "path": ""}), dict(value, resource={"id": "app-data", "path": "/"}),
                    dict(value, resource={"id": ACCESS, "path": ""}), dict(value, client="client.app-writer")):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(refused, "d" * 64, request)


core_request = core.core_request
