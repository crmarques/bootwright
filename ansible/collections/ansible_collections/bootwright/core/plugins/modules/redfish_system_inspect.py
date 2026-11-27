#!/usr/bin/python
"""Observe one physical machine through its management controller.

This is how a physical target is proved before anything it holds is erased: the
exact ComputerSystem is read for its identity, its complete hardware inventory
is collected, and its power state is reported. Nothing is changed.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: redfish_system_inspect
short_description: Read one machine's identity, hardware and power state
version_added: "0.1.0"
description:
  - Reports the identity the controller gives this system, every hardware
    address it reports, and its power state.
  - A system that cannot be read reports its bounded controller error and
    marks the observation unanswered.
  - A controller whose certificate the declared trust refuses fails the
    module rather than reporting an empty identity, and so does a trust bundle
    that cannot be loaded.
  - An interface collection that cannot be read in full reports no addresses
    and names why, because a partial inventory proves nothing about which
    machine this is.
  - Performs no change and is safe to repeat.
options:
  endpoint:
    description: The exact ComputerSystem resource this machine is managed through.
    type: str
    required: true
  user:
    description: The account the controller answers.
    type: str
    required: true
  password:
    description: That account's password.
    type: str
    required: true
  verify:
    description: Whether the controller's own transport is verified.
    type: bool
    default: true
  verification:
    description:
      - Client TLS policy; V(certificate) requires the exact declared server certificate without matching its name.
      - O(verify) must agree; V(none) requires O(verify=false) and no O(ca_data).
    type: str
    choices:
      - full
      - certificate
      - none
  ca_data:
    description:
      - Public PEM CA or server certificates that are the only anchors the controller's own
        transport is verified against.
      - Empty verifies against the system trust store. Refused beside I(verify=false).
    type: str
    default: ""
  require_tpm:
    description: Require a positive read of an enabled TPM 2.0 before installation.
    type: bool
    default: false
  volume_ref:
    description: Positive storageId and volumeId of an existing logical root volume to discover.
    type: dict
    default: {}
  hardware_check:
    description:
      - Report bounded read-only observations for one target.
      - Carries the expected MAC set and optional base64-encoded system pins.
    type: dict
  root_disk:
    description:
      - Optional dictionary with a WWN, an exact serial and size, or both for the one whole root drive to prove before agent boot.
      - The serial must match exactly one drive and that drive's capacity must equal sizeBytes.
      - Empty requests no disk proof.
    type: dict
    default: {}
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Prove the machine before inserting anything
  bootwright.core.redfish_system_inspect:
    endpoint: '{{ request.controller.endpoint }}'
    user: '{{ user }}'
    password: '{{ password }}'
"""

RETURN = r"""
tpm_usable:
  description: Whether the requested TPM check positively proved an enabled TPM 2.0.
  returned: success
  type: bool
observation:
  description: The identity, addresses and power state the controller reported.
  returned: always
  type: dict
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import redfish_control, redfish_hardware_check, redfish_root_disk, redfish_root_volume


def main():
    module = AnsibleModule(
        argument_spec={
            "endpoint": {"type": "str", "required": True},
            "user": {"type": "str", "required": True},
            "password": {"type": "str", "required": True, "no_log": True},
            "verify": {"type": "bool", "default": True},
            "ca_data": {"type": "str", "default": ""},
            "verification": {"type": "str", "choices": ["full", "certificate", "none"]},
            "require_tpm": {"type": "bool", "default": False},
            "hardware_check": {"type": "dict"},
            "root_disk": {"type": "dict", "default": {}},
            "volume_ref": {"type": "dict", "default": {}},
        },
        supports_check_mode=True,
    )
    try:
        check = module.params.get("hardware_check")
        client_type = redfish_hardware_check.InspectionClient if check is not None else redfish_control.Client
        client = client_type(
            module.params["endpoint"], module.params["user"], module.params["password"],
            verify=bool(module.params["verify"]), ca_data=module.params["ca_data"] or "", verification=module.params.get("verification") or "",
        )
        if check is not None:
            if not module.params["verify"] or module.params.get("verification") == "none" or not module.params["endpoint"].startswith("https://"):
                raise redfish_control.ControllerError("hardware check requires verified HTTPS")
            module.exit_json(changed=False, checks=redfish_hardware_check.inspect(client, check))
            return
        observation = observe(client)
        if module.params.get("volume_ref"):
            if not module.params["verify"] or not module.params["endpoint"].startswith("https://"):
                raise redfish_root_volume.Refused(redfish_root_volume.REFUSAL)
            observation["rootVolume"] = redfish_root_volume.discover(client, module.params["volume_ref"])
        if module.params.get("root_disk"):
            if not module.params["verify"] or module.params.get("verification") == "none" or not module.params["endpoint"].startswith("https://"):
                raise redfish_root_disk.Refused("the root disk proof requires verified HTTPS BMC access")
            redfish_root_disk.prove(client, module.params["root_disk"])
        tpm = usable_tpm(client.system()) if module.params.get("require_tpm", False) else False
    except redfish_root_disk.Refused as failure:
        module.fail_json(msg=str(failure), root_disk_refused=True, unanswered=False)
    except redfish_control.ControllerError as failure:
        module.fail_json(msg="the management controller could not be inspected: %s" % failure,
                         unanswered=not isinstance(failure, redfish_control.UnverifiedCertificate))
    else:
        module.exit_json(changed=False, observation=observation, tpm_usable=tpm)


def usable_tpm(system):
    """Only a positive enabled TPM 2.0 declaration proves installer suitability."""
    modules = system.get("TrustedModules")
    return isinstance(modules, list) and any(
        isinstance(item, dict) and item.get("InterfaceType") == "TPM2_0"
        and isinstance(item.get("Status"), dict) and item["Status"].get("State") == "Enabled"
        for item in modules)


def observe(client):
    """What the controller reports about this machine, without changing it.

    The system is read once. An unanswered read preserves its bounded controller
    error; an answered system missing identity remains a hardware mismatch.
    Virtual media is not queried because this proof does not need it.
    """
    return redfish_control.hardware_observation(client)


if __name__ == "__main__":
    main()
