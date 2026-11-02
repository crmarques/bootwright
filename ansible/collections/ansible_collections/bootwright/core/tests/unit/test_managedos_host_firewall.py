"""The managed OS host firewall realizes firewalld services on an installed machine, owns only the service files that carry its
mark, and publishes the evidence of each service.

The guest worker is driven against a fake firewalld and a temporary services directory, so nothing here reaches a host. The
controller module is driven over a recording runner, the protocol action over a recording channel, and the role's task files
are rendered with Ansible's controller, which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import hashlib
import io
import json
import os
import pathlib
import shlex
from types import SimpleNamespace

import pytest
import yaml
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

from ansible_collections.bootwright.core.plugins.action import managedos_firewall_protocol as protocol
from ansible_collections.bootwright.core.plugins.module_utils import bounded_process, managedos_firewall_worker as worker, managedos_guest_ssh
from ansible_collections.bootwright.core.plugins.modules import managedos_firewall as module

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
ROLE = COLLECTION / "roles" / "managedos_host_firewall"
LOADER = DataLoader()
DIGEST = "0123456789abcdef" * 4
MARK = "owner context=lab block=os-firewall-ceph-01 apply=op-0123"
RGW = "lab-ceph-ingress-rgw"
NFS = "lab-ceph-ingress-nfs"
NETWORK = "192.0.2.60/32"
ADDRESSES = {"eth0": ["192.0.2.10"], "eth1": ["198.51.100.5"], "lo": ["127.0.0.1"]}
ZONES = {"eth0": "public", "eth1": "internal"}


def xml(name, mark=MARK, port=443):
    return ('<?xml version="1.0" encoding="utf-8"?>\n<service>\n  <short>%s</short>\n  <description>%s</description>\n'
            '  <port protocol="tcp" port="%d"/>\n</service>\n' % (name, mark, port))


def service(name=RGW, networks=(NETWORK,), port=443, mark=MARK):
    return {"name": name, "networks": list(networks), "content": xml(name, mark, port)}


class FakeFirewalld:
    """A recording firewalld and ip: the permanent and runtime zone memberships, the zone of each interface, the services
    firewalld has loaded, which only a reload refreshes from the service files on disk, and a reload that makes the runtime what
    the permanent configuration holds."""

    def __init__(self, directory, running=True, addresses=None, zones=None, default="public"):
        self.directory = directory
        self.running = running
        self.addresses = dict(ADDRESSES if addresses is None else addresses)
        self.zones = dict(ZONES if zones is None else zones)
        self.default = default
        self.permanent = {"public": set(), "internal": set(), "drop": set()}
        self.runtime = {"public": set(), "internal": set(), "drop": set()}
        self.known = set()
        self.bare = {}
        self.calls = []
        self.fail = {}

    @staticmethod
    def info(text):
        local, _slash, length = text.partition("/")
        return {"family": "inet6" if ":" in local else "inet", "local": local, "prefixlen": int(length or 24)}

    def installed(self, name):
        return (self.directory / (name + ".xml")).exists()

    def __call__(self, argv):
        self.calls.append(list(argv))
        arguments = argv[1:]
        if argv[0] == worker.IP:
            return 0, json.dumps([{"ifname": name, "addr_info": [self.info(local) for local in locals_] + self.bare.get(name, [])}
                                  for name, locals_ in sorted(self.addresses.items())]).encode()
        verb = next((word for word in arguments if word.startswith("--") and not word.startswith("--zone=") and word != "--permanent"), "")
        for key, code in self.fail.items():
            if verb.startswith(key):
                return code, b""
        if verb == "--state":
            return (0, b"running\n") if self.running else (252, b"")
        if verb.startswith("--get-zone-of-interface="):
            zone = self.zones.get(verb.partition("=")[2])
            return (0, zone.encode() + b"\n") if zone else (1, b"")
        if verb == "--get-default-zone":
            return 0, self.default.encode() + b"\n"
        if verb == "--get-zones":
            return 0, " ".join(sorted(self.permanent)).encode() + b"\n"
        if verb == "--get-services":
            assert "--permanent" in arguments
            return 0, " ".join(["ssh"] + sorted(self.known)).encode() + b"\n"
        if verb == "--reload":
            self.runtime = {zone: set(names) for zone, names in self.permanent.items()}
            self.known = {path.stem for path in self.directory.glob("*.xml")}
            return 0, b""
        return self.zoned(arguments, verb)

    def zoned(self, arguments, verb):
        permanent = "--permanent" in arguments
        zone = next(word.partition("=")[2] for word in arguments if word.startswith("--zone="))
        name = verb.partition("=")[2]
        table = self.permanent if permanent else self.runtime
        if verb.startswith("--query-service="):
            if name not in self.known:
                return 101, b""
            return (0, b"yes\n") if name in table[zone] else (1, b"no\n")
        if verb.startswith("--add-service="):
            assert permanent and self.installed(name), "a service is added to a zone only after its file is written"
            if name not in self.known:
                return 101, b""
            table[zone].add(name)
            return 0, b""
        assert verb.startswith("--remove-service=") and permanent
        table[zone].discard(name)
        return 0, b""

    def verbs(self):
        return [word for call in self.calls for word in call[1:] if word in ("--reload",) or word.startswith(("--add-service", "--remove-service"))]


@pytest.fixture
def directory(tmp_path):
    path = tmp_path / "services"
    path.mkdir()
    return path


@pytest.fixture
def host(directory):
    return FakeFirewalld(directory)


def guest(host):
    return worker.Guest(run=host, directory=str(host.directory), owner=os.getuid())


def ask(host, operation, services, mark=MARK):
    """The worker's one answer to a request on its standard input."""
    stdout = io.StringIO()
    request = {"operation": operation, "mark": mark, "services": services}
    assert worker.main(stdin=io.BytesIO(json.dumps(request).encode()), stdout=stdout, guest=guest(host)) == 0
    assert stdout.getvalue().count("\n") == 1 and stdout.getvalue().endswith("\n")
    return json.loads(stdout.getvalue())


def entry(name, state="owned", covered=True, memberships=((("public", "eth0")),), content=None):
    digest = hashlib.sha256(content.encode()).hexdigest() if content else ""
    return {"covered": covered, "digest": digest, "memberships": [{"interface": i, "zone": z} for z, i in memberships],
            "name": name, "state": state}


def test_synthetic_realization_writes_the_file_adds_the_membership_reloads_and_proves_it(host, directory):
    wanted = service()
    reply = ask(host, "apply", [wanted])
    assert reply == {"ok": True, "result": {"changed": True, "services": [entry(RGW, content=wanted["content"])]}}
    path = directory / (RGW + ".xml")
    assert path.read_text() == wanted["content"] and path.stat().st_mode & 0o777 == 0o644
    assert sorted(directory.iterdir()) == [path]
    assert host.permanent["public"] == {RGW} and host.runtime["public"] == {RGW}
    assert host.verbs() == ["--reload", "--add-service=" + RGW, "--reload"]
    assert worker.valid_evidence(reply["result"]["services"], [RGW])
    assert ask(host, "inspect", [wanted])["result"] == {"changed": False, "services": reply["result"]["services"]}


def test_a_change_rewrites_only_the_owned_file_whose_bytes_differ(host, directory):
    first, second = service(RGW), service(NFS, port=2049)
    ask(host, "apply", [first, second])
    untouched = (directory / (NFS + ".xml")).stat().st_mtime_ns
    host.calls.clear()
    changed = service(RGW, port=8443)
    reply = ask(host, "apply", [changed, second])
    assert reply["ok"] and reply["result"]["changed"] is True
    assert (directory / (RGW + ".xml")).read_text() == changed["content"]
    assert (directory / (NFS + ".xml")).stat().st_mtime_ns == untouched
    assert host.verbs() == ["--reload"]
    digests = {item["name"]: item["digest"] for item in reply["result"]["services"]}
    assert digests == {RGW: hashlib.sha256(changed["content"].encode()).hexdigest(), NFS: hashlib.sha256(second["content"].encode()).hexdigest()}


def test_a_replay_of_a_realized_service_writes_adds_and_reloads_nothing(host, directory):
    wanted = service()
    ask(host, "apply", [wanted])
    before = (directory / (RGW + ".xml")).stat().st_mtime_ns
    host.calls.clear()
    reply = ask(host, "apply", [wanted])
    assert reply["ok"] and reply["result"]["changed"] is False
    assert host.verbs() == []
    assert (directory / (RGW + ".xml")).stat().st_mtime_ns == before
    assert reply["result"]["services"] == [entry(RGW, content=wanted["content"])]


def test_a_replay_reloads_when_the_runtime_lost_a_permanent_membership(host):
    wanted = service()
    ask(host, "apply", [wanted])
    host.runtime["public"].clear()
    host.calls.clear()
    reply = ask(host, "apply", [wanted])
    assert reply["result"]["changed"] is True and host.verbs() == ["--reload"] and host.runtime["public"] == {RGW}


def test_a_foreign_service_file_refuses_and_nothing_is_written_for_any_service(host, directory):
    (directory / (NFS + ".xml")).write_text(xml(NFS, mark="someone-else"))
    reply = ask(host, "apply", [service(RGW), service(NFS, port=2049)])
    assert reply == {"ok": False, "reason": "host-firewall-foreign", "detail": NFS}
    assert sorted(path.name for path in directory.iterdir()) == [NFS + ".xml"]
    assert host.verbs() == []


@pytest.mark.parametrize("kind", ["unmarked", "symlink", "directory", "oversize"])
def test_a_file_that_is_not_ours_is_foreign_and_survives_a_removal(host, directory, kind):
    target = directory / (RGW + ".xml")
    if kind == "unmarked":
        target.write_text("<?xml version='1.0'?><service/>")
    elif kind == "symlink":
        other = directory.parent / "other.xml"
        other.write_text(xml(RGW))
        target.symlink_to(other)
    elif kind == "directory":
        target.mkdir()
    else:
        target.write_text(xml(RGW) + " " * worker.MAX_SERVICE_FILE)
    for operation in ("apply", "remove"):
        assert ask(host, operation, [service()]) == {"ok": False, "reason": "host-firewall-foreign", "detail": RGW}
    assert host.verbs() == []
    assert ask(host, "inspect", [service()])["result"]["services"][0]["state"] == "foreign"
    assert target.is_symlink() or target.is_dir() or target.read_text() == "<?xml version='1.0'?><service/>" or kind == "oversize"


def test_a_file_owned_by_another_account_is_foreign(host, directory):
    (directory / (RGW + ".xml")).write_text(xml(RGW))
    stranger = worker.Guest(run=host, directory=str(directory), owner=os.getuid() + 1)
    assert stranger.ownership(RGW, MARK)[0] == "foreign"
    assert guest(host).ownership(RGW, MARK)[0] == "owned"


def test_a_crash_between_the_write_and_the_first_reload_reloads_before_the_membership(host, directory):
    wanted = service()
    (directory / (RGW + ".xml")).write_text(wanted["content"])
    before = ask(host, "inspect", [wanted])["result"]["services"][0]
    assert before["state"] == "owned" and before["covered"] is False and before["memberships"] == []
    host.calls.clear()
    reply = ask(host, "apply", [wanted])
    assert reply["ok"] and reply["result"]["changed"] is True
    assert reply["result"]["services"] == [entry(RGW, content=wanted["content"])]
    assert host.verbs() == ["--reload", "--add-service=" + RGW, "--reload"]


def test_a_written_file_firewalld_already_loaded_is_added_without_a_first_reload(host, directory):
    wanted = service()
    (directory / (RGW + ".xml")).write_text(wanted["content"])
    host.known.add(RGW)
    reply = ask(host, "apply", [wanted])
    assert reply["ok"] and reply["result"]["services"] == [entry(RGW, content=wanted["content"])]
    assert host.verbs() == ["--add-service=" + RGW, "--reload"]


def test_firewalld_refuses_to_add_a_service_it_has_not_loaded(host, directory):
    (directory / (RGW + ".xml")).write_text(xml(RGW))
    with pytest.raises(worker.Refused) as refused:
        guest(host).alter("public", RGW, "add")
    assert refused.value.cause == "zone" and host.permanent["public"] == set()


def test_a_stale_temporary_file_does_not_block_the_write(host, directory):
    (directory / ("." + RGW + ".xml.tmp")).write_text("left by a crash")
    assert ask(host, "apply", [service()])["ok"]
    assert sorted(path.name for path in directory.iterdir()) == [RGW + ".xml"]


def test_destroy_removes_the_membership_from_every_zone_deletes_owned_files_and_reloads(host, directory):
    wanted = [service(RGW), service(NFS, port=2049)]
    ask(host, "apply", wanted)
    host.permanent["drop"].add(RGW)
    host.runtime["drop"].add(RGW)
    host.calls.clear()
    reply = ask(host, "remove", wanted)
    assert reply["ok"] and reply["result"]["changed"] is True
    assert list(directory.iterdir()) == []
    assert all(not names for names in host.permanent.values()) and all(not names for names in host.runtime.values())
    assert host.verbs()[-1] == "--reload" and host.verbs().count("--reload") == 1
    assert reply["result"]["services"] == [entry(RGW, "absent", False, ()), entry(NFS, "absent", False, ())]
    host.calls.clear()
    again = ask(host, "remove", wanted)
    assert again["result"]["changed"] is False and host.verbs() == []


def test_a_crash_between_the_delete_and_the_reload_is_reloaded_on_retry_and_not_proved_before(host, directory):
    wanted = service()
    ask(host, "apply", [wanted])
    host.permanent["public"].discard(RGW)
    (directory / (RGW + ".xml")).unlink()
    shown = ask(host, "inspect", [wanted])["result"]["services"][0]
    assert shown == entry(RGW, "absent", False, (("public", "eth0"),))
    host.calls.clear()
    reply = ask(host, "remove", [wanted])
    assert reply["ok"] and reply["result"]["changed"] is True and host.verbs() == ["--reload"]
    assert host.runtime["public"] == set() and reply["result"]["services"] == [entry(RGW, "absent", False, ())]
    host.calls.clear()
    assert ask(host, "remove", [wanted])["result"]["changed"] is False and host.verbs() == []


def test_a_runtime_listing_in_a_zone_without_its_interfaces_is_a_membership(host):
    wanted = service()
    ask(host, "apply", [wanted])
    host.runtime["drop"].add(RGW)
    shown = ask(host, "inspect", [wanted])["result"]["services"][0]
    assert shown["memberships"] == [{"interface": "", "zone": "drop"}, {"interface": "eth0", "zone": "public"}]


def test_destroy_keeps_a_membership_whose_service_file_is_not_ours(host, directory):
    host.permanent["drop"].add(RGW)
    host.runtime["drop"].add(RGW)
    host.calls.clear()
    reply = ask(host, "remove", [service(RGW)])
    assert reply["ok"] and reply["result"]["changed"] is False and host.verbs() == []
    assert RGW in host.permanent["drop"] and list(directory.iterdir()) == []


def test_destroy_keeps_a_foreign_file_and_changes_nothing_for_the_others(host, directory):
    ask(host, "apply", [service(RGW)])
    (directory / (NFS + ".xml")).write_text(xml(NFS, mark="someone-else"))
    host.calls.clear()
    assert ask(host, "remove", [service(RGW), service(NFS)]) == {"ok": False, "reason": "host-firewall-foreign", "detail": NFS}
    assert host.verbs() == [] and host.permanent["public"] == {RGW}


def test_a_zone_that_lists_the_service_without_its_interfaces_is_reported_with_an_empty_interface(host, directory):
    wanted = service()
    ask(host, "apply", [wanted])
    host.permanent["drop"].add(RGW)
    shown = ask(host, "inspect", [wanted])["result"]["services"][0]
    assert shown["memberships"] == [{"interface": "", "zone": "drop"}, {"interface": "eth0", "zone": "public"}]
    assert worker.valid_evidence([shown])


def test_an_interface_in_no_zone_uses_the_default_zone_and_each_interface_of_a_network_is_covered(host):
    host.zones.pop("eth0")
    host.addresses["eth2"] = ["192.0.2.77"]
    host.zones["eth2"] = "internal"
    reply = ask(host, "apply", [service()])
    shown = reply["result"]["services"][0]
    assert shown["covered"] is True
    assert sorted((item["zone"], item["interface"]) for item in shown["memberships"]) == [("internal", "eth2"), ("public", "eth0")]


def test_a_service_without_an_interface_refuses_apply_and_is_uncovered_in_an_inspection(host, directory):
    unreachable = service(NFS, networks=("203.0.113.60/32", "198.51.100.0/25"))
    reply = ask(host, "apply", [service(RGW), unreachable])
    assert reply == {"ok": False, "reason": "host-firewall-no-interface", "detail": NFS}
    assert list(directory.iterdir()) == [] and host.verbs() == []
    shown = ask(host, "inspect", [unreachable])["result"]["services"][0]
    assert shown["covered"] is False and shown["state"] == "absent" and shown["memberships"] == []


def test_an_interface_whose_network_contains_the_virtual_address_is_chosen_and_one_on_a_later_network_is_ignored(host):
    reply = ask(host, "apply", [service(networks=(NETWORK, "198.51.100.0/24"))])
    assert reply["result"]["services"][0]["memberships"] == [{"interface": "eth0", "zone": "public"}]
    assert reply["result"]["services"][0]["covered"] is True
    assert host.permanent["internal"] == set()


def test_the_interface_whose_network_contains_the_virtual_address_wins_over_one_on_another_subnet(host):
    host.addresses = {"eth0": ["10.0.1.12/24"], "eth1": ["10.0.2.12/24"]}
    host.zones = {"eth0": "public", "eth1": "internal"}
    reply = ask(host, "apply", [service(networks=("10.0.1.60/32", "10.0.0.0/16"))])
    assert reply["result"]["services"][0]["memberships"] == [{"interface": "eth0", "zone": "public"}]
    assert host.permanent["internal"] == set()


def test_with_nothing_containing_the_virtual_address_the_interface_whose_network_is_the_second_is_chosen(host):
    reply = ask(host, "apply", [service(networks=("203.0.113.60/32", "198.51.100.0/24"))])
    assert reply["result"]["services"][0]["memberships"] == [{"interface": "eth1", "zone": "internal"}]
    assert host.permanent["public"] == set() and host.permanent["internal"] == {RGW}


def test_an_address_inside_a_later_network_of_another_prefix_length_is_not_chosen(host):
    host.addresses["eth2"] = ["198.51.100.130/25"]
    host.zones["eth2"] = "drop"
    reply = ask(host, "apply", [service(networks=("203.0.113.60/32", "198.51.100.128/25"))])
    assert reply["result"]["services"][0]["memberships"] == [{"interface": "eth2", "zone": "drop"}]
    assert host.permanent["internal"] == set()
    refused = ask(host, "apply", [service(NFS, networks=("203.0.113.60/32", "198.51.100.0/25"))])
    assert refused == {"ok": False, "reason": "host-firewall-no-interface", "detail": NFS}


def test_every_candidate_zone_has_to_list_the_service_for_it_to_be_covered(host):
    host.addresses["eth2"] = ["192.0.2.11"]
    host.zones["eth2"] = "internal"
    wanted = service()
    ask(host, "apply", [wanted])
    host.permanent["internal"].discard(RGW)
    shown = ask(host, "inspect", [wanted])["result"]["services"][0]
    assert shown["covered"] is False and shown["memberships"] == [{"interface": "eth2", "zone": "internal"}, {"interface": "eth0", "zone": "public"}]
    host.runtime["internal"].discard(RGW)
    shown = ask(host, "inspect", [wanted])["result"]["services"][0]
    assert shown["covered"] is False and shown["memberships"] == [{"interface": "eth0", "zone": "public"}]


def test_an_ipv6_network_is_matched_by_the_addresses_inside_it(host):
    host.addresses["eth3"] = ["2001:db8::5/64"]
    host.zones["eth3"] = "internal"
    reply = ask(host, "apply", [service(networks=("2001:db8::60/128",))])
    assert reply["ok"] and reply["result"]["services"][0]["memberships"] == [{"interface": "eth3", "zone": "internal"}]


@pytest.mark.parametrize("operation", ["apply", "inspect", "remove"])
def test_an_inactive_firewalld_refuses_every_operation_and_changes_nothing(host, directory, operation):
    host.running = False
    assert ask(host, operation, [service()]) == {"ok": False, "reason": "host-firewall-inactive"}
    assert list(directory.iterdir()) == [] and host.verbs() == []


@pytest.mark.parametrize("failing, cause", [("--reload", "reload"), ("--add-service", "zone"), ("--remove-service", "zone")])
def test_a_command_firewalld_rejects_is_a_failure_with_its_cause(host, failing, cause):
    if failing == "--remove-service":
        ask(host, "apply", [service()])
    host.fail[failing] = 2
    operation = "remove" if failing == "--remove-service" else "apply"
    assert ask(host, operation, [service()]) == {"ok": False, "reason": "host-firewall-failed", "cause": cause}


def test_a_tool_that_answers_outside_the_closed_vocabulary_is_a_tooling_failure(host):
    host.zones["eth0"] = "public zone\nwith text"
    assert ask(host, "inspect", [service()]) == {"ok": False, "reason": "host-firewall-failed", "cause": "tooling"}
    host.zones["eth0"] = "public"
    host.fail["--query-service"] = 2
    assert ask(host, "inspect", [service()])["cause"] == "tooling"


def test_a_write_the_filesystem_refuses_is_a_write_failure_that_leaves_no_temporary_file(host, directory, monkeypatch):
    def refused(_source, _target):
        raise OSError("read-only")

    monkeypatch.setattr(worker.os, "replace", refused)
    assert ask(host, "apply", [service()]) == {"ok": False, "reason": "host-firewall-failed", "cause": "write"}
    assert host.verbs() == []


def test_an_address_entry_without_a_local_address_is_skipped(host):
    host.bare["eth0"] = [{"family": "inet6", "prefixlen": 64}]
    reply = ask(host, "apply", [service()])
    assert reply["ok"] and reply["result"]["services"][0]["memberships"] == [{"interface": "eth0", "zone": "public"}]


def test_a_malformed_ip_listing_is_an_address_or_tooling_failure():
    def broken(payload):
        return lambda argv: (0, payload) if argv[0] == worker.IP else (0, b"running\n")

    for payload, cause in ((b"not json", "tooling"), (b"{}", "tooling"), (b'[{"ifname": "eth0", "addr_info": [{"local": "x"}]}]', "address")):
        out = io.StringIO()
        request = json.dumps({"operation": "inspect", "mark": MARK, "services": [service()]}).encode()
        assert worker.main(stdin=io.BytesIO(request), stdout=out, guest=worker.Guest(run=broken(payload))) == 0
        assert json.loads(out.getvalue()) == {"ok": False, "reason": "host-firewall-failed", "cause": cause}


@pytest.mark.parametrize("request_bytes", [
    b"", b"not json", b"[]", b'{"operation": "apply"}',
    json.dumps({"operation": "wipe", "mark": MARK, "services": [service()]}).encode(),
    json.dumps({"operation": "apply", "mark": "a<b", "services": [service()]}).encode(),
    json.dumps({"operation": "apply", "mark": MARK, "services": []}).encode(),
    json.dumps({"operation": "apply", "mark": MARK, "services": [service(), service()]}).encode(),
    json.dumps({"operation": "apply", "mark": MARK, "services": [service("Bad_Name")]}).encode(),
    json.dumps({"operation": "apply", "mark": MARK, "services": [service(networks=("192.0.2.5/24",))]}).encode(),
    json.dumps({"operation": "apply", "mark": MARK, "services": [dict(service(), extra=1)]}).encode(),
    json.dumps({"operation": "apply", "mark": MARK, "services": [service(networks=("192.0.2.0/24", NETWORK))]}).encode(),
], ids=range(12))
def test_a_request_the_worker_does_not_admit_is_refused_before_any_command(request_bytes):
    ran = []
    stdout = io.StringIO()
    worker.main(stdin=io.BytesIO(request_bytes), stdout=stdout, guest=worker.Guest(run=ran.append))
    assert json.loads(stdout.getvalue()) == {"ok": False, "reason": "transport"} and ran == []


def test_the_worker_never_prints_a_traceback_or_command_output():
    stdout = io.StringIO()

    def exploding(_argv):
        raise RuntimeError("SENTINEL secret")

    request = {"operation": "inspect", "mark": MARK, "services": [service()]}
    worker.main(stdin=io.BytesIO(json.dumps(request).encode()), stdout=stdout, guest=worker.Guest(run=exploding))
    assert json.loads(stdout.getvalue()) == {"ok": False, "reason": "transport"} and "SENTINEL" not in stdout.getvalue()


def test_every_command_is_a_fixed_absolute_argv_without_a_shell(host):
    ask(host, "apply", [service()])
    for call in host.calls:
        assert call[0] in (worker.FIREWALL_CMD, worker.IP) and all(isinstance(word, str) and "\0" not in word for word in call)
    assert worker.ENVIRONMENT == {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}


class FakePopen:
    """A recording bounded runner that answers with fixed bytes."""

    def __init__(self, stdout=b"", stderr=b"", code=0, timeout=False, failure=None):
        self.calls = []
        self.stdout, self.stderr, self.code, self.timeout, self.failure = stdout, stderr, code, timeout, failure

    def __call__(self, argv, data, timeout, limit, environment, **options):
        self.calls.append({"argv": list(argv), "stdin": data, "timeout": timeout, "env": environment})
        if self.timeout:
            raise bounded_process.Failure("timeout")
        if self.failure:
            raise bounded_process.Failure(self.failure)
        return bounded_process.Result(self.code, self.stdout, self.stderr)


def session(popen):
    return lambda argv, data, timeout: managedos_guest_ssh.run(argv, data, timeout, runner=popen)


def params(tmp_path, operation="apply", **values):
    paths = {}
    for name, data in (("ssh_config", b"Ciphers aes256-gcm@openssh.com\n"), ("known_hosts", b"192.0.2.10 ssh-ed25519 AAAA\n"),
                       ("identity", b"private\n")):
        path = tmp_path / name
        path.write_bytes(data)
        paths[name] = str(path)
    value = {"operation": operation, "address": "192.0.2.10", "user": "fleet", "identity": paths["identity"],
             "ssh_config": paths["ssh_config"], "known_hosts": paths["known_hosts"],
             "host_key_algorithms": "ecdsa-sha2-nistp256", "policy_host_key_algorithms": "ecdsa-sha2-nistp256,rsa-sha2-512",
             "mark": MARK, "services": [service()], "timeout": 300}
    value.update(values)
    return value


def encoded(value):
    return (json.dumps(value) + "\n").encode()


def evidence(*names):
    return [entry(name, content=xml(name)) for name in names]


def test_the_session_is_the_pinned_guest_argv_and_only_the_operation_mark_and_services_reach_the_worker(tmp_path):
    popen = FakePopen(stdout=encoded({"ok": True, "result": {"changed": True, "services": evidence(RGW)}}))
    outcome = module.execute(params(tmp_path), run=session(popen))
    assert outcome == {"changed": True, "evidence": {"services": evidence(RGW)}, "ok": True}
    call = popen.calls[0]
    assert json.loads(call["stdin"]) == {"mark": MARK, "operation": "apply", "services": [service()]}
    assert set(json.loads(call["stdin"])) == {"operation", "mark", "services"}
    assert call["stdin"] == json.dumps(json.loads(call["stdin"]), sort_keys=True, separators=(",", ":")).encode()
    assert call["timeout"] == 300 and call["env"] == {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
    remote = shlex.split(call["argv"][-1])
    assert remote == ["/usr/bin/sudo", "-n", "/usr/bin/python3", "-I", "-c", module.worker_source()]
    assert remote[-1].endswith("\nraise SystemExit(main())\n")
    expected = managedos_guest_ssh.guest_argv(
        ssh_config=str(tmp_path / "ssh_config"), known_hosts=str(tmp_path / "known_hosts"), host_key_algorithms="ecdsa-sha2-nistp256",
        policy_host_key_algorithms="ecdsa-sha2-nistp256,rsa-sha2-512", identity=str(tmp_path / "identity"), user="fleet",
        address="192.0.2.10", remote_argv=module.remote_command.fixed_worker_argv(module.worker_source()))
    assert call["argv"] == expected
    assert "UserKnownHostsFile=" + str(tmp_path / "known_hosts") in call["argv"] and "StrictHostKeyChecking=yes" in call["argv"]


def test_the_worker_source_runs_alone_with_only_the_standard_library():
    source = module.worker_source()
    compile(source, "worker", "exec")
    imports = {line.split()[1].split(".")[0] for line in source.splitlines() if line.startswith(("import ", "from "))}
    assert imports <= {"__future__", "hashlib", "ipaddress", "json", "os", "re", "stat", "subprocess", "sys"}, imports


@pytest.mark.parametrize("operation", ["inspect", "remove"])
def test_each_operation_reaches_the_worker_by_name(tmp_path, operation):
    popen = FakePopen(stdout=encoded({"ok": True, "result": {"changed": False, "services": evidence(RGW)}}))
    module.execute(params(tmp_path, operation), run=session(popen))
    assert json.loads(popen.calls[0]["stdin"])["operation"] == operation


@pytest.mark.parametrize("override, message", [
    ({"mark": "a<b"}, "mark"), ({"mark": ""}, "mark"), ({"mark": "x" * 257}, "mark"), ({"mark": "tab\there"}, "mark"),
    ({"services": []}, "services"), ({"services": [service(RGW + str(i)) for i in range(65)]}, "services"),
    ({"services": [service(), service()]}, "repeated"),
    ({"services": [service("UPPER")]}, "name"), ({"services": [service("-x")]}, "name"), ({"services": [service("a" * 64)]}, "name"),
    ({"services": [service(networks=())]}, "network"), ({"services": [service(networks=("192.0.2.5/24",))]}, "network"),
    ({"services": [service(networks=("not-a-cidr",))]}, "network"),
    ({"services": [service(networks=tuple("10.%d.0.0/16" % i for i in range(17)))]}, "network"),
    ({"services": [dict(service(), content="plain")]}, "service file"),
    ({"services": [dict(service(), content="<?xml " + "x" * worker.MAX_CONTENT)]}, "service file"),
    ({"services": [dict(service(), content="<?xml café")]}, "service file"),
    ({"services": [dict(service(), extra="x")]}, "name, networks and content"), ({"timeout": 0}, "timeout"), ({"timeout": 901}, "timeout"),
    ({"timeout": True}, "timeout"),
])
def test_an_argument_the_module_refuses_never_reaches_a_session(tmp_path, override, message):
    popen = FakePopen()
    with pytest.raises(module.ArgumentError, match=message):
        module.execute(params(tmp_path, **override), run=session(popen))
    assert popen.calls == []


def test_a_session_the_builder_refuses_is_a_session_transport_refusal_that_names_its_check(tmp_path):
    outcome = module.execute(params(tmp_path, ssh_config="/dev/null"), run=session(FakePopen()))
    assert outcome["reason"] == "transport" and outcome["cause"] == "session" and outcome["ok"] is False
    assert outcome["detail"] == "the guest session requires the reduced placement-host policy configuration"


@pytest.mark.parametrize("kind, expected", [
    ({"timeout": True}, ("transport", "timeout")), ({"failure": "start"}, ("transport", "session")),
    ({"failure": "output"}, ("transport", "answer")), ({"code": 255, "stderr": b"Host key verification failed."}, ("transport", "host-key")),
    ({"code": 255}, ("transport", "unreachable")), ({"code": 1}, ("transport", "exit")),
    ({"stdout": b"not json\n"}, ("transport", "answer")), ({"stdout": b'{"ok":true}\n'}, ("transport", "answer")),
    ({"stdout": b'{"ok":false,"reason":"anything"}\n'}, ("transport", "answer")),
    ({"stdout": b'{"ok":false,"reason":"transport"}\n'}, ("transport", "answer")),
])
def test_a_failed_session_or_an_unclosed_answer_is_a_transport_refusal_with_its_cause(tmp_path, kind, expected):
    outcome = module.execute(params(tmp_path), run=session(FakePopen(**kind)))
    assert (outcome["reason"], outcome["cause"]) == expected and outcome["ok"] is False and outcome["changed"] is False


@pytest.mark.parametrize("answer, shape", [
    ({"ok": False, "reason": "host-firewall-foreign", "detail": RGW}, ("host-firewall-foreign", None, RGW)),
    ({"ok": False, "reason": "host-firewall-no-interface", "detail": RGW}, ("host-firewall-no-interface", None, RGW)),
    ({"ok": False, "reason": "host-firewall-inactive"}, ("host-firewall-inactive", None, None)),
    ({"ok": False, "reason": "host-firewall-failed", "cause": "reload"}, ("host-firewall-failed", "reload", None)),
])
def test_the_workers_closed_refusals_pass_through_with_their_cause_and_detail(tmp_path, answer, shape):
    outcome = module.execute(params(tmp_path), run=session(FakePopen(stdout=encoded(answer))))
    reason, cause, detail = shape
    expected = {"changed": False, "ok": False, "reason": reason}
    expected.update({key: value for key, value in (("cause", cause), ("detail", detail)) if value is not None})
    assert outcome == expected


@pytest.mark.parametrize("answer", [
    {"ok": False, "reason": "host-firewall-foreign"},
    {"ok": False, "reason": "host-firewall-foreign", "detail": "another-service"},
    {"ok": False, "reason": "host-firewall-failed"},
    {"ok": False, "reason": "host-firewall-failed", "cause": "SENTINEL"},
    {"ok": False, "reason": "host-firewall-inactive", "detail": RGW},
    {"ok": True, "result": {"changed": True, "services": []}},
    {"ok": True, "result": {"changed": "yes", "services": evidence(RGW)}},
    {"ok": True, "result": {"changed": True, "services": evidence(NFS)}},
    {"ok": True, "result": {"changed": True, "services": evidence(RGW), "extra": 1}},
    {"ok": True, "result": {"changed": True, "services": [dict(evidence(RGW)[0], state="mine")]}},
    {"ok": True, "result": {"changed": True, "services": [dict(evidence(RGW)[0], digest="")]}},
    {"ok": True, "result": {"changed": True, "services": [dict(evidence(RGW)[0], extra=1)]}},
    {"ok": True, "result": {"changed": True, "services": [dict(evidence(RGW)[0], covered=1)]}},
    {"ok": True, "result": {"changed": True, "services": [dict(evidence(RGW)[0], memberships=[{"interface": "eth0", "zone": "a b"}])]}},
    {"ok": True, "result": {"changed": True, "services": [dict(evidence(RGW)[0], memberships=[
        {"interface": "eth1", "zone": "public"}, {"interface": "eth0", "zone": "public"}])]}},
], ids=range(15))
def test_an_answer_that_is_not_the_closed_shape_for_this_request_is_refused_as_an_unclosed_answer(tmp_path, answer):
    outcome = module.execute(params(tmp_path), run=session(FakePopen(stdout=encoded(answer))))
    assert (outcome["reason"], outcome["cause"]) == ("transport", "answer")


def test_an_inspection_that_claims_a_change_is_refused(tmp_path):
    popen = FakePopen(stdout=encoded({"ok": True, "result": {"changed": True, "services": evidence(RGW)}}))
    assert module.execute(params(tmp_path, "inspect"), run=session(popen))["cause"] == "answer"


def test_evidence_orders_services_as_requested_and_accepts_foreign_without_a_digest():
    foreign = entry(NFS, "foreign", False, (), content=None)
    assert worker.valid_evidence([entry(RGW, content=xml(RGW)), foreign], [RGW, NFS])
    assert not worker.valid_evidence([foreign, entry(RGW, content=xml(RGW))], [RGW, NFS])
    assert not worker.valid_evidence([entry(RGW, "absent", content=xml(RGW))])


def completed(**fields):
    arguments = {"phase": "completed", "outcome": "changed", "digest": DIGEST, "services": evidence(RGW, NFS)}
    arguments.update(fields)
    return arguments


def publish(arguments, monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda record, **kwargs: published.append(record))
    action = protocol.ActionModule.__new__(protocol.ActionModule)
    action._task = SimpleNamespace(args=arguments)
    return action.run(task_vars={}), published


def test_a_proved_realization_publishes_exactly_the_evidence_the_engine_reads(monkeypatch):
    result, published = publish(completed(), monkeypatch)
    assert result == {"changed": False}
    assert published == [{"phase": "completed", "outcome": "changed", "evidence": {
        "absent": False, "postcondition": True, "request": DIGEST, "services": evidence(RGW, NFS),
        "version": "os-host-firewall-evidence-v1"}}]
    assert list(published[0]["evidence"]) == ["absent", "postcondition", "request", "services", "version"]


@pytest.mark.parametrize("broken", [
    {"state": "absent", "digest": ""}, {"state": "foreign", "digest": ""}, {"covered": False}, {"memberships": []},
], ids=["absent", "foreign", "uncovered", "no-membership"])
def test_a_realization_that_is_not_proved_fails_instead_of_publishing_but_an_observation_reports_it(broken, monkeypatch):
    services = [dict(evidence(RGW)[0], **broken)]
    result, published = publish(completed(services=services), monkeypatch)
    assert result == {"failed": True, "msg": "the host firewall did not reach its postcondition; not proved: services"} and published == []
    result, published = publish(completed(services=services, outcome="unchanged", observed=True), monkeypatch)
    assert result == {"changed": False} and published[0]["evidence"]["postcondition"] is False


def test_a_removal_is_proved_only_when_every_service_is_absent_and_held_by_no_zone(monkeypatch):
    gone = [entry(RGW, "absent", False, ()), entry(NFS, "absent", False, ())]
    _result, published = publish(completed(services=gone, removed=True), monkeypatch)
    assert published[0]["evidence"] == {"absent": True, "postcondition": True, "request": DIGEST, "services": gone,
                                        "version": "os-host-firewall-evidence-v1"}
    for left in (dict(gone[0], memberships=[{"interface": "", "zone": "drop"}]), dict(evidence(RGW)[0])):
        result, published = publish(completed(services=[left], removed=True), monkeypatch)
        assert result["failed"] is True and published == []
    result, published = publish(completed(services=[left], removed=True, observed=True), monkeypatch)
    assert published[0]["evidence"]["postcondition"] is False


@pytest.mark.parametrize("arguments", [
    completed(services=None), completed(services=[]), completed(services=[{"name": RGW}]), completed(digest="abc"),
    completed(outcome="maybe"), completed(services=[dict(evidence(RGW)[0], state="mine")]),
], ids=range(6))
def test_evidence_the_engine_would_refuse_is_never_published(arguments, monkeypatch):
    result, published = publish(arguments, monkeypatch)
    assert result == {"failed": True, "msg": "the host firewall result could not be published"} and published == []


@pytest.mark.parametrize("reason", ["host-firewall-foreign", "host-firewall-no-interface", "host-firewall-inactive", "host-firewall-failed"])
def test_each_refusal_is_named_to_the_runner_with_a_closed_detail(reason, monkeypatch):
    result, published = publish({"phase": "refused", "reason": reason, "detail": RGW}, monkeypatch)
    assert result == {"changed": False} and published == [{"phase": "refused", "reason": reason, "detail": RGW}]
    _result, published = publish({"phase": "refused", "reason": reason, "detail": "bad detail!"}, monkeypatch)
    assert published == [{"phase": "refused", "reason": reason}]


@pytest.mark.parametrize("reason", [None, "", "transport", "host-firewall-foreign ", "media-changed-tree"], ids=repr)
def test_a_refusal_this_publication_does_not_name_is_never_published(reason, monkeypatch):
    arguments = {"phase": "refused"}
    if reason is not None:
        arguments["reason"] = reason
    result, published = publish(arguments, monkeypatch)
    assert result == {"failed": True, "msg": "the host firewall result could not be published"} and published == []


def test_the_protocol_names_its_phases_and_refusals():
    assert protocol.CAPABILITY.phases == ("loaded", "group", "refused", "completed")
    assert protocol.CAPABILITY.refusals == ("host-firewall-foreign", "host-firewall-no-interface", "host-firewall-inactive",
                                            "host-firewall-failed") and protocol.CAPABILITY.detail is True


DEFAULTS = LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True)


def tasks(name):
    return LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True)


def flattened(listed):
    for task in listed:
        if "block" in task:
            for section in ("block", "rescue", "always"):
                yield from flattened(task.get(section, []))
        else:
            yield task


def calls(name, fqcn):
    return [task for task in flattened(tasks(name)) if fqcn in task]


def variables(**extra):
    services = [service(), service(NFS, port=2049)]
    request = {"version": "os-host-firewall-v1", "address": "192.0.2.10", "user": "fleet", "fleetKeyRef": "fleet-key",
               "identity": {"block": "os-firewall-ceph-01", "context": "lab", "object": "ceph-01"},
               "placement": {"connection": "ssh"}, "services": [{"name": RGW}, {"name": NFS}]}
    values = dict(DEFAULTS, bootwright_run_scratch="/var/tmp/bootwright-run-scratch-1-1", bootwright_os_firewall_request=request,
                  bootwright_os_firewall_digest=DIGEST,
                  bootwright_os_firewall_material={"fleetIdentity": "/run/material/fleet-id", "hostKey": "ssh-ed25519 AAAA",
                                                   "mark": MARK, "services": json.dumps(services)})
    values.update(extra)
    return values


def render(value, **extra):
    return Templar(loader=LOADER, variables=variables(**extra)).template(trust_as_template(value) if isinstance(value, str) else value)


def holds(conditions, **extra):
    templar = Templar(loader=LOADER, variables=variables(**extra))
    return all(templar.evaluate_conditional(trust_as_template(condition)) for condition in
               (conditions if isinstance(conditions, list) else [conditions]))


@pytest.mark.parametrize("name", ["apply.yml", "observe.yml", "destroy.yml"])
def test_every_entry_point_validates_the_request_before_the_handoff_and_any_scratch(name):
    listed = tasks(name)
    assert "ansible.builtin.assert" in listed[0] and listed[0]["no_log"] is True
    assert listed[1]["bootwright.core.managedos_firewall_protocol"] == {"phase": "loaded"}
    conditions = listed[0]["ansible.builtin.assert"]["that"]
    assert holds(conditions)
    for override in ({"version": "os-host-firewall-v2"}, {"identity": {"context": "Lab", "object": "ceph-01"}},
                     {"identity": {"context": "lab", "object": "../x"}}, {"services": []}, {"services": "text"}):
        request = dict(variables()["bootwright_os_firewall_request"], **override)
        assert not holds(conditions, bootwright_os_firewall_request=request), override
    assert not holds(conditions, bootwright_os_firewall_digest="A" * 64)
    assert not holds(conditions, bootwright_os_firewall_digest="a" * 63)
    wrong = dict(variables()["bootwright_os_firewall_material"], services=json.dumps([service(NFS)]))
    assert not holds(conditions, bootwright_os_firewall_material=wrong)


def test_the_argument_specification_declares_the_same_request_for_every_entry_point():
    declared = yaml.safe_load((ROLE / "meta" / "argument_specs.yml").read_text())["argument_specs"]
    assert sorted(declared) == ["apply", "destroy", "observe"]
    options = declared["apply"]["options"]
    assert all(declared[name]["options"] == options for name in declared)
    request = options["bootwright_os_firewall_request"]["options"]
    assert sorted(request) == ["address", "fleetKeyRef", "identity", "placement", "services", "user", "version"]
    assert request["version"]["choices"] == ["os-host-firewall-v1"]
    assert options["bootwright_os_firewall_digest"]["type"] == "str" and options["bootwright_os_firewall_material"]["type"] == "dict"


@pytest.mark.parametrize("name", ["apply.yml", "destroy.yml", "observe.yml"])
def test_scratch_is_prepared_first_and_removed_at_the_end(name):
    block = next(task for task in tasks(name) if "block" in task)
    prepare, cleanup = block["block"][0], block["always"][0]
    scratch = "bootwright.core.lifecycle_scratch"
    assert prepare[scratch] == {"root": "{{ bootwright_run_scratch }}", "scope": "managedos-firewall"}
    assert cleanup[scratch] == dict(prepare[scratch], state="absent", remove_root="{{ bootwright_os_firewall_request.placement.connection == 'ssh' }}")
    assert cleanup["when"] == prepare["register"] + " is succeeded"
    assert render("{{ managedos_host_firewall_scratch }}") == "/var/tmp/bootwright-run-scratch-1-1/managedos-firewall"


def test_the_observation_is_self_contained_inspects_literally_and_writes_only_beneath_the_scratch_directory():
    keys = {key for task in flattened(tasks("observe.yml")) for key in task}
    assert not keys & {"ansible.builtin.include_tasks", "ansible.builtin.import_tasks", "ansible.builtin.include_role",
                       "ansible.builtin.import_role"}
    assert "session.yml" not in str(list(flattened(tasks("observe.yml"))))
    inspect = calls("observe.yml", "bootwright.core.managedos_firewall")
    assert len(inspect) == 1 and inspect[0]["bootwright.core.managedos_firewall"]["operation"] == "inspect"
    writes = [task for task in flattened(tasks("observe.yml")) if "ansible.builtin.copy" in task or "ansible.builtin.file" in task]
    assert len(writes) == 4
    for task in writes:
        body = task.get("ansible.builtin.copy") or task.get("ansible.builtin.file")
        assert render(body["dest" if "dest" in body else "path"]).startswith("/var/tmp/bootwright-run-scratch-1-1/managedos-firewall/")
    assert {key for task in flattened(tasks("observe.yml")) for key in task if key.startswith("ansible.builtin.")} <= {
        "ansible.builtin.assert", "ansible.builtin.copy", "ansible.builtin.file", "ansible.builtin.set_fact", "ansible.builtin.fail"}
    complete = calls("observe.yml", "bootwright.core.managedos_firewall_protocol")[-1]["bootwright.core.managedos_firewall_protocol"]
    assert complete["phase"] == "completed" and complete["outcome"] == "unchanged" and complete["observed"] is True
    assert "removed" not in complete


@pytest.mark.parametrize("name, operation, removed", [("apply.yml", "apply", False), ("destroy.yml", "remove", True)])
def test_apply_and_destroy_share_one_session_parameterized_by_operation(name, operation, removed):
    included = [task for task in flattened(tasks(name)) if task.get("ansible.builtin.include_tasks") == "session.yml"]
    assert len(included) == 1 and included[0]["vars"] == {"managedos_host_firewall_operation": operation}
    session = calls("session.yml", "bootwright.core.managedos_firewall")
    assert len(session) == 1
    arguments = session[0]["bootwright.core.managedos_firewall"]
    assert render(arguments["operation"], managedos_host_firewall_operation=operation) == operation
    completion = calls(name, "bootwright.core.managedos_firewall_protocol")[-1]["bootwright.core.managedos_firewall_protocol"]
    assert completion.get("removed", False) is removed and completion["phase"] == "completed"
    assert render(completion["outcome"], managedos_host_firewall_answer={"changed": True}) == "changed"
    assert render(completion["outcome"], managedos_host_firewall_answer={"changed": False}) == "unchanged"


@pytest.mark.parametrize("name, key", [("session.yml", "bootwright.core.managedos_firewall"), ("observe.yml", "bootwright.core.managedos_firewall")])
def test_the_module_call_pins_the_frozen_connection_and_hands_it_only_the_mark_and_the_services(name, key):
    task = calls(name, key)[0]
    assert task["failed_when"] is False and task["no_log"] is True
    arguments = task[key]
    assert set(arguments) == {"operation", "address", "user", "identity", "ssh_config", "known_hosts", "host_key_algorithms",
                              "policy_host_key_algorithms", "mark", "services", "timeout"}
    scratch = "/var/tmp/bootwright-run-scratch-1-1/managedos-firewall"
    extra = {"managedos_host_firewall_operation": "apply", "managedos_host_firewall_observe_host_key_algorithms": "ssh-ed25519",
             "managedos_host_firewall_ssh_policy": {"hostKeyAlgorithms": "ssh-ed25519"},
             "managedos_host_firewall_observe_ssh_policy": {"hostKeyAlgorithms": "ssh-ed25519"},
             "managedos_host_firewall_host_key_algorithms": "ssh-ed25519"}
    assert render(arguments["ssh_config"], **extra) == scratch + "/ssh_config"
    assert render(arguments["known_hosts"], **extra) == scratch + "/known_hosts"
    assert render(arguments["address"]) == "192.0.2.10" and render(arguments["user"]) == "fleet"
    assert render(arguments["identity"]) == "/run/material/fleet-id" and render(arguments["mark"]) == MARK
    assert [item["name"] for item in render(arguments["services"])] == [RGW, NFS]
    assert render(arguments["services"])[0]["content"] == xml(RGW)
    known = next(task for task in flattened(tasks(name)) if "ansible.builtin.copy" in task and task["ansible.builtin.copy"]["dest"].endswith("known_hosts"))
    assert render(known["ansible.builtin.copy"]["content"]) == "192.0.2.10 ssh-ed25519 AAAA"


@pytest.mark.parametrize("answer, message, detail", [
    ({"ok": False, "reason": "host-firewall-foreign", "detail": NFS}, "host-firewall-foreign", NFS),
    ({"ok": False, "reason": "host-firewall-no-interface", "detail": NFS}, "host-firewall-no-interface", NFS),
    ({"ok": False, "reason": "host-firewall-inactive"}, "host-firewall-inactive", ""),
    ({"ok": False, "reason": "host-firewall-failed", "cause": "reload"}, "host-firewall-failed (reload)", "reload"),
    ({"ok": False, "reason": "transport", "cause": "host-key", "detail": "SENTINEL"}, "transport (host-key)", None),
    ({"ok": False, "reason": "SENTINEL", "cause": "SENTINEL"}, "unexpected answer", None),
    ({"failed": True, "msg": "SENTINEL"}, "unexpected answer", None),
], ids=["foreign", "no-interface", "inactive", "failed", "transport", "unknown", "no-answer"])
@pytest.mark.parametrize("name", ["session.yml", "observe.yml"])
def test_a_refusal_is_named_with_its_detail_and_the_failure_names_only_closed_vocabulary(name, answer, message, detail):
    named = next(task for task in flattened(tasks(name)) if (task.get("bootwright.core.managedos_firewall_protocol") or {}).get("phase") == "refused")
    refused = next(task for task in flattened(tasks(name)) if "ansible.builtin.fail" in task)
    facts = {"managedos_host_firewall_answer": answer, "managedos_host_firewall_operation": "apply"}
    assert holds(named["when"], **facts) is (detail is not None) and named["no_log"] is True
    if detail is not None:
        assert render(named["bootwright.core.managedos_firewall_protocol"]["detail"], **facts) == detail
    assert holds(refused["when"], **facts) and not holds(refused["when"], managedos_host_firewall_answer={"ok": True})
    rendered = render(refused["ansible.builtin.fail"]["msg"], **dict(facts, **refused["vars"]))
    assert " ".join(rendered.split()).endswith(message) and "Machine ceph-01" in rendered and "SENTINEL" not in rendered
    assert NFS not in rendered
    assert set(named["when"][1].split("in")[-1].split()) == {"managedos_host_firewall_refusals"}


def test_the_failure_says_whether_the_firewall_was_realized_or_removed():
    refused = next(task for task in flattened(tasks("session.yml")) if "ansible.builtin.fail" in task)
    facts = {"managedos_host_firewall_answer": {"ok": False, "reason": "host-firewall-inactive"}}
    for operation, word in (("apply", "realized"), ("remove", "removed")):
        rendered = render(refused["ansible.builtin.fail"]["msg"], managedos_host_firewall_operation=operation, **dict(facts, **refused["vars"]))
        assert "was not %s:" % word in " ".join(rendered.split())


def test_the_playbooks_import_their_entry_point_by_the_declared_key():
    for operation in ("apply", "observe", "destroy"):
        plays = yaml.safe_load((COLLECTION / "playbooks" / "managedos" / ("firewall_%s.yml" % operation)).read_text())
        assert len(plays) == 1 and plays[0]["hosts"] == "bootwright_target"
        assert plays[0]["tasks"][0]["ansible.builtin.import_role"] == {"name": "bootwright.core.managedos_host_firewall", "tasks_from": operation}
