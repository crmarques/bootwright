"""Edit the native AgentClusterInstall using its structured disk encryption field."""
from __future__ import annotations

from ansible.errors import AnsibleFilterError

DOCUMENTATION = r"""
name: agent_disk_encryption
short_description: Set TPM2 encryption in a generated AgentClusterInstall
version_added: "0.1.0"
description:
  - Validates the generated object's identity before adding encryption intent.
options:
  _input:
    description: The installer-generated AgentClusterInstall object.
    type: dict
    required: true
  encryption:
    description: Frozen enableOn and mode fields.
    type: dict
    required: true
  cluster:
    description: Expected cluster name.
    type: str
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Edit the generated manifest
  ansible.builtin.set_fact:
    manifest: '{{ manifest | bootwright.core.agent_disk_encryption(encryption, cluster) }}'
"""

RETURN = r"""
_value:
  description: The generated object with disk encryption selected.
  type: dict
"""


def agent_disk_encryption(manifest, encryption, cluster):
    """Preserve every generated field and refuse another native object."""
    if (not isinstance(manifest, dict) or manifest.get("apiVersion") != "extensions.hive.openshift.io/v1beta1"
            or manifest.get("kind") != "AgentClusterInstall" or manifest.get("metadata", {}).get("name") != cluster
            or not isinstance(manifest.get("spec"), dict)):
        raise AnsibleFilterError("generated AgentClusterInstall has an unexpected identity or shape")
    if (encryption.get("enableOn") not in ("all", "masters", "workers")
            or encryption.get("mode") != "tpmv2" or set(encryption) != {"enableOn", "mode"}):
        raise AnsibleFilterError("unsupported agent disk encryption selection")
    result = dict(manifest)
    result["spec"] = dict(manifest["spec"], diskEncryption=dict(encryption))
    return result


class FilterModule:
    """The structured native manifest edit."""

    def filters(self):
        return {"agent_disk_encryption": agent_disk_encryption}
