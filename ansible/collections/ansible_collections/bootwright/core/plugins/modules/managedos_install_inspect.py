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
  - Reports the identity the published tree carries, the digest of the image
    it was extracted from, and counts the tree complete only when that
    identity is the digest the request froze, so a tree extracted from another
    image is extracted again.
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
  description:
    - Whether each published path, the staging tree and the work area exist.
    - C(treeIdentity) is the digest the published tree's identity file holds,
      64 lowercase hexadecimal characters, or empty when it holds none.
  returned: always
  type: dict
"""

import os

from ansible.module_utils.basic import AnsibleModule

# TREE_MARKER is what makes a published tree complete: it is renamed into place
# last, so a fetching installer never sees a partial tree.
TREE_MARKER = ".treeinfo"
# TREE_IDENTITY holds the digest of the image the tree was extracted from,
# written into the staged tree before the rename publishes it.
TREE_IDENTITY = ".bootwright-tree-identity"
HEX = frozenset("0123456789abcdef")


def tree_identity(path):
    """The digest a tree's identity file holds, or '' when it holds none."""
    try:
        descriptor = os.open(os.path.join(path, TREE_IDENTITY), os.O_RDONLY | os.O_NOFOLLOW)
    except OSError:
        return ""
    try:
        data = os.read(descriptor, 65)
    except OSError:
        return ""
    finally:
        os.close(descriptor)
    value = data.decode("ascii", "replace").strip()
    if len(value) != 64 or set(value) - HEX:
        return ""
    return value


def private_image(root):
    """Whether a direct child of the private subtree holds a regular install.iso.

    A delivered-key installation publishes its image beneath the unguessable
    directory its attempt minted, which no request names, so every directory
    one level down is looked in and nothing deeper.
    """
    try:
        children = os.listdir(root)
    except OSError:
        return False
    for child in children:
        directory = os.path.join(root, child)
        image = os.path.join(directory, "install.iso")
        if os.path.isdir(directory) and not os.path.islink(directory) and os.path.isfile(image) and not os.path.islink(image):
            return True
    return False


def observe(request, staging, work):
    """Whether each piece of content this installation publishes is present,
    and each remnant of an attempt that did not finish."""
    tree = request.get("tree")
    private = request.get("private")
    identity = tree_identity(tree["path"]) if tree else ""
    frozen = (request.get("treeMedia") or {}).get("sha256", "")
    image = request.get("image")
    return {
        # The installer image is published publicly, or beneath the private
        # subtree for a delivered-key installation.
        "image": (bool(image) and os.path.isfile(image["path"])) or (bool(private) and private_image(private["path"])),
        # Material that only needed to exist for one boot must not outlive it,
        # so a subtree still present is unfinished work rather than a state.
        "private": bool(private) and os.path.isdir(private["path"]),
        # A tree is complete only when it was extracted from the image this
        # operation froze; one from another image is extracted again.
        "tree": bool(tree) and os.path.isfile(os.path.join(tree["path"], TREE_MARKER)) and identity == frozen,
        "treeIdentity": identity,
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
