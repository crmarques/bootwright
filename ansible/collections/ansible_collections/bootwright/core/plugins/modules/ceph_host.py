#!/usr/bin/python
"""Prepare one Ceph host for its frozen cluster through isolated controller SSH."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_host
short_description: Prepare one Ceph host for its frozen cluster
version_added: "0.1.0"
description:
  - Installs the signed packages, pulls the frozen image digest and proves its runtime, route and device identities.
  - Uses strict host-key verification and the fixed privileged native workers.
  - Observation never pulls, installs or changes host state; release retains the shared packages and image.
options:
  request:
    description: Frozen Ceph host request with its cluster request and durable runtime identity.
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
- name: Observe one Ceph host preparation
  bootwright.core.ceph_host:
    request: '{{ bootwright_ceph_host_request }}'
    operation: observe
    digest: '{{ bootwright_ceph_host_digest }}'
    material: '{{ bootwright_ceph_host_material }}'
"""

RETURN = r"""
evidence:
  description: Bounded host preparation observations without credentials.
  type: dict
  returned: success
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
from ansible_collections.bootwright.core.plugins.module_utils import ceph_core


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
        request = ceph_core.host_request(params["request"])
        transport = ceph_core.Transport(request["cluster"], params["material"])
        result = ceph_core.execute_host(request, params["operation"], params["digest"], transport)
    except Exception as error:
        reason, detail = ceph_core.refusal(error, ceph_core.HOST_REASONS)
        failure = {"reason": reason}
        if detail:
            failure["detail"] = detail
        module.fail_json(msg="Ceph host preparation refused", **failure)
    module.exit_json(**result)


if __name__ == "__main__":
    main()
