"""Parse a guest's network convergence state and name its next step.

The convergence reads one bounded key=value state from the guest and acts on
exactly one step of the interruption matrix. Both filters answer every input:
a read they cannot parse is invalid, and an invalid state is unreadable.
"""

from __future__ import annotations

DOCUMENTATION = r"""
name: managedos_network_state
short_description: Parse a guest's network convergence state
version_added: "0.1.0"
description:
  - Parses the key=value read of a guest's network convergence state.
  - A missing, repeated or unknown key, or a boot id that is not one, gives an
    invalid state whose reason names the key; every key is still present.
  - The companion filter managedos_network_step names the one next step,
    inspect, reboot, wait, rollback, apply or unreadable, of a state for a
    request digest; anything but a valid state is unreadable.
  - Neither filter raises.
options:
  _input:
    description: The state read's standard output.
    type: str
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Name the next convergence step
  ansible.builtin.set_fact:
    network_step: "{{ network_read.stdout | bootwright.core.managedos_network_state | bootwright.core.managedos_network_step(request_digest) }}"
"""

RETURN = r"""
_value:
  description: The parsed state, with listed a list or none.
  type: dict
"""

from ansible_collections.bootwright.core.plugins.module_utils.managedos_network import (
    guest_state,
    neutral_state,
    next_step,
)


def managedos_network_state(stdout):
    """The guest's convergence state, invalid for anything unparsable."""
    try:
        return guest_state(stdout)
    except (TypeError, ValueError, AttributeError, KeyError, RecursionError):
        return neutral_state("the network state read could not be parsed")


def managedos_network_step(state, digest=""):
    """The next convergence step of a state, unreadable for anything but a valid state."""
    if not isinstance(state, dict):
        return "unreadable"
    try:
        return next_step(state, digest)
    except (TypeError, ValueError, AttributeError, KeyError):
        return "unreadable"


class FilterModule:
    """The filters this collection offers its roles."""

    def filters(self):
        return {
            "managedos_network_state": managedos_network_state,
            "managedos_network_step": managedos_network_step,
        }
