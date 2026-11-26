#!/usr/bin/python
"""Blank one kept virtual machine's root disk, or read whether it was."""

from __future__ import annotations

DOCUMENTATION = r"""
module: libvirt_root_reset
short_description: Blank one kept virtual machine's root disk for a staged destroy
version_added: "0.1.0"
description:
  - Creates the root disk again, empty, at the frozen path and size, the way
    the apply created it, and then writes a mark beside it naming the staged
    destroy and the step whose removal blanked it.
  - Touches only this context's own domain, and only while the hypervisor
    reports it shut off; the domain, its MACs, UUID, controller and data disks
    stay as they are.
  - A root disk already carrying this mark at its frozen size is left as it
    is, so a repeated reset changes nothing.
  - With reset false it only reads the domain, the root disk and the mark,
    and so runs in check mode; a reset refuses to run in check mode.
options:
  request:
    description: The frozen machine request.
    type: dict
    required: true
  removal:
    description: The staged destroy's operation identity.
    type: str
    required: true
  step:
    description: The block whose removal blanks the root disk.
    type: str
    required: true
  reset:
    description: Whether to blank the root disk rather than only read it.
    type: bool
    default: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Blank the kept machine's root disk
  bootwright.core.libvirt_root_reset:
    request: '{{ bootwright_substrate_machine_request }}'
    removal: op-00000000000000000000000000000000
    step: cluster-install-edge
    reset: true
"""

RETURN = r"""
observation:
  description:
    - The domain identity and state, the root disk and the mark beside it.
  returned: always
  type: dict
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.libvirt_root_reset import Refused, mark_of, observe, reset
from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import command_runner


def main():
    module = AnsibleModule(
        argument_spec={"request": {"type": "dict", "required": True}, "removal": {"type": "str", "required": True},
                       "step": {"type": "str", "required": True}, "reset": {"type": "bool", "default": False}},
        supports_check_mode=True,
    )
    params = module.params
    if params["reset"] and module.check_mode:
        module.fail_json(msg="a root disk reset does not run in check mode")
        return
    try:
        mark_of(params["removal"], params["step"])
        if params["reset"]:
            observation, changed = reset(command_runner(), params["request"], params["removal"], params["step"])
        else:
            observation, changed = observe(command_runner(), params["request"]), False
    except Refused as refusal:
        module.fail_json(msg=str(refusal))
        return
    except (OSError, ValueError, KeyError) as failure:
        module.fail_json(msg="the root disk could not be reset: %s" % type(failure).__name__)
        return
    module.exit_json(changed=changed, observation=observation)


if __name__ == "__main__":
    main()
