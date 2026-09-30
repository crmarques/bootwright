#!/usr/bin/python
"""Observe one installation's published content without changing any of it."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_install_inspect
short_description: Observe the content one Bootwright installation publishes
version_added: "0.1.0"
description:
  - Reports whether the per-machine installer image, the private subtree and,
    when the profile uses one, the hosted package tree are published beneath
    the served root.
  - Reports the package tree twice, complete by its C(.treeinfo) marker and
    present by anything at its path, because a removal withdraws the marker
    before the rest of the tree, so one stopped part way leaves the rest
    without it.
  - Reports the staging tree beside it and the installer work area, which an
    attempt killed part way leaves behind and a removal takes back.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen installation request.
    type: dict
    required: true
  staging:
    description:
      - The path the package tree is extracted at before its rename, or empty
        when the request publishes no tree.
    type: str
    default: ""
  work:
    description: The work area the installer image is built in.
    type: str
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the published installer content
  bootwright.core.managedos_install_inspect:
    request: '{{ bootwright_os_install_request }}'
    staging: '{{ managedos_install_anaconda_staged_tree }}'
    work: '{{ managedos_install_anaconda_work }}'
"""

RETURN = r"""
observation:
  description: Whether each published path, the staging tree and the work area exist.
  returned: always
  type: dict
"""

import os

from ansible.module_utils.basic import AnsibleModule

# TREE_MARKER is what makes a published tree complete: it is renamed into place
# last, so a fetching installer never sees a partial tree.
TREE_MARKER = ".treeinfo"


def observe(request, staging, work):
    """Whether each piece of content this installation publishes is present,
    and each remnant of an attempt that did not finish."""
    tree = request.get("tree")
    private = request.get("private")
    return {
        "image": os.path.isfile(request["image"]["path"]),
        # Material that only needed to exist for one boot must not outlive it,
        # so a subtree still present is unfinished work rather than a state.
        "private": bool(private) and os.path.isdir(private["path"]),
        "tree": bool(tree) and os.path.isfile(os.path.join(tree["path"], TREE_MARKER)),
        # A removal withdraws the marker before the rest of the tree, so one
        # stopped part way leaves the rest without it: content still to take
        # back, though no complete tree.
        "treeContent": bool(tree) and os.path.lexists(tree["path"]),
        # An apply killed while it extracted the tree leaves the staging copy
        # part way written beneath the served root, where it is served.
        "treeStaging": bool(tree) and bool(staging) and os.path.lexists(staging),
        # An attempt killed before it finished leaves the work area the image
        # is built in, with the rendered Kickstart and any image it wrote.
        "work": bool(work) and os.path.lexists(work),
    }


def main():
    module = AnsibleModule(
        argument_spec={
            "request": {"type": "dict", "required": True},
            "staging": {"type": "str", "default": ""},
            "work": {"type": "str", "required": True},
        },
        supports_check_mode=True,
    )
    params = module.params
    module.exit_json(changed=False, observation=observe(params["request"], params["staging"], params["work"]))


if __name__ == "__main__":
    main()
