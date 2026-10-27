#!/usr/bin/python
"""Create and remove one role's private invocation scratch."""

from __future__ import annotations

DOCUMENTATION = r"""
module: lifecycle_scratch
short_description: Manage one lifecycle role's private scratch
version_added: "0.1.0"
description:
  - Creates private scratch on the execution host using the runner's invocation identity.
  - Creates the root-owned C(/var/lib/bootwright-staging/runs) parent, mode 0700, when absent.
  - Refuses an unsafe parent or one on a noexec filesystem, naming the directory and its remedy.
  - Refuses links, foreign ownership and non-private directories before accessing files.
  - Removes only the named role's files and empty invocation directory.
options:
  root:
    description: The runner-issued scratch directory beneath C(/var/lib/bootwright-staging/runs).
    type: str
    required: true
  scope:
    description: The role that owns the scratch files.
    type: str
    required: true
    choices: [libvirt-host, libvirt-machine, managedos-install]
  remove_root:
    description: Remove the empty root on a remote execution host; the local runner owns its root.
    type: bool
    default: false
  state:
    description: Whether the private role directory exists.
    type: str
    choices: [present, absent]
    default: present
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Prepare private network definitions
  bootwright.core.lifecycle_scratch:
    root: '{{ bootwright_run_scratch }}'
    scope: libvirt-host
"""
RETURN = r"""
path:
  description: The role's private directory.
  returned: always
  type: str
"""

import errno
import fcntl
import os
import re
import stat

from ansible.module_utils.basic import AnsibleModule

SCOPES = frozenset(("libvirt-host", "libvirt-machine", "managedos-install"))
PARENT = "/var/lib/bootwright-staging/runs"
FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


def private(parent, name, owner):
    descriptor = os.open(name, FLAGS, dir_fd=parent)
    info = os.fstat(descriptor)
    if info.st_uid != owner or stat.S_IMODE(info.st_mode) != 0o700:
        os.close(descriptor)
        raise ValueError("scratch directory is not private to its owner")
    return descriptor


def owned(path, owner, mode, create):
    """Create path when asked and absent, then require a directory owner holds that no other account may write."""
    if create:
        try:
            os.mkdir(path, mode)
        except FileExistsError:
            pass
    try:
        info = os.lstat(path)
    except FileNotFoundError:
        if create:
            raise
        return False
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != owner
            or mode == 0o700 and stat.S_IMODE(info.st_mode) != 0o700 or stat.S_IMODE(info.st_mode) & 0o022):
        raise ValueError("the run scratch parent %s is not a directory user %d owns and alone may write; remove it or make it"
                         " a directory owned by user %d with mode %04o" % (path, owner, owner, mode))
    return True


def manage(root, scope, state, parent_path=PARENT, owner=0, remove_root=False):
    name = os.path.basename(root)
    if (os.path.dirname(root) != parent_path
            or not re.fullmatch(r"bootwright-run-scratch-[0-9]+-[0-9]+", name)
            or scope not in SCOPES or state not in ("present", "absent")):
        raise ValueError("scratch is not a runner-issued role directory")
    present = state == "present"
    if not owned(os.path.dirname(parent_path), owner, 0o711, present) and not present:
        return os.path.join(root, scope)
    if not owned(parent_path, owner, 0o700, present) and not present:
        return os.path.join(root, scope)
    if present and os.statvfs(parent_path).f_flag & os.ST_NOEXEC:
        raise ValueError("the run scratch parent %s lies on a noexec filesystem; remount it with exec" % parent_path)
    parent = os.open(parent_path, FLAGS)
    try:
        if state == "present":
            try:
                os.mkdir(name, 0o700, dir_fd=parent)
            except FileExistsError:
                pass
        try:
            invocation = private(parent, name, owner)
        except FileNotFoundError:
            if state == "absent":
                return os.path.join(root, scope)
            raise
        try:
            fcntl.flock(invocation, fcntl.LOCK_EX)
            current = os.stat(name, dir_fd=parent, follow_symlinks=False)
            held = os.fstat(invocation)
            if (current.st_dev, current.st_ino) != (held.st_dev, held.st_ino):
                raise ValueError("scratch directory changed while opening it")
            if state == "present":
                os.mkdir(scope, 0o700, dir_fd=invocation)
            else:
                try:
                    child = private(invocation, scope, owner)
                except FileNotFoundError:
                    return os.path.join(root, scope)
                try:
                    names = os.listdir(child)
                    if len(names) > 256:
                        raise ValueError("scratch contains too many entries")
                    for entry in names:
                        info = os.stat(entry, dir_fd=child, follow_symlinks=False)
                        if stat.S_ISDIR(info.st_mode) or info.st_dev != held.st_dev:
                            raise ValueError("scratch contains a directory or another filesystem")
                    for entry in names:
                        os.unlink(entry, dir_fd=child)
                finally:
                    os.close(child)
                os.rmdir(scope, dir_fd=invocation)
                if remove_root:
                    try:
                        os.rmdir(name, dir_fd=parent)
                    except OSError as error:
                        if error.errno != errno.ENOTEMPTY:
                            raise
        finally:
            os.close(invocation)
    finally:
        os.close(parent)
    return os.path.join(root, scope)


def main():
    module = AnsibleModule(argument_spec={
        "root": {"type": "str", "required": True},
        "remove_root": {"type": "bool", "default": False},
        "scope": {"type": "str", "required": True, "choices": sorted(SCOPES)},
        "state": {"type": "str", "default": "present", "choices": ["present", "absent"]},
    }, supports_check_mode=False)
    try:
        path = manage(module.params["root"], module.params["scope"], module.params["state"],
                      remove_root=module.params["remove_root"])
    except ValueError as error:
        module.fail_json(msg=str(error))
    except OSError:
        module.fail_json(msg="private lifecycle scratch could not be safely prepared or removed")
    module.exit_json(changed=False, path=path)


if __name__ == "__main__":
    main()
