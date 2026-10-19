#!/usr/bin/python
"""Resolve the authored IBM Ceph tag or digest to its linux/amd64 manifest digest."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_image_resolve
short_description: Resolve the authored IBM Ceph image manifest-only
version_added: "0.1.0"
description:
  - Resolves the authored tag or digest with a bounded origin-confined OCI client.
  - The request may carry the auxiliary image set; each member resolves to its own linux/amd64 manifest digest at the same registry authority.
  - Downloads no image layers, starts no container and retains nothing.
  - Observation and removal read nothing remote and report the resolution absent.
options:
  operation:
    description: Fixed lifecycle operation.
    type: str
    choices: [apply, observe, destroy]
    required: true
  request:
    description: Frozen image resolution request, with the auxiliary image set when the cluster selects one.
    type: dict
    required: true
  digest:
    description: Digest of the frozen lifecycle request.
    type: str
    required: true
  material:
    description: Protected registry JSON file loan, for apply only.
    type: dict
    default: {}
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Resolve the authored provider image
  bootwright.core.ceph_image_resolve:
    operation: apply
    request: '{{ bootwright_ceph_image_request }}'
    digest: '{{ bootwright_ceph_image_digest }}'
    material: '{{ bootwright_ceph_image_material }}'
  no_log: true
"""

RETURN = r"""
evidence:
  description: The resolved immutable reference, or the resolution's absence.
  type: dict
  returned: success
nativeError:
  description: Bounded redacted native cause and selected public coordinate.
  type: dict
  returned: failure
reason:
  description: Fixed refusal without native output or credentials.
  type: str
  returned: failure
detail:
  description: Non-secret cause, a refusal name or an exception type and code site.
  type: str
  returned: failure
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import native_diagnostics
from ansible_collections.bootwright.core.plugins.module_utils import ceph_registry_discovery as discovery


def main():
    module = AnsibleModule(argument_spec={
        "operation": {"type": "str", "required": True, "choices": ["apply", "observe", "destroy"]},
        "request": {"type": "dict", "required": True},
        "digest": {"type": "str", "required": True},
        "material": {"type": "dict", "default": {}, "no_log": True},
    }, supports_check_mode=False)
    params = module.params
    try:
        result = discovery.execute(params["operation"], params["request"], params["digest"], params["material"])
    except Exception as error:
        reason, detail = discovery.refusal(error)
        failure = {"reason": reason}
        native_error = getattr(error, "native_error", None)
        if native_diagnostics.valid(native_error):
            failure["nativeError"] = native_error
        if detail:
            failure["detail"] = detail
        module.fail_json(msg="Ceph image resolution refused", **failure)
    module.exit_json(**result)


if __name__ == "__main__":
    main()
