"""A live power reading carries each pinned machine's own identity to its port.

A KubeVirt port re-reads the host's kube-system UID and the VirtualMachine's
UID before it answers, and refuses with no pin (specs/substrates.md, KubeVirt
machine realization), so a reading reports a KubeVirt Machine's power only
when its target carries the pin its machine block recorded. The power service
names, per target, the material values that hold that pin base64-encoded; the
read renders exactly those values into the port's pinned arguments, which the
action decodes strictly, so the rendered value must be the encoded pin alone.
A refusal, a mismatch among them, leaves that one machine unknown.
"""

from __future__ import annotations

import base64
import pathlib

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar
from ansible_collections.bootwright.core.plugins.action import machine_power_protocol

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()

CLUSTER = "9a1f2c3d-0000-4000-8000-00000000c1a5"
VM = "6b2e4f80-0000-4000-8000-0000000000aa"


def encoded(value):
    return base64.b64encode(value.encode()).decode()


def port(target, material):
    """The port arguments read_one.yml renders for one target."""
    loaded = LOADER.load_from_file(str(ROLES / "machine_power_read" / "tasks" / "read_one.yml"), trusted_as_template=True)
    arguments = loaded[0]["vars"]["bootwright_machine_port"]
    variables = {"machine_power_read_target": target, "bootwright_machine_power_read_material": material}
    rendering = Templar(loader=LOADER, variables=variables)
    return {key: rendering.template(arguments[key]) for key in ("user", "pinned_uuid_base64", "pinned_serial_base64")}


def target(index, pinned=True):
    found = {"arm": "kubevirt", "object": "node-%d" % index, "userVariable": "controllerUser%d" % index,
             "controller": {"credentialsRef": "", "endpoint": "kubevirt:nested/node-%d-0000000%d" % (index, index),
                            "hostCluster": "host", "tlsVerify": True}}
    if pinned:
        found["pinnedUUIDVariable"] = "pinnedUUIDBase64%d" % index
        found["pinnedSerialVariable"] = "pinnedSerialBase64%d" % index
    return found


MATERIAL = {
    "controllerUser0": "/run/lent-0", "controllerUser1": "/run/lent-1",
    "pinnedUUIDBase640": encoded(CLUSTER), "pinnedSerialBase640": encoded(VM),
    "pinnedUUIDBase641": encoded(CLUSTER), "pinnedSerialBase641": encoded("another-vm-uid"),
}


def test_each_target_reads_with_its_own_pin():
    first, second = port(target(0), MATERIAL), port(target(1), MATERIAL)
    assert first == {"user": "/run/lent-0", "pinned_uuid_base64": encoded(CLUSTER), "pinned_serial_base64": encoded(VM)}
    assert second["pinned_serial_base64"] == encoded("another-vm-uid") and second["user"] == "/run/lent-1"
    for value in (first["pinned_uuid_base64"], first["pinned_serial_base64"]):
        assert base64.b64decode(value, validate=True)


def test_a_target_with_no_recorded_pin_reads_with_none():
    assert port(target(0, pinned=False), MATERIAL)["pinned_uuid_base64"] == ""
    assert port(target(0, pinned=False), MATERIAL)["pinned_serial_base64"] == ""


def test_a_refused_kubevirt_read_is_unknown_never_a_state():
    loaded = LOADER.load_from_file(str(ROLES / "substrate_kubevirt_machine" / "tasks" / "state_read.yml"), trusted_as_template=True)
    normalize = [task for task in loaded if task.get("name") == "Normalize an unanswered read as unknown"]
    assert len(normalize) == 1
    expression = normalize[0]["ansible.builtin.set_fact"]["substrate_kubevirt_machine_port_result"]
    refused = {"failed": True, "msg": "identity-mismatch", "refusal": "identity-mismatch", "power": "On", "power_state": "on"}
    rendered = Templar(loader=LOADER, variables={"substrate_kubevirt_machine_port_result": refused}).template(expression)
    assert rendered["power_state"] == "unknown"
    evidence = machine_power_protocol.reading_evidence_for({"digest": "d" * 64, "readings": [dict(rendered, item=target(0))]})
    assert evidence["machines"] == [{"machine": "node-0", "power": "unknown"}]
