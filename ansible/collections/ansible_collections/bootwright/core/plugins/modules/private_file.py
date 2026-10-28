#!/usr/bin/python
"""Write one file whose bytes arrive as the module's own argument."""

from __future__ import annotations

DOCUMENTATION = r"""
module: private_file
short_description: Write a file that carries lent material on its execution host
version_added: "0.1.0"
description:
  - Writes the content its argument carries, so lent material reaches a remote host only
    inside the pipelined module payload and never through a file transfer.
  - Stages the bytes in a private sibling, sets ownership and mode before publication and
    renames it over the destination, so no reader sees partial or wider content.
  - Refuses a destination that is a link or not a regular file, removes its own stage
    when it fails, and removes the stages of the same destination an interrupted run left.
  - On a local connection the argument crosses the controller-local module payload, which
    the adapter runs pipelined too.
options:
  path:
    description: The absolute destination, in a directory that already exists.
    type: str
    required: true
  content:
    description: The bytes to write, as text. Never returned or logged.
    type: str
    required: true
  mode:
    description: The destination's permission bits.
    type: str
    choices: ['0600', '0640', '0644']
    default: '0600'
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Write the install configuration with its bound material
  bootwright.core.private_file:
    path: /var/lib/example/install-config.yaml
    content: "{{ lookup('ansible.builtin.file', example_material.pullSecret) }}"
  no_log: true
  diff: false
"""
RETURN = r"""
path:
  description: The destination written.
  returned: always
  type: str
"""

import errno
import os
import re
import secrets
import stat

from ansible.module_utils.basic import AnsibleModule

MODES = {"0600": 0o600, "0640": 0o640, "0644": 0o644}
DIRECTORY = os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC
EXISTING = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC
STAGE = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC


def current(directory, name, data, mode, owner, group):
    """Whether the destination already holds these bytes under this ownership
    and mode. A link or a non-regular entry refuses rather than being replaced."""
    try:
        descriptor = os.open(name, EXISTING, dir_fd=directory)
    except FileNotFoundError:
        return False
    except OSError as error:
        if error.errno == errno.ELOOP:
            raise ValueError("the destination is a symbolic link, which this module never follows or replaces")
        raise
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode):
            raise ValueError("the destination is not a regular file")
        if (stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid, info.st_size) != (mode, owner, group, len(data)):
            return False
        held = b""
        while len(held) <= len(data):
            block = os.read(descriptor, len(data) + 1 - len(held))
            if not block:
                break
            held += block
        return held == data
    finally:
        os.close(descriptor)


def stage(directory, name, data, mode, owner, group):
    """Write the bytes to a private sibling and return its name."""
    staged = ".%s.%s" % (name, secrets.token_hex(8))
    descriptor = os.open(staged, STAGE, 0o600, dir_fd=directory)
    try:
        os.fchown(descriptor, owner, group)
        view = memoryview(data)
        while view:
            view = view[os.write(descriptor, view):]
        os.fchmod(descriptor, mode)
        os.fsync(descriptor)
    except BaseException:
        os.close(descriptor)
        discard(directory, staged)
        raise
    os.close(descriptor)
    return staged


def discard(directory, staged):
    """Remove a stage, never letting its removal hide the failure that left it."""
    try:
        os.unlink(staged, dir_fd=directory)
    except OSError:
        pass


def sweep(directory, name, owner):
    """Remove the stages of this destination an interrupted run left: regular
    files of the stage's own name shape and owner, never anything else."""
    shape = re.compile(r"\.%s\.[0-9a-f]{16}\Z" % re.escape(name))
    os.lseek(directory, 0, os.SEEK_SET)
    for entry in os.listdir(directory):
        if shape.match(entry) is None:
            continue
        info = os.stat(entry, dir_fd=directory, follow_symlinks=False)
        if stat.S_ISREG(info.st_mode) and info.st_uid == owner:
            discard(directory, entry)


def write(path, content, mode="0600", owner=0, group=0):
    """Publish content at path and report whether it changed anything."""
    if not os.path.isabs(path) or os.path.normpath(path) != path or path.endswith("/"):
        raise ValueError("the destination is not a normalized absolute path")
    if mode not in MODES:
        raise ValueError("the destination mode is not one this module writes")
    data, bits = content.encode("utf-8"), MODES[mode]
    directory = os.open(os.path.dirname(path) or "/", DIRECTORY)
    try:
        name = os.path.basename(path)
        sweep(directory, name, owner)
        if current(directory, name, data, bits, owner, group):
            return False
        staged = stage(directory, name, data, bits, owner, group)
        try:
            os.rename(staged, name, src_dir_fd=directory, dst_dir_fd=directory)
        except BaseException:
            discard(directory, staged)
            raise
        os.fsync(directory)
        return True
    finally:
        os.close(directory)


def main():
    module = AnsibleModule(argument_spec={
        "path": {"type": "str", "required": True},
        "content": {"type": "str", "required": True, "no_log": True},
        "mode": {"type": "str", "default": "0600", "choices": sorted(MODES)},
    }, supports_check_mode=False)
    path = module.params["path"]
    try:
        changed = write(path, module.params["content"], module.params["mode"])
    except ValueError as error:
        module.fail_json(msg="%s could not be written: %s" % (path, error))
    except OSError as error:
        module.fail_json(msg="%s could not be written: %s" % (path, error.strerror or "an operating system error"))
    module.exit_json(changed=changed, path=path)


if __name__ == "__main__":
    main()
