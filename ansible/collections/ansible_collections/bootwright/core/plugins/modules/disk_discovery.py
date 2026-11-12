#!/usr/bin/python
# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)
"""Reach and prove one frozen RAM disk inventory without writing data disks."""

from __future__ import annotations

DOCUMENTATION = r"""
module: disk_discovery
short_description: Hold private RAM disk inventory custody and authenticated reports
version_added: "0.1.0"
description:
  - Executes the fixed procedure of one frozen discovery request.
  - Never starts an installer or opens a data disk for writing.
options:
  operation:
    description: Selected lifecycle operation.
    type: str
    required: true
    choices:
      - apply
      - observe
      - destroy
  request:
    description: Frozen typed discovery request.
    type: dict
    required: true
  digest:
    description: Frozen request digest.
    type: str
    required: true
  material:
    description: Private invocation paths and durable attempt binding.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Read the retained discovery receipt
  bootwright.core.disk_discovery:
    operation: observe
    request: '{{ bootwright_disk_discovery_request }}'
    digest: '{{ bootwright_disk_discovery_digest }}'
    material: '{{ bootwright_disk_discovery_material }}'
  no_log: true
"""
RETURN = r"""
evidence:
  description: Bounded inventory and cleanup proof, containing no credential material.
  returned: always
  type: dict
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_controller as custody


def main():
    module = AnsibleModule(
        argument_spec={
            "operation": {"type": "str", "required": True, "choices": ["apply", "observe", "destroy"]},
            "request": {"type": "dict", "required": True},
            "digest": {"type": "str", "required": True},
            "material": {"type": "dict", "required": True, "no_log": True},
        },
        supports_check_mode=False,
    )
    controller = None
    try:
        operation = module.params["operation"]
        controller = custody.Controller(module.params["request"], module.params["digest"], module.params["material"], create=operation in ("apply", "destroy"))
        evidence = {"apply": controller.apply, "observe": controller.evidence, "destroy": controller.destroy}[operation]()
        module.exit_json(changed=operation != "observe", evidence=evidence)
    except FileNotFoundError:
        if module.params["operation"] == "observe":
            module.exit_json(changed=False, evidence={"version": custody.VERSION, "request": module.params["digest"], "completed": False, "cleaned": False})
        module.fail_json(msg="disk discovery custody is incomplete; continue the original operation")
    except (OSError, ValueError, KeyError, TypeError, custody.redfish_control.ControllerError, bounded_process.Failure):
        module.fail_json(msg="disk discovery did not prove its bounded media, hardware identity, inventory and cleanup contract")
    finally:
        if controller is not None:
            controller.close()


if __name__ == "__main__":
    main()
