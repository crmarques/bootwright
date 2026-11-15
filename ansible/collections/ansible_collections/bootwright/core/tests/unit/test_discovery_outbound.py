"""Inventory travels out through the declared HTTPS artifact endpoint only."""

import io
import os
from email.message import Message
from pathlib import Path
from tempfile import TemporaryDirectory
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import canonical_json
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_controller as controller
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_receipt as receipt
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_worker as worker


def test_worker_posts_to_declared_artifact_port_without_a_node_listener(monkeypatch):
    connection = Mock()
    response = connection.getresponse.return_value
    report = b'{"fixture":true}'
    response.getheader.return_value = "64"
    response.status = 200
    response.read.return_value = worker.hashlib.sha256(report).hexdigest().encode()
    monkeypatch.setattr(worker, "receipt_connection", lambda request: (connection, "/_bootwright/inventory/" + "a" * 32), raising=False)
    assert worker.send_report({"reportToken": "b" * 64}, report)
    connection.connect.assert_called_once_with()
    assert connection.mock_calls[0][0] == "connect"
    args, kwargs = connection.request.call_args
    assert args[:2] == ("POST", "/_bootwright/inventory/" + "a" * 32)
    assert kwargs["body"] == report
    assert kwargs["headers"]["Authorization"] == "Bearer " + "b" * 64
    connection.close.assert_called_once_with()


def report_fixture(tmp_path, monkeypatch):
    owner = controller.Controller.__new__(controller.Controller)
    owner.root, owner.binding = tmp_path, {"nonce": "c" * 64}
    monkeypatch.setattr(receipt.records, "read", lambda path, limit=32768: path.read_bytes())
    wire = canonical_json.canonical({"version": "machine-disk-inventory-v1", "binding": owner.binding, "disks": [{"size": 1}]})
    return owner, wire


def post(owner, wire, token="b" * 64, path="/report", extra=(), failed_ack=False):
    handler = receipt.Handler.__new__(receipt.Handler)
    handler.path, handler.rfile = path, io.BytesIO(wire)
    handler.server = SimpleNamespace(owner=owner, token="b" * 64)
    handler.headers = Message()
    handler.headers["Authorization"] = "Bearer " + token
    handler.headers["Content-Length"] = str(len(wire))
    for key, value in extra:
        handler.headers[key] = value
    handler.answer = Mock(side_effect=BrokenPipeError("fixture lost ACK") if failed_ack else None)
    handler.do_POST()
    return handler.answer


def test_report_is_durable_before_ack_and_replays_after_lost_ack(tmp_path, monkeypatch):
    owner, wire = report_fixture(tmp_path, monkeypatch)
    expected = worker.hashlib.sha256(wire).hexdigest().encode()
    with pytest.raises(BrokenPipeError):
        post(owner, wire, failed_ack=True)
    assert (tmp_path / "report.json").read_bytes() == wire
    first_inode = (tmp_path / "report.json").stat().st_ino
    # A new controller reads the same durable receipt; no new boot or rewrite.
    recovered = controller.Controller.__new__(controller.Controller)
    recovered.root, recovered.binding = owner.root, owner.binding
    post(recovered, wire).assert_called_once_with(200, expected)
    assert (tmp_path / "report.json").stat().st_ino == first_inode
    changed = wire.replace(b'"size":1', b'"size":2')
    post(recovered, changed).assert_called_once_with(409)
    assert (tmp_path / "report.json").read_bytes() == wire


@pytest.mark.parametrize("case", ("auth", "path", "large", "noncanonical", "foreign", "duplicate-length", "chunked", "duplicate-auth", "short"))
def test_receiver_refuses_unsafe_request_without_a_receipt(tmp_path, monkeypatch, case):
    owner, wire = report_fixture(tmp_path, monkeypatch)
    token, path, extra = "b" * 64, "/report", ()
    if case == "auth":
        token = "wrong"
    elif case == "path":
        path = "/another"
    elif case == "large":
        wire = b"x" * (receipt.MAX_REPORT + 1)
    elif case == "noncanonical":
        wire += b"\n"
    elif case == "foreign":
        wire = wire.replace(b"c" * 64, b"d" * 64)
    elif case == "duplicate-length":
        extra = (("Content-Length", str(len(wire))),)
    elif case == "chunked":
        extra = (("Transfer-Encoding", "chunked"),)
    elif case == "duplicate-auth":
        extra = (("Authorization", "Bearer " + "b" * 64),)
    elif case == "short":
        wire = b""
    answer = post(owner, wire, token, path, extra)
    assert answer.call_args[0][0] in (400, 403)
    assert not (tmp_path / "report.json").exists()


def test_failed_durable_write_never_acknowledges(tmp_path, monkeypatch):
    owner, wire = report_fixture(tmp_path, monkeypatch)
    monkeypatch.setattr(receipt.records, "write", Mock(side_effect=OSError("fixture fsync failure")))
    with pytest.raises(OSError):
        post(owner, wire)
    assert not (tmp_path / "report.json").exists()


def test_worker_does_not_send_bearer_when_tls_refuses(monkeypatch):
    connection = Mock()
    connection.connect.side_effect = worker.tls_trust.ssl.SSLCertVerificationError("fixture")
    monkeypatch.setattr(worker, "receipt_connection", lambda request: (connection, "/report"))
    assert not worker.send_report({"reportToken": "b" * 64}, b"report")
    connection.request.assert_not_called()
    connection.close.assert_called_once_with()


@pytest.mark.parametrize("endpoint", (
    "http://192.0.2.1:8443/_bootwright/inventory/" + "a" * 32,
    "https://user@192.0.2.1:8443/_bootwright/inventory/" + "a" * 32,
    "https://192.0.2.1:8443/elsewhere", "https://192.0.2.1:8443/_bootwright/inventory/../escape"))
def test_worker_refuses_endpoint_before_reading_trust_or_connecting(endpoint):
    with pytest.raises(ValueError, match="endpoint"):
        worker.receipt_connection({"receiptURL": endpoint})


def test_unix_receiver_rejects_live_and_foreign_sockets_and_reaps_only_its_stale_socket(monkeypatch):
    # A short synthetic Unix path; simulate root ownership only, retaining real
    # socket binds/connects, mode checks, replay and cleanup on an unprivileged CI.
    from contextlib import nullcontext
    monkeypatch.setattr(receipt, "socket_context", lambda label: nullcontext())
    original = Path.lstat

    def root_socket(path, *args, **kwargs):
        fields = list(original(path, *args, **kwargs))
        fields[4], fields[5] = 0, 0
        return os.stat_result(fields)
    monkeypatch.setattr(Path, "lstat", root_socket)
    monkeypatch.setattr(receipt.records, "directory", lambda path, create, mode: None)
    with TemporaryDirectory(prefix="bw-receipt-") as directory:
        root = Path(directory)
        owner = SimpleNamespace(
            request={"receipt": {"socketRoot": directory}}, load=lambda: {"token": "a" * 64, "reportToken": "b" * 64},
            label_receipt=Mock(), receipt_label=lambda: "",
        )
        receiver = receipt.Receiver(owner)
        with pytest.raises(ValueError, match="active"):
            receipt.Receiver(owner)
        receiver.server_close()
        path = root / ("a" * 32)
        path.write_bytes(b"foreign fixture")
        with pytest.raises(ValueError, match="foreign"):
            receipt.Receiver(owner)
        assert path.read_bytes() == b"foreign fixture"
        path.unlink()
        import socket
        stale = socket.socket(socket.AF_UNIX)
        stale.bind(str(path))
        os.chmod(path, 0o660)
        stale.close()
        with receipt.Receiver(owner):
            assert path.exists()
        assert not path.exists()


def test_cleanup_keeps_receipt_service_alive_until_verified_poweroff(monkeypatch):
    owner = controller.Controller.__new__(controller.Controller)
    owner.request = {"controller": {"virtualMedia": {"restoreVerification": False, "removeCertificate": False}}}
    owner.material = {"artifactCertificate": "/fixture"}
    owner.target = Mock(side_effect=["On", "On", "Off"])
    client = Mock(inserted=Mock(return_value="https://example.test/owned.iso"))
    owner.client = lambda: client
    owner.publication = lambda: (Path("/absent-fixture/private"), "https://example.test/owned.iso")
    monkeypatch.setattr(controller.records, "read", lambda path: b"fixture certificate")
    receiver = Mock()
    owner.cleanup(True, receiver)
    assert receiver.poll.call_count == 2
    client.power.assert_not_called()


def test_retry_completes_failed_post_link_directory_sync(tmp_path, monkeypatch):
    owner, wire = report_fixture(tmp_path, monkeypatch)
    original = receipt.os.fsync
    calls = []

    def sync(fd):
        calls.append(fd)
        if len(calls) == 2:
            raise OSError("fixture failure after immutable link")
        original(fd)
    monkeypatch.setattr(receipt.os, "fsync", sync)
    with pytest.raises(OSError):
        post(owner, wire)
    assert (tmp_path / "report.json").read_bytes() == wire
    post(owner, wire).assert_called_once_with(200, worker.hashlib.sha256(wire).hexdigest().encode())
    assert len(calls) == 3


def test_socket_context_restores_previous_label_when_creation_fails(monkeypatch):
    library = Mock()
    library.getsockcreatecon_raw.return_value = 0
    library.setsockcreatecon_raw.return_value = 0
    monkeypatch.setattr(receipt.Path, "exists", lambda path: True)
    monkeypatch.setattr(receipt.ctypes, "CDLL", lambda *args, **kwargs: library)
    with pytest.raises(OSError, match="fixture"):
        with receipt.socket_context("system_u:system_r:container_t:s0:c12,c34"):
            raise OSError("fixture socket creation")
    assert library.setsockcreatecon_raw.call_count == 2
    assert library.setsockcreatecon_raw.call_args_list[0].args == (b"system_u:system_r:container_t:s0:c12,c34",)
    library.freecon.assert_called_once()


def test_socket_context_disabled_selinux_and_foreign_domains(monkeypatch):
    monkeypatch.setattr(receipt.Path, "exists", lambda path: False)
    monkeypatch.setattr(receipt.ctypes, "CDLL", Mock(side_effect=AssertionError("loaded SELinux on disabled host")))
    with receipt.socket_context(""):
        pass
    with pytest.raises(ValueError, match="state"):
        with receipt.socket_context("system_u:system_r:container_t:s0"):
            pass
    monkeypatch.setattr(receipt.Path, "exists", lambda path: True)
    with pytest.raises(ValueError, match="label"):
        with receipt.socket_context("system_u:system_r:unconfined_t:s0"):
            pass


def test_receipt_connection_preserves_declared_port_and_full_verification(monkeypatch):
    monkeypatch.setattr(worker.Path, "read_text", lambda *args, **kwargs: "fixture certificate")
    monkeypatch.setattr(worker.tls_trust, "context", lambda data: ("full-context", None))
    monkeypatch.setattr(worker.tls_trust, "anchors", lambda data: (["leaf"], []))
    monkeypatch.setattr(worker.tls_trust.ssl, "PEM_cert_to_DER_cert", lambda data: b"leaf-der")
    connection = Mock()
    monkeypatch.setattr(worker.tls_trust, "CertificateConnection", connection)
    result, path = worker.receipt_connection({"receiptURL": "https://media.example.test:8443/_bootwright/inventory/" + "a" * 32})
    connection.assert_called_once_with("media.example.test", b"leaf-der", port=8443, context="full-context", timeout=10)
    assert result == connection.return_value and path.endswith("a" * 32)


def test_concurrent_attempts_share_no_socket_or_receipt(monkeypatch):
    import http.client
    import socket
    from concurrent.futures import ThreadPoolExecutor
    from contextlib import ExitStack, nullcontext
    original = Path.lstat

    def root_socket(path, *args, **kwargs):
        fields = list(original(path, *args, **kwargs))
        fields[4], fields[5] = 0, 0
        return os.stat_result(fields)
    monkeypatch.setattr(Path, "lstat", root_socket)
    monkeypatch.setattr(receipt, "socket_context", lambda label: nullcontext())
    monkeypatch.setattr(receipt.records, "directory", lambda *args: None)
    monkeypatch.setattr(receipt.records, "read", lambda path, limit=32768: path.read_bytes())
    with TemporaryDirectory(prefix="bw-many-", dir="/tmp") as directory, ExitStack() as stack, ThreadPoolExecutor(3) as pool:
        receivers, reports = [], []
        for index, token in enumerate(("a", "b", "c")):
            owner = controller.Controller.__new__(controller.Controller)
            owner.root = Path(directory) / str(index)
            owner.root.mkdir()
            owner.binding = {"nonce": token * 64}
            owner.request = {"receipt": {"socketRoot": directory}}
            owner.load = lambda token=token: {"token": token * 64, "reportToken": token * 64}
            owner.label_receipt, owner.receipt_label = Mock(), lambda: ""
            receivers.append(stack.enter_context(receipt.Receiver(owner)))
            reports.append(canonical_json.canonical({"version": "machine-disk-inventory-v1", "binding": owner.binding, "disks": [{"fixture": index}]}))

        def send(server, data):
            connection = http.client.HTTPConnection("localhost", timeout=5)
            connection.sock = socket.socket(socket.AF_UNIX)
            connection.sock.settimeout(5)
            connection.sock.connect(str(server.path))
            try:
                connection.request("POST", "/report", data, {"Authorization": "Bearer " + server.token})
                response = connection.getresponse()
                return response.status, response.read(65)
            finally:
                connection.close()
        pending = [pool.submit(send, server, data) for server, data in zip(receivers, reports)]
        for server in receivers:
            server.poll()
        for future, server, data in zip(pending, receivers, reports):
            assert future.result() == (200, worker.hashlib.sha256(data).hexdigest().encode())
            assert server.owner.report() == data
