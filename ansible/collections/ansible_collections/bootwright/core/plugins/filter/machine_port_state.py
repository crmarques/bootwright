"""Normalize a fetched machine state without treating an unknown read as absence."""
DOCUMENTATION = r"""
name: machine_port_state
short_description: Normalize the machine port power and media observation
version_added: "0.1.0"
description:
  - Unknown native reads never prove media absence.
options:
  _input:
    description: The native machine port result.
    type: dict
    required: true
  expected_url:
    description: The expected media address, compared without its query.
    type: str
    default: ''
  media:
    description: Whether the read requested media discovery.
    type: bool
    default: true
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
# {{ result | bootwright.core.machine_port_state(expected_url) }}
"""
RETURN = r"""
_value:
  description: Normalized power_state and media_state.
  type: dict
"""

from urllib.parse import urlsplit


def machine_port_state(result, expected_url="", media=True):
    power = {"On": "on", "Off": "off"}.get(result.get("power"), "unknown")
    presented = result.get("media")
    state = "unknown"
    if not result.get("msg") and media and isinstance(presented, str):
        if not presented:
            state = "none"
        elif expected_url:
            try:
                actual, expected = urlsplit(presented), urlsplit(expected_url)
                state = "own" if (actual.scheme.lower(), actual.netloc.lower(), actual.path) == (
                    expected.scheme.lower(), expected.netloc.lower(), expected.path) else "foreign"
            except ValueError:
                state = "unknown"
        else:
            state = "foreign"
    return {"power_state": "unknown" if result.get("msg") else power, "media_state": state}


class FilterModule:
    def filters(self):
        return {"machine_port_state": machine_port_state}
