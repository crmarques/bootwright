"""Manifest-only resolution never gives registry secrets to another origin or tool."""

from __future__ import annotations

import contextlib
import copy
import hashlib
import http.client
import json
import os
import pathlib
import signal
import socket
import ssl
import threading
import time
from types import SimpleNamespace

import pytest
import yaml

from ansible_collections.bootwright.core.plugins.module_utils import ceph_registry_discovery as discovery
from ansible_collections.bootwright.core.plugins.modules import ceph_image_resolve as module

COLLECTION = pathlib.Path(__file__).resolve().parents[4]
GOLDEN = json.loads((COLLECTION / "tests" / "fixtures" / "ceph-image-call.golden").read_text())
GOLDEN["request"]["version"] = "ceph-image-v3"
REPOSITORY = GOLDEN["request"]["base"]
NAMESPACE = REPOSITORY.split("/", 1)[1]
TAG_NAME = GOLDEN["request"]["tag"]
REGISTRY = GOLDEN["request"]["registry"]
RELEASE = GOLDEN["request"]["release"]
SCOPE = "repository:" + NAMESPACE + ":pull"
AUTH = 'Bearer realm="https://cp.icr.io/oauth/token",service="registry",scope="' + SCOPE + '"'


def raw(value):
    return json.dumps(value, separators=(",", ":")).encode()


def sha(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


def response(value, media=None, digest_header=True):
    data = raw(value)
    headers = {"content-type": media or "application/json"}
    if digest_header:
        headers["docker-content-digest"] = sha(data)
    return 200, headers, data


class Client:
    def __init__(self, indexed=True, authenticated=True, digest_headers=True):
        self.calls = []
        self.config = {"architecture": "amd64", "os": "linux", "rootfs": {"type": "layers", "diff_ids": []}}
        self.manifest = {
            "schemaVersion": 2,
            "mediaType": discovery.MANIFEST_TYPES[0],
            "config": {
                "mediaType": discovery.CONFIG_TYPES[0],
                "digest": sha(raw(self.config)),
                "size": len(raw(self.config)),
            },
            "layers": [],
        }
        self.selected = {
            "mediaType": discovery.MANIFEST_TYPES[0],
            "digest": sha(raw(self.manifest)),
            "size": len(raw(self.manifest)),
            "platform": {"os": "linux", "architecture": "amd64"},
        }
        self.index = {"schemaVersion": 2, "mediaType": discovery.INDEX_TYPES[0], "manifests": [self.selected]}
        self.rows = []
        if authenticated:
            self.rows += [(401, {"www-authenticate": AUTH}, b""), response({"token": "synthetic-token", "expires_in": 3600})]
        self.rows.append(
            response(self.index if indexed else self.manifest, discovery.INDEX_TYPES[0] if indexed else discovery.MANIFEST_TYPES[0], digest_headers)
        )
        if indexed:
            self.rows.append(response(self.manifest, discovery.MANIFEST_TYPES[0], digest_headers))

    def get(self, path, authorization=None, maximum=discovery.MAX_BODY):
        self.calls.append((path, authorization, maximum))
        assert self.rows, "unexpected registry request: " + path
        return self.rows.pop(0)


@pytest.mark.parametrize("authenticated", [True, False])
@pytest.mark.parametrize("digest_headers", [True, False])
def test_pinned_index_freezes_raw_linux_amd64_digest_without_blob_requests(authenticated, digest_headers):
    client = Client(True, authenticated, digest_headers)
    assert discovery.resolve("Basic synthetic-secret", client, GOLDEN["request"]) == sha(raw(client.manifest))
    assert client.rows == []
    assert all(path.startswith("/v2/" + NAMESPACE + "/") or path.startswith("/oauth/token?") for path, unused_auth, unused_limit in client.calls)
    assert [path for path, unused_auth, unused_limit in client.calls if "/blobs/" in path] == []
    if authenticated:
        assert client.calls[0][1] is None and client.calls[1][1] == "Basic synthetic-secret"
        assert all(auth == "Bearer synthetic-token" for unused_path, auth, unused_limit in client.calls[2:])
    else:
        assert all(auth is None for unused_path, auth, unused_limit in client.calls)


@pytest.mark.parametrize("media", discovery.MANIFEST_TYPES)
@pytest.mark.parametrize("authenticated", [True, False])
def test_single_manifest_tag_authenticates_config_platform(media, authenticated):
    client = Client(indexed=False, authenticated=authenticated)
    client.manifest["mediaType"] = media
    client.rows[-1] = response(client.manifest, media)
    client.rows.append(response(client.config, discovery.CONFIG_TYPES[0]))
    assert discovery.resolve("Basic synthetic-secret", client, GOLDEN["request"]) == sha(raw(client.manifest))
    assert client.rows == [] and len(client.calls) == (4 if authenticated else 2)
    assert client.calls[-1][0].startswith("/v2/" + NAMESPACE + "/blobs/")


def test_optional_body_media_type_uses_supported_http_type_and_raw_digest():
    client = Client(authenticated=False)
    client.manifest.pop("mediaType")
    client.selected.update(digest=sha(raw(client.manifest)), size=len(raw(client.manifest)))
    client.index.pop("mediaType")
    client.rows = [response(client.index, discovery.INDEX_TYPES[0]), response(client.manifest, discovery.MANIFEST_TYPES[0])]
    assert discovery.resolve("Basic synthetic", client, GOLDEN["request"]) == sha(raw(client.manifest))


def test_present_body_media_type_must_match_the_supported_http_type():
    client = Client(authenticated=False)
    client.manifest["mediaType"] = discovery.MANIFEST_TYPES[1]
    client.selected.update(digest=sha(raw(client.manifest)), size=len(raw(client.manifest)))
    client.rows = [response(client.index, discovery.INDEX_TYPES[0]), response(client.manifest, discovery.MANIFEST_TYPES[0])]
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        discovery.resolve("Basic synthetic", client, GOLDEN["request"])


@pytest.mark.parametrize("change", ["http", "foreign", "userinfo", "port", "query", "scope", "duplicate", "escaping", "extra"])
def test_unapproved_auth_challenge_refuses_before_secret_delivery(change):
    value = AUTH
    replacements = {
        "http": ("https:", "http:"),
        "foreign": ("cp.icr.io/oauth", "foreign.invalid/oauth"),
        "userinfo": ("https://cp", "https://cp@cp"),
        "port": ("cp.icr.io/oauth", "cp.icr.io:444/oauth"),
        "path": ("/oauth/token", "/other"),
        "query": ("/oauth/token", "/oauth/token?escape=1"),
        "service": ('service="registry"', 'service="foreign"'),
        "scope": (":pull", ":pull,push"),
        "escaping": ("oauth/token", "oauth\\/token"),
    }
    if change in replacements:
        value = value.replace(*replacements[change])
    else:
        value += ',realm="https://cp.icr.io/oauth/token"' if change == "duplicate" else ',other="value"'
    client = Client()
    client.rows[0] = (401, {"www-authenticate": value}, b"")
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        discovery.resolve("Basic synthetic-secret", client, GOLDEN["request"])
    assert len(client.calls) == 1 and client.calls[0][1] is None


@pytest.mark.parametrize("slot", [0, 1, 2, 3])
def test_redirects_never_receive_followup_or_forwarded_credentials(slot):
    client = Client()
    client.rows[slot] = (302, {"location": "https://foreign.invalid/steal"}, b"")
    with pytest.raises(discovery.DiscoveryRefusal):
        discovery.resolve("Basic synthetic-secret", client, GOLDEN["request"])
    assert len(client.calls) == slot + 1


@pytest.mark.parametrize(
    "change",
    [
        "tag-hash",
        "child-hash",
        "config-size",
        "wrong-os",
        "wrong-architecture",
        "duplicate-platform",
        "variant",
        "schema-bool",
        "config-unknown",
        "config-media",
        "media",
        "token-conflict",
        "invalid-layer",
    ],
)
def test_content_and_platform_ambiguity_cannot_publish_identity(change):
    client = Client()
    if change in ("tag-hash", "child-hash"):
        client.rows[{"tag-hash": 2, "child-hash": 3}[change]][1]["docker-content-digest"] = "sha256:" + "f" * 64
    elif change == "token-conflict":
        client.rows[1] = response({"token": "one", "access_token": "two"})
    else:
        if change == "config-size":
            client.manifest["config"]["size"] = 0
        elif change == "config-unknown":
            client.manifest["config"]["foreign"] = "unapproved"
        elif change == "config-media":
            client.manifest["config"]["mediaType"] = discovery.LAYER_TYPES[0]
        elif change == "invalid-layer":
            client.manifest["layers"] = [None]
        elif change == "wrong-os":
            client.selected["platform"]["os"] = "windows"
        elif change == "wrong-architecture":
            client.selected["platform"]["architecture"] = "arm64"
        elif change == "duplicate-platform":
            client.index["manifests"].append(copy.deepcopy(client.selected))
        elif change == "variant":
            client.selected["platform"]["variant"] = "v3"
        elif change == "schema-bool":
            client.index["schemaVersion"] = True
        else:
            client.index["mediaType"] = "foreign"
        client.selected.update(digest=sha(raw(client.manifest)), size=len(raw(client.manifest)))
        client.rows[2:] = [response(client.index, discovery.INDEX_TYPES[0]), response(client.manifest, discovery.MANIFEST_TYPES[0])]
    with pytest.raises(discovery.DiscoveryRefusal):
        discovery.resolve("Basic synthetic-secret", client, GOLDEN["request"])


@pytest.mark.parametrize(
    "value",
    [
        b'{"token":"one","Token":"two"}',
        b'{"token":NaN}',
        b'{"token":"line\\nsecret"}',
        b'{"token":"x","refresh_token":"secret"}',
        b'{"token":"x","expires_in":true}',
    ],
)
def test_token_parser_is_closed_and_never_returns_raw_errors(value):
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        discovery.token(value)


def test_optional_oci_hints_are_never_followed_or_used_as_authority():
    client = Client()
    client.selected.update(urls=["https://foreign.invalid/manifest"], data="ignored", artifactType="ignored")
    client.selected["platform"].update({"os.version": "ignored", "os.features": ["ignored"]})
    client.manifest["config"].update(urls=["https://foreign.invalid/config"], data="ignored")
    client.selected.update(digest=sha(raw(client.manifest)), size=len(raw(client.manifest)))
    client.rows[2:] = [response(client.index, discovery.INDEX_TYPES[0]), response(client.manifest, discovery.MANIFEST_TYPES[0])]
    assert discovery.resolve("Basic synthetic", client, GOLDEN["request"]) == sha(raw(client.manifest))
    assert all("foreign.invalid" not in path for path, unused_auth, unused_limit in client.calls)


@pytest.mark.parametrize(
    "value",
    [
        None,
        [None],
        [{"mediaType": "foreign", "digest": "sha256:" + "a" * 64, "size": 1}],
        [{"mediaType": discovery.LAYER_TYPES[0], "digest": "sha256:" + "a" * 64, "size": True}],
        [{"mediaType": discovery.LAYER_TYPES[0], "digest": "sha256:" + "a" * 64, "size": -1}],
        [{"mediaType": discovery.LAYER_TYPES[0], "digest": "sha256:" + "0" * 64, "size": 1}],
    ],
)
def test_layer_descriptors_are_validated_without_layer_downloads(value):
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        discovery.image_layers(value)


def test_fixed_connection_ignores_proxies_and_never_follows_redirect(monkeypatch):
    calls = []
    monkeypatch.setenv("HTTPS_PROXY", "http://foreign.invalid")
    monkeypatch.setenv("HTTP_PROXY", "http://foreign.invalid")

    class Connection:
        def __init__(self, host, **options):
            calls.append((host, options))

        def request(self, method, path, **options):
            calls.append((method, path, options))

        def getresponse(self):
            return SimpleNamespace(status=302, getheaders=lambda: [("Location", "https://foreign.invalid")], read=lambda limit: b"")

        def close(self):
            calls.append("closed")

    monkeypatch.setattr(discovery.http.client, "HTTPSConnection", Connection)
    context = object()
    status, unused_headers, unused_body = discovery.Registry(context, GOLDEN["request"]).get("/v2/" + NAMESPACE + "/manifests/" + TAG_NAME)
    assert status == 302 and calls[0][0] == REGISTRY and calls[0][1]["context"] is context
    assert calls[1][0] == "GET" and calls[-1] == "closed" and len(calls) == 3


@pytest.mark.parametrize(
    "headers,body",
    [
        ([("Content-Length", "2")], b"abc"),
        ([("Content-Length", str(discovery.MAX_BODY + 1))], b""),
        ([("Content-Type", "one"), ("content-type", "two")], b""),
        ([("Content-Encoding", "gzip")], b""),
        ([], b"x" * (discovery.MAX_BODY + 1)),
    ],
)
def test_response_bounds_and_ambiguity_close_connection_without_evidence(monkeypatch, headers, body):
    closed = []

    class Connection:
        def __init__(self, *_args, **_options):
            pass

        def request(self, *_args, **_options):
            pass

        def getresponse(self):
            return SimpleNamespace(status=200, getheaders=lambda: headers, read=lambda _limit: body)

        def close(self):
            closed.append(True)

    monkeypatch.setattr(discovery.http.client, "HTTPSConnection", Connection)
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        discovery.Registry(object(), GOLDEN["request"]).get("/v2/" + NAMESPACE + "/manifests/" + TAG_NAME)
    assert closed == [True]


@pytest.mark.parametrize(
    "path",
    [
        "https://foreign.invalid",
        "/v2/other/manifests/tag",
        "/oauth/token?service=foreign",
        "/v2/" + NAMESPACE + "/blobs/sha256:not-a-digest",
    ],
)
def test_no_arbitrary_endpoint_can_reach_the_socket_factory(monkeypatch, path):
    monkeypatch.setattr(discovery.http.client, "HTTPSConnection", lambda *args, **kwargs: pytest.fail("unapproved network"))
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        discovery.Registry(object(), GOLDEN["request"]).get(path, "Bearer synthetic")


def test_tls_roots_are_held_explicit_and_ignore_ambient_ssl_paths(monkeypatch):
    monkeypatch.setenv("SSL_CERT_FILE", "/foreign/ca")
    monkeypatch.setenv("SSL_CERT_DIR", "/foreign/cas")
    calls = []
    certificate = "-----BEGIN CERTIFICATE-----\nAQID\n-----END CERTIFICATE-----\n"
    bundle = ("# Synthetic café comment\n" + certificate).encode("utf-8")

    class Context:
        verify_mode = ssl.CERT_REQUIRED
        check_hostname = True

        def __init__(self, protocol):
            calls.append(protocol)

        def load_verify_locations(self, **options):
            calls.append(options)

        def cert_store_stats(self):
            return {"x509_ca": 1}

    class Handle:
        def read(self, maximum):
            assert maximum == discovery.MAX_CA + 1
            return bundle

        def fileno(self):
            return 999

    @contextlib.contextmanager
    def protected(path, maximum, **options):
        assert path == "/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem" and maximum == 8 << 20 and options == {"root": True}
        yield Handle(), lambda: calls.append("reproved")

    monkeypatch.setattr(discovery, "protected_file", protected)
    monkeypatch.setattr(discovery.os, "fstat", lambda fd: SimpleNamespace(st_size=len(bundle)))
    monkeypatch.setattr(discovery.ssl, "SSLContext", Context)
    with discovery.tls_proof() as (context, prove):
        assert context.minimum_version == ssl.TLSVersion.TLSv1_2
        prove()
    assert calls == ["reproved", ssl.PROTOCOL_TLS_CLIENT, {"cadata": certificate}, "reproved", "reproved"]


@pytest.mark.parametrize(
    "bundle",
    [
        b"# no certificates\n",
        b"# invalid UTF-8\xff\n-----BEGIN CERTIFICATE-----\nAQID\n-----END CERTIFICATE-----\n",
        b"uncommented arbitrary bytes\n-----BEGIN CERTIFICATE-----\nAQID\n-----END CERTIFICATE-----\n",
        b"-----BEGIN PRIVATE KEY-----\nAQID\n-----END PRIVATE KEY-----\n",
        b"-----BEGIN CERTIFICATE-----\nAQID\n",
        b"-----BEGIN CERTIFICATE-----\n-----END CERTIFICATE-----\n",
        b"-----BEGIN CERTIFICATE-----\n?invalid-base64\n-----END CERTIFICATE-----\n",
        "-----BEGIN CERTIFICATE-----\nAQéD\n-----END CERTIFICATE-----\n".encode("utf-8"),
        b"-----BEGIN CERTIFICATE-----\nAQID\n# comments inside certificate are invalid\n-----END CERTIFICATE-----\n",
    ],
)
def test_public_ca_projection_refuses_unexpected_or_malformed_blocks(bundle):
    with pytest.raises(discovery.DiscoveryRefusal, match="^artifact$"):
        discovery.certificates(bundle)


@pytest.mark.parametrize("bound", ["bytes", "count", "certificate"])
def test_public_ca_projection_has_fixed_byte_certificate_and_count_bounds(monkeypatch, bound):
    block = b"-----BEGIN CERTIFICATE-----\nAQID\n-----END CERTIFICATE-----\n"
    if bound == "bytes":
        monkeypatch.setattr(discovery, "MAX_CA", len(block) - 1)
    elif bound == "count":
        monkeypatch.setattr(discovery, "MAX_CERTIFICATES", 1)
        block *= 2
    else:
        monkeypatch.setattr(discovery, "MAX_CERTIFICATE", 3)
    with pytest.raises(discovery.DiscoveryRefusal, match="^artifact$"):
        discovery.certificates(block)


def test_native_ssl_parser_refuses_invalid_der_even_after_syntactically_valid_projection():
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    projected = discovery.certificates("# Synthetic café\n".encode("utf-8") + b"-----BEGIN CERTIFICATE-----\nAQID\n-----END CERTIFICATE-----\n")
    with pytest.raises(ssl.SSLError):
        context.load_verify_locations(cadata=projected[0])


def test_native_ssl_context_loads_synthetic_ca_with_utf8_comment(monkeypatch):
    # Generated offline for this regression with a fresh P-256 key, generic
    # subject and fixed dates. Only its public certificate was retained; no
    # private key was serialized and no host/operator CA bytes were copied.
    bundle = (COLLECTION / "tests" / "fixtures" / "ceph-discovery-synthetic-ca.golden").read_bytes()
    calls = []

    class Handle:
        def read(self, maximum):
            assert maximum == discovery.MAX_CA + 1
            return bundle

        def fileno(self):
            return 999

    @contextlib.contextmanager
    def protected(path, maximum, **options):
        assert path == discovery.CA_PATH and maximum == discovery.MAX_CA and options == {"root": True}
        yield Handle(), lambda: calls.append("reproved")

    monkeypatch.setattr(discovery, "protected_file", protected)
    monkeypatch.setattr(discovery.os, "fstat", lambda fd: SimpleNamespace(st_size=len(bundle)))
    with discovery.tls_proof() as (context, prove):
        assert context.cert_store_stats()["x509_ca"] == 1
        assert context.verify_mode == ssl.CERT_REQUIRED and context.check_hostname
        prove()
    assert calls == ["reproved", "reproved", "reproved"]


@pytest.mark.parametrize(
    "name,value",
    [
        ("image", REPOSITORY + ":latest"),
        ("image", "registry.example.test/ceph@sha256:" + "a" * 64),
        ("image", REPOSITORY + "@sha256:" + "0" * 64),
        ("tag", "latest"),
        ("base", "cp.icr.io/cp/ibm-ceph/ceph-8-rhel9"),
        ("registry", "foreign.invalid"),
        ("version", "ceph-image-v0"),
        ("cluster", "../escape"),
        ("registryRef", True),
        ("podman", {"path": "/foreign"}),
    ],
)
def test_unfrozen_or_open_request_refuses_before_tls_or_private_loan(monkeypatch, name, value):
    original = copy.deepcopy(GOLDEN["request"])
    original[name] = value
    monkeypatch.setattr(discovery, "tls_proof", lambda: pytest.fail("TLS before admission"))
    with pytest.raises(discovery.DiscoveryRefusal, match="^request$"):
        discovery.execute("apply", original, GOLDEN["digest"], {"registryJSON": "/owned/loan"})


def fake_tls(monkeypatch, checks=None):
    @contextlib.contextmanager
    def proof():
        def check():
            if checks is not None:
                checks.append("tls")

        yield object(), check
        check()

    monkeypatch.setattr(discovery, "tls_proof", proof)


@pytest.mark.parametrize("operation", ["observe", "destroy"])
def test_observation_and_removal_read_nothing_remote_and_report_absence(monkeypatch, operation):
    monkeypatch.setattr(discovery, "tls_proof", lambda: pytest.fail("TLS without a resolution"))
    monkeypatch.setattr(discovery, "protected_file", lambda *args, **kwargs: pytest.fail("secret loan"))
    monkeypatch.setattr(discovery, "Registry", lambda *args: pytest.fail("network"))
    result = discovery.execute(operation, GOLDEN["request"], GOLDEN["digest"], {})
    assert result == {"changed": False, "evidence": {"request": GOLDEN["digest"], "postcondition": True, "absent": True}}
    with pytest.raises(discovery.DiscoveryRefusal, match="^material$"):
        discovery.execute(operation, GOLDEN["request"], GOLDEN["digest"], {"registryJSON": "/owned/loan"})


def test_tls_refusal_precedes_private_read(monkeypatch):
    @contextlib.contextmanager
    def refused():
        discovery.require(False, "artifact")
        yield

    monkeypatch.setattr(discovery, "tls_proof", refused)
    monkeypatch.setattr(discovery, "protected_file", lambda *args, **kwargs: pytest.fail("private read"))
    with pytest.raises(discovery.DiscoveryRefusal, match="^artifact$"):
        discovery.execute("apply", GOLDEN["request"], GOLDEN["digest"], {"registryJSON": "/owned/loan"})


@pytest.mark.parametrize("change", ["symlink", "fifo", "hardlink", "mode", "parent-mode", "replaced-file", "replaced-parent"])
def test_original_loan_custody_refuses_unsafe_or_replaced_files(tmp_path, change):
    tmp_path.chmod(0o700)
    parent = tmp_path / "owned"
    parent.mkdir(mode=0o700)
    path = parent / "loan"
    path.write_bytes(b"synthetic-secret")
    path.chmod(0o600)
    if change == "symlink":
        path.rename(parent / "original")
        path.symlink_to("original")
    elif change == "fifo":
        path.unlink()
        os.mkfifo(path, 0o600)
    elif change == "hardlink":
        os.link(path, parent / "alias")
    elif change == "mode":
        path.chmod(0o644)
    elif change == "parent-mode":
        parent.chmod(0o755)
    with pytest.raises(discovery.DiscoveryRefusal, match="^material$"):
        with discovery.protected_file(str(path), discovery.MAX_REGISTRY, private=True) as (handle, prove):
            if change == "replaced-file":
                path.rename(parent / "original")
                path.write_bytes(b"synthetic-secret")
                path.chmod(0o600)
            elif change == "replaced-parent":
                parent.rename(tmp_path / "original-parent")
                parent.mkdir(mode=0o700)
                path.write_bytes(b"synthetic-secret")
                path.chmod(0o600)
            prove()
            pytest.fail("unsafe private read permitted: " + str(handle.fileno()))


def test_private_loan_is_retained_and_no_file_or_cache_is_created(tmp_path, monkeypatch):
    tmp_path.chmod(0o700)
    path = tmp_path / "registry"
    path.write_bytes(raw({"url": REGISTRY, "username": "cp", "password": "synthetic-secret"}))
    path.chmod(0o600)
    checks = []
    fake_tls(monkeypatch, checks)
    client = Client()
    monkeypatch.setattr(discovery, "Registry", lambda *args: client)
    result = discovery.execute("apply", GOLDEN["request"], GOLDEN["digest"], {"registryJSON": str(path)})
    assert result == {"changed": False, "evidence": {
        "request": GOLDEN["digest"], "postcondition": True, "release": RELEASE, "base": REPOSITORY,
        "tag": TAG_NAME, "digest": sha(raw(client.manifest)), "platform": "linux/amd64",
    }}
    assert checks == ["tls", "tls", "tls"] and list(tmp_path.iterdir()) == [path]
    assert "synthetic-secret" not in str(result)


@pytest.mark.parametrize("signal_number", [signal.SIGTERM, signal.SIGHUP, signal.SIGINT])
def test_cancellation_is_bounded_and_restores_handlers(signal_number):
    original = signal.getsignal(signal_number)
    with pytest.raises(discovery.DiscoveryRefusal, match="^timeout$"):
        with discovery.bounded_lifetime(1):
            os.kill(os.getpid(), signal_number)
    assert signal.getsignal(signal_number) == original and signal.getitimer(signal.ITIMER_REAL) == (0.0, 0.0)


@pytest.mark.parametrize("slow_body", [False, True])
def test_slow_http_headers_and_body_are_stopped_by_whole_call_budget(monkeypatch, slow_body):
    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen(1)
    address = listener.getsockname()
    stopped = threading.Event()

    def server():
        connection, unused_address = listener.accept()
        with connection:
            connection.recv(4096)
            if slow_body:
                connection.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nx")
            stopped.wait(0.3)
        listener.close()

    thread = threading.Thread(target=server)
    thread.start()
    opened = []

    def connect(host, **options):
        assert host == "cp.icr.io" and options.get("port", 443) == 443
        client = http.client.HTTPConnection(address[0], address[1], timeout=1)
        opened.append(client)
        return client

    monkeypatch.setattr(discovery.http.client, "HTTPSConnection", connect)
    started = time.monotonic()
    try:
        with pytest.raises(discovery.DiscoveryRefusal, match="^timeout$"):
            with discovery.bounded_lifetime(0.05):
                discovery.Registry(object(), GOLDEN["request"]).get("/v2/" + NAMESPACE + "/manifests/" + TAG_NAME)
        assert time.monotonic() - started < 1 and opened[0].sock is None
    finally:
        stopped.set()
        thread.join(timeout=1)
        listener.close()


@pytest.mark.parametrize("entry,operation", [("resolve", "apply"), ("observe", "observe"), ("destroy", "destroy")])
def test_roles_are_local_protected_and_publish_only_closed_completion(entry, operation):
    role = COLLECTION / "roles" / "storage_ceph_image"
    tasks = yaml.safe_load((role / "tasks" / (entry + ".yml")).read_text())
    call = next(task for task in tasks if "bootwright.core.ceph_image_resolve" in task)
    assert call["no_log"] is True and call["diff"] is False and call["failed_when"] is False
    assert call["bootwright.core.ceph_image_resolve"]["operation"] == operation
    assert call["bootwright.core.ceph_image_resolve"]["material"] == "{{ bootwright_ceph_image_material }}"
    phases = [task["bootwright.core.ceph_image_protocol"]["phase"] for task in tasks if "bootwright.core.ceph_image_protocol" in task]
    assert phases == ["loaded", "refused", "completed"]
    assert tasks[-1]["bootwright.core.ceph_image_protocol"]["outcome"] == "unchanged"


def test_an_authored_digest_must_be_the_resolved_child_and_never_the_index():
    child = "sha256:" + "c" * 64
    tagged = discovery.resolution(GOLDEN["request"], GOLDEN["digest"], child)
    assert tagged["digest"] == child and tagged["platform"] == "linux/amd64"
    authored = dict(GOLDEN["request"], image=REPOSITORY + "@" + child)
    assert discovery.resolution(authored, GOLDEN["digest"], child) == tagged
    index = dict(GOLDEN["request"], image=REPOSITORY + "@sha256:" + "e" * 64)
    with pytest.raises(discovery.DiscoveryRefusal, match="^image-digest-mismatch$"):
        discovery.resolution(index, GOLDEN["digest"], child)


def test_authored_index_digest_refuses_after_resolution_without_evidence(tmp_path, monkeypatch):
    tmp_path.chmod(0o700)
    path = tmp_path / "registry"
    path.write_bytes(raw({"url": REGISTRY, "username": "cp", "password": "synthetic-secret"}))
    path.chmod(0o600)
    fake_tls(monkeypatch)
    client = Client()
    monkeypatch.setattr(discovery, "Registry", lambda *args: client)
    index = dict(GOLDEN["request"], tag="", image=REPOSITORY + "@" + sha(raw(client.index)))
    with pytest.raises(discovery.DiscoveryRefusal, match="^image-digest-mismatch$"):
        discovery.execute("apply", index, GOLDEN["digest"], {"registryJSON": str(path)})
    assert client.rows == [] and discovery.refusal(discovery.DiscoveryRefusal("image-digest-mismatch")) == ("image-digest-mismatch", None)


@pytest.mark.parametrize("change", [
    {"request": "f" * 64}, {"postcondition": False}, {"platform": "linux/arm64"}, {"digest": "sha256:" + "0" * 64},
    {"index": "sha256:" + "e" * 64}, {"absent": True},
])
def test_published_evidence_is_exactly_one_pinned_resolution_or_its_absence(change):
    value = discovery.resolution(GOLDEN["request"], GOLDEN["digest"], "sha256:" + "c" * 64)
    value.update(change)
    with pytest.raises(discovery.DiscoveryRefusal):
        discovery.evidence(value, GOLDEN["digest"])


def test_module_refusal_never_discloses_unbounded_native_error(monkeypatch):
    captured = {}

    class Finished(Exception):
        pass

    class FakeModule:
        def __init__(self, **options):
            captured.update(options)
            self.params = {"operation": "observe", "request": {}, "digest": "", "material": {}}

        def fail_json(self, **options):
            captured["failure"] = options
            raise Finished()

    monkeypatch.setattr(module, "AnsibleModule", FakeModule)
    monkeypatch.setattr(discovery, "execute", lambda *args: (unused_value for unused_value in ()).throw(ValueError("synthetic-secret")))
    with pytest.raises(Finished):
        module.main()
    assert captured["argument_spec"]["material"]["no_log"] is True and captured["supports_check_mode"] is False
    detail = captured["failure"].pop("detail")
    assert captured["failure"] == {"msg": "Ceph image resolution refused", "reason": "adapter-exception"}
    assert detail.startswith("ValueError@ceph_image_resolve.main:") and "secret" not in detail


def test_every_discovery_exit_names_a_reason_and_no_value():
    assert discovery.refusal(discovery.DiscoveryRefusal("timeout")) == ("timeout", None)
    assert discovery.refusal(discovery.DiscoveryRefusal("synthetic-unlisted")) == ("unknown", "synthetic-unlisted")
    assert discovery.refusal(OSError("synthetic secret")) == ("adapter-exception", "OSError@unknown.frame:0")


def test_authored_direct_digest_is_resolved_without_any_tag_request():
    client = Client(indexed=False, authenticated=False)
    client.rows.append(response(client.config, discovery.CONFIG_TYPES[0]))
    selected = sha(raw(client.manifest))
    request = dict(GOLDEN["request"], tag="", image=REPOSITORY + "@" + selected)
    assert discovery.validate_request(request) == request
    assert discovery.resolve("Basic synthetic", client, request) == selected
    assert client.calls[0][0] == "/v2/" + NAMESPACE + "/manifests/" + selected
    assert all(TAG_NAME not in path for path, unused_auth, unused_maximum in client.calls)


@pytest.mark.parametrize("tag", ["synthetic-future-release", "not a tag", "--bad-tag", "sha256:not-a-digest"])
def test_authored_tag_reaches_registry_without_version_grammar(tag):
    client = Client(authenticated=False)
    request = dict(GOLDEN["request"], base="registry.example.test/vendor/other", registry="registry.example.test", release="unrelated",
                   tag=tag, image="registry.example.test/vendor/other:" + tag)
    assert discovery.validate_request(request) == request
    discovery.resolve("Basic synthetic", client, request)
    assert client.calls[0][0] == "/v2/vendor/other/manifests/" + discovery.urllib.parse.quote(tag, safe=":")


def test_native_registry_failure_keeps_selection_and_redacts_credentials():
    client = Client(authenticated=False)
    client.rows = [(404, {}, b'{"errors":[{"code":"MANIFEST_UNKNOWN","message":"no manifest; password=synthetic-secret"}]}')]
    request = dict(GOLDEN["request"], tag="unavailable", image=REPOSITORY + ":unavailable")
    with pytest.raises(discovery.DiscoveryRefusal) as caught:
        discovery.resolve("Basic synthetic-secret", client, request)
    assert caught.value.native_error["coordinate"] == request["image"]
    assert "MANIFEST_UNKNOWN" in caught.value.native_error["cause"]
    assert "synthetic-secret" not in str(caught.value.native_error)


DELIVERY = "delivery.example.test"
SIGNED = "https://" + DELIVERY + "/object?signature=private-delivery-capability"


def dns_row(ip="8.8.8.8", family=socket.AF_INET):
    return [family, socket.SOCK_STREAM, socket.IPPROTO_TCP, [ip, 443] + ([0, 0] if family == socket.AF_INET6 else [])]


def delegation(redirect=307, location=SIGNED, rows=None, delivered=None):
    """A real Registry whose registry GETs and delivery socket are synthetic; the hop logic is production code."""
    client = Client(indexed=False, authenticated=True)
    selected = sha(raw(client.manifest))
    request = dict(GOLDEN["request"], tag="", image=REPOSITORY + "@" + selected)
    client.rows.append((redirect, {"location": location, "set-cookie": "synthetic-cookie"}, b""))
    context = SimpleNamespace(check_hostname=True)
    registry = discovery.Registry(context, request)
    registry.get = client.get
    resolved, connections = [], []

    def resolver(host):
        resolved.append(host)
        return json.dumps(rows or [dns_row()]).encode()

    class Connection:
        def __init__(self, host, **options):
            assert options["context"] is context and options["port"] == 443 and 0 < options["timeout"] <= 30
            self.host, self.options = host, options
            connections.append(self)

        def request(self, method, path, headers):
            self.sent = (method, path, headers)

        def getresponse(self):
            status, headers, body = delivered or (200, [("Content-Type", "application/octet-stream")], raw(client.config))
            return SimpleNamespace(status=status, getheaders=lambda: headers, read=lambda limit: body[:limit])

        def close(self):
            self.closed = True

    registry.resolver, registry.delivery = resolver, Connection
    return client, registry, request, selected, resolved, connections


@pytest.mark.parametrize("redirect", [307, 302])
def test_authored_digest_follows_one_config_delegation_without_credentials(redirect):
    client, registry, request, selected, resolved, connections = delegation(redirect)
    assert discovery.resolve("Basic synthetic-secret", registry, request) == selected
    assert client.rows == [] and client.calls[-1][1] == "Bearer synthetic-token"
    assert resolved == [DELIVERY] and len(connections) == 1 and connections[0].host == DELIVERY
    assert connections[0].options["addresses"] == [(socket.AF_INET, socket.SOCK_STREAM, socket.IPPROTO_TCP, "", ("8.8.8.8", 443))]
    method, path, headers = connections[0].sent
    assert method == "GET" and path == "/object?signature=private-delivery-capability" and connections[0].closed
    assert not {"authorization", "cookie", "referer", "proxy-authorization"} & {name.lower() for name in headers}
    assert all("synthetic" not in value for value in headers.values())


def test_delegated_config_bytes_must_match_descriptor():
    unused, registry, request, *unused_rest = delegation(delivered=(200, [], raw({"architecture": "amd64", "os": "linux"})))
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        discovery.resolve("Basic synthetic-secret", registry, request)


@pytest.mark.parametrize("change", ["http", "userinfo", "port", "private", "mixed", "second-hop"])
def test_config_delegation_refuses_bad_location_private_answer_and_second_hop(change):
    location = {"http": "http://" + DELIVERY + "/object", "userinfo": "https://user:secret@" + DELIVERY + "/object",
                "port": "https://" + DELIVERY + ":444/object"}.get(change, SIGNED)
    rows = {"private": [dns_row("10.0.0.1")], "mixed": [dns_row(), dns_row("fd00::1", socket.AF_INET6)]}.get(change)
    delivered = (307, [("Location", SIGNED)], b"") if change == "second-hop" else None
    client, registry, request, unused, resolved, connections = delegation(location=location, rows=rows, delivered=delivered)
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        discovery.resolve("Basic synthetic-secret", registry, request)
    assert client.rows == []
    assert resolved == ([] if change in ("http", "userinfo", "port") else [DELIVERY])
    assert len(connections) == (1 if change == "second-hop" else 0)


def test_config_delegation_never_reflects_location_in_native_error():
    unused, registry, request, *unused_rest = delegation(delivered=(403, [("Location", SIGNED)], SIGNED.encode()))
    with pytest.raises(discovery.DiscoveryRefusal) as caught:
        discovery.resolve("Basic synthetic-secret", registry, request)
    assert caught.value.native_error["cause"] == "config delivery answered HTTP 403"
    assert "private-delivery-capability" not in str(caught.value.native_error) + str(caught.value)


def test_refusal_keeps_coordinate_when_username_is_a_coordinate_segment():
    coordinate = "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-27200"
    blob = discovery.base64.b64encode(b"cp:pw").decode("ascii")
    body = ("denied " + coordinate + " for pw with " + blob).encode()
    with pytest.raises(discovery.DiscoveryRefusal) as caught:
        discovery.http_success(401, body, coordinate, ("Basic " + blob,))
    assert caught.value.native_error["coordinate"] == coordinate
    assert coordinate in caught.value.native_error["cause"]
    assert "pw" not in caught.value.native_error["cause"] and blob not in str(caught.value.native_error)


AUX_BASE = REGISTRY + "/cp/ibm-ceph/haproxy-synthetic"
AUX_NAMESPACE = AUX_BASE.split("/", 1)[1]
AUX_ENTRY = {"name": "haproxy", "base": AUX_BASE, "tag": "synthetic-2", "image": AUX_BASE + ":synthetic-2"}


class AuxiliaryClient(Client):
    """Another repository of the same authority whose index lists an arm64 child before the amd64 one."""

    def __init__(self):
        super().__init__()
        self.config["config"] = {"Entrypoint": ["/usr/sbin/haproxy"]}
        self.manifest["config"].update(digest=sha(raw(self.config)), size=len(raw(self.config)))
        self.selected.update(digest=sha(raw(self.manifest)), size=len(raw(self.manifest)))
        arm = dict(self.manifest, layers=[])
        arm["config"] = dict(self.manifest["config"], digest="sha256:" + "9" * 64)
        self.arm = {"mediaType": discovery.MANIFEST_TYPES[0], "digest": sha(raw(arm)), "size": len(raw(arm)),
                    "platform": {"os": "linux", "architecture": "arm64"}}
        self.index["manifests"] = [self.arm, self.selected]
        challenge = AUTH.replace(SCOPE, "repository:" + AUX_NAMESPACE + ":pull")
        self.rows = [(401, {"www-authenticate": challenge}, b""), response({"token": "synthetic-token", "expires_in": 3600}),
                     response(self.index, discovery.INDEX_TYPES[0]), response(self.manifest, discovery.MANIFEST_TYPES[0])]


def test_auxiliary_reference_resolves_its_own_linux_amd64_digest(tmp_path, monkeypatch):
    tmp_path.chmod(0o700)
    path = tmp_path / "registry"
    path.write_bytes(raw({"url": REGISTRY, "username": "cp", "password": "synthetic-secret"}))
    path.chmod(0o600)
    checks = []
    fake_tls(monkeypatch, checks)
    clients = {REPOSITORY: Client(), AUX_BASE: AuxiliaryClient()}
    requests = []

    def registry(context, request):
        requests.append(request)
        return clients[request["base"]]

    monkeypatch.setattr(discovery, "Registry", registry)
    request = dict(GOLDEN["request"], auxiliary=[AUX_ENTRY])
    result = discovery.execute("apply", request, GOLDEN["digest"], {"registryJSON": str(path)})
    child = sha(raw(clients[AUX_BASE].manifest))
    assert child != sha(raw(clients[REPOSITORY].manifest)) and child != clients[AUX_BASE].arm["digest"]
    assert result["evidence"]["digest"] == sha(raw(clients[REPOSITORY].manifest))
    assert result["evidence"]["auxiliary"] == [{"name": "haproxy", "base": AUX_BASE, "tag": "synthetic-2", "digest": child, "platform": "linux/amd64"}]
    assert [value["base"] for value in requests] == [REPOSITORY, AUX_BASE] and "auxiliary" not in requests[1]
    assert all(client.rows == [] for client in clients.values())
    token_path = clients[AUX_BASE].calls[1][0]
    assert discovery.urllib.parse.parse_qs(token_path.split("?", 1)[1])["scope"] == ["repository:" + AUX_NAMESPACE + ":pull"]
    assert clients[AUX_BASE].calls[1][1] == clients[REPOSITORY].calls[1][1]
    assert all(call[0].startswith("/v2/" + AUX_NAMESPACE + "/") for call in clients[AUX_BASE].calls if not call[0].startswith("/oauth/"))
    assert checks == ["tls"] * 4 and "synthetic-secret" not in str(result)
    digested = dict(AUX_ENTRY, tag="", image=AUX_BASE + "@sha256:" + "e" * 64)
    with pytest.raises(discovery.DiscoveryRefusal, match="^image-digest-mismatch$"):
        discovery.resolution(dict(GOLDEN["request"], auxiliary=[digested]), GOLDEN["digest"], "sha256:" + "c" * 64, [child])


@pytest.mark.parametrize("auxiliary", [
    [dict(AUX_ENTRY, name="base")],
    [dict(AUX_ENTRY, name="keepalived"), AUX_ENTRY],
    [AUX_ENTRY, AUX_ENTRY],
    [],
    [dict(AUX_ENTRY, name=name) for name in discovery.AUXILIARY_NAMES] + [AUX_ENTRY],
    [dict(AUX_ENTRY, base="quay.io/ceph/haproxy", image="quay.io/ceph/haproxy:synthetic-2")],
    [dict(AUX_ENTRY, base=REGISTRY + "x/haproxy", image=REGISTRY + "x/haproxy:synthetic-2")],
    [dict(AUX_ENTRY, digest="sha256:" + "a" * 64)],
    [dict(AUX_ENTRY, tag="other")],
    [dict(AUX_ENTRY, tag="synthetic-2", image=AUX_BASE + "@sha256:" + "a" * 64)],
    "haproxy",
])
def test_auxiliary_request_is_closed(monkeypatch, auxiliary):
    monkeypatch.setattr(discovery, "tls_proof", lambda: pytest.fail("TLS before admission"))
    with pytest.raises(discovery.DiscoveryRefusal, match="^request$"):
        discovery.execute("apply", dict(GOLDEN["request"], auxiliary=auxiliary), GOLDEN["digest"], {"registryJSON": "/owned/loan"})


@pytest.mark.parametrize("change", [
    {"auxiliary": []},
    {"auxiliary": [{"name": "haproxy", "base": AUX_BASE, "tag": "synthetic-2", "digest": "sha256:" + "c" * 64, "platform": "linux/arm64"}]},
    {"auxiliary": [{"name": "base", "base": AUX_BASE, "tag": "synthetic-2", "digest": "sha256:" + "c" * 64, "platform": "linux/amd64"}]},
    {"auxiliary": [{"name": "haproxy", "base": AUX_BASE, "tag": "synthetic-2", "digest": "sha256:" + "0" * 64, "platform": "linux/amd64"}]},
    {"auxiliary": [{"name": "haproxy", "base": AUX_BASE, "tag": "synthetic-2", "digest": "sha256:" + "c" * 64, "platform": "linux/amd64",
                    "index": "x"}]},
    {"auxiliary": [{"name": name, "base": AUX_BASE, "tag": "", "digest": "sha256:" + "c" * 64, "platform": "linux/amd64"}
                   for name in ("keepalived", "haproxy")]},
])
def test_published_auxiliary_evidence_is_closed(change):
    value = discovery.resolution(GOLDEN["request"], GOLDEN["digest"], "sha256:" + "c" * 64)
    value.update(change)
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        discovery.evidence(value, GOLDEN["digest"])
