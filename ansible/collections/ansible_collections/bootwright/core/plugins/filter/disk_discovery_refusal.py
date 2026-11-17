"""Name why disk discovery stopped, from the code and phase its result carries.

The descriptions live beside the codes, in module_utils/disk_discovery_runtime,
and only that text is printed: a code or phase the result carries is a key,
never part of the message.
"""

from __future__ import annotations

DOCUMENTATION = r"""
name: disk_discovery_refusal
short_description: Name why disk discovery stopped
version_added: "0.1.0"
description:
  - Maps the C(discovery_failure) code and C(discovery_phase) of a registered
    disk discovery result to the operator diagnostic the runtime holds for them.
  - A result with no code, or a code or phase that is not text, is reported as
    a result the adapter did not bound; an unknown code is reported as a failure
    with no description. Neither prints what the result carried.
options:
  _input:
    description: The result the disk discovery task registered.
    type: raw
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Stop at an unproved disk discovery operation
  ansible.builtin.fail:
    msg: "{{ managedos_disk_discovery_result | bootwright.core.disk_discovery_refusal }}"
"""

RETURN = r"""
_value:
  description: The operator diagnostic, naming only text the collection holds.
  type: str
"""

from ansible_collections.bootwright.core.plugins.module_utils.disk_discovery_runtime import described


def disk_discovery_refusal(result):
    """The operator diagnostic of one registered discovery result."""
    return described(result)


class FilterModule:
    """The filters this collection offers its roles."""

    def filters(self):
        return {"disk_discovery_refusal": disk_discovery_refusal}
