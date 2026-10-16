"""The readiness barrier precedes network starts and survives until cleanup."""

from pathlib import Path

import pytest
import yaml

from ansible_collections.bootwright.core.plugins.action import substrate_host_protocol as protocol

ROLE = Path(__file__).resolve().parents[2] / "roles/substrate_libvirt_host"
READY = "bootwright.core.libvirt_firewall_ready"


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


def test_readiness_follows_ownership_guards_and_precedes_every_network_start():
    apply = tasks("apply")
    prepare = next(i for i, task in enumerate(apply) if READY in task)
    foreign = next(i for i, task in enumerate(apply) if task["name"] == "Refuse a same-named network this context does not own")
    assert foreign < prepare
    start = next(i for i, task in enumerate(apply) if "net-start" in argv(task))
    fresh = apply[start - 1]
    assert argv(fresh) == ["/usr/bin/timeout", "15", "/usr/bin/firewall-cmd", "--state"]
    assert "timeout" not in fresh and fresh["retries"] == 30 and fresh["delay"] == 2
    assert fresh["changed_when"] is False
    assert prepare < start
    # The explicit zone remains necessary: libvirt otherwise overrides the
    # bridge to its default zone on network start and firewall reload.
    assert 'zone="trusted"' in (ROLE / "templates/network.xml.j2").read_text()


def test_destroy_keeps_barrier_until_resources_are_removed_and_proves_foreign_ownership_first():
    destroy = tasks("destroy")
    guard = next(i for i, task in enumerate(destroy) if "unknown firewall readiness" in task["name"])
    stop = next(i for i, task in enumerate(destroy) if "net-destroy" in argv(task))
    remove = next(i for i, task in enumerate(destroy) if READY in task)
    pool = next(i for i, task in enumerate(destroy) if "pool-undefine" in argv(task))
    assert guard < stop < pool < remove
    assert destroy[remove][READY]["state"] == "absent"


@pytest.mark.parametrize("field,value", [("answered", False), ("present", True), ("owned", True), ("enabled", True), ("ready", True), ("state", "inactive")])
def test_absence_requires_each_readiness_resource_proved_gone(field, value):
    ready = {"name": "bootwright-lab-host-firewalld-ready.service", "answered": True, "present": False,
             "owned": False, "enabled": False, "ready": False, "state": ""}
    assert protocol.firewall_gone({"firewallReady": ready})
    assert not protocol.firewall_gone({"firewallReady": dict(ready, **{field: value})})


def test_missing_or_malformed_readiness_proof_refuses():
    for observation in ({}, {"firewallReady": {}}, {"firewallReady": {"answered": "yes"}}):
        with pytest.raises(ValueError):
            protocol.firewall_ready(observation)
