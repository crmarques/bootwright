"""Starting firewalld on a host first proves sshd stays reachable.

A remote placement proves the port of its own connection in the zone of the
interface it reaches. A local start has no connection to name, and the operator
may still be logged in over SSH, so it proves every port a running sshd listens
on in every zone a connected interface uses, and starts nothing otherwise.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import managedos_firewall_worker as worker
from ansible_collections.bootwright.core.plugins.modules import managedos_host_firewall as module

ZONES = "public (default, active)\n  target: default\n  interfaces: eth0\n  sources: \n  rich rules: \n"


class Host:
    """A host whose commands answer from a table and record what started."""

    def __init__(self, sshd="port 22\n", admitted=("22",), zone=b"", devices=b"eth0:connected\nlo:connected (externally)\n"):
        self.started = []
        self.answers = {
            ("/usr/bin/systemctl", "is-enabled", "firewalld.service"): (1, b"disabled"),
            ("/usr/bin/systemctl", "is-active", "sshd.service"): (0 if sshd is not None else 3, b""),
            ("/usr/sbin/sshd", "-T"): (0, (sshd or "").encode()),
            (module.OFFLINE, "--list-all-zones"): (0, ZONES.encode()),
            (module.OFFLINE, "--get-policies"): (0, b""),
            (module.OFFLINE, "--direct", "--get-all-rules"): (0, b""),
            (module.OFFLINE, "--direct", "--get-all-passthroughs"): (0, b""),
            (module.OFFLINE, "--direct", "--get-all-chains"): (0, b""),
            (module.OFFLINE, "--get-default-zone"): (0, b"public"),
            (module.NMCLI, "--get-values", "DEVICE,STATE", "device"): (0, devices),
            (module.NMCLI, "--get-values", "GENERAL.CONNECTION", "device", "show", "eth0"): (0, b"System eth0"),
            (module.NMCLI, "--get-values", "connection.zone", "connection", "show", "System eth0"): (0, zone),
            (module.OFFLINE, "--zone=public", "--query-service=ssh"): (1, b""),
            (module.OFFLINE, "--info-service=ssh"): (0, b"ssh\n  ports: 22/tcp\n"),
        }
        for port in admitted:
            self.answers[(module.OFFLINE, "--zone=public", "--query-port=%s/tcp" % port)] = (0, b"")

    def command(self, *arguments):
        return (1, b"not running")

    def run(self, argv):
        if argv[:2] == ["/usr/bin/systemctl", "enable"]:
            self.started.append(argv)
            return (0, b"")
        return self.answers.get(tuple(argv), (1, b""))


def prepared(host):
    return module.prepare(host, {"ssh_port": 0, "ssh_address": ""})


def test_a_local_start_proves_every_listening_port_before_it_starts():
    host = Host(sshd="port 22\nport 2222\n", admitted=("22", "2222"))
    assert prepared(host) is True
    assert host.started == [["/usr/bin/systemctl", "enable", "--now", "firewalld.service"]]


@pytest.mark.parametrize("host", [
    Host(sshd="port 2222\n", admitted=("22",)),
    Host(sshd="port 22\nport 2222\n", admitted=("2222",)),
    Host(sshd="", admitted=()),
    Host(devices=b"lo:connected (externally)\n"),
], ids=["an unadmitted port", "one of two ports", "no port in sshd's configuration", "no connected interface"])
def test_a_local_start_that_would_lock_out_ssh_refuses_and_starts_nothing(host):
    with pytest.raises(worker.Refused) as refused:
        prepared(host)
    assert (refused.value.reason, refused.value.cause) == ("host-firewall-failed", "ssh-admission")
    assert host.started == []


def test_a_local_start_without_a_running_sshd_has_nothing_to_prove():
    host = Host(sshd=None, admitted=())
    assert prepared(host) is True and host.started
