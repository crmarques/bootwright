#!/usr/bin/python
"""Observe one realized virtual machine and its controller, changing nothing."""

from __future__ import annotations

DOCUMENTATION = r"""
module: libvirt_domain_inspect
short_description: Observe one Bootwright virtual machine and its controller
version_added: "0.1.0"
description:
  - Reports whether the domain is defined, whether it carries this context's
    ownership, the size of every owned disk, and the controller unit and image.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen machine request.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the realized machine
  bootwright.core.libvirt_domain_inspect:
    request: '{{ bootwright_substrate_machine_request }}'
"""

RETURN = r"""
observation:
  description: The owned domain, disks and controller with their observed state.
  returned: always
  type: dict
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import observe_machine


def main():
    module = AnsibleModule(
        argument_spec={"request": {"type": "dict", "required": True}},
        supports_check_mode=True,
    )
    try:
        observation = observe_machine(module.run_command, module.params["request"])
    except (OSError, ValueError, KeyError) as failure:
        module.fail_json(msg="the machine could not be observed: %s" % type(failure).__name__)
        return
    module.exit_json(changed=False, observation=observation)


if __name__ == "__main__":
    main()
