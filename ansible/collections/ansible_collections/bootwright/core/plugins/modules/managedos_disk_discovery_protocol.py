# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)
"""Controller-side disk discovery result publication."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_disk_discovery_protocol
short_description: Publish RAM inventory lifecycle evidence
version_added: "0.1.0"
description:
  - Publishes bounded evidence and acknowledges the controller receipt before shutdown.
options:
  phase:
    description: Result phase.
    type: str
    required: true
    choices:
      - loaded
      - group
      - completed
  group:
    description: Fixed presentation group identity.
    type: str
  status:
    description: Presentation group status.
    type: str
    choices:
      - running
      - ok
      - failed
      - skipped
  digest:
    description: Frozen request digest.
    type: str
  evidence:
    description: Bounded completion evidence.
    type: dict
  observed:
    description: Whether this publication is read-only.
    type: bool
    default: false
  outcome:
    description: Reported mutation outcome.
    type: str
    choices:
      - changed
      - unchanged
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Complete the disk discovery handoff
  bootwright.core.managedos_disk_discovery_protocol:
    phase: loaded
"""
RETURN = r"""
extends_documentation_fragment: bootwright.core.protocol
"""
