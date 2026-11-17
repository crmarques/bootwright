# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)
"""Controller-side wait for the offline erase receipt."""

from __future__ import annotations

DOCUMENTATION = r"""
module: physical_erase_await
short_description: Wait for the offline erase receipt and report each target's progress
version_added: "0.1.0"
description:
  - Polls the RAM erase worker through the physical_erase module's poll phase until its complete receipt, a refusal or a bound.
  - Reports each target's written and read-back bytes, and a worker no session reaches, as running progress groups.
  - Progress is presentation only and is never completion evidence.
options:
  request:
    description: Frozen physical erase request.
    type: dict
    required: true
  digest:
    description: Frozen request digest.
    type: str
    required: true
  material:
    description: Private invocation paths and original identity.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Wait for every target byte to be overwritten and independently read back
  bootwright.core.physical_erase_await:
    request: '{{ bootwright_physical_erase_request }}'
    digest: '{{ bootwright_physical_erase_digest }}'
    material: '{{ bootwright_physical_erase_material }}'
  no_log: true
"""
RETURN = r"""
receipt:
  description: The complete receipt, once recorded.
  returned: when complete
  type: dict
refused:
  description: The closed code of a refused wait.
  returned: when the wait refused
  type: str
detail:
  description: The bounded value the worker session reported for the refusal, or empty.
  returned: when the wait refused
  type: str
controller:
  description: Always empty for a wait, which reaches no management controller.
  returned: when the wait refused
  type: str
"""
