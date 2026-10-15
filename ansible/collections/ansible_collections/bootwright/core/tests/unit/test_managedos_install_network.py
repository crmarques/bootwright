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

from ansible.errors import AnsibleUndefinedVariable
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
EARLIER_CHECKPOINT = "/org/freedesktop/NetworkManager/Checkpoint/6"
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


def test_the_declared_network_applies_under_a_checkpoint_it_does_not_commit():
    scope = dict(defaults(), **failing_scope())
    argv = [str(word) for word in Templar(loader=LOADER, variables=scope).template(
        trust_as_template("{{ managedos_install_anaconda_network_start }}"))]
    shell = argv.index("/usr/bin/sh")
    assert argv[:shell] == ["/usr/bin/sudo", "-n", "/usr/bin/systemd-run", "--no-block", "--quiet", "--collect",
                            "--unit=bootwright-network-apply.service", "-p", "Type=exec"]
    assert argv[shell + 1] == "-c"
    script = argv[shell + 2]
    assert script.startswith('/usr/bin/nmstatectl apply --no-commit --timeout="$1" "$2" ')
    assert "--memory-only" not in script and "/etc/nmstate" not in script
    assert argv[shell + 3:] == ["bootwright-network", "300", "/etc/bootwright/network.yml", "/run/bootwright"]


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


# nmstatectl 2.x prints only the applied diff as YAML on standard output and
# names the checkpoint in env_logger INFO lines on standard error
# (rust/src/cli/apply.rs apply_state and rust/src/cli/result.rs; "Created
# checkpoint" and "Skipping commit for checkpoint" in
# rust/src/lib/query_apply/net_state.rs, base branch of
# https://github.com/nmstate/nmstate).
APPLY_STDOUT = "---\ninterfaces:\n- name: bond0\n  type: bond\n  state: up\n"
APPLY_STDERR = (
    "[2026-10-15T10:00:00Z INFO  nmstatectl] Nmstate version: 2.2.62\n"
    "[2026-10-15T10:00:01Z INFO  nmstate::query_apply::net_state] Created checkpoint %s\n"
    "[2026-10-15T10:00:04Z INFO  nmstate::query_apply::net_state] Skipping commit for checkpoint %s\n"
) % (CHECKPOINT, CHECKPOINT)


def test_the_read_script_output_parses(tmp_path):
    tools = {name: shutil.which(name) for name in ("grep", "head", "tail", "tr")}
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
    (run / "network-apply.log").write_text(APPLY_STDOUT)
    (run / "network-apply.err").write_text(APPLY_STDERR)
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
    assert state["error"].endswith("Skipping commit for checkpoint %s " % CHECKPOINT)


NEW_BOOT_ID = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4e"
EARLIER_BOOT_ID = "1a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4f"
SET_FACT = "ansible.builtin.set_fact"
COMPARE = "bootwright.core.managedos_network_compare"
REDFISH = "bootwright.core.redfish_system_read"
NETWORK = {"checkpointSeconds": 300, "document": DOCUMENT, "documentPath": "/etc/bootwright/network.yml",
           "markerPath": "/etc/bootwright/network-marker.json"}


def guest_read(boot=BOOT_ID, rc="", checkpoint="", listed=(), marker=""):
    """The READ script's output, one key=value line each."""
    return "".join("%s=%s\n" % pair for pair in (
        ("nmstatectl", "yes"), ("bootId", boot), ("unit", "inactive"), ("rc", rc), ("checkpoint", checkpoint),
        ("listed", "ao %d%s" % (len(listed), "".join(' "%s"' % path for path in listed))),
        ("marker", marker), ("error", "")))


def marker_of(boot):
    return '{"bootId":"%s","request":"%s"}' % (boot, DIGEST)


def answered(stdout="", rc=0):
    return {"rc": rc, "stdout": stdout, "stderr": ""}


class Guest:
    """Network task files run in order over one guest, each command answered
    from a queue; every refusal that would fire is recorded, never raised."""

    def __init__(self, answers, matched=True):
        self.answers = {name: list(queue) for name, queue in answers.items()}
        self.matched = matched
        self.ran = []
        self.argv = {}
        self.fired = []
        self.scope = dict(defaults(), **{
            "bootwright_os_install_digest": DIGEST,
            "bootwright_os_install_request": {
                "identity": {"object": MACHINE}, "address": ADDRESS, "placement": {"machine": "controller"},
                "target": {"substrate": "libvirt"}, "network": NETWORK,
                "budgets": {name: {"attempts": 12, "delaySeconds": 5}
                            for name in ("networkApply", "networkReconnect", "reboot")},
            },
        })

    def run(self, name):
        self.perform(LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True), [])
        assert not any(self.answers.values()), ("commands never ran", self.answers)
        return self

    def reached(self, conditions):
        templar = Templar(loader=LOADER, variables=self.scope)
        return all(templar.evaluate_conditional(condition) for condition in conditions)

    def perform(self, listed, inherited):
        for task in listed:
            own = task.get("when", [])
            conditions = inherited + (own if isinstance(own, list) else [own])
            if "block" in task:
                if self.reached(conditions):
                    self.perform(task["block"], [])
                continue
            if not self.reached(conditions):
                if "register" in task:
                    self.scope[task["register"]] = {"changed": False, "skipped": True}
                continue
            self.step(task)

    def step(self, task):
        templar = Templar(loader=LOADER, variables=self.scope)
        if task.get(INCLUDE) == "guest.yml":
            command = task["vars"][GUEST].rsplit("managedos_install_anaconda_network_", 1)[-1].strip(" }")
            self.ran.append(command)
            self.argv.setdefault(command, []).append([str(word) for word in templar.template(task["vars"][GUEST])])
            self.scope["managedos_install_anaconda_guest"] = self.answers[command].pop(0)
        elif FAIL in task:
            self.fired.append(task["name"])
        elif "ansible.builtin.assert" in task:
            if not all(templar.evaluate_conditional(condition) for condition in task["ansible.builtin.assert"]["that"]):
                self.fired.append(task["name"])
        elif SET_FACT in task:
            for fact, value in task[SET_FACT].items():
                self.scope[fact] = templar.template(value)
        elif COMPARE in task:
            self.scope[task["register"]] = {"matched": self.matched,
                                            "differences": [] if self.matched else ["bond0 is down, declared up"]}
        elif REDFISH in task:
            self.scope[task["register"]] = {"power": "Off"}
        else:
            assert {"ansible.builtin.include_role", "ansible.builtin.pause"} & set(task), task["name"]


def converging(first, *after):
    """Answers for a guest whose network converges after the first read."""
    answers = {"wait": [answered()], "read": [answered(first)], "boot_id": [answered(NEW_BOOT_ID + "\n")],
               "show": [answered("{}")]}
    for name in after:
        answers.setdefault(name, []).append(answered())
    return answers


APPLIED = guest_read(rc="0", checkpoint=CHECKPOINT, listed=[CHECKPOINT])


@pytest.mark.parametrize("first, after, expected", [
    (guest_read(), ("prepare", "start", "wait", "commit", "mark", "poweroff"),
     ["wait", "read", "prepare", "start", "wait", "read", "commit", "mark", "poweroff", "boot_id", "show"]),
    (guest_read(rc="", checkpoint=EARLIER_CHECKPOINT, listed=[EARLIER_CHECKPOINT]),
     ("rollback", "prepare", "start", "wait", "commit", "mark", "poweroff"),
     ["wait", "read", "rollback", "prepare", "start", "wait", "read", "commit", "mark", "poweroff", "boot_id", "show"]),
    (guest_read(marker=marker_of(BOOT_ID)), ("poweroff",), ["wait", "read", "poweroff", "boot_id", "show"]),
    (guest_read(marker=marker_of(EARLIER_BOOT_ID)), (), ["wait", "read", "show"]),
], ids=["apply", "rollback", "reboot", "inspect"])
def test_a_converging_network_fires_no_refusal(first, after, expected):
    answers = converging(first, *after)
    if "commit" in after:
        answers["read"].append(answered(APPLIED))
    if "poweroff" not in after:
        answers.pop("boot_id")
    guest = Guest(answers).run("network.yml")
    assert guest.fired == [] and guest.ran == expected
    assert guest.scope["managedos_install_anaconda_network_result"] == {
        "network": True, "networkReboot": True, "networkDifferences": [], "changed": expected != ["wait", "read", "show"]}
    if "rollback" in after:
        rollback = guest.argv["rollback"][0]
        assert rollback[rollback.index("bootwright-network") + 1] == EARLIER_CHECKPOINT
    if "commit" in after:
        assert guest.argv["commit"] == [["/usr/bin/sudo", "-n", "/usr/bin/nmstatectl", "commit", CHECKPOINT]]


@pytest.mark.parametrize("first, matched, network, reboot", [
    (guest_read(marker=marker_of(EARLIER_BOOT_ID)), True, True, True),
    (guest_read(marker=marker_of(EARLIER_BOOT_ID)), False, False, True),
    (guest_read(marker=marker_of(BOOT_ID)), True, True, False),
    (guest_read(), True, False, False),
    (None, True, False, False),
], ids=["settled", "drifted", "this-boot", "unmarked", "unreadable"])
def test_an_observation_proves_the_network_only_from_its_own_marker(first, matched, network, reboot):
    if first is None:
        answers = {"read": [answered(rc=255)]}
    else:
        answers = {"read": [answered(first)], "show": [answered("{}")]}
    guest = Guest(answers, matched=matched).run("network_observe.yml")
    assert guest.fired == []
    assert guest.scope["managedos_install_anaconda_network_result"] == {
        "network": network, "networkReboot": reboot,
        "networkDifferences": [] if matched or first is None else ["bond0 is down, declared up"], "changed": False}


def publication(name, **scope):
    task = next(task for task in flattened(name) if (task.get(PROTOCOL) or {}).get("phase") == "completed")
    variables = dict(defaults(), **{
        "bootwright_os_install_digest": DIGEST,
        "bootwright_os_install_request": {"address": ADDRESS},
        "managedos_install_anaconda_was_installed": True,
        "managedos_install_anaconda_after": {"observation": {"image": True, "tree": True}},
        "managedos_install_anaconda_observed": {"observation": {"image": True, "tree": True}},
        "managedos_install_anaconda_marker": '{"context":"lab"}',
        "managedos_install_anaconda_host_key": "ssh-ed25519 AAAA",
        "managedos_install_anaconda_reachable": {"rc": 0},
        "managedos_install_anaconda_ejected": {"media": "", "power": "On"},
        "managedos_install_anaconda_controller": {"media": "", "power": "On"},
        "managedos_install_anaconda_delivers_key": False,
    })
    variables.update(scope)
    return protocol.completion(Templar(loader=LOADER, variables=variables).template(task[PROTOCOL]))


@pytest.mark.parametrize("name", ["apply.yml", "observe.yml"])
def test_a_completion_publishes_the_network_the_role_proved(name):
    proved = {"network": True, "networkReboot": True, "networkDifferences": [], "changed": False}
    evidence, unmet = publication(name, managedos_install_anaconda_network_result=proved)
    assert unmet is None and evidence["postcondition"] is True
    assert (evidence["network"], evidence["networkReboot"], evidence["networkDifferences"]) == (True, True, [])
    drifted = dict(proved, network=False, networkDifferences=["bond0 is down, declared up"])
    evidence = publication(name, managedos_install_anaconda_network_result=drifted)[0]
    assert (evidence["network"], evidence["networkDifferences"]) == (False, ["bond0 is down, declared up"])
    evidence, unmet = publication(name)
    assert (evidence["network"], evidence["networkReboot"], evidence["networkDifferences"]) == (False, False, [])
    assert evidence["postcondition"] is False and protocol.unproved(evidence) == ["network", "networkReboot"]
    if name == "apply.yml":
        assert unmet is not None


NETWORK_CONDITIONS = [
    "bootwright_os_install_request.network.documentPath == '/etc/bootwright/network.yml'",
    "bootwright_os_install_request.network.markerPath == '/etc/bootwright/network-marker.json'",
    "bootwright_os_install_request.network.checkpointSeconds | int > 0",
    "bootwright_os_install_request.network.document is mapping",
]


def refuses(conditions, request):
    templar = Templar(loader=LOADER, variables={"bootwright_os_install_request": request})
    try:
        return not all(templar.evaluate_conditional(condition) for condition in conditions)
    except AnsibleUndefinedVariable:
        return True


@pytest.mark.parametrize("name", ["apply.yml", "observe.yml"])
def test_a_request_without_its_network_refuses_before_any_effect(name):
    first = LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True)[0]
    assert first["name"] == "Validate the frozen installation request"
    that = first["ansible.builtin.assert"]["that"]
    conditions = [condition for condition in that if "request.network" in condition]
    assert conditions == NETWORK_CONDITIONS
    assert not refuses(conditions, {"network": NETWORK})
    for request in ({}, {"network": dict(NETWORK, documentPath="/etc/nmstate/network.yml")},
                    {"network": dict(NETWORK, markerPath="/etc/bootwright/install-marker.json")},
                    {"network": dict(NETWORK, checkpointSeconds=0)},
                    {"network": dict(NETWORK, document=[DOCUMENT])},
                    {"network": {key: value for key, value in NETWORK.items() if key != "document"}}):
        assert refuses(conditions, request), request
