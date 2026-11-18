"""A provider host's removal proves each network it stops idle first.

Stopping a libvirt network disconnects every domain plugged into it, whatever
context or operator defined it, so the removal refuses while one runs there, or
while the hypervisor does not answer which do, before its first effect.
"""

from __future__ import annotations

import pathlib

import yaml

DESTROY = pathlib.Path(__file__).resolve().parents[2] / "roles" / "substrate_libvirt_host" / "tasks" / "destroy.yml"


def tasks():
    return [task for task in yaml.safe_load(DESTROY.read_text()) if isinstance(task, dict)]


def test_every_network_is_proved_idle_before_the_first_effect():
    listed = tasks()
    guard = next(index for index, task in enumerate(listed) if task.get("name") == "Refuse to remove a network nothing proves idle")
    effects = [index for index, task in enumerate(listed)
               if "bootwright.core.libvirt_nm_connections" in task or "net-destroy" in (task.get("ansible.builtin.command") or {}).get("argv", [])]
    assert effects and guard < min(effects)
    check = listed[guard]["ansible.builtin.assert"]
    assert check["that"] == ["item.guestsAnswered", "item.guests | length == 0"]
    assert listed[guard]["loop"] == "{{ substrate_libvirt_host_before.observation.networkGuests }}"
