"""Admin commands name every placed MON on the retained bootstrap configuration, whose mon_host names only the seed."""

from __future__ import annotations

import copy
import json
import os
import re

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_tunnel_custody as custody
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_bootstrap_recovery as recovery
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tunnel_native as fixtures

cluster_request = fixtures.cluster_request
native = fixtures.native

SEED_CONFIG = (b"# minimal ceph.conf for 4c0a4300-aa43-458c-86d7-ac2256d1fc00\n[global]\n\tfsid = 4c0a4300-aa43-458c-86d7-ac2256d1fc00\n"
               b"\tmon_host = [v2:192.0.2.1:3300/0,v1:192.0.2.1:6789/0]\n")
KEYRING = b"[client.admin]\n\tkey = AQBsynthetic==\n"


def test_the_list_is_the_seed_then_each_placed_mon_inside_a_public_network(cluster_request):
    assert ceph_native.admin_monitors(cluster_request) == "192.0.2.1,192.0.2.2,192.0.2.3"
    request = copy.deepcopy(cluster_request)
    request["hosts"][2]["address"] = "203.0.113.3"
    request["services"][0]["placement"]["hosts"] = ["node-2", "node-3", "node-1"]
    assert ceph_native.admin_monitors(request) == "192.0.2.1,192.0.2.2"
    request["services"][0]["placement"]["hosts"] = ["node-1"]
    assert ceph_native.admin_monitors(request) == "192.0.2.1"


def test_an_ipv6_monitor_is_bracketed_and_a_name_or_uncanonical_address_is_never_named(cluster_request):
    request = copy.deepcopy(cluster_request)
    request["publicNetworks"] = ["2001:db8::/64", "192.0.2.0/24"]
    request["monIP"] = request["hosts"][0]["address"] = "2001:db8::1"
    request["hosts"][1]["address"] = "2001:db8:0::2"
    request["hosts"][2]["address"] = "node-3.example.test"
    assert ceph_native.admin_monitors(request) == "[2001:db8::1]"
    request["hosts"][1]["address"] = "2001:db8::2"
    assert ceph_native.admin_monitors(request) == "[2001:db8::1],[2001:db8::2]"


def captured(native):
    """The seed's retained bootstrap outputs, as cephadm writes them: its mon_host names the seed MON alone."""
    recovery.protocol(native)
    recovery.sources(native)
    for name, content in (("config", SEED_CONFIG), ("keyring", KEYRING)):
        os.unlink(native.bootstrap_sources()[name])
        ceph_native.private_write(native.bootstrap_sources()[name], content)
    native.daemons = lambda: fixtures.initial_daemons(native)
    native.capture_bootstrap_outputs()
    del native.ceph, native.command


def mon_addresses(value):
    return set(re.findall(r"[0-9]+(?:\.[0-9]+){3}", value))


class Monitors:
    """Three MONs whose seed is stopped: the other two hold quorum, so a client answers once it names any of them."""

    def __init__(self, native, running=("192.0.2.2", "192.0.2.3")):
        self.native, self.running, self.calls = native, {address: "node-" + address[-1] for address in running}, []

    def run(self, argv, data=b"", timeout=60):
        del data
        self.calls.append(list(argv))
        assert argv[1:3] == ["--conf", self.native.bootstrap_sources()["config"]]
        with open(argv[2], "rb") as handle:
            assert handle.read() == SEED_CONFIG
        named = argv[argv.index("-m") + 1] if "-m" in argv else re.search(rb"mon_host = (.*)", SEED_CONFIG).group(1).decode()
        if not mon_addresses(named) & set(self.running) or len(self.running) < 2:
            return 124, b"monclient(hunting): authenticate timed out after " + str(timeout).encode()
        return 0, json.dumps({"fsid": self.native.request["fsid"], "quorum_names": sorted(self.running.values())}).encode()


def test_an_admin_command_reaches_a_quorum_mon_with_the_seed_stopped(native):
    captured(native)
    monitors = Monitors(native)
    native.run = monitors.run
    assert native.ceph("quorum_status")["quorum_names"] == ["node-2", "node-3"]
    assert native.ceph_text("fsid") and native.ceph_status("health") == 0
    assert [argv[3:5] for argv in monitors.calls] == [["-m", "192.0.2.1,192.0.2.2,192.0.2.3"]] * 3
    with native.bootstrap_hold() as material:
        assert material["config"] == SEED_CONFIG
    first = monitors.calls[0]
    assert monitors.run(first[:3] + first[5:])[0] == 124, "the retained seed-only mon_host alone times out"


class Impostor(Monitors):
    """The seed is stopped and a listener on its address offers auth method none; only a client that admits none answers it."""

    def run(self, argv, data=b"", timeout=60):
        if "--auth_client_required=cephx" not in argv:
            self.calls.append(list(argv))
            return 0, json.dumps({"fsid": "00000000-0000-4000-8000-000000000000", "quorum_names": ["impostor"]}).encode()
        return super().run(argv, data, timeout)


def test_an_admin_command_requires_cephx_so_a_listener_offering_none_is_never_answered(native):
    captured(native)
    impostor = Impostor(native)
    native.run = impostor.run
    assert native.ceph("quorum_status")["quorum_names"] == ["node-2", "node-3"]
    assert all("--auth_client_required=cephx" in argv for argv in impostor.calls)


def test_lost_quorum_still_fails_the_admin_command(native):
    captured(native)
    native.run = Monitors(native, running=("192.0.2.2",)).run
    with pytest.raises(ceph_native.NativeFailure, match="^native-command$"):
        native.ceph("quorum_status")


@pytest.mark.parametrize("name", ["config", "keyring"])
def test_changed_retained_material_refuses_before_any_admin_command(native, name):
    captured(native)
    monitors = Monitors(native)
    native.run = monitors.run
    path = native.bootstrap_sources()[name]
    before = os.stat(path)
    with open(path, "r+b") as handle:
        content = handle.read()
        handle.seek(0)
        handle.write(content.replace(b"192.0.2.1", b"192.0.2.9") if name == "config" else content.replace(b"AQBsynthetic", b"AQBsynthetiX"))
    os.utime(path, ns=(before.st_atime_ns, before.st_mtime_ns))
    assert os.stat(path).st_size == before.st_size
    with pytest.raises((ceph_native.Refusal, custody.CustodyRefusal)):
        native.ceph("quorum_status")
    assert monitors.calls == []


@pytest.mark.parametrize("name", ["config", "keyring"])
def test_foreign_material_with_the_same_bytes_refuses_before_any_admin_command(native, name):
    captured(native)
    monitors = Monitors(native)
    native.run = monitors.run
    path = native.bootstrap_sources()[name]
    replacement = path + ".foreign"
    ceph_native.private_write(replacement, SEED_CONFIG if name == "config" else KEYRING)
    os.replace(replacement, path)
    with pytest.raises((ceph_native.Refusal, custody.CustodyRefusal)):
        native.ceph("quorum_status")
    assert monitors.calls == []
