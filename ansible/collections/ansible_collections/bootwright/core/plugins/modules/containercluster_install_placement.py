#!/usr/bin/python
"""Read, or add once, the labels and taints an installed cluster's nodes declare."""
from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_install_placement
short_description: Read or place the declared labels and taints of a cluster's nodes
version_added: "0.1.0"
description:
  - Reads every node through the cluster API with the kept administrator
    kubeconfig and reports, for each node the frozen request declares a
    placement for, the values of its declared label keys and its taints of a
    declared key.
  - With I(operation=place), first refuses any declared key or taint the node
    carries with another value, then adds what each node lacks in one JSON
    patch pinned to the uid, label map and spec it read, and reads the
    placement back. It never removes or replaces another manager's label or
    taint.
options:
  client:
    description: The absolute path of the located oc executable.
    type: path
    required: true
  kubeconfig:
    description: The absolute path of the kept administrator kubeconfig.
    type: path
    required: true
  nodes:
    description: The frozen request's nodes.
    type: list
    elements: dict
    required: true
  operation:
    description: Whether to read the placement or to place it.
    type: str
    choices: [observe, place]
    required: true
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Place the declared labels and taints
  bootwright.core.containercluster_install_placement:
    client: "{{ containercluster_install_agent_client }}"
    kubeconfig: "{{ containercluster_install_agent_kubeconfig }}"
    nodes: "{{ bootwright_cluster_install_request.nodes }}"
    operation: place
  no_log: true
"""
RETURN = r"""
answered:
  description: Whether the cluster answered the read of its nodes.
  returned: success
  type: bool
placement:
  description: What each declared node carries of its placement.
  returned: success
  type: list
  elements: dict
unplaced:
  description: The declared nodes short of their placement.
  returned: success
  type: list
  elements: str
refused:
  description: Why placing refused, naming nodes and keys only.
  returned: when placing refused
  type: str
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import containercluster_node_placement as placement


def main():
    module = AnsibleModule(argument_spec={
        "client": {"type": "path", "required": True},
        "kubeconfig": {"type": "path", "required": True, "no_log": False},
        "nodes": {"type": "list", "elements": "dict", "required": True},
        "operation": {"type": "str", "required": True, "choices": ["observe", "place"]},
    }, supports_check_mode=False)
    params = module.params
    try:
        nodes = placement.declared(params["nodes"])
        if params["operation"] == "observe":
            result = placement.observe(nodes, placement.read(params["client"], params["kubeconfig"]))
            module.exit_json(changed=False, **result)
            return
        result = placement.place(params["client"], params["kubeconfig"], nodes)
    except placement.Refused as refused:
        module.exit_json(changed=False, refused=str(refused))
        return
    except (ValueError, TypeError, KeyError):
        module.fail_json(msg="the node placement request is malformed")
        return
    module.exit_json(**result)


if __name__ == "__main__":
    main()
