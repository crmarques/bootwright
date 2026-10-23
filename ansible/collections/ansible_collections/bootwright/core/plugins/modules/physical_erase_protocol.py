# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)
"""Controller-side physical erase result publication."""

from __future__ import annotations

DOCUMENTATION = r"""
module: physical_erase_protocol
short_description: Publish offline erase lifecycle evidence
version_added: "0.1.0"
description:
  - Publishes bounded evidence and acknowledges the controller receipt before shutdown.
options:
  phase:
    description: Result phase.
    type: str
    required: true
    choices: [loaded, prepared, completed]
  digest:
    description: Frozen request digest.
    type: str
  evidence:
    description: Bounded completion evidence.
    type: dict
  receipt:
    description: Complete readback receipt to record durably.
    type: dict
  observed:
    description: Whether this publication is read-only.
    type: bool
    default: false
  outcome:
    description: Reported mutation outcome.
    type: str
    choices: [changed, unchanged]
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Complete the physical erase handoff
  bootwright.core.physical_erase_protocol:
    phase: loaded
"""
RETURN = r"""
changed:
  description: Always false; publication performs no host effect.
  returned: always
  type: bool
"""
