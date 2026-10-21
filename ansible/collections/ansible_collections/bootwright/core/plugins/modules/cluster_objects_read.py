#!/usr/bin/python
"""Bounded native cluster object read transport."""
from __future__ import annotations

DOCUMENTATION = r"""
module: cluster_objects_read
short_description: Run the qualified cluster object read transport
version_added: "0.1.0"
description:
  - Uses the located oc executable and the lent kubeconfig with projected output.
options:
  request:
    description: The bounded transport request, supplied by the paired action.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Run the shared object transport
  bootwright.core.cluster_objects_read:
    request: "{{ cluster_objects_transport }}"
  no_log: true
"""
RETURN = r"""
changed:
  description: Whether a native write completed.
  returned: always
  type: bool
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_native as native


def main():
    module = AnsibleModule(argument_spec={"request": {"type": "dict", "required": True}},
                           no_log=True, supports_check_mode=True)
    request = module.params["request"]
    try:
        result = native.get(request)
    except (ValueError, TypeError, KeyError, OSError):
        module.fail_json(msg="cluster object transport refused its request", invocation={})
        return
    module.exit_json(invocation={}, changed=False, **result)


if __name__ == "__main__":
    main()
