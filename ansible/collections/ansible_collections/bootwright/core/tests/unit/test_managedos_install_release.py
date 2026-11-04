"""Every observation that reaches the installed system reads its release.

The read names ID and VERSION_ID of /etc/os-release
(https://www.freedesktop.org/software/systemd/man/latest/os-release.html) and
the running kernel's release, /proc/sys/kernel/osrelease, over the pinned fleet
connection guest.yml owns, after the fleet account answered. It writes
nothing, needs no privilege, and cuts each value to the characters and length
the evidence admits, so whatever the files hold the protocol publishes a
bounded release or none.

Rendering the role's files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib
import subprocess

import pytest

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as protocol


ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
LOADER = DataLoader()
PROTOCOL = "bootwright.core.managedos_install_protocol"
INCLUDE = "ansible.builtin.include_tasks"
REACHED = "managedos_install_anaconda_reachable.rc | default(1) == 0"
SET_FACT = "ansible.builtin.set_fact"
OS_RELEASE = "/etc/os-release"
KERNEL = "/proc/sys/kernel/osrelease"


def tasks(name):
    return LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True)


def flattened(name):
    """A task file's tasks in play order, each block opened and its condition
    carried onto every task inside it."""
    def opened(listed, inherited):
        for task in listed or []:
            conditions = task.get("when", [])
            conditions = inherited + (conditions if isinstance(conditions, list) else [conditions])
            if "block" in task:
                for section in ("block", "rescue", "always"):
                    yield from opened(task.get(section), conditions)
                continue
            yield dict(task, when=conditions)
    return list(opened(tasks(name), []))


def only(listed, predicate, description):
    found = [index for index, task in enumerate(listed) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def command():
    read = tasks("os_release.yml")[0]
    assert read[INCLUDE] == "guest.yml"
    return read["vars"]["managedos_install_anaconda_guest_command"]


def test_an_observation_reads_the_release_once_the_fleet_account_answered():
    observed = flattened("observe.yml")
    reachable = only(observed, lambda task: task.get(INCLUDE) == "reachable.yml", "try the fleet account")
    read = only(observed, lambda task: task.get(INCLUDE) == "os_release.yml", "read the release")
    published = only(observed, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish the observation")
    assert reachable < read < published
    assert observed[read]["when"][-1] == REACHED
    assert "managedos_install_anaconda_observes_removal" in observed[read]["when"][0]
    assert observed[published][PROTOCOL]["os"] == "{{ managedos_install_anaconda_os | default({}) }}"
    assert not any(task.get(INCLUDE) == "os_release.yml" for task in flattened("apply.yml") + flattened("destroy.yml"))


def test_the_release_read_only_reads():
    shell, flag, script, name = command()
    assert (shell, flag, name) == ("/usr/bin/sh", "-c", "bootwright-os-release")
    assert subprocess.run(["/bin/sh", "-n", "-c", script], check=False).returncode == 0
    assert script.replace("2>/dev/null", "").count(">") == 0
    assert not any(word in script for word in ("sudo", "tee", "rm ", "mv ", "cp ", ". /", "source", "eval"))
    assert script.count(OS_RELEASE) == 1 and script.count(KERNEL) == 1


def read(tmp_path, os_release, kernel):
    """The read's output over files standing in for the system's two."""
    script = command()[2]
    for path, content in ((OS_RELEASE, os_release), (KERNEL, kernel)):
        stand_in = tmp_path / pathlib.Path(path).name
        if content is not None:
            stand_in.write_text(content)
        script = script.replace(path, str(stand_in))
    return subprocess.run(["/bin/sh", "-c", script, "bootwright-os-release"], capture_output=True, text=True, check=True)


def named(guest):
    fact = tasks("os_release.yml")[1]
    variables = {"managedos_install_anaconda_guest": guest}
    templar = Templar(loader=LOADER, variables=variables)
    variables["managedos_install_anaconda_os_lines"] = templar.template(fact["vars"]["managedos_install_anaconda_os_lines"])
    return Templar(loader=LOADER, variables=variables).template(fact[SET_FACT]["managedos_install_anaconda_os"])


@pytest.mark.parametrize("os_release, kernel, expected", [
    ('NAME="Red Hat Enterprise Linux"\nVERSION="9.6 (Plow)"\nID="rhel"\nID_LIKE="fedora"\nVERSION_ID="9.6"\n',
     "5.14.0-570.12.1.el9_6.x86_64\n", {"id": "rhel", "kernel": "5.14.0-570.12.1.el9_6.x86_64", "versionId": "9.6"}),
    ("ID_LIKE=fedora\nID=fedora\nVERSION_ID=44\n", "7.2.13-200.fc44.x86_64\n",
     {"id": "fedora", "kernel": "7.2.13-200.fc44.x86_64", "versionId": "44"}),
    ("ID='rhel'\nVERSION_ID='10.0'\nVERSION_ID=11.1\n", "6.12.0\n", {"id": "rhel", "kernel": "6.12.0", "versionId": "10.0"}),
    ('ID="rh el;$(true)"\nVERSION_ID="' + "9" * 80 + '"\n', "k/" * 100,
     {"id": "rheltrue", "kernel": "k" * 64, "versionId": "9" * 64}),
    (None, None, {"id": "", "kernel": "", "versionId": ""}),
], ids=["rhel", "fedora", "first-assignment", "bounded", "unreadable"])
def test_the_read_names_a_bounded_release_the_protocol_admits(tmp_path, os_release, kernel, expected):
    answered = read(tmp_path, os_release, kernel)
    assert named({"rc": 0, "stdout": answered.stdout}) == expected
    assert protocol.system(expected) == expected


@pytest.mark.parametrize("guest", [
    {"rc": 255, "stdout": "id=rhel\nversion=9.6\nkernel=5.14\n"},
    {"rc": 0, "stdout": ""},
    {"rc": 0, "stdout": "id=rhel\nversion=9.6\n"},
    {"rc": 0, "stdout": "version=9.6\nid=rhel\nkernel=5.14\n"},
    {"rc": 0, "stdout": "id=rhel\nversion=9.6\nkernel=5.14\nextra=1\n"},
    {},
], ids=["unreached", "empty", "short", "reordered", "long", "unread"])
def test_an_answer_not_in_the_read_s_form_names_no_release(guest):
    assert named(guest) == {}
