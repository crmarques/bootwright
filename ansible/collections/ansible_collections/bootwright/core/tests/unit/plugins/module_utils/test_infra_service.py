"""The probe packets are built and read here, so they are tested without sockets.

The observation is read over the answers systemctl and podman give, faked below,
and over real files whose modification times the tests set.
"""

from __future__ import annotations

import os
import struct

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import infra_service
from ansible_collections.bootwright.core.plugins.module_utils.infra_service import (
    PODMAN,
    SYSTEMCTL,
    dns_answers,
    dns_query,
    observe,
    probe_targets,
)

UNIT = "bootwright-lab-proxy-egress"
IMAGE = "registry.example/service@sha256:" + "0" * 64

# podman inspect executes its --format template over the container's inspect
# data, whose ImageName is a string and whose State.StartedAt is a time.Time
# (InspectContainerData and InspectContainerState in
# libpod/define/container_inspect.go, https://github.com/containers/podman), so
# .State.StartedAt.UnixNano prints the start in nanoseconds since the epoch: for
# a container started at 2026-09-30 11:38:20.675992231 -03, podman 5.8.4 printed
# this, as tests/unit/test_substrate_libvirt_replay.py records. podman exits 125
# when the error is podman's own, such as no container of that name
# (docs/source/markdown/podman.1.md, "Exit Codes"). systemctl show --value
# prints the value alone (systemctl(1), --value), which systemd 258 printed as
# "active" and a newline for a running unit on the host these tests were written
# on.
STARTED = 1790779100675992231
MINUTE, HOUR = 60 * 10**9, 3600 * 10**9


class Host:
    """systemctl and podman on a host running the service's container."""

    def __init__(self, start=(0, str(STARTED) + "\n", "")):
        self.start = start

    def __call__(self, argv, check_rc=False, environ_update=None):
        assert check_rc is False and environ_update == infra_service.ENVIRONMENT
        if argv[:2] == [SYSTEMCTL, "show"]:
            return 0, "active\n", ""
        if argv[:3] == [PODMAN, "container", "exists"]:
            return 0, "", ""
        if argv[:2] == [PODMAN, "inspect"] and argv[-1] == UNIT:
            template = argv[argv.index("--format") + 1]
            if template == "{{.ImageName}}":
                return 0, IMAGE + "\n", ""
            if template == "{{.State.StartedAt.UnixNano}}":
                return self.start
        raise AssertionError("unexpected command %r" % (argv,))


@pytest.fixture(name="service")
def service_files(tmp_path, monkeypatch):
    """The service's unit definition and configuration, each written an hour before the start."""
    monkeypatch.setattr(infra_service, "UNIT_DIRECTORY", str(tmp_path) + "/")
    root = tmp_path / "content"
    (root / "config").mkdir(parents=True)
    files = {"unit": tmp_path / (UNIT + ".container"), "configuration": root / "config" / "squid.conf"}
    for path in files.values():
        path.write_text("published\n")
        os.utime(path, ns=(STARTED - HOUR, STARTED - HOUR))
    return {"unit": UNIT, "contentRoot": str(root)}, files


def written(path, when):
    os.utime(path, ns=(when, when))


def test_a_container_started_after_its_files_runs_them(service):
    request, files = service
    assert observe(Host(), request, [str(files["configuration"])]) == {
        "unit": "active", "container": IMAGE, "containerPresent": True, "contentRoot": True, "startedAfterFiles": True,
    }
    written(files["configuration"], STARTED)
    assert observe(Host(), request, [str(files["configuration"])])["startedAfterFiles"] is True


# An apply stopped between publishing a file and restarting leaves the daemon on
# the file it read at its start, so the container is older than the file.
@pytest.mark.parametrize("newer", ["configuration", "unit"])
def test_a_container_older_than_a_file_it_runs_from_runs_an_earlier_one(service, newer):
    request, files = service
    written(files[newer], STARTED + MINUTE)
    observation = observe(Host(), request, [str(files["configuration"])])
    assert observation["startedAfterFiles"] is False
    assert (observation["unit"], observation["container"], observation["contentRoot"]) == ("active", IMAGE, True)


def test_the_unit_definition_is_compared_when_no_other_file_is_named(service):
    request, files = service
    written(files["unit"], STARTED + HOUR)
    assert observe(Host(), request)["startedAfterFiles"] is False


@pytest.mark.parametrize("start", [
    (125, "", ""),
    (0, "not a time\n", ""),
], ids=["a podman error", "an unreadable start"])
def test_a_start_that_cannot_be_read_proves_nothing(service, start):
    request, files = service
    assert observe(Host(start), request, [str(files["configuration"])])["startedAfterFiles"] is False


def test_a_file_that_cannot_be_read_proves_nothing(service):
    request, files = service
    files["configuration"].unlink()
    assert observe(Host(), request, [str(files["configuration"])])["startedAfterFiles"] is False


@pytest.mark.parametrize("host", [Host(), Host((125, "", ""))], ids=["running", "a podman error"])
def test_a_relative_file_is_refused(service, host):
    request, _files = service
    with pytest.raises(ValueError):
        observe(host, request, ["config/squid.conf"])


def test_dns_query_asks_one_a_record_for_the_exact_name():
    payload = dns_query("controller.lab.example.test")
    identifier, flags, questions, answers = struct.unpack(">HHHH", payload[:8])
    assert (identifier, flags, questions, answers) == (0x4257, 0x0100, 1, 0)
    assert payload.endswith(struct.pack(">HH", 1, 1))
    assert b"\x0acontroller" in payload


def test_dns_answers_counts_only_a_matching_reply():
    payload = dns_query("controller.lab.example.test")
    reply = payload[:6] + struct.pack(">H", 2) + payload[8:]
    assert dns_answers(payload, reply) == 2
    with pytest.raises(ValueError):
        dns_answers(payload, b"\x00\x00" + reply[2:])
    with pytest.raises(ValueError):
        dns_answers(payload, b"short")


def test_probe_targets_follow_the_bind_address_then_the_endpoints():
    exact = {"bindAddress": "192.0.2.1", "endpoints": [{"address": "192.0.2.9"}]}
    assert probe_targets(exact) == ["192.0.2.1"]
    wildcard = {
        "bindAddress": "0.0.0.0",
        "endpoints": [{"address": "192.0.2.9"}, {"address": "192.0.2.1"}, {"address": "192.0.2.9"}],
    }
    assert probe_targets(wildcard) == ["192.0.2.1", "192.0.2.9"]
    assert probe_targets({"bindAddress": "::", "endpoints": []}) == []
