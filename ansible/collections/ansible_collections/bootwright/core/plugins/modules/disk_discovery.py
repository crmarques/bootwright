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
  returned: success
  type: dict
discovery_failure:
  description: Static allowlisted failure code, never exception text or native output.
  returned: failure
  type: str
discovery_phase:
  description: Static execution phase at failure.
  returned: failure
  type: str
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_controller as custody


def diagnostic(error, controller):
    """Never publish exception text, request values, subprocess output or paths."""
    refusals = {
        "discovery request shape": "request",
        "discovery binding": "binding",
        "discovery controller TLS": "bmc-tls",
        "discovery private media TLS": "media-tls",
        "discovery controller identity": "hardware-identity",
        "missing discovery custody has unresolved external ownership": "external-ownership",
        "missing discovery custody has unresolved publication": "publication-ownership",
        "discovery target is already running": "target-running",
        "discovery target power is unknown": "power-unknown",
        "foreign virtual media": "foreign-media",
        "discovery source image identity": "media-identity",
        "discovery source image digest": "media-digest",
        "discovery publication size": "publication-size",
        "RHEL initrd lacks discovery bootstrap tools": "initrd-tools",
        "unqualified discovery initrd layout": "initrd-layout",
        "discovery report deadline": "inventory-timeout",
        "custody-directory": "custody-directory",
        "custody-mode": "custody-mode",
        "custody-file": "custody-file",
    }
    code = "contract"
    if isinstance(error, custody.media.CommandFailure):
        code = "native-" + error.tool
    elif isinstance(error, custody.redfish_control.UnverifiedCertificate):
        code = "bmc-tls"
    elif isinstance(error, custody.redfish_control.ControllerError):
        code = "bmc-request"
    elif isinstance(error, FileNotFoundError):
        code = "missing-file"
    elif isinstance(error, OSError):
        code = "filesystem"
    elif isinstance(error, bounded_process.Failure):
        code = "native-process"
    elif isinstance(error, ValueError):
        code = refusals.get(str(error), "contract")
    phase = getattr(controller, "phase", "custody")
    if phase not in ("custody", "controller", "media", "tls", "media-delivery", "boot", "inventory", "acknowledgement", "cleanup"):
        phase = "custody"
    return {"discovery_failure": code, "discovery_phase": phase}


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
    except FileNotFoundError as error:
        if module.params["operation"] == "observe":
            module.exit_json(changed=False, evidence={"version": custody.VERSION, "request": module.params["digest"], "completed": False, "cleaned": False})
        module.fail_json(msg="disk discovery custody is incomplete; continue the original operation", **diagnostic(error, controller))
    except (OSError, ValueError, KeyError, TypeError, custody.redfish_control.ControllerError, bounded_process.Failure) as error:
        module.fail_json(
            msg="disk discovery did not prove its bounded media, hardware identity, inventory and cleanup contract",
            **diagnostic(error, controller),
        )
    finally:
        if controller is not None:
            controller.close()


if __name__ == "__main__":
    main()
