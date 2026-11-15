#!/usr/bin/python
"""Documentation entrypoint; the paired action owns publication."""

# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)

from __future__ import annotations

DOCUMENTATION = r"""
module: storage_ceph_admin_protocol
short_description: Publish bounded administrator credential proof
version_added: "0.1.0"
description:
  - Publishes no credential bytes.
options:
  phase:
    description: The result publication phase.
    type: str
    required: true
  digest:
    description: The frozen request digest.
    type: str
  outcome:
    description: The bounded operation outcome.
    type: str
  proved:
    description: Whether the native operation proved its result.
    type: bool
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Complete the qualified execution handoff
  bootwright.core.storage_ceph_admin_protocol:
    phase: loaded
"""
RETURN = r"""
changed:
  description: Always false.
  type: bool
  returned: always
"""
