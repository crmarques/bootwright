"""The service inspection reports a listener the artifact server does not own.

The socket rule itself is tested over host_sockets and artifact_server.foreign
in tests/unit/plugins/module_utils/test_host_sockets.py. These cases run the
module as a task would, so the role's `foreign: true` reaches that rule, an
unreadable table fails the task, and a run without the option is unchanged.
"""

from __future__ import annotations

import json
import socket

import pytest
from ansible.module_utils.basic import AnsibleModule
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.module_utils import artifact_server, bounded_process, host_sockets, infra_service
from ansible_collections.bootwright.core.plugins.modules import infra_service_inspect as inspect

UNIT = "bootwright-lab-artifacts"
VERSION = "artifact-server-nginx-v1"
IMAGE = "registry.example/nginx@sha256:" + "0" * 64


def state_host(present):
    """podman on a host where the server's container is or is not present."""
    def run_command(_module, argv, check_rc=False, environ_update=None, handle_exceptions=True):
        assert check_rc is False and environ_update == infra_service.ENVIRONMENT
        if argv[:3] == [infra_service.PODMAN, "container", "exists"]:
            return (0 if present else 1), "", ""
        if argv[:2] == [infra_service.PODMAN, "inspect"]:
            return (0, IMAGE + "\n", "") if present else (125, "", "no such container")
        raise AssertionError("unexpected command %r" % (argv,))
    return run_command


def bounded_host(monkeypatch, host):
    """Answer the module's bounded runs from a run_command-shaped fake host; Ansible's own run_command must not run."""
    def run(argv, data=b"", timeout=60, limit=bounded_process.MAX_OUTPUT, *, environment, **options):
        assert timeout == infra_service.COMMAND_SECONDS and limit == bounded_process.MAX_OUTPUT and data == b"" and options == {}
        code, out, err = host(None, argv, check_rc=False, environ_update=environment)
        return bounded_process.Result(code, out.encode(), err.encode())

    monkeypatch.setattr(bounded_process, "run", run)
    monkeypatch.setattr(AnsibleModule, "run_command", lambda *args, **kwargs: pytest.fail("the module used run_command"))


def run(arguments, capsys):
    """One module run in process, as a task would make it, and what it returned."""
    capsys.readouterr()
    with patch_module_args(arguments), pytest.raises(SystemExit):
        inspect.main()
    return json.loads(capsys.readouterr().out)


@pytest.fixture(name="listener")
def planted_listener(monkeypatch):
    """A real TCP listener this process holds, read from its own socket tables in place of PID 1's."""
    own = host_sockets.table_lines
    monkeypatch.setattr(host_sockets, "table_lines", lambda path: own(path.replace("/proc/1/", "/proc/self/")))
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.bind(("127.0.0.1", 0))
    sock.listen(1)
    yield sock.getsockname()[1]
    sock.close()


def server(tmp_path, port):
    """A request whose second listener binds the planted port."""
    free = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    free.bind(("127.0.0.1", 0))
    other = free.getsockname()[1]
    free.close()
    return {
        "unit": UNIT, "contentRoot": str(tmp_path), "bindAddress": "127.0.0.1", "version": VERSION,
        "listeners": [{"name": "http", "port": other, "protocol": "http"},
                      {"name": "https", "port": port, "protocol": "https"}],
    }


# A unit that is active, or a container that exists, may hold the socket
# itself, so only a server with neither reads what listens there as foreign.
@pytest.mark.parametrize("unit, present, foreign", [
    ("active", False, False), ("failed", True, False), ("", False, True), ("failed", False, True),
], ids=["active", "container present", "undefined", "failed"])
def test_the_module_reports_a_listener_the_server_does_not_own(tmp_path, listener, monkeypatch, capsys, unit, present, foreign):
    request = server(tmp_path, listener)
    monkeypatch.setattr(infra_service, "unit_state", lambda _runner, _path, _service: unit)
    bounded_host(monkeypatch, state_host(present))
    result = run({"request": request, "foreign": True}, capsys)
    assert result.get("failed") is not True
    assert result["foreign"] == ([{"transport": "tcp", "address": "127.0.0.1", "port": listener}] if foreign else [])
    assert "foreign" not in run({"request": request}, capsys)


@pytest.mark.parametrize("error", [OSError, ValueError])
def test_an_unreadable_socket_table_fails_the_task(tmp_path, monkeypatch, capsys, error):
    def unreadable(_path):
        raise error("table")
    monkeypatch.setattr(host_sockets, "table_lines", unreadable)
    monkeypatch.setattr(infra_service, "unit_state", lambda _runner, _path, _service: "")
    bounded_host(monkeypatch, state_host(False))
    result = run({"request": server(tmp_path, 8443), "foreign": True}, capsys)
    assert result["failed"] is True
    assert result["msg"] == "the host's socket tables could not be read: %s" % error.__name__
    assert "foreign" not in result


@pytest.mark.parametrize("failure,reason", [(socket.timeout(), "readiness-timeout"), (ConnectionRefusedError(), "listener-failed")])
def test_readiness_classifies_timeout_and_refusal(monkeypatch, capsys, failure, reason):
    monkeypatch.setattr(artifact_server, "observe", lambda *_: {"unit": "active"})
    monkeypatch.setattr(artifact_server, "probe_targets", lambda _: [{"name": "http", "address": "192.0.2.1", "port": 8080, "protocol": "http"}])

    def fail(_target):
        raise failure
    monkeypatch.setattr(artifact_server, "probe", fail)
    result = run({"request": {"version": VERSION}, "readiness": True, "attempts": 1}, capsys)
    assert result["failures"] == [{"port": 8080, "reason": reason}]
    assert not result["listeners"]


def test_readiness_certificate_mismatch_never_completes(monkeypatch, capsys):
    target = {"name": "https", "address": "192.0.2.1", "port": 8443, "protocol": "https"}
    monkeypatch.setattr(artifact_server, "observe", lambda *_: {"unit": "active"})
    monkeypatch.setattr(artifact_server, "probe_targets", lambda _: [target])
    monkeypatch.setattr(artifact_server, "probe", lambda _: dict(target, fingerprint="b" * 64, status="HTTP/1.1 200 OK"))
    result = run({"request": {"version": VERSION}, "readiness": True, "attempts": 1, "fingerprint": "a" * 64}, capsys)
    assert result["failures"] == [{"port": 8443, "reason": "certificate-mismatch"}]
    assert not result["listeners"]


@pytest.mark.parametrize("fingerprint", [None, "", "invalid", "A" * 64])
def test_missing_or_malformed_fingerprint_is_unknown(monkeypatch, capsys, fingerprint):
    target = {"name": "https", "address": "192.0.2.1", "port": 8443, "protocol": "https"}
    answer = dict(target, status="HTTP/1.1 200 OK")
    if fingerprint is not None:
        answer["fingerprint"] = fingerprint
    monkeypatch.setattr(artifact_server, "observe", lambda *_: {"unit": "active"})
    monkeypatch.setattr(artifact_server, "probe_targets", lambda _: [target])
    monkeypatch.setattr(artifact_server, "probe", lambda _: answer)
    result = run({"request": {"version": VERSION}, "readiness": True, "attempts": 1, "fingerprint": "a" * 64}, capsys)
    assert result["failures"] == [{"port": 8443, "reason": "readiness-unproved"}]
    assert not result["listeners"]


def test_a_host_command_past_its_bound_fails_the_observation(tmp_path, monkeypatch, capsys):
    def outran(*args, **kwargs):
        raise bounded_process.Failure("timeout")
    monkeypatch.setattr(bounded_process, "run", outran)
    monkeypatch.setattr(AnsibleModule, "run_command", lambda *args, **kwargs: pytest.fail("the module used run_command"))
    result = run({"request": server(tmp_path, 8443)}, capsys)
    assert result["failed"] is True and result["msg"] == "the artifact server could not be observed: TimeoutError"
