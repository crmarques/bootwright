"""The substrate owns the machine port, and consumers compose only its entry points.

Each machine role declares its port, pre_boot, boot_media, boot_disk and
identity_read, beside its lifecycle entry points in meta/argument_specs.yml
(specs/substrates.md, Identity and power operations). ansible-core validates an
entry point's inputs before its first task only when tasks_from names it by
exactly that key: one written with its extension runs unvalidated
(.agents/knowledge/ansible-role-argument-specs.md). These cases find every
include of a machine role in every role, require it to name an entry point by
its key and to pass exactly that entry point's inputs, and validate what each
include renders, for a libvirt and a bare-metal managed-OS target and cluster
node, with the validator ansible-core's prepended task uses. They also pin what
the port's own files do and what no consumer does for itself any longer.

The targets carry the values three goldens freeze: request-lab-rhel.golden in
internal/managedos/installation/testdata, and install-request-lab-sno.golden
and install-nodes-physical.golden in internal/containercluster/agentinstall/
testdata.
Rendering needs Ansible's controller (DataLoader, Templar and the role argument
spec validator), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import base64
import pathlib
import re

import pytest
from ansible.module_utils.common.arg_spec import ArgumentSpecValidator
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()

INCLUDES = ("ansible.builtin.include_role", "ansible.builtin.import_role", "include_role", "import_role")
TASK_INCLUDES = ("ansible.builtin.include_tasks", "ansible.builtin.import_tasks")
MACHINE = re.compile(r"^(?:bootwright\.core\.)?(substrate_\w+_machine)$")
DISPATCH = re.compile(r"\.(?:substrate|channel)\s*==")
BOOT = "bootwright.core.redfish_boot"
READ = "bootwright.core.redfish_system_read"

SUBSTRATES = {"libvirt": "substrate_libvirt_machine", "baremetal": "substrate_baremetal_machine"}
PORT = {"pre_boot", "boot_media", "boot_disk", "identity_read"}
LIFECYCLE = {"apply", "observe", "destroy"}
INSTALLATIONS = ("containercluster_install_agent", "managedos_install_anaconda")
MANAGED_OS = "managedos_install_anaconda"
CLUSTER = "containercluster_install_agent"

# Every include of the port, by consumer file, as each role's arm and the entry
# point, in file order. Finding fewer means the walk stopped seeing them.
COMPOSED = {
    "containercluster_install_agent/tasks/boot.yml": [
        ("libvirt", "pre_boot"), ("baremetal", "pre_boot"), ("libvirt", "boot_media"), ("baremetal", "boot_media")],
    "containercluster_install_agent/tasks/release.yml": [("libvirt", "boot_disk"), ("baremetal", "boot_disk")],
    "managedos_install_anaconda/tasks/await.yml": [("libvirt", "boot_disk"), ("baremetal", "boot_disk")],
    "managedos_install_anaconda/tasks/boot.yml": [
        ("libvirt", "pre_boot"), ("baremetal", "pre_boot"), ("libvirt", "boot_media"), ("baremetal", "boot_media")],
    "managedos_install_anaconda/tasks/identity.yml": [("libvirt", "identity_read"), ("baremetal", "identity_read")],
}

# Only these read the media a machine presents; every other read is a power read.
MEDIA_READS = {"containercluster_install_agent/tasks/state.yml", "managedos_install_anaconda/tasks/observe.yml"}
READS = MEDIA_READS | {
    "machine_power_read_redfish/tasks/read.yml",
    "machine_power_redfish/tasks/power.yml",
    "managedos_install_anaconda/tasks/await.yml",
    "substrate_libvirt_machine/tasks/apply.yml",
    "substrate_libvirt_machine/tasks/observe.yml",
    "substrate_libvirt_machine/tasks/pre_boot.yml",
}


def access(prefix, bundled=False):
    """The controller an entry point reaches: where, how it is verified, and the
    paths of the material files holding its account and, on an arm whose
    controller can declare one, its trust bundle, never their bytes."""
    found = {prefix + "_endpoint": ("str", True, None), prefix + "_verify": ("bool", True, None),
             prefix + "_user": ("str", True, None), prefix + "_password": ("str", True, None)}
    if bundled:
        found[prefix + "_ca"] = ("str", True, None)
    return found


def identity(prefix, required):
    found = {prefix + "_" + name: ("str", True, None) for name in required}
    found.update({prefix + "_retries": ("int", False, None), prefix + "_delay": ("int", False, None)})
    return found


LIBVIRT, BAREMETAL = "substrate_libvirt_machine", "substrate_baremetal_machine"
OPTIONS = {
    "libvirt": {
        "pre_boot": access(LIBVIRT + "_target"),
        "boot_media": dict(access(LIBVIRT + "_boot"), **{LIBVIRT + "_boot_installer_powers_off": ("bool", True, None)}),
        "boot_disk": dict(access(LIBVIRT + "_disk"), **{LIBVIRT + "_disk_power_on": ("bool", True, None)}),
        "identity_read": identity(LIBVIRT + "_identity", ("uri", "domain", "marker_path", "host_key_path")),
    },
    "baremetal": {
        "pre_boot": dict(access(BAREMETAL + "_target", bundled=True), **{
            BAREMETAL + "_target_expected": ("list", True, "str"),
            BAREMETAL + "_target_pinned_uuid_base64": ("str", True, None),
            BAREMETAL + "_target_pinned_serial_base64": ("str", True, None)}),
        "boot_media": access(BAREMETAL + "_boot", bundled=True),
        "boot_disk": dict(access(BAREMETAL + "_disk", bundled=True), **{BAREMETAL + "_disk_power_on": ("bool", True, None)}),
        "identity_read": identity(BAREMETAL + "_identity", ("address", "user", "file", "key", "marker_path", "work")),
    },
}

# What each consumer tells the port, by consumer file, arm and entry point: every
# boolean input but the controller's verification, which follows the request.
# Anaconda powers the machine off when it is done, so the libvirt arm selects
# the media for it (specs/managed-os.md, Installation), while the agent
# installer reboots into what it wrote, and a media selection the emulator keeps
# across that reboot would boot it back into the installer (specs/substrates.md,
# Identity and power operations). The installed disk the managed OS boots once
# the installer has powered off is powered on for the identity wait; a release
# powers nothing on (specs/container-clusters.md, Releasing the media).
ASKED = {
    ("containercluster_install_agent/tasks/boot.yml", "libvirt", "boot_media"): {
        LIBVIRT + "_boot_installer_powers_off": False},
    ("containercluster_install_agent/tasks/release.yml", "libvirt", "boot_disk"): {LIBVIRT + "_disk_power_on": False},
    ("containercluster_install_agent/tasks/release.yml", "baremetal", "boot_disk"): {
        BAREMETAL + "_disk_power_on": False},
    ("managedos_install_anaconda/tasks/await.yml", "libvirt", "boot_disk"): {LIBVIRT + "_disk_power_on": True},
    ("managedos_install_anaconda/tasks/await.yml", "baremetal", "boot_disk"): {BAREMETAL + "_disk_power_on": True},
    ("managedos_install_anaconda/tasks/boot.yml", "libvirt", "boot_media"): {
        LIBVIRT + "_boot_installer_powers_off": True},
}

ENDPOINTS = {
    "libvirt": "http://192.0.2.1:8000/redfish/v1/Systems/7b9ec716-85d4-8e28-84d3-f0d571d55f15",
    "baremetal": "https://metal-01-bmc.lab.example.test/redfish/v1/Systems/1",
}
INTERFACES = [{"macAddress": "aa:bb:cc:dd:ee:01", "name": "enp1s0"}]
HOST_KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGJvb3R3cmlnaHQtcG9ydC10ZXN0LWtleQ"
BUDGETS = {"identity": {"attempts": 120, "delaySeconds": 30}, "installer": {"attempts": 180, "delaySeconds": 20},
           "reachability": {"attempts": 30, "delaySeconds": 10}}
MATERIAL = "/run/bootwright/material/"
FAILED = {"changed": False, "failed": True, "msg": "the management controller could not be read: reading HTTP 503"}


def loaded(path):
    """A task file as a play loads it: its templates trusted."""
    return [task for task in LOADER.load_from_file(str(path), trusted_as_template=True) if isinstance(task, dict)]


def walk(tasks):
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)])


def label(path):
    return path.relative_to(ROLES).as_posix()


def task_files():
    return sorted(ROLES.glob("*/tasks/*.yml"))


def specs(arm):
    return LOADER.load_from_file(str(ROLES / SUBSTRATES[arm] / "meta" / "argument_specs.yml"))["argument_specs"]


def defaults(role):
    return dict(LOADER.load_from_file(str(ROLES / role / "defaults" / "main.yml"), trusted_as_template=True) or {})


def one(found, description):
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def composed():
    """Every include of a machine role in any role: its file, arm, tasks_from and task."""
    arms = {role: arm for arm, role in SUBSTRATES.items()}
    found = []
    for path in task_files():
        for task in walk(loaded(path)):
            for action in INCLUDES:
                arguments = task.get(action)
                match = MACHINE.match(str(arguments.get("name", ""))) if isinstance(arguments, dict) else None
                if match:
                    found.append((label(path), arms.get(match.group(1)), arguments.get("tasks_from"), task))
    return found


def managed_os(arm):
    """What a managed-OS installation of a Machine on one arm reads: the role's
    defaults and the request and material the runner writes."""
    target = {"controller": {"credentialsRef": "lab-bmc-credentials", "endpoint": ENDPOINTS[arm], "tlsVerify": True},
              "physical": arm == "baremetal", "substrate": arm}
    material = {"controllerUser": MATERIAL + "bmc-user", "controllerPassword": MATERIAL + "bmc-password",
                "fleetIdentity": MATERIAL + "fleet-id", "marker": "{}"}
    if arm == "libvirt":
        target.update({"channel": "guest-agent", "domain": "bootwright-lab-rhel-01", "uri": "qemu:///system"})
    else:
        target.update({"channel": "delivered-key", "hardware": {"interfaces": INTERFACES}})
        material.update({"hostKey": HOST_KEY, "pinnedSerialBase64": base64.b64encode(b"CZJ2440ABC").decode()})
    variables = defaults(MANAGED_OS)
    variables.update({
        "bootwright_os_install_material": material,
        "bootwright_os_install_request": {
            "address": "198.51.100.11", "budgets": BUDGETS, "hostKeyPath": "/etc/bootwright/host-key.pub",
            "identity": {"object": "rhel-01"}, "markerPath": "/etc/bootwright/install-marker.json",
            "target": target, "user": "bootwright"},
    })
    return variables


def cluster(arm, name):
    """What one cluster node reads in a consumer file apply.yml loops over: the
    role's defaults, the material the runner writes, the loop's own variables
    and the node it is on."""
    node = {"address": "198.51.100.21", "machine": "sno-01", "name": "master-0",
            "controller": {"credentialsRef": "lab-bmc-credentials", "endpoint": ENDPOINTS[arm], "tlsVerify": True},
            "physical": arm == "baremetal", "substrate": arm}
    material = {"controllerUserNode0": MATERIAL + "bmc-user-node0", "controllerPasswordNode0": MATERIAL + "bmc-password-node0"}
    if arm == "baremetal":
        node.update({"address": "198.51.100.41", "machine": "metal-01", "hardware": {"interfaces": INTERFACES}})
        material["pinnedUUIDBase64Node0"] = base64.b64encode(b"4c4c4544-0042-3510-8052-b4c04f4d4e31").decode()
    variables = defaults(CLUSTER)
    variables.update({"bootwright_cluster_install_material": material,
                      "bootwright_cluster_install_request": {"nodes": [node]}})
    variables.update(one([task for task in loaded(ROLES / CLUSTER / "tasks" / "apply.yml")
                          if task.get("ansible.builtin.include_tasks") == name], "loop over " + name).get("vars") or {})
    variables.update({"containercluster_install_agent_node": node, "containercluster_install_agent_index": 0})
    return variables


def scope(consumer, arm):
    role, name = consumer.split("/tasks/")
    return managed_os(arm) if role == MANAGED_OS else cluster(arm, name)


def operations(arm, entry):
    """Each controller effect one entry point performs: operation, target and task."""
    tasks = walk(loaded(ROLES / SUBSTRATES[arm] / "tasks" / (entry + ".yml")))
    return [(task[BOOT]["operation"], task[BOOT].get("target"), task) for task in tasks if BOOT in task]


def applies(task, variables):
    return "when" not in task or Templar(loader=LOADER, variables=variables).evaluate_conditional(task["when"])


def dispatches(tasks):
    """Whether a task file selects among substrates, by a comparison or a machine role."""
    for task in walk(tasks):
        conditions = task.get("when")
        conditions = conditions if isinstance(conditions, list) else [conditions]
        if any(DISPATCH.search(str(condition)) for condition in conditions if condition):
            return True
        if any(isinstance(task.get(action), dict) and MACHINE.match(str(task[action].get("name", "")))
               for action in INCLUDES):
            return True
    return False


# The exit evidence of B5: a consumer reaches a machine role only through an
# entry point, spelled exactly as its key, because that is the only spelling
# ansible-core validates.
def test_every_consumer_includes_a_machine_role_only_by_an_entry_point_key():
    problems, seen = [], {}
    for name, arm, entry, _task in composed():
        if arm is None or entry not in PORT or entry not in specs(arm):
            problems.append("%s includes a machine role at %r, which is no port entry point's key" % (name, entry))
        seen.setdefault(name, []).append((arm, entry))
    assert not problems, "\n".join(problems)
    assert seen == COMPOSED


def test_every_include_passes_exactly_its_entry_points_inputs():
    problems = []
    for name, arm, entry, task in composed():
        options = specs(arm)[entry]["options"]
        passed = set(task.get("vars") or {})
        required = {option for option, spec in options.items() if spec.get("required")}
        if passed - set(options):
            problems.append("%s passes %s, which %s does not declare" % (name, sorted(passed - set(options)), entry))
        if required - passed:
            problems.append("%s does not pass %s, which %s requires" % (name, sorted(required - passed), entry))
    assert not problems, "\n".join(problems)


@pytest.mark.parametrize("consumer", sorted(COMPOSED))
def test_what_each_include_renders_passes_its_entry_points_validation(consumer):
    checked = 0
    for name, arm, entry, task in composed():
        if name != consumer:
            continue
        variables = scope(consumer, arm)
        rendering = Templar(loader=LOADER, variables=variables)
        assert rendering.evaluate_conditional(task["when"]), "%s: the %s include does not apply to its own arm" % (
            name, arm)
        values = {option: rendering.template(value) for option, value in task["vars"].items()}
        options = specs(arm)[entry]["options"]
        result = ArgumentSpecValidator(options).validate(values, validate_role_argument_spec=True)
        assert not result.error_messages, "%s, %s %s: %s" % (name, arm, entry, result.error_messages)
        asked = {option: value for option, value in values.items()
                 if options[option]["type"] == "bool" and not option.endswith("_verify")}
        assert asked == ASKED.get((name, arm, entry), {}), "%s, %s %s asks %s" % (name, arm, entry, asked)
        checked += 1
    assert checked == len(COMPOSED[consumer])
    assert {key for key in ASKED if key[0] == consumer} <= {(consumer, arm, entry) for arm, entry in COMPOSED[consumer]}


@pytest.mark.parametrize("arm", sorted(SUBSTRATES))
def test_each_machine_role_declares_exactly_the_port_beside_its_lifecycle(arm):
    declared = specs(arm)
    assert set(declared) == PORT | LIFECYCLE
    for entry, spec in declared.items():
        assert (ROLES / SUBSTRATES[arm] / "tasks" / (entry + ".yml")).is_file(), entry
        assert spec.get("short_description"), entry
    for entry in sorted(PORT):
        found = {name: (option["type"], option.get("required", False), option.get("elements"))
                 for name, option in declared[entry]["options"].items()}
        assert found == OPTIONS[arm][entry], entry


# A failed validation echoes the value it refuses, and a required input a role
# default could satisfy would never be refused when a consumer forgot it.
@pytest.mark.parametrize("arm", sorted(SUBSTRATES))
def test_no_port_input_is_a_version_or_has_a_role_default(arm):
    role_defaults = set(defaults(SUBSTRATES[arm]))
    for entry in sorted(PORT):
        options = specs(arm)[entry]["options"]
        assert "version" not in options and not [name for name, option in options.items() if "options" in option]
        required = {name for name, option in options.items() if option.get("required")}
        assert not required & role_defaults, "%s: %s have a role default" % (entry, sorted(required & role_defaults))


def test_installations_only_insert_and_eject_and_nothing_reads_through_the_boot_module():
    problems, performed = [], set()
    for path in task_files():
        role = path.relative_to(ROLES).parts[0]
        for task in walk(loaded(path)):
            operation = (task.get(BOOT) or {}).get("operation")
            if operation == "read":
                problems.append("%s reads through a module that can drive the machine" % label(path))
            if role in INSTALLATIONS and BOOT in task:
                performed.add(operation)
                if operation not in ("insert", "eject"):
                    problems.append("%s performs %s itself rather than through the port" % (label(path), operation))
    assert not problems, "\n".join(problems)
    assert performed == {"insert", "eject"}


def test_only_the_state_reads_look_for_media():
    reads = {}
    for path in task_files():
        for task in walk(loaded(path)):
            if READ in task:
                reads.setdefault(label(path), []).append(task[READ].get("media"))
    assert set(reads) == READS
    assert all(isinstance(media, bool) for found in reads.values() for media in found), reads
    assert {name for name, found in reads.items() if True in found} == MEDIA_READS


def test_no_bare_metal_port_entry_point_powers_a_machine_off():
    for entry in sorted(PORT):
        for operation, _target, task in operations("baremetal", entry):
            assert operation not in ("power-off", "shutdown"), "%s: %r" % (entry, task.get("name"))


@pytest.mark.parametrize("powers_off", [True, False])
def test_the_boot_entry_points_boot_the_media_their_own_way(powers_off):
    steps = operations("libvirt", "boot_media")
    assert [(operation, target) for operation, target, _task in steps] == [
        ("power-off", None), ("boot", "Cd"), ("power-on", None)]
    variables = {LIBVIRT + "_boot_installer_powers_off": powers_off}
    assert [applies(task, variables) for _operation, _target, task in steps] == [True, powers_off, True]

    steps = operations("baremetal", "boot_media")
    assert [(operation, target) for operation, target, _task in steps] == [("boot", "Cd"), ("power-on", None)]
    assert all(applies(task, {}) for _operation, _target, task in steps)


@pytest.mark.parametrize("power_on", [True, False])
@pytest.mark.parametrize("arm", sorted(SUBSTRATES))
def test_the_disk_boot_selects_the_disk_and_powers_on_only_when_asked(arm, power_on):
    steps = operations(arm, "boot_disk")
    assert [(operation, target) for operation, target, _task in steps] == [("boot", "Hdd"), ("power-on", None)]
    variables = {SUBSTRATES[arm] + "_disk_power_on": power_on}
    assert [applies(task, variables) for _operation, _target, task in steps] == [True, power_on]


# A removal takes back only the media, so a destroy selects no boot device and
# reaches no substrate entry point.
def test_a_cluster_removal_dispatches_nothing():
    destroy = loaded(ROLES / CLUSTER / "tasks" / "destroy.yml")
    assert not dispatches(destroy)
    for task in walk(destroy):
        for action in TASK_INCLUDES:
            if action in task:
                name = task[action] if isinstance(task[action], str) else task[action]["file"]
                assert not dispatches(loaded(ROLES / CLUSTER / "tasks" / name)), name
    eject = one([task for task in walk(destroy) if BOOT in task], "drive a controller")
    assert eject[BOOT]["operation"] == "eject" and eject["no_log"] is True
    assert eject["loop"] == "{{ bootwright_cluster_install_request.nodes }}"


# A read the controller does not answer registers a failure without a power
# state; each poll reads it as not yet the answer it waits for, so it spends
# one attempt rather than ending the poll.
@pytest.mark.parametrize("path, register, awaited, pending", [
    ("managedos_install_anaconda/tasks/await.yml", "managedos_install_anaconda_installer", "Off", "On"),
    ("substrate_libvirt_machine/tasks/apply.yml", "substrate_libvirt_machine_controller", "On", ""),
])
def test_a_poll_spends_an_attempt_on_a_read_that_failed(path, register, awaited, pending):
    poll = one([task for task in walk(loaded(ROLES / path)) if task.get("register") == register], "poll " + register)
    assert READ in poll and poll[READ]["media"] is False
    for result, expected in ((FAILED, False), ({"power": pending}, False), ({"power": awaited}, True)):
        rendering = Templar(loader=LOADER, variables={register: dict(result, changed=False)})
        assert rendering.evaluate_conditional(poll["until"]) is expected, result
