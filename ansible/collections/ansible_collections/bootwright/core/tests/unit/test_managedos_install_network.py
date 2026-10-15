"""Every installed Machine converges to its declared network, and is then proved.

The convergence runs fixed commands on the installed machine over the fleet
connection pinned to the key its identity channel reported: nmstatectl applies
the declaration under a checkpoint NetworkManager rolls back by itself unless
it is committed (nmstatectl(8)), the machine is powered off and its installed
disk booted through the substrate, and the network it starts with is compared
with the declaration. An observation only reads.

Rendering the role's files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib
import re
import shutil
import subprocess
from types import SimpleNamespace

import pytest

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as protocol
from ansible_collections.bootwright.core.plugins.module_utils.managedos_network import guest_state

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
ROLES = ROLE.parent
LOADER = DataLoader()
PROTOCOL = "bootwright.core.managedos_install_protocol"
FAIL = "ansible.builtin.fail"
INCLUDE = "ansible.builtin.include_tasks"
COMMAND = "ansible.builtin.command"
GUEST = "managedos_install_anaconda_guest_command"
DIGEST = "0123456789abcdef" * 4
BOOT_ID = "6f1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
CHECKPOINT = "/org/freedesktop/NetworkManager/Checkpoint/7"
SENTINEL = "sentinel-0f6e2a-network-document"
REPLAN = "plan the installation again with this release"
MACHINE = "rhel-01"
ADDRESS = "198.51.100.11"
# The document itself, not the path it is written to.
DOCUMENT_REFERENCE = re.compile(r"network\.document(?!Path)")
DOCUMENT = {"interfaces": [{"name": "bond0", "type": "bond", "state": "up", "description": SENTINEL}]}


def flattened(name):
    """A task file's tasks in the order a play reaches them, each block opened."""
    def opened(listed):
        for task in listed or []:
            if not isinstance(task, dict):
                continue
            yield task
            for section in ("block", "rescue", "always"):
                yield from opened(task.get(section))
    return list(opened(LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True)))


def defaults():
    return dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))


def commands():
    """The fixed guest command lists the defaults name."""
    return {name: value for name, value in defaults().items()
            if name.startswith("managedos_install_anaconda_network_") and isinstance(value, list)}


def index_of(tasks, predicate, description):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def runs(command):
    """Whether a task includes guest.yml with the named network command."""
    return lambda task: task.get(INCLUDE) == "guest.yml" and (task.get("vars") or {}).get(GUEST) == (
        "{{ managedos_install_anaconda_network_%s }}" % command)


def named(name):
    return lambda task: task.get("name") == name


def scripts():
    """Every /usr/bin/sh -c script the defaults name, by command."""
    found = {}
    for name, words in commands().items():
        for position, word in enumerate(words[:-1]):
            if word == "/usr/bin/sh" and words[position + 1] == "-c":
                found[name] = words[position + 2]
    return found


def test_network_task_order():
    tasks = flattened("network.yml")
    g0 = index_of(tasks, named("Stop at a machine that cannot apply a declared network"), "G0")
    prepare = index_of(tasks, runs("prepare"), "prepare")
    commit = index_of(tasks, runs("commit"), "commit")
    mark = index_of(tasks, runs("mark"), "mark")
    poweroff = index_of(tasks, runs("poweroff"), "poweroff")
    off = index_of(tasks, lambda task: "ansible.builtin.assert" in task, "the Off assert")
    disk = min(index for index, task in enumerate(tasks)
               if (task.get("ansible.builtin.include_role") or {}).get("tasks_from") == "boot_disk")
    boot_id = index_of(tasks, runs("boot_id"), "boot id")
    compare = index_of(tasks, lambda task: "bootwright.core.managedos_network_compare" in task, "compare")
    assert g0 < prepare < commit < mark < poweroff < off < disk < boot_id < compare

    applied = flattened("apply.yml")
    verify = index_of(applied, lambda task: task.get(INCLUDE) == "verify.yml", "verify")
    withdraw = index_of(applied, named("Withdraw the private material now that the machine has it"), "withdraw")
    network = index_of(applied, lambda task: task.get(INCLUDE) == "network.yml", "network")
    inspect = index_of(applied, named("Prove everything this installation publishes is in place"), "inspect")
    reported = index_of(applied, lambda task: (task.get(PROTOCOL) or {}).get("group") == "verify-installation"
                        and task[PROTOCOL].get("status") == "ok", "verification completion")
    assert verify < withdraw < network < reported < inspect
    # One code path: the include has no predicate on the network's content.
    assert "when" not in applied[network]

    observed = LOADER.load_from_file(str(ROLE / "tasks" / "observe.yml"), trusted_as_template=True)
    block = next(task for task in observed if "block" in task)["block"]
    reachable = index_of(block, lambda task: task.get(INCLUDE) == "reachable.yml", "reachable")
    reading = index_of(block, lambda task: task.get(INCLUDE) == "network_observe.yml", "network observe")
    assert reachable < reading
    assert block[reading]["when"] == "managedos_install_anaconda_reachable.rc | default(1) == 0"


def test_restart_boots_the_installed_disk_through_the_substrate():
    tasks = flattened("network.yml")
    index_of(tasks, runs("poweroff"), "poweroff")
    listed = commands()
    assert "poweroff" in listed["managedos_install_anaconda_network_poweroff"]
    for name, words in listed.items():
        assert not any("reboot" in str(word) for word in words), name
    disks = [index for index, task in enumerate(tasks)
             if (task.get("ansible.builtin.include_role") or {}).get("tasks_from") == "boot_disk"]
    assert len(disks) == 2
    for index, role in zip(disks, ("libvirt", "baremetal")):
        include = tasks[index]
        assert include["ansible.builtin.include_role"]["name"] == "bootwright.core.substrate_%s_machine" % role
        assert include["vars"]["substrate_%s_machine_disk_power_on" % role] is True
        assert include["when"] == "bootwright_os_install_request.target.substrate == '%s'" % role
    refusal = tasks[disks[-1] + 1]
    assert refusal["when"] == "bootwright_os_install_request.target.substrate not in ['libvirt', 'baremetal']"
    assert refusal[FAIL]["msg"].strip().endswith(REPLAN)


def failing_scope(**overrides):
    """Every variable a network refusal reads, for a Machine that failed there."""
    state = {"valid": True, "reason": "", "nmstatectl": True, "bootId": BOOT_ID, "unit": "inactive",
             "running": False, "rc": 1, "checkpoint": CHECKPOINT, "listed": [CHECKPOINT], "marker": {},
             "error": "NmstateVerificationError: the bond is not up"}
    scope = {
        "bootwright_os_install_digest": DIGEST,
        "bootwright_os_install_request": {
            "identity": {"object": MACHINE}, "address": ADDRESS, "placement": {"machine": "controller"},
            "target": {"substrate": "libvirt"},
            "network": {"checkpointSeconds": 300, "document": DOCUMENT, "documentPath": "/etc/bootwright/network.yml",
                        "markerPath": "/etc/bootwright/network-marker.json"},
            "budgets": {name: {"attempts": 12, "delaySeconds": 5} for name in ("networkApply", "networkReconnect", "reboot")},
        },
        "managedos_install_anaconda_guest": {"rc": 1, "stdout": "", "stderr": "nmstatectl: refused\n"},
        "managedos_install_anaconda_network_state": dict(state, nmstatectl=False),
        "managedos_install_anaconda_network_applied": state,
        "managedos_install_anaconda_network_step": "apply",
        "managedos_install_anaconda_network_waited": 255,
        "managedos_install_anaconda_network_lost": True,
        "managedos_install_anaconda_network_compared": {"matched": False, "differences": ["bond0 is down, declared up"]},
    }
    scope.update(overrides)
    return scope


DESTROYING = (
    "Stop at a machine that cannot apply a declared network",
    "Stop at a rollback NetworkManager refused",
    "Stop at a declared network NetworkManager refused",
    "Stop at a network change still uncommitted after the connection returned",
    "Stop at a declared network that cut the controller off",
    "Stop at a running network that differs from its declaration",
)


def test_network_failures_are_partial_and_name_destroy():
    tasks = flattened("network.yml")
    templar = Templar(loader=LOADER, variables=failing_scope())
    for name in DESTROYING:
        task = tasks[index_of(tasks, named(name), name)]
        conditions = task.get("when", [])
        for condition in conditions if isinstance(conditions, list) else [conditions]:
            assert templar.evaluate_conditional(condition), name
        message = templar.template(trust_as_template(task[FAIL]["msg"]))
        assert MACHINE in message and "bootwright destroy" in message, (name, message)
        assert message.isprintable(), (name, message)
    for task in tasks:
        if FAIL in task:
            text = task[FAIL]["msg"]
            assert "no effect" not in text and "nothing was installed" not in text, task["name"]
            assert "repeat bootwright apply" in text or "bootwright destroy" in text or REPLAN in text, task["name"]


def option_values(argv):
    return [argv[position + 1] for position, word in enumerate(argv) if word == "-o"]


def ssh_task(name, register):
    return next(task for task in flattened(name) if task.get("register") == register)


def test_guest_commands_pin_the_host_key():
    guest = ssh_task("guest.yml", "managedos_install_anaconda_guest")
    reachable = ssh_task("reachable.yml", "managedos_install_anaconda_reachable")
    options = option_values(guest[COMMAND]["argv"])
    assert options == [value.replace("managedos_install_anaconda_host_key_algorithms",
                                     "managedos_install_anaconda_guest_host_key_algorithms")
                       for value in option_values(reachable[COMMAND]["argv"])]
    for required in ("StrictHostKeyChecking=yes", "KnownHostsCommand=none", "GlobalKnownHostsFile=none",
                     "UserKnownHostsFile={{ managedos_install_anaconda_scratch }}/known_hosts"):
        assert required in options, required
    argv = guest[COMMAND]["argv"]
    assert argv[:3] == ["/usr/bin/ssh", "-F", "{{ managedos_install_anaconda_scratch }}/ssh_config"]
    assert argv[-1] == '{{ managedos_install_anaconda_guest_command | map("quote") | join(" ") }}'
    assert guest["no_log"] is True and guest["failed_when"] is False
    known = next(task for task in flattened("guest.yml")
                 if (task.get("ansible.builtin.copy") or {}).get("dest", "").endswith("/known_hosts"))
    assert known["ansible.builtin.copy"]["content"] == (
        "{{ bootwright_os_install_request.address }} {{ managedos_install_anaconda_host_key | trim }}")
    assert known["ansible.builtin.copy"]["mode"] == "0600"
    for path in sorted(ROLES.glob("*/tasks/*.yml")) + sorted(ROLES.glob("*/defaults/*.yml")):
        text = path.read_text()
        assert "StrictHostKeyChecking=no" not in text and "accept-new" not in text, path


def test_document_reaches_the_guest_on_stdin_only():
    tasks = flattened("network.yml")
    prepare = tasks[index_of(tasks, runs("prepare"), "prepare")]
    scope = dict(defaults(), **failing_scope())
    templar = Templar(loader=LOADER, variables=scope)
    stdin = templar.template(prepare["vars"]["managedos_install_anaconda_guest_stdin"])
    command = templar.template(prepare["vars"][GUEST])
    assert SENTINEL in stdin and isinstance(stdin, str)
    assert SENTINEL not in " ".join(str(word) for word in command)
    guest = ssh_task("guest.yml", "managedos_install_anaconda_guest")
    joined = Templar(loader=LOADER, variables=dict(scope, **{GUEST: command})).template(guest[COMMAND]["argv"][-1])
    assert SENTINEL not in joined
    for name, words in commands().items():
        assert not DOCUMENT_REFERENCE.search(repr(words)), name
    for task in tasks:
        assert not DOCUMENT_REFERENCE.search(repr({key: value for key, value in (task.get("vars") or {}).items()
                                                   if key != "managedos_install_anaconda_guest_stdin"})), task.get("name")
    mark = tasks[index_of(tasks, runs("mark"), "mark")]
    marker = Templar(loader=LOADER, variables=dict(scope, managedos_install_anaconda_network_applied={"bootId": BOOT_ID})
                     ).template(mark["vars"]["managedos_install_anaconda_guest_stdin"])
    assert marker == '{"bootId":"%s","request":"%s"}' % (BOOT_ID, DIGEST)


def install(**fields):
    arguments = {"phase": "completed", "outcome": "changed", "digest": DIGEST,
                 "observation": {"image": True, "tree": True}, "marker": '{"context":"lab"}',
                 "hostKey": "ssh-ed25519 AAAA", "address": ADDRESS, "media": "", "power": "On", "reachable": True,
                 "network": True, "networkReboot": True}
    arguments.update(fields)
    return arguments


def published(arguments):
    emitted = []
    module = protocol.ActionModule.__new__(protocol.ActionModule)
    module._task = SimpleNamespace(args=arguments)
    original = protocol.emit
    protocol.emit = lambda message, **kwargs: emitted.append(message)
    try:
        return module.run(task_vars={}), emitted
    finally:
        protocol.emit = original


def test_protocol_bounds_network_evidence():
    most = ["d" * 200] * 16
    evidence, unmet = protocol.completion(install(networkDifferences=most, observed=True))
    assert unmet is None and evidence["networkDifferences"] == most
    for differences in (["d"] * 17, ["d" * 201], ["bond0\x07"], "bond0", [3]):
        with pytest.raises(ValueError):
            protocol.completion(install(networkDifferences=differences))
        result, emitted = published(install(networkDifferences=differences, observed=True))
        assert result["failed"] is True and not emitted, differences
        assert result["msg"] == "the installation capability result could not be published"
    assert protocol.presence(install(), DIGEST)["postcondition"] is True
    for unproved in ({"network": False}, {"networkReboot": False}):
        evidence = protocol.presence(install(**unproved), DIGEST)
        assert evidence["postcondition"] is False, unproved
        assert protocol.unproved(evidence) == list(unproved), unproved
    assert protocol.presence(install(networkDifferences=None), DIGEST)["networkDifferences"] == []
    absent = protocol.absence({"observation": {}}, DIGEST)
    assert (absent["network"], absent["networkDifferences"], absent["networkReboot"]) == (False, [], False)


FORBIDDEN = ("nmstatectl apply", "commit", "rollback", "systemd-run", "poweroff", "reboot", "rm -", "mv -",
             "install -", "cat >", "chmod")


def test_observe_reads_network_read_only():
    tasks = flattened("network_observe.yml")
    included = [task["vars"][GUEST] for task in tasks if task.get(INCLUDE)]
    assert all(task.get(INCLUDE) in (None, "guest.yml") for task in tasks)
    assert included == ["{{ managedos_install_anaconda_network_read }}", "{{ managedos_install_anaconda_network_show }}"]
    listed = commands()
    for name in ("managedos_install_anaconda_network_read", "managedos_install_anaconda_network_show"):
        text = " ".join(listed[name])
        assert not any(word in text for word in FORBIDDEN), name
    for task in tasks:
        modules = [key for key in task if "." in key]
        assert not any(module.startswith("bootwright.core.redfish_") for module in modules), task["name"]
        assert not any(module in modules for module in (
            "ansible.builtin.include_role", FAIL, "ansible.builtin.assert", "ansible.builtin.pause", COMMAND)), task["name"]


def test_the_guest_scripts_parse():
    found = scripts()
    assert set(found) == {"managedos_install_anaconda_network_%s" % name
                          for name in ("read", "wait", "prepare", "start", "rollback", "mark")}
    for name, script in found.items():
        assert "{{" not in script and "{%" not in script and "{#" not in script, name
        checked = subprocess.run(["/bin/sh", "-n", "-c", script], capture_output=True, text=True, check=False)
        assert checked.returncode == 0, (name, checked.stderr)


def stub(directory, name, output, sentinel):
    path = directory / name
    path.write_text("#!/bin/sh\n: > '%s'\nprintf '%%s\\n' '%s'\n" % (sentinel, output))
    path.chmod(0o755)


def test_the_read_script_output_parses(tmp_path):
    tools = {name: shutil.which(name) for name in ("head", "tail", "tr")}
    assert all(tools.values()), tools
    stubs = tmp_path / "bin"
    stubs.mkdir()
    for name, target in tools.items():
        (stubs / name).symlink_to(target)
    stub(stubs, "systemctl", "inactive", tmp_path / "systemctl-ran")
    stub(stubs, "busctl", 'ao 1 "%s"' % CHECKPOINT, tmp_path / "busctl-ran")
    run = tmp_path / "run"
    run.mkdir()
    (run / "network-apply.rc").write_text("0\n")
    (run / "network-apply.log").write_text("applying\n%s\n" % CHECKPOINT)
    (run / "network-apply.err").write_text("warning\tline\n")
    marker = tmp_path / "network-marker.json"
    marker.write_text('{"bootId":"%s","request":"%s"}\n' % (BOOT_ID, DIGEST))
    script = scripts()["managedos_install_anaconda_network_read"]
    read = subprocess.run(["/bin/sh", "-c", script, "bootwright-network", "bootwright-network-apply.service",
                           str(run), str(marker)], capture_output=True, text=True, check=False,
                          env={"PATH": str(stubs)})
    assert read.returncode == 0, read.stderr
    assert (tmp_path / "systemctl-ran").exists() and (tmp_path / "busctl-ran").exists()
    state = guest_state(read.stdout)
    assert state["valid"] is True, (state, read.stdout)
    assert state["unit"] == "inactive" and state["running"] is False
    assert state["rc"] == 0 and state["checkpoint"] == CHECKPOINT and state["listed"] == [CHECKPOINT]
    assert state["marker"] == {"bootId": BOOT_ID, "request": DIGEST}
    assert state["error"].startswith("warning line")
