#!/usr/bin/python
"""Remove one cluster's installer work area, the evidence of an insert last."""

from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_work_area
short_description: Remove the installer work area one container cluster shares
version_added: "0.1.0"
description:
  - Removes the work area a cluster's media and install blocks share, never
    following a link inside it.
  - Removes what says a node may have been handed the cluster's image last, so a
    removal stopped at any step leaves an area that reads as one a node was
    handed the image in for as long as it held an insert record.
  - With I(refuse_inserted), keeps an area a node may have been handed the image
    in whole and fails, so a rebuild never discards it.
options:
  path:
    description: The absolute path of the work area.
    type: str
    required: true
  refuse_inserted:
    description: Fail, removing nothing, when a node may have been handed the image built there.
    type: bool
    default: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Discard the installer work area a previous build left
  bootwright.core.containercluster_work_area:
    path: /var/lib/example/clusters/sno
    refuse_inserted: true
"""

RETURN = r"""
changed:
  description: Whether a work area was there to remove.
  returned: always
  type: bool
"""

import os

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.containercluster_work_area import (
    InsertedError,
    remove,
)


def main():
    module = AnsibleModule(
        argument_spec={
            "path": {"type": "str", "required": True},
            "refuse_inserted": {"type": "bool", "default": False},
        },
    )
    path = module.params["path"]
    if not os.path.isabs(path) or os.path.normpath(path) != path:
        module.fail_json(msg="the work area path must be absolute and normal")
    try:
        changed = remove(path, module.params["refuse_inserted"])
    except InsertedError:
        module.fail_json(msg="a node may have been handed the cluster's image built in the work area %s, so the "
                             "area was kept and nothing was removed" % path)
    except OSError as error:
        module.fail_json(msg="the work area %s could not be removed: %s" % (path, error.strerror or error))
    module.exit_json(changed=changed)


if __name__ == "__main__":
    main()
