#!/usr/bin/python
# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)
"""Manage one authorized offline erase's private custody and receipt."""

from __future__ import annotations

DOCUMENTATION = r"""
module: physical_erase
short_description: Hold offline erase custody and verify its RAM worker receipt
version_added: "0.1.0"
description:
  - Operates on the frozen physical erase request through bounded phases.
  - Never opens a physical disk on the execution host.
options:
  phase:
    description: Fixed lifecycle phase.
    type: str
    required: true
    choices: [prepare, build, start, await, ack, stop-proof, finish, observe]
  request:
    description: Frozen physical erase request.
    type: dict
    required: true
  digest:
    description: Frozen request digest.
    type: str
    required: true
  material:
    description: Private invocation paths and original identity.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Read the durable erase receipt
  bootwright.core.physical_erase:
    phase: observe
    request: '{{ bootwright_physical_erase_request }}'
    digest: '{{ bootwright_physical_erase_digest }}'
    material: '{{ bootwright_physical_erase_material }}'
  no_log: true
"""
RETURN = r"""
evidence:
  description: Bounded erase evidence.
  returned: when observed or finished
  type: dict
"""

import json
from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_controller as custody
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker


def main():
    module = AnsibleModule(
        argument_spec={
            "phase": {"type": "str", "required": True, "choices": ["prepare", "build", "start", "await", "ack", "stop-proof", "finish", "observe"]},
            "request": {"type": "dict", "required": True},
            "digest": {"type": "str", "required": True},
            "material": {"type": "dict", "required": True, "no_log": True},
        },
        supports_check_mode=False,
    )
    controller = None
    try:
        phase = module.params["phase"]
        controller = custody.Controller(module.params["request"], module.params["digest"], module.params["material"], create=phase == "prepare")
        if phase == "prepare":
            module.exit_json(changed=True, **controller.prepare())
        if phase == "build":
            controller.build()
            module.exit_json(changed=True)
        if phase in ("start", "stop-proof"):
            controller.control(phase)
            module.exit_json(changed=True)
        if phase == "await":
            module.exit_json(changed=True, receipt=controller.await_receipt())
        if phase == "ack":
            module.exit_json(changed=controller.acknowledge())
        receipt = controller.receipt()
        evidence = {"version": worker.VERSION, "request": module.params["digest"], "prepared": True, "removed": False}
        if receipt:
            evidence["receipt"] = receipt
        path = controller.root / "completed.json"
        finalizing = controller.root / "finalizing.json"
        if phase == "finish":
            if not receipt:
                raise worker.Refusal("receipt-missing")
            evidence["removed"] = True
            controller.finish(evidence)
        elif path.exists():
            stored = json.loads(custody.read(path))
            evidence["removed"] = True
            if stored != evidence:
                raise worker.Refusal("completion-drift")
        elif finalizing.exists():
            stored = json.loads(custody.read(finalizing))
            if stored != dict(evidence, removed=True):
                raise worker.Refusal("completion-drift")
        module.exit_json(changed=phase == "finish", evidence=evidence, finalizing=finalizing.exists())
    except FileNotFoundError:
        if module.params["phase"] == "observe":
            module.exit_json(
                changed=False, evidence={"version": worker.VERSION, "request": module.params["digest"], "prepared": False, "removed": False}, finalizing=False
            )
        module.fail_json(msg="physical erase custody is incomplete")
    except worker.Refusal as refusal:
        if str(refusal) == "worker-failed":
            module.fail_json(
                msg=(
                    "the RAM erase worker stopped without a complete receipt; power off this Machine "
                    "and continue the same destroy to repeat the full frozen overwrite"
                )
            )
        module.fail_json(msg="physical erase did not prove its bounded custody or receipt contract")
    except (OSError, ValueError, KeyError, custody.redfish.redfish_control.ControllerError):
        module.fail_json(msg="physical erase did not prove its bounded custody or receipt contract")
    finally:
        if controller is not None:
            controller.close()


if __name__ == "__main__":
    main()
