#!/usr/bin/python
"""Prove, create and remove the private directories one context's hosted content lives beneath."""

from __future__ import annotations

DOCUMENTATION = r"""
module: hosted_area
short_description: Manage the private directories a context's hosted content lives beneath
version_added: "0.1.0"
description:
  - Reaches C(/var/lib/bootwright-hosted/<context>/<area>/...) by no-follow descent, proving each existing directory a
    root-owned directory of mode 0700 that is not a link, and refuses without repair when one is not.
  - With I(state=present), creates each missing directory with mode 0700 beneath its proved parent, top-down.
  - With I(state=proved), creates nothing and reports whether the whole path exists.
  - With I(state=absent), removes each empty directory from the path up to the context's own, deepest first, and
    stops at one that still holds anything. The hosted root itself is never removed.
options:
  context:
    description: The context whose hosted directory the path lies beneath.
    type: str
    required: true
  path:
    description: The directories beneath the context's, opening with C(services), C(machines) or C(clusters).
    type: list
    elements: str
    required: true
  state:
    description: Whether to create, only prove, or remove the empty directories of the path.
    type: str
    choices:
      - present
      - proved
      - absent
    default: present
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Prove and create the context's service kind directory
  bootwright.core.hosted_area:
    context: lab
    path:
      - services
      - dns
"""
RETURN = r"""
path:
  description: The directory the path names.
  returned: always
  type: str
exists:
  description: Whether that directory exists once the module returns.
  returned: always
  type: bool
"""

from ansible.module_utils.basic import AnsibleModule

from ansible_collections.bootwright.core.plugins.module_utils import hosted_area


def main():
    module = AnsibleModule(argument_spec={
        "context": {"type": "str", "required": True},
        "path": {"type": "list", "elements": "str", "required": True},
        "state": {"type": "str", "default": "present", "choices": ["present", "proved", "absent"]},
    }, supports_check_mode=False)
    context, path, state = module.params["context"], module.params["path"], module.params["state"]
    changed, exists, target = False, False, ""
    try:
        target = hosted_area.target(context, path)
        if state == "present":
            changed = hosted_area.ensure(context, path)
            exists = True
        elif state == "proved":
            exists = hosted_area.prove(context, path)
        else:
            changed = hosted_area.remove(context, path)
            exists = hosted_area.prove(context, path)
    except hosted_area.Refusal as refusal:
        module.fail_json(msg=str(refusal))
    except OSError:
        module.fail_json(msg="the hosted directories of context %s could not be safely proved, created or removed" % context)
    module.exit_json(changed=changed, path=target, exists=exists)


if __name__ == "__main__":
    main()
