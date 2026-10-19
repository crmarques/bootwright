"""A FIPS installation proves FIPS mode on the installed system.

FIPS mode is set only at installation, so the role never changes it; it reads
it after the machine's network converged, over the pinned fleet connection
guest.yml owns, from the two files fips-mode-setup --check reads: the kernel's
/proc/sys/crypto/fips_enabled and /etc/crypto-policies/state/current
(https://gitlab.com/redhat-crypto/fedora-crypto-policies/-/raw/rhel9/fips-mode-setup,
lines 88 and 95). Only a kernel answer of exactly 1 and the policy FIPS itself
prove it; no field selects a subpolicy, so FIPS:OSPP proves nothing here.

Rendering the role's files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib
import subprocess

import pytest

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar


ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
LOADER = DataLoader()
PROTOCOL = "bootwright.core.managedos_install_protocol"
INCLUDE = "ansible.builtin.include_tasks"
REQUESTED = "managedos_install_anaconda_fips_requested | bool"
UNPROVED = "not (managedos_install_anaconda_fips | bool)"
READ = (
    "set -u\n"
    "printf 'kernel=%s\\n' \"$(head -c 8 /proc/sys/crypto/fips_enabled 2>/dev/null | tr -cd 0-9)\"\n"
    "printf 'policy=%s\\n' \"$(head -n 1 /etc/crypto-policies/state/current 2>/dev/null | head -c 64 | tr -cd '[:graph:]')\""
)


def flattened(name):
    """A task file's tasks in the order a play reaches them, each block opened
    and its condition carried onto every task inside it."""
    def opened(listed, inherited):
        for task in listed or []:
            if not isinstance(task, dict):
                continue
            conditions = task.get("when", [])
            conditions = inherited + (conditions if isinstance(conditions, list) else [conditions])
            if "block" in task:
                for section in ("block", "rescue", "always"):
                    yield from opened(task.get(section), conditions)
                continue
            yield dict(task, when=conditions)
    return list(opened(LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True), []))


def only(tasks, predicate, description):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def test_fips_state_read_runs_through_guest_yml_after_network():
    applied = flattened("apply.yml")
    network = only(applied, lambda task: task.get(INCLUDE) == "network.yml", "converge the network")
    converged = only(applied, lambda task: (task.get(PROTOCOL) or {}).get("group") == "converge-network"
                     and task[PROTOCOL].get("status") == "ok", "report network convergence")
    read = only(applied, lambda task: task.get(INCLUDE) == "host_security.yml", "read the FIPS state")
    inspect = only(applied, lambda task: "bootwright.core.managedos_install_inspect" in task
                   and task.get("register") == "managedos_install_anaconda_after", "prove the publication")
    assert network < converged < read < inspect
    named, refused = applied[read + 1], applied[read + 2]
    assert named[PROTOCOL] == {"phase": "refused", "reason": "fips-not-enabled"}
    assert named["no_log"] is True
    assert "ansible.builtin.fail" in refused
    assert named["when"] == refused["when"] == [REQUESTED, UNPROVED]
    assert read + 3 == inspect

    observed = flattened("observe.yml")
    network = only(observed, lambda task: task.get(INCLUDE) == "network_observe.yml", "read the network")
    read = only(observed, lambda task: task.get(INCLUDE) == "host_security.yml", "read the FIPS state")
    assert read == network + 1
    assert observed[read]["when"] == observed[network]["when"]
    assert observed[read]["when"][-1] == "managedos_install_anaconda_reachable.rc | default(1) == 0"

    security = flattened("host_security.yml")
    guest = only(security, lambda task: task.get(INCLUDE) == "guest.yml", "read through the pinned connection")
    assert security[guest]["vars"] == {"managedos_install_anaconda_guest_command": ["/usr/bin/sh", "-c", READ, "bootwright-fips"]}
    assert security[guest]["when"] == [REQUESTED]
    assert not any("ansible.builtin.command" in task or task.get("become") for task in security)


def proves(**variables):
    """Whether the last set_fact of host_security.yml names FIPS mode proved."""
    named = flattened("host_security.yml")[-1]["ansible.builtin.set_fact"]["managedos_install_anaconda_fips"]
    values = {"managedos_install_anaconda_fips_requested": True}
    values.update(variables)
    return Templar(loader=LOADER, variables=values).template(named)


@pytest.mark.parametrize("guest, requested, expected", [
    ({"rc": 0, "stdout": "kernel=1\npolicy=FIPS\n"}, True, True),
    ({"rc": 0, "stdout": "kernel=0\npolicy=FIPS\n"}, True, False),
    ({"rc": 0, "stdout": "kernel=1\npolicy=DEFAULT\n"}, True, False),
    ({"rc": 0, "stdout": "kernel=1\npolicy=FIPS:OSPP\n"}, True, False),
    ({"rc": 0, "stdout": ""}, True, False),
    ({"rc": 1, "stdout": "kernel=1\npolicy=FIPS\n"}, True, False),
    (None, True, False),
    ({"rc": 0, "stdout": "kernel=1\npolicy=FIPS\n"}, False, False),
], ids=["fips", "kernel-0", "default", "ospp", "empty", "rc-1", "unread", "not-requested"])
def test_fips_state_reader_accepts_only_kernel_1_and_policy_FIPS(guest, requested, expected):
    variables = {"managedos_install_anaconda_fips_requested": requested}
    if guest is not None:
        variables["managedos_install_anaconda_guest"] = guest
    assert proves(**variables) is expected


def test_the_fips_intent_is_the_frozen_installer_argument():
    requested = flattened("host_security.yml")[0]["ansible.builtin.set_fact"]["managedos_install_anaconda_fips_requested"]
    for arguments, expected in ((["inst.ks=hd:LABEL=x", "fips=1"], True), (["inst.ks=hd:LABEL=x"], False), (["fips=0"], False)):
        templar = Templar(loader=LOADER, variables={"bootwright_os_install_request": {"installerArguments": arguments}})
        assert templar.template(requested) is expected


# An observation runs this read too, so it writes nothing, needs no privilege
# and names only the two files, bounded, whatever they hold.
def test_the_fips_read_only_reads():
    assert subprocess.run(["/bin/sh", "-n", "-c", READ], check=False).returncode == 0
    assert READ.replace("2>/dev/null", "").count(">") == 0
    assert not any(word in READ for word in ("sudo", "fips-mode-setup", "update-crypto-policies", "tee", "rm ", "mv ", "cp "))
    assert READ.count("/proc/sys/crypto/fips_enabled") == 1 and READ.count("/etc/crypto-policies/state/current") == 1
