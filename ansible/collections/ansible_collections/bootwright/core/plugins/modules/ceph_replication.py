#!/usr/bin/python
"""Realize one RBD replication relationship between its frozen clusters through isolated controller SSH."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_replication
short_description: Realize one RBD replication relationship between its frozen clusters
version_added: "0.1.0"
description:
  - Admits the completed replication block request, both cluster scopes and the private material files its operation binds.
  - Applies or observes both owned endpoints through isolated controller SSH; bootstrap tokens remain in memory and worker stdin.
options:
  request:
    description: Completed Ceph replication request with both proved cluster scopes and resolved Ceph images.
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
- name: Observe one Ceph replication resource
  bootwright.core.ceph_replication:
    request: '{{ bootwright_ceph_replication_request }}'
    operation: observe
    digest: '{{ bootwright_ceph_replication_digest }}'
    material: '{{ bootwright_ceph_replication_material }}'
"""

RETURN = r"""
evidence:
  description: Bounded replication resource observations without credentials.
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
from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_replication, ceph_replication_native, native_diagnostics


def admitted(params):
    """Validate both completed scopes before opening runner-owned private files."""
    request = ceph_replication_native.validate(params["request"])
    ceph_native.require(ceph_native.digest(params["digest"]) and params["operation"] in ("apply", "observe"))
    material = params["material"]
    ceph_native.require(isinstance(material, dict) and set(material) == {"primarySSHKey", "secondarySSHKey", "knownHosts"}, "material")
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
        transports = {}
        for side in ("primary", "secondary"):
            material = {"sshKey": params["material"][side + "SSHKey"], "knownHosts": params["material"]["knownHosts"]}
            transports[side] = ceph_core.Transport(request[side]["scope"], material, scope=True)
        result = ceph_replication.execute(request, params["operation"], params["digest"], transports)
    except Exception as error:
        reason, detail = ceph_core.refusal(error, ceph_replication.REASONS)
        failure = {"reason": reason}
        native_error = getattr(error, "native_error", None)
        if native_diagnostics.valid(native_error):
            failure["nativeError"] = native_error
        if detail:
            failure["detail"] = detail
        module.fail_json(msg="Ceph replication operation refused", **failure)
    module.exit_json(**result)


if __name__ == "__main__":
    main()
