"""Every Redfish call a role makes reaches its controller through the declared trust.

A physical controller may declare a caBundle Secret as the one anchor of its
transport (specs/api/machines.md, BMC and root-device shape). The capability hands the
bundle to the adapter as a material file, and each Redfish module takes its
PEM as ca_data. A task that forgot ca_data would verify that controller against
the system trust store instead, which fails for an internal authority and is
exactly what the declaration exists to avoid, so these cases walk every role
that reaches a physical controller and require every Redfish task, and every
include of the bare-metal port, to pass it. The walk asserts how many tasks it
checked, so one that stops seeing them fails rather than passing empty.

Both installation consumers carry frozen virtual-media trust through the
substrate port; physical nodes require verified private delivery (B67).

Rendering needs Ansible's controller (DataLoader and Templar), which
ansible-test does not offer to unit tests under tests/unit/plugins, so these
checks live here.
"""

from __future__ import annotations

import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.plugins.loader import init_plugin_loader
from ansible.template import Templar
from ansible.utils.collection_loader import AnsibleCollectionConfig

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()

MODULES = ("bootwright.core.redfish_boot", "bootwright.core.redfish_system_read", "bootwright.core.redfish_system_inspect")
BOOT = "bootwright.core.redfish_boot"
INCLUDES = ("ansible.builtin.include_role", "ansible.builtin.import_role", "include_role", "import_role")
BAREMETAL = "bootwright.core.substrate_baremetal_machine"
PORT_BUNDLE = {"pre_boot": "substrate_baremetal_machine_target_ca", "boot_media": "substrate_baremetal_machine_boot_ca",
               "boot_disk": "substrate_baremetal_machine_disk_ca"}

# The roles that reach a controller a bundle can anchor, and how many Redfish
# module tasks each holds. Finding another number means the walk or a role
# changed, and either must be looked at.
BUNDLED = {"substrate_baremetal_machine": 13, "substrate_libvirt_machine": 14}
# The roles that make Redfish calls without a bundle, and why.
UNBUNDLED = {
    "containercluster_media_agent": "it makes no Redfish call",
}
CLUSTER_CALLS = 5


def loaded(path):
    """A task file as a play loads it: its templates trusted."""
    return [task for task in LOADER.load_from_file(str(path), trusted_as_template=True) if isinstance(task, dict)]


def walk(tasks):
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)])


def calls(role):
    """Every Redfish module task in one role: its file, its module and its arguments."""
    found = []
    for path in sorted((ROLES / role / "tasks").glob("*.yml")):
        for task in walk(loaded(path)):
            for module in MODULES:
                if module in task:
                    found.append((path.name, module, task[module], task))
    return found


def spec(role):
    return LOADER.load_from_file(str(ROLES / role / "meta" / "argument_specs.yml"))["argument_specs"]


def defaults(role):
    return dict(LOADER.load_from_file(str(ROLES / role / "defaults" / "main.yml"), trusted_as_template=True) or {})


@pytest.fixture(name="lookups")
def lookups_fixture():
    """The plugin loader a rendered lookup needs. ansible-test configures the
    collection loader first, and configuring it again only warns."""
    if AnsibleCollectionConfig.collection_finder is None:
        init_plugin_loader()


def test_every_role_that_makes_a_redfish_call_is_accounted_for():
    making = {role.name for role in ROLES.iterdir() if (role / "tasks").is_dir() and calls(role.name)}
    assert making == set(BUNDLED) | (set(UNBUNDLED) - {"containercluster_media_agent"})
    assert not calls("containercluster_media_agent")


@pytest.mark.parametrize("role", sorted(BUNDLED))
def test_every_redfish_call_passes_the_controller_bundle(role):
    found = calls(role)
    missing = ["%s: %s" % (name, task.get("name")) for name, _module, arguments, task in found if "ca_data" not in arguments]
    assert not missing, "tasks reaching a controller without its bundle:\n" + "\n".join(missing)
    assert len(found) == BUNDLED[role]


# Each lifecycle role renders the bundle's PEM from the material file the
# capability wrote, and nothing when the controller declares no bundle.
@pytest.mark.parametrize("role, material, variable", [
    ("managedos_install_anaconda", "bootwright_os_install_material", "managedos_install_anaconda_ca_data"),
    ("substrate_baremetal_machine", "bootwright_substrate_physical_material", "substrate_baremetal_machine_ca_data"),
    ("machine_power", "bootwright_machine_power_material", "machine_power_ca_data"),
])
def test_each_lifecycle_role_reads_the_bundle_it_was_handed(lookups, tmp_path, role, material, variable):
    bundle = tmp_path / "bmc-ca"
    bundle.write_text("-----BEGIN CERTIFICATE-----\nQ0E=\n-----END CERTIFICATE-----\n")
    pem = "-----BEGIN CERTIFICATE-----\nQ0E=\n-----END CERTIFICATE-----"
    for handed, expected in (({"controllerCA": str(bundle)}, pem), ({}, "")):
        variables = dict(defaults(role), **{material: handed})
        assert Templar(loader=LOADER, variables=variables).template(variables[variable]) == expected
    for name, _module, arguments, _task in calls(role):
        port = PORT_BUNDLE.get(name[:-len(".yml")])
        generic = "bootwright_machine_port.ca" in str(arguments["ca_data"])
        assert generic or (port or variable) in str(arguments["ca_data"]), (name, arguments["ca_data"])
        for path, expected in ((str(bundle), pem), ("", "")):
            if generic or port:
                scope = {"bootwright_machine_port": {"ca": path}} if generic else {port: path}
                assert Templar(loader=LOADER, variables=scope).template(arguments["ca_data"]) == expected


# Each target of a reading hands its machine port its own bundle's path under
# its own variable, and a target that declares none hands it nothing.
def test_each_read_target_reads_its_own_bundle(lookups, tmp_path):
    [port] = [task["vars"]["bootwright_machine_port"] for task in walk(loaded(ROLES / "machine_power_read" / "tasks" / "read_one.yml"))
              if "ansible.builtin.include_role" in task]
    first, second = tmp_path / "bmc-ca-0", tmp_path / "bmc-ca-1"
    material = {"controllerCA0": str(first), "controllerCA2": str(second)}
    for target, expected in (({"caVariable": "controllerCA0"}, str(first)), ({"caVariable": "controllerCA2"}, str(second)),
                             ({}, "")):
        variables = {"bootwright_machine_power_read_material": material, "machine_power_read_target": target}
        assert Templar(loader=LOADER, variables=variables).template(port["ca"]) == expected


# Every include of the bare-metal port hands it the path of the controller's
# bundle material, because the entry point reads a path and never a default.
def test_every_bare_metal_include_passes_its_bundle():
    seen = []
    for path in sorted(ROLES.glob("*/tasks/*.yml")):
        for task in walk(loaded(path)):
            for action in INCLUDES:
                arguments = task.get(action)
                if isinstance(arguments, dict) and arguments.get("tasks_from") in PORT_BUNDLE:
                    entry = arguments.get("tasks_from")
                    if entry in PORT_BUNDLE:
                        seen.append((path.relative_to(ROLES).as_posix(), entry))
                        assert "ca" in task["vars"]["bootwright_machine_port"], (path.name, entry)
    assert sorted(seen) == sorted([
        ("containercluster_install_agent/tasks/boot.yml", "boot_media"),
        ("containercluster_install_agent/tasks/boot.yml", "pre_boot"),
        ("containercluster_install_agent/tasks/release.yml", "boot_disk"),
        ("managedos_install_anaconda/tasks/await.yml", "boot_disk"),
        ("managedos_install_anaconda/tasks/boot.yml", "boot_media"),
        ("managedos_install_anaconda/tasks/boot.yml", "pre_boot"),
        ("managedos_install_anaconda/tasks/network.yml", "boot_disk"),
        ("managedos_install_anaconda/tasks/proved_boot.yml", "boot_disk"),
    ])


def test_the_argument_specs_admit_the_frozen_bundle():
    controllers = {
        "substrate_baremetal_machine": spec("substrate_baremetal_machine")["apply"]["options"][
            "bootwright_substrate_physical_request"]["options"]["controller"],
        "machine_power": spec("machine_power")["power"]["options"][
            "bootwright_machine_power_request"]["options"]["controller"],
    }
    targets = spec("machine_power_read")["read"]["options"]["bootwright_machine_power_read_request"][
        "options"]["targets"]
    controllers["machine_power_read"] = targets["options"]["controller"]
    for role, controller in controllers.items():
        assert controller["options"]["trustBundleRef"] == {"type": "str", "required": False}, role
    assert targets["options"]["caVariable"] == {"type": "str", "required": False}
    for entry, option in PORT_BUNDLE.items():
        assert spec("substrate_baremetal_machine")[entry]["options"][option] == {"type": "str", "required": True}


# The managed-OS insert sets the virtual-media trust the request froze, with
# the server's certificate, and every eject after the installer's power-off
# settles it; nothing else a managed-OS installation runs touches it.
def port_calls(role):
    found = []
    for path in sorted((ROLES / role / "tasks").glob("*.yml")):
        for task in walk(loaded(path)):
            include = task.get("ansible.builtin.include_role", {})
            if include.get("tasks_from") in ("media_insert", "media_eject"):
                found.append((path.name, include["tasks_from"], task["vars"]["bootwright_machine_port"]))
    return found


def test_the_managed_os_installation_sets_and_settles_the_frozen_trust(lookups, tmp_path):
    driven = port_calls("managedos_install_anaconda")
    inserts = [arguments for _name, entry, arguments in driven if entry == "media_insert"]
    ejects = [(name, arguments) for name, entry, arguments in driven if entry == "media_eject"]
    assert len(inserts) == 1 and sorted(name for name, _arguments in ejects) == ["apply.yml", "await.yml"]
    served = tmp_path / "artifact-ca"
    served.write_text("SERVED CERTIFICATE\n")
    for trust, restore, remove, material, certificate in (
            ("import-certificate", False, True, {"artifactCertificate": str(served)}, "SERVED CERTIFICATE"),
            ("disable-verification", True, False, {}, ""),
            ("established", False, False, {}, "")):
        variables = dict(defaults("managedos_install_anaconda"), **{
            "bootwright_run_scratch": "/run/private",
            "bootwright_os_install_material": {"controllerUser": "/run/user", "controllerPassword": "/run/password", **material},
            "bootwright_os_install_request": {"target": {"physical": True, "controller": {
                "endpoint": "https://bmc.example.test/redfish/v1/Systems/1", "tlsVerify": True, "virtualMedia": {
                    "trust": trust, "restoreVerification": restore, "removeCertificate": remove}}}},
        })
        rendering = Templar(loader=LOADER, variables=variables)
        inserted = rendering.template(inserts[0])
        assert inserted["trust"] == trust
        for name, request in [("boot.yml", inserts[0]), *ejects]:
            port = rendering.template(request)
            entry = "media_insert" if name == "boot.yml" else "media_eject"
            if entry == "media_eject":
                assert "trust" not in port
                assert port["restore_verification"] is restore
                assert port["remove_certificate"] is remove
            native = next(args for filename, module, args, _task in calls("substrate_baremetal_machine")
                          if filename == entry + ".yml" and module == BOOT)
            assert Templar(loader=LOADER, variables={"bootwright_machine_port": port}).template(native["certificate"]) == certificate


@pytest.mark.parametrize("physical", [False, True])
def test_cluster_media_calls_carry_each_nodes_frozen_trust(physical):
    tasks = loaded(ROLES / "containercluster_install_agent/tasks/port.yml")
    invocation = next(task for task in tasks if "ansible.builtin.include_role" in task)
    variables = {**invocation["vars"],
                 "containercluster_install_agent_node": {"physical": physical, "controller": {
                     "endpoint": "https://bmc.example.test/redfish/v1/Systems/1", "tlsVerify": True,
                     "virtualMedia": {"trust": "import-certificate", "restoreVerification": False, "removeCertificate": True}}},
                 "containercluster_install_agent_user": "/run/user", "containercluster_install_agent_password": "/run/password",
                 "containercluster_install_agent_ca": "/run/ca", "bootwright_run_scratch": "/run/private",
                 "containercluster_install_agent_index": 2,
                 "bootwright_cluster_install_material": {"artifactCertificate": "/run/certificate"}}
    for operation in ("state_read", "media_insert", "media_eject"):
        rendering = Templar(loader=LOADER, variables={**variables, "containercluster_install_agent_port_operation": operation})
        port = rendering.template(invocation["vars"]["bootwright_machine_port"])
        assert port["ca"] == "/run/ca"
        if operation == "media_insert":
            assert port["private"] is physical
            assert port["trust"] == "import-certificate" and port["certificate"] == "/run/certificate"
        elif operation == "media_eject":
            assert port["remove_certificate"] is True and port["restore_verification"] is False
            assert port["certificate"] == "/run/certificate"
