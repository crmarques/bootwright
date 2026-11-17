"""Every realized arm exposes the same bounded machine operations."""
from pathlib import Path

import pytest
import yaml

from ansible_collections.bootwright.core.plugins.filter.machine_port_state import machine_port_state
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER
from ansible_collections.bootwright.core.tests.unit import role_tasks

ROOT = Path(__file__).parents[2]


@pytest.mark.parametrize("arm", ["libvirt", "baremetal"])
@pytest.mark.parametrize("entry", ["state_read", "media_insert", "media_eject", "power"])
def test_machine_port_contract(arm, entry):
    role = "substrate_" + arm + "_machine"
    directory = ROOT / "roles" / role
    options = yaml.safe_load((directory / "meta/argument_specs.yml").read_text())["argument_specs"][entry]["options"]
    request = options["bootwright_machine_port"]
    assert request["required"]
    for credential in ("user", "password", "ca"):
        assert request["options"][credential]["type"] == "path"
    tasks = yaml.safe_load((directory / "tasks" / (entry + ".yml")).read_text())
    assert any(task.get("register") == role + "_port_result" or role + "_port_result" in task.get("ansible.builtin.set_fact", {}) for task in tasks)
    assert any(role + "_port_refusal" in task.get("ansible.builtin.set_fact", {}) for task in tasks)
    for task in tasks:
        if any(key.startswith("bootwright.core.redfish_") for key in task):
            assert task["no_log"] is True
            assert task["failed_when"] is False


@pytest.mark.parametrize("arm", ["libvirt", "baremetal"])
@pytest.mark.parametrize("enable", [False, True])
def test_media_insert_admits_and_forwards_verification_setup(arm, enable):
    from ansible.module_utils.common.arg_spec import ArgumentSpecValidator
    from ansible.template import Templar, trust_as_template

    role = ROOT / "roles" / ("substrate_" + arm + "_machine")
    spec = yaml.safe_load((role / "meta/argument_specs.yml").read_text())["argument_specs"]["media_insert"]["options"]
    port = {"endpoint": "https://bmc.example.test/redfish/v1/Systems/1", "user": "/run/user", "password": "/run/password",
            "verify": True, "private": True, "image_url_file": "/run/image-url", "trust": "established", "enable_verification": enable}
    assert not ArgumentSpecValidator(spec).validate({"bootwright_machine_port": port}).error_messages
    translated = yaml.safe_load((ROOT / "roles/substrate_machine/tasks/redfish_media_insert.yml").read_text())
    native = next(task["bootwright.core.redfish_boot"] for task in translated if "bootwright.core.redfish_boot" in task)
    templar = Templar(loader=LOADER, variables={"bootwright_machine_port": port})
    assert templar.template(trust_as_template(native["enable_verification"])) is enable


@pytest.mark.parametrize("result,expected", [
    ({"power": "Off", "media": ""}, {"power_state": "off", "media_state": "none"}),
    ({"power": "On", "media": "https://MEDIA.test/image?token=redacted"}, {"power_state": "on", "media_state": "own"}),
    ({"power": "On", "media": "https://media.test/other"}, {"power_state": "on", "media_state": "foreign"}),
    ({"msg": "unreachable"}, {"power_state": "unknown", "media_state": "unknown"}),
    ({"power": "Off"}, {"power_state": "off", "media_state": "unknown"}),
])
def test_machine_port_normalizes_unknown_without_proving_absence(result, expected):
    assert machine_port_state(result, "https://media.test/image") == expected


def test_power_only_read_never_proves_media_absent():
    assert machine_port_state({"power": "Off", "media": ""}, media=False)["media_state"] == "unknown"


@pytest.mark.parametrize("answered,marker,foreign", [
    (False, "other", False), (True, "", False),
    (True, "this-install", False), (True, "other", True),
])
def test_only_a_positive_foreign_marker_names_a_refusal(answered, marker, foreign):
    from ansible.template import Templar, trust_as_template

    tasks = role_tasks.flatten(yaml.safe_load((ROOT / "roles/managedos_install_anaconda/tasks/apply.yml").read_text()))
    classified = next(task for task in tasks if "managedos_install_anaconda_foreign" in task.get("ansible.builtin.set_fact", {}))
    templar = Templar(loader=LOADER, variables={
        "managedos_install_anaconda_answered": answered,
        "managedos_install_anaconda_marker": marker,
        "bootwright_os_install_material": {"marker": "this-install"},
    })
    assert templar.template(trust_as_template(classified["ansible.builtin.set_fact"]["managedos_install_anaconda_foreign"])) is foreign
    assert classified["no_log"] is True
    refusal = next(task for task in tasks if task.get("bootwright.core.managedos_install_protocol", {}).get("reason") == "foreign-installation")
    assert refusal["no_log"] is True
    assert refusal["when"] == "managedos_install_anaconda_foreign | bool"


def test_every_realized_role_publishes_all_eight_machine_entrypoints():
    entries = {"pre_boot", "boot_media", "boot_disk", "identity_read", "state_read", "media_insert", "media_eject", "power"}
    for arm in ("libvirt", "baremetal"):
        role = ROOT / "roles" / ("substrate_" + arm + "_machine")
        specs = yaml.safe_load((role / "meta/argument_specs.yml").read_text())["argument_specs"]
        assert entries <= specs.keys()
        assert all((role / "tasks" / (entry + ".yml")).is_file() for entry in entries)


def test_machine_consumers_compose_substrate_ports_instead_of_native_modules():
    import re
    for role in ("managedos_install_anaconda", "containercluster_install_agent", "machine_power", "machine_power_read"):
        tasks = ROOT / "roles" / role / "tasks"
        assert tasks.is_dir() and any(tasks.glob("*.yml")), role
        for path in tasks.glob("*.yml"):
            source = path.read_text()
            assert "bootwright.core.redfish_" not in source, str(path)
            assert not re.search(r"(?:not )?in \[['\"]libvirt['\"], ['\"]baremetal['\"]\]", source), str(path)


@pytest.mark.parametrize("arm", ["libvirt", "baremetal"])
@pytest.mark.parametrize("entry,extra", [
    ("pre_boot", {"expected": ["02:00:00:00:00:01"], "pinned_uuid_base64": "", "pinned_serial_base64": "", "require_tpm": False}),
    ("pre_boot", {"expected": ["02:00:00:00:00:01"], "pinned_uuid_base64": "", "pinned_serial_base64": "", "require_tpm": False,
                  "firmware": {"mode": "prove", "secureBoot": True, "bootMode": "UEFI",
                               "biosAttributes": [{"name": "TpmEnable", "boolean": True}]}}),
    ("boot_media", {"installer_powers_off": False}),
    ("boot_disk", {"power_on": True}),
])
def test_generic_boot_port_validates_native_aliases_before_effects(arm, entry, extra):
    from ansible.module_utils.common.arg_spec import ArgumentSpecValidator
    from ansible.template import Templar

    role = ROOT / "roles" / ("substrate_" + arm + "_machine")
    loader = LOADER
    defaults = loader.load_from_file(str(role / "defaults/main.yml"), trusted_as_template=True)
    spec = yaml.safe_load((role / "meta/argument_specs.yml").read_text())["argument_specs"][entry]["options"]
    request = {"endpoint": "https://bmc.example.test/redfish/v1/Systems/1", "user": "/run/user", "password": "/run/password",
               "ca": "", "verify": True, **extra}
    scope = Templar(loader=loader, variables={**defaults, "bootwright_machine_port": request})
    supplied = {"bootwright_machine_port": request}
    supplied.update({name: scope.template(defaults[name]) for name in spec if name in defaults})
    assert not ArgumentSpecValidator(spec).validate(supplied).error_messages


ACCESS_KEYS = ("'endpoint'", "'user'", "'password'", "'ca'", "'verify'", "'verification'", "'pinned_uuid_base64'",
               "'pinned_serial_base64'", "tlsVerification", "controllerUser", "controllerPassword", "controllerCA")
RESOLVED = ("bootwright.core.machine_port_access(", "managedos_install_anaconda_port_access", "machine_power_access")


def port_assignments(value, found):
    if isinstance(value, dict):
        for key, item in value.items():
            if key == "bootwright_machine_port" or str(key).endswith("_port_access") or key == "machine_power_access":
                found.append(str(item))
            port_assignments(item, found)
    elif isinstance(value, list):
        for item in value:
            port_assignments(item, found)
    return found


# Go freezes one machinePort object per target and the machine_port_access
# filter is the one place it is resolved: a consumer adds only its operation's
# arguments and never builds the access dictionary itself.
def test_no_role_or_playbook_builds_a_machine_port_access_dictionary():
    files = sorted(ROOT.glob("roles/*/tasks/*.yml")) + sorted(ROOT.glob("roles/*/defaults/*.yml")) + sorted(ROOT.glob("playbooks/**/*.yml"))
    seen = 0
    for path in files:
        for assigned in port_assignments(yaml.safe_load(path.read_text()), []):
            seen += 1
            where = path.relative_to(ROOT).as_posix()
            assert any(resolved in assigned for resolved in RESOLVED), (where, assigned)
            assert not any(key in assigned for key in ACCESS_KEYS), (where, assigned)
    assert seen >= 20
