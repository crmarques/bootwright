"""The refusing routes are checked against the host before anything is defined and removed last."""

from pathlib import Path

import yaml

ROLE = Path(__file__).resolve().parents[2] / "roles/substrate_libvirt_host"
MODULE = "bootwright.core.libvirt_vlan_refusal"


def flattened(tasks):
    for task in tasks:
        if "block" in task:
            yield from flattened(task["block"])
        else:
            yield task


def tasks(entry):
    return list(flattened(yaml.safe_load((ROLE / "tasks" / (entry + ".yml")).read_text())))


def argv(task):
    return task.get("ansible.builtin.command", {}).get("argv", [])


def effect(task):
    return "ansible.builtin.template" in task or (argv(task)[:1] == ["/usr/bin/virsh"] and argv(task)[-1:] != ["version"])


def test_apply_checks_the_host_routes_after_its_collision_guards_and_before_any_definition():
    apply = tasks("apply")
    hold = next(i for i, task in enumerate(apply) if MODULE in task)
    guard = next(i for i, task in enumerate(apply) if "collisions | length == 0" in str(task.get("ansible.builtin.assert")))
    first_effect = next(i for i, task in enumerate(apply) if effect(task))
    assert guard < hold < first_effect
    assert apply[hold][MODULE] == {"request": "{{ bootwright_substrate_host_request }}"}
    assert "state" not in apply[hold][MODULE]
    assert "when" not in apply[hold]
    outcome = next(task for task in apply if "outcome" in task.get("bootwright.core.substrate_host_protocol", {}))
    assert "substrate_libvirt_host_vlan_refusal is changed" in outcome["bootwright.core.substrate_host_protocol"]["outcome"]


def test_destroy_refuses_a_foreign_unit_before_its_first_effect_and_removes_the_routes_after_the_networks():
    destroy = tasks("destroy")
    refuse = next(i for i, task in enumerate(destroy) if "vlanRefusal.owned" in str(task.get("ansible.builtin.assert")))
    first_effect = next(i for i, task in enumerate(destroy) if effect(task))
    undefine = max(i for i, task in enumerate(destroy) if "net-undefine" in argv(task))
    remove = next(i for i, task in enumerate(destroy) if MODULE in task)
    verify = next(i for i, task in enumerate(destroy) if task.get("register") == "substrate_libvirt_host_after")
    assert refuse < first_effect < undefine < remove < verify
    assert destroy[remove][MODULE]["state"] == "absent"
    outcome = next(task for task in destroy if "outcome" in task.get("bootwright.core.substrate_host_protocol", {}))
    assert "substrate_libvirt_host_vlan_refusal is changed" in outcome["bootwright.core.substrate_host_protocol"]["outcome"]


def test_observation_reports_the_refusal_unit_and_routes():
    from ansible_collections.bootwright.core.plugins.module_utils import substrate_libvirt

    source = Path(substrate_libvirt.__file__).read_text()
    assert '"vlanRefusal": observe_vlan_refusal(runner, request)' in source
