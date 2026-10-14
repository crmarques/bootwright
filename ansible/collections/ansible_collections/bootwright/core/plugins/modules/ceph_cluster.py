#!/usr/bin/python
"""Execute a frozen Ceph core operation through isolated controller SSH."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_cluster
short_description: Execute one frozen Ceph core operation
version_added: "0.1.0"
description:
  - Uses strict host-key verification and a fixed privileged native worker.
  - Only qualified core services and concrete proved devices are accepted.
  - Observation never claims ownership or changes native configuration.
options:
  request:
    description: Frozen Ceph core request with durable runtime identity.
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
  outputs:
    description:
      - Runner-owned private produced-material output paths.
      - C(client) receives a client action's generated CephX identity on apply and on a ready observe.
      - C(dashboard) receives a cluster action's dashboard administrator password on apply and on a ready observe.
    type: dict
    default: {}
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the frozen Ceph cluster
  bootwright.core.ceph_cluster:
    request: '{{ bootwright_ceph_request }}'
    operation: observe
    digest: '{{ bootwright_ceph_digest }}'
    material: '{{ bootwright_ceph_material }}'
"""

RETURN = r"""
evidence:
  description: Bounded Ceph identity and readiness observations without credentials.
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
            "outputs": {"type": "dict", "default": {}, "no_log": True},
        },
        supports_check_mode=False,
    )
    params = module.params
    try:
        transport = ceph_core.Transport(params["request"], params["material"])
        result = ceph_core.execute(params["request"], params["operation"], params["digest"], transport, params["outputs"])
    except Exception as error:
        reason, detail = ceph_core.refusal(error, ceph_core.CLUSTER_REASONS)
        failure = {"reason": reason}
        if detail:
            failure["detail"] = detail
        module.fail_json(msg="Ceph operation refused", **failure)
    module.exit_json(**result)


if __name__ == "__main__":
    main()
