"""The artifact-server inspection reports a listener the server does not own.

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

from ansible_collections.bootwright.core.plugins.module_utils import artifact_server, host_sockets
from ansible_collections.bootwright.core.plugins.modules import artifact_server_inspect as inspect

UNIT = "bootwright-lab-artifacts"
IMAGE = "registry.example/nginx@sha256:" + "0" * 64


def state_host(present):
    """podman on a host where the server's container is or is not present."""
    def run_command(_module, argv, check_rc=False, environ_update=None):
        assert check_rc is False and environ_update == artifact_server.ENVIRONMENT
        if argv[:3] == [artifact_server.PODMAN, "container", "exists"]:
            return (0 if present else 1), "", ""
        if argv[:2] == [artifact_server.PODMAN, "inspect"]:
            return (0, IMAGE + "\n", "") if present else (125, "", "no such container")
        raise AssertionError("unexpected command %r" % (argv,))
    return run_command


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
        "unit": UNIT, "contentRoot": str(tmp_path), "bindAddress": "127.0.0.1",
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
    monkeypatch.setattr(artifact_server, "unit_state", lambda _runner, _path, _service: unit)
    monkeypatch.setattr(AnsibleModule, "run_command", state_host(present))
    result = run({"request": request, "foreign": True}, capsys)
    assert result.get("failed") is not True
    assert result["foreign"] == ([{"transport": "tcp", "address": "127.0.0.1", "port": listener}] if foreign else [])
    assert "foreign" not in run({"request": request}, capsys)


@pytest.mark.parametrize("error", [OSError, ValueError])
def test_an_unreadable_socket_table_fails_the_task(tmp_path, monkeypatch, capsys, error):
    def unreadable(_path):
        raise error("table")
    monkeypatch.setattr(host_sockets, "table_lines", unreadable)
    monkeypatch.setattr(artifact_server, "unit_state", lambda _runner, _path, _service: "")
    monkeypatch.setattr(AnsibleModule, "run_command", state_host(False))
    result = run({"request": server(tmp_path, 8443), "foreign": True}, capsys)
    assert result["failed"] is True
    assert result["msg"] == "the host's socket tables could not be read: %s" % error.__name__
    assert "foreign" not in result
