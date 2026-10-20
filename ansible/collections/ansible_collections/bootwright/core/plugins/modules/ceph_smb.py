#!/usr/bin/python
"""Realize one Ceph SMB manager resource of its frozen cluster through isolated controller SSH."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_smb
short_description: Realize one Ceph SMB manager resource of its frozen cluster
version_added: "0.1.0"
description:
  - Admits the completed SMB block request, its cluster scope and the private material files its operation binds.
  - Applies or observes one SMB manager resource on the cluster's bootstrap host through isolated controller SSH; passwords reach the host
    only on the worker's stdin and are read back masked.
options:
  request:
    description: Completed Ceph SMB request with its cluster scope and resolved samba images.
    type: dict
    required: true
  operation:
    description: The fixed lifecycle operation.
    type: str
    choices: [apply, observe, destroy]
    required: true
  digest:
    description: Digest of the frozen lifecycle request.
    type: str
    required: true
  material:
    description: Runner-owned private material file paths.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe one Ceph SMB resource
  bootwright.core.ceph_smb:
    request: '{{ bootwright_ceph_smb_request }}'
    operation: observe
    digest: '{{ bootwright_ceph_smb_digest }}'
    material: '{{ bootwright_ceph_smb_material }}'
"""

RETURN = r"""
evidence:
  description: Bounded SMB resource observations without credentials.
  type: dict
  returned: success
nativeError:
  description: Bounded redacted native cause and selected public coordinate.
  type: dict
  returned: failure
reason:
  description: Fixed refusal the block's caller names, without native output or credentials.
  type: str
  returned: failure
detail:
  description: Non-secret cause, a refusal name or an exception type and code site.
  type: str
  returned: failure
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, native_diagnostics


def admitted(params):
    """The request, digest and material names an operation binds, each checked before any material file is read."""
    request = ceph_core.smb_request(params["request"])
    ceph_native.require(ceph_native.digest(params["digest"]))
    expected = {"sshKey", "knownHosts"}
    if params["operation"] == "apply":
        expected |= {"smbSecret" + str(index) for index in range(len(request["bindings"]))}
    material = params["material"]
    ceph_native.require(isinstance(material, dict) and set(material) == expected, "material")
    return request


def main():
    module = AnsibleModule(
        argument_spec={
            "request": {"type": "dict", "required": True},
            "operation": {"type": "str", "required": True, "choices": ["apply", "observe", "destroy"]},
            "digest": {"type": "str", "required": True},
            "material": {"type": "dict", "required": True, "no_log": True},
        },
        supports_check_mode=False,
    )
    params = module.params
    try:
        request = admitted(params)
        secrets = len(request["bindings"]) if params["operation"] == "apply" else 0
        transport = ceph_core.Transport(request["scope"], params["material"], scope=True, smb_secrets=secrets)
        result = ceph_core.execute_smb(request, params["operation"], params["digest"], transport)
    except Exception as error:
        reason, detail = ceph_core.refusal(error, ceph_core.SMB_REASONS)
        failure = {"reason": reason}
        native_error = getattr(error, "native_error", None)
        if native_diagnostics.valid(native_error):
            failure["nativeError"] = native_error
        if detail:
            failure["detail"] = detail
        module.fail_json(msg="Ceph SMB operation refused", **failure)
    module.exit_json(**result)


if __name__ == "__main__":
    main()
