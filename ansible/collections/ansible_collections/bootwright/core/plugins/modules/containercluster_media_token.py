#!/usr/bin/python
"""Publish and retire per-insert links beneath a cluster's reserved image root."""

from __future__ import annotations

DOCUMENTATION = r'''
module: containercluster_media_token
short_description: Manage private per-insert image links
description:
  - Publishes hard links and proves ownership by device and inode.
options:
  request:
    description: Frozen installation request naming the reserved publication root.
    type: dict
    required: true
  source:
    description: Private URL of the original published image.
    type: str
    required: true
  image:
    description: Private image URL being inspected or retired.
    type: str
    default: ''
  operation:
    description: Publication, ownership observation, or retirement after eject.
    type: str
    choices: [publish, inspect, remove]
    required: true
author:
  - Bootwright contributors (@crmarques)
'''
EXAMPLES = r'''
- name: Inspect inserted media
  bootwright.core.containercluster_media_token:
    request: '{{ request }}'
    source: '{{ source }}'
    image: '{{ image }}'
    operation: inspect
  no_log: true
'''
RETURN = r'''
own:
  description: Whether the image is the published file.
  type: bool
  returned: always
url:
  description: Private URL of a newly published link.
  type: str
  returned: on publication
'''

import errno
import os
import re
import secrets
import stat
from urllib.parse import urlsplit

from ansible.module_utils.basic import AnsibleModule

DIRECTORY = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW


def open_root(path):
    """Walk every component without following a symbolic link."""
    if not path.startswith('/') or any(p in ('.', '..') for p in path.split('/')):
        raise ValueError('invalid publication root')
    fd = os.open('/', DIRECTORY)
    try:
        for part in filter(None, path.split('/')):
            child = os.open(part, DIRECTORY, dir_fd=fd)
            os.close(fd)
            fd = child
        return fd
    except Exception:
        os.close(fd)
        raise


def relative(base, image):
    try:
        return relative_url(base, image)
    except ValueError:
        return None


def relative_url(base, image):
    origin, target = urlsplit(base), urlsplit(image)
    if (origin.scheme.lower(), origin.hostname, origin.port or 443) != (target.scheme.lower(), target.hostname, target.port or 443):
        return None
    if target.query or target.fragment or target.username or target.password:
        return None
    prefix = origin.path.rstrip('/') + '/'
    if not target.path.startswith(prefix):
        return None
    parts = target.path[len(prefix):].split('/')
    if len(parts) != 2 or not re.fullmatch(r'[a-zA-Z0-9_-]+', parts[0]) or parts[1] != 'agent.iso':
        return None
    return parts[0]


def identity(root, token):
    directory = os.open(token, DIRECTORY, dir_fd=root)
    try:
        info = os.stat('agent.iso', dir_fd=directory, follow_symlinks=False)
        if not stat.S_ISREG(info.st_mode):
            raise ValueError('image is not a regular file')
        return info.st_dev, info.st_ino
    finally:
        os.close(directory)


def operate(request, source, image, operation):
    publication = request['publicationRoot']
    source_token = relative(publication['url'], source)
    if not source_token or source_token.startswith('insert-'):
        raise ValueError('published source is unavailable')
    root = open_root(publication['path'])
    try:
        expected = identity(root, source_token)
        token = relative(publication['url'], image)
        own = False
        if token:
            try:
                own = identity(root, token) == expected
            except (OSError, ValueError):
                pass
        if operation == 'inspect':
            return {'changed': False, 'own': own}
        if operation == 'remove':
            if own and token.startswith('insert-'):
                directory = os.open(token, DIRECTORY, dir_fd=root)
                try:
                    if identity(root, token) != expected:
                        raise ValueError('image changed before removal')
                    os.unlink('agent.iso', dir_fd=directory)
                finally:
                    os.close(directory)
                os.rmdir(token, dir_fd=root)
                return {'changed': True, 'own': True}
            return {'changed': False, 'own': own}
        token = 'insert-' + secrets.token_hex(32)
        os.mkdir(token, mode=0o711, dir_fd=root)
        directory = os.open(token, DIRECTORY, dir_fd=root)
        source_directory = None
        try:
            os.fchmod(directory, 0o711)
            source_directory = os.open(source_token, DIRECTORY, dir_fd=root)
            try:
                label = os.getxattr(root, 'security.selinux')
            except OSError as error:
                if error.errno not in (errno.ENODATA, errno.ENOTSUP):
                    raise
            else:
                os.setxattr(directory, 'security.selinux', label)
            os.link('agent.iso', 'agent.iso', src_dir_fd=source_directory, dst_dir_fd=directory, follow_symlinks=False)
            if identity(root, token) != expected:
                raise ValueError('published source changed')
        except Exception:
            try:
                os.unlink('agent.iso', dir_fd=directory)
            except FileNotFoundError:
                pass
            os.rmdir(token, dir_fd=root)
            raise
        finally:
            os.close(directory)
            if source_directory is not None:
                os.close(source_directory)
        return {'changed': True, 'own': True, 'url': publication['url'] + '/' + token + '/agent.iso'}
    finally:
        os.close(root)


def main():
    module = AnsibleModule(argument_spec={
        'request': {'type': 'dict', 'required': True},
        'source': {'type': 'str', 'required': True, 'no_log': True},
        'image': {'type': 'str', 'default': '', 'no_log': True},
        'operation': {'type': 'str', 'required': True, 'choices': ['publish', 'inspect', 'remove']},
    }, supports_check_mode=False)
    try:
        result = operate(**module.params)
    except (OSError, ValueError, KeyError):
        module.fail_json(msg='the reserved cluster image publication could not be proved')
    module.exit_json(**result)


if __name__ == '__main__':
    main()
