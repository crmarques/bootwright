#!/usr/bin/python
"""Prove the reserved image publication's layout before the media block touches it."""

from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_media_layout
short_description: Prove the layout a cluster's boot image is published in
version_added: "0.1.0"
description:
  - Proves, before the media block creates, mints, publishes or removes
    anything, the layout the installation block's walk to the reserved root
    accepts, so a layout that walk would refuse is refused here instead of
    after the image is published.
  - The reserved root is C(<content root>/public/private/<consumer>/<object>).
    No component from the artifact server's content root down may be a
    symbolic link, nor any entry beneath the reserved root or in a directory
    directly beneath it. The directories above the content root may be links,
    and every directory the walk to the content root passes through, links
    resolved, must be owned by root and writable by neither group nor other.
  - A refusal names the directory to fix, never a URL or a token.
  - A directory missing beneath the content root is accepted, because the
    publication creates it. A missing content root refuses a publication and
    is accepted by a removal, which then has nothing to remove.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen boot-media request naming the reserved root.
    type: dict
    required: true
  operation:
    description: Whether the layout is proved before a publication or before a removal.
    type: str
    choices: [publish, remove]
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Prove the publication layout before building
  bootwright.core.containercluster_media_layout:
    request: '{{ bootwright_cluster_media_request }}'
    operation: publish
"""

RETURN = r"""
present:
  description: Whether the reserved root exists.
  returned: success
  type: bool
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.containercluster_publication import (
    LayoutError,
    prove,
)

EFFECT = {"publish": "published", "remove": "removed"}


def main():
    module = AnsibleModule(
        argument_spec={
            "request": {"type": "dict", "required": True},
            "operation": {"type": "str", "required": True, "choices": ["publish", "remove"]},
        },
        supports_check_mode=True,
    )
    operation = module.params["operation"]
    try:
        present = prove(module.params["request"]["image"]["path"], removal=operation == "remove")
    except LayoutError as error:
        module.fail_json(msg="the boot image of this cluster cannot be %s safely: %s. Nothing was %s; fix that "
                             "directory and repeat the operation" % (EFFECT[operation], error, EFFECT[operation]))
    except (OSError, ValueError, KeyError, TypeError):
        module.fail_json(msg="the layout the boot image of this cluster is %s in could not be proved; nothing was %s"
                             % (EFFECT[operation], EFFECT[operation]))
    module.exit_json(changed=False, present=present)


if __name__ == "__main__":
    main()
