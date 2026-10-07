"""The managed-service inspection compares the start with the files its task names.

The comparison itself is tested over infra_service.observe in
tests/unit/plugins/module_utils/test_infra_service.py. These cases run the
module as a task would, so a role's runs_from reaches that comparison and a
configuration published after the start is not read as the one the service
runs.
"""

from __future__ import annotations

import json
import os
import socket

import pytest
from ansible.module_utils.basic import AnsibleModule
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.module_utils import host_sockets, infra_service
from ansible_collections.bootwright.core.plugins.modules import infra_service_inspect as inspect

UNIT = "bootwright-lab-proxy-egress"
IMAGE = "registry.example/service@sha256:" + "0" * 64

# systemctl show --value prints the value alone (systemctl(1), --value), and
# podman inspect executes its --format template over the container's inspect
# data, whose State.StartedAt is a time.Time (InspectContainerState in
# libpod/define/container_inspect.go, https://github.com/containers/podman):
# for a container started at 2026-09-30 11:38:20.675992231 -03, podman 5.8.4
# printed this start, as tests/unit/test_substrate_libvirt_replay.py records.
STARTED = 1790779100675992231
MINUTE = 60 * 10**9
HOUR = 3600 * 10**9


def host(_module, argv, check_rc=False, environ_update=None):
    """systemctl and podman on a host running the service's container."""
    assert check_rc is False and environ_update == infra_service.ENVIRONMENT
    if argv[:2] == [infra_service.SYSTEMCTL, "show"]:
        return 0, "active\n", ""
    if argv[:3] == [infra_service.PODMAN, "container", "exists"]:
        return 0, "", ""
    if argv[:2] == [infra_service.PODMAN, "inspect"] and argv[-1] == UNIT:
        template = argv[argv.index("--format") + 1]
        if template == "{{.ImageName}}":
            return 0, IMAGE + "\n", ""
        if template == "{{.State.StartedAt.UnixNano}}":
            return 0, str(STARTED) + "\n", ""
    raise AssertionError("unexpected command %r" % (argv,))


@pytest.fixture(name="service")
def service_files(tmp_path, monkeypatch):
    """A unit definition written an hour before the start and a configuration file."""
    monkeypatch.setattr(infra_service, "UNIT_DIRECTORY", str(tmp_path) + "/")
    monkeypatch.setattr(AnsibleModule, "run_command", host)
    unit = tmp_path / (UNIT + ".container")
    unit.write_text("published\n")
    os.utime(unit, ns=(STARTED - HOUR, STARTED - HOUR))
    root = tmp_path / "content"
    (root / "config").mkdir(parents=True)
    configuration = root / "config" / "squid.conf"
    configuration.write_text("published\n")
    return {"unit": UNIT, "contentRoot": str(root)}, configuration


# An apply stopped between publishing the configuration and restarting leaves
# the daemon on the file it read at its start, so the container is older than
# the file the role names.
@pytest.mark.parametrize("written, started_after", [
    (STARTED + MINUTE, False),
    (STARTED - HOUR, True),
], ids=["published after the start", "published before the start"])
def test_the_module_compares_the_start_with_the_files_its_task_names(service, capsys, written, started_after):
    request, configuration = service
    os.utime(configuration, ns=(written, written))
    result = run({"request": request, "runs_from": [str(configuration)]}, capsys)
    assert result["changed"] is False
    assert result["observation"] == {
        "unit": "active", "container": IMAGE, "containerPresent": True, "contentRoot": True,
        "startedAfterFiles": started_after,
    }


def run(arguments, capsys):
    """One module run in process, as a task would make it, and what it returned."""
    capsys.readouterr()
    with patch_module_args(arguments), pytest.raises(SystemExit):
        inspect.main()
    return json.loads(capsys.readouterr().out)


def state_host(unit, present):
    """systemctl and podman on a host whose unit is in `unit` and whose container is or is not present."""
    def run_command(_module, argv, check_rc=False, environ_update=None):
        assert check_rc is False and environ_update == infra_service.ENVIRONMENT
        if argv[:2] == [infra_service.SYSTEMCTL, "show"]:
            return 0, unit + "\n", ""
        if argv[:3] == [infra_service.PODMAN, "container", "exists"]:
            return (0 if present else 1), "", ""
        if argv[:2] == [infra_service.PODMAN, "inspect"]:
            return (0, IMAGE + "\n", "") if present else (125, "", "no such container")
        raise AssertionError("unexpected command %r" % (argv,))
    return run_command


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


# A unit that is active, or a container that exists, may hold the socket
# itself, so only a service with neither reads what listens there as foreign.
@pytest.mark.parametrize("unit, present, foreign", [
    ("active", True, False), ("failed", True, False), ("", False, True), ("failed", False, True),
], ids=["active", "container present", "undefined", "failed"])
def test_the_module_reports_a_listener_the_service_does_not_own(service, listener, monkeypatch, capsys, unit, present, foreign):
    request, _configuration = service
    request = dict(request, kind="Proxy", bindAddress="127.0.0.1", port=listener)
    monkeypatch.setattr(AnsibleModule, "run_command", state_host(unit, present))
    result = run({"request": request, "foreign": True}, capsys)
    assert result["foreign"] == ([{"transport": "tcp", "address": "127.0.0.1", "port": listener}] if foreign else [])
    assert "foreign" not in run({"request": request}, capsys)
