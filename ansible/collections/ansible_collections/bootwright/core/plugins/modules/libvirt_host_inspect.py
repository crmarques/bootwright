#!/usr/bin/python
"""Observe one libvirt provider host without changing any of it."""

from __future__ import annotations

DOCUMENTATION = r"""
module: libvirt_host_inspect
short_description: Observe one Bootwright libvirt provider host
version_added: "0.1.0"
description:
  - Reports the hypervisor closure, the virtualization daemon, whether the
    declared connection answers, the state and ownership of every declared
    network, the virtual-media pool, and whether the pool directory exists.
  - Networks and the pool are read only through a connection that answers,
    so without an answer their empty fields prove nothing. Each network and
    the pool also report whether the driver that owns it answered for it,
    because a driver that is silent reports them undefined too. The pool
    directory is observed by its path either way.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen provider host request.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the provider host
  bootwright.core.libvirt_host_inspect:
    request: '{{ bootwright_substrate_host_request }}'
"""

RETURN = r"""
observation:
  description:
    - The owned host resources and their observed state.
    - The uri key is whether the declared connection answered, each
      network's answered key and the poolAnswered key whether the network
      and storage drivers answered for them, and the directory key whether
      anything exists at the pool directory's path.
    - Each managed network's autostart key and the poolAutostart key are
      whether it starts with the host, each network's owned key whether its
      metadata names this context and that network, and the poolOwned key
      whether the pool targets the frozen directory.
  returned: always
  type: dict
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import observe_host


def main():
    module = AnsibleModule(
        argument_spec={"request": {"type": "dict", "required": True}},
        supports_check_mode=True,
    )
    try:
        observation = observe_host(module.run_command, module.params["request"])
    except (OSError, ValueError, KeyError) as failure:
        module.fail_json(msg="the provider host could not be observed: %s" % type(failure).__name__)
        return
    module.exit_json(changed=False, observation=observation)


if __name__ == "__main__":
    main()
