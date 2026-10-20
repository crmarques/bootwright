#!/usr/bin/python
"""Publish and retire per-insert links beneath a cluster's reserved image root."""

from __future__ import annotations

DOCUMENTATION = r'''
module: containercluster_media_token
short_description: Manage private per-insert image links
description:
  - Publishes hard links and proves ownership by device and inode.
  - A sweep retires every insert link beneath the reserved root whose URL no
    management controller presents, so the link of an insert that failed or
    that a later insert replaced stops serving the image.
  - A sweep keeps every insert link whose token names a path component of any
    presented image URL, whatever host and port that URL names, because a
    controller may echo the image without its non-default port or under
    another spelling of its host, while a token is 256 bits of randomness no
    foreign URL names by accident.
  - A sweep removes an unpresented C(insert-*) entry only when it is a
    directory holding nothing but C(agent.iso), a regular file that is the same
    file, by device and inode, as the published source, or an empty directory
    an interrupted publication left. Any other unpresented C(insert-*) entry is
    left in place and refuses the sweep, after every provable link was
    removed, because skipping it could hide tampering with the reserved root.
  - The source publication and every entry not named C(insert-*) are never
    touched.
  - The reserved root is C(<content root>/public/private/<consumer>/<object>).
    No component from the artifact server's content root down may be a
    symbolic link. Its ancestors may be, and every directory the walk to the
    content root passes through, links resolved, must be owned by root and
    writable by neither group nor other. The media block proves the same
    layout before it publishes, and a refusal of either names the directory to
    fix, never a URL or a token.
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
  presented:
    description:
      - For a sweep, the image URL each management controller presents; every
        insert link whose token is a path component of one of them is kept.
    type: list
    elements: str
    default: []
  operation:
    description: Publication, ownership observation, retirement after eject, or the sweep of every unpresented insert link.
    type: str
    choices: [publish, inspect, remove, sweep]
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

- name: Retire every insert link no controller presents
  bootwright.core.containercluster_media_token:
    request: '{{ request }}'
    source: '{{ source }}'
    presented: '{{ presented }}'
    operation: sweep
  no_log: true
'''
RETURN = r'''
own:
  description: Whether the image is the published file.
  type: bool
  returned: for publish, inspect and remove
url:
  description: Private URL of a newly published link.
  type: str
  returned: on publication
removed:
  description: How many unpresented insert links the sweep removed.
  type: int
  returned: for a sweep
kept:
  description: How many insert links the sweep kept because a controller presents them.
  type: int
  returned: for a sweep
refused:
  description: How many unpresented insert entries the sweep could not prove and left in place.
  type: int
  returned: for a sweep
'''

import errno
import os
import re
import secrets
import stat
from urllib.parse import urlsplit

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.containercluster_publication import (
    DIRECTORY,
    LayoutError,
    open_root,
)


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


def source_identity(root, token, path):
    """The published image's identity, refusing a link in its place by the reserved root's name."""
    try:
        return identity(root, token)
    except OSError as error:
        if error.errno not in (errno.ELOOP, errno.ENOTDIR):
            raise
    except ValueError:
        pass
    raise LayoutError(path, 'holds the published image through a symbolic link or another entry that is not a '
                            'directory holding a regular file, and no entry from the artifact server\'s content root '
                            'down may be one')


def retire(root, token, expected):
    try:
        info = os.stat(token, dir_fd=root, follow_symlinks=False)
    except FileNotFoundError:
        return 'removed'
    if not stat.S_ISDIR(info.st_mode):
        return 'refused'
    directory = os.open(token, DIRECTORY, dir_fd=root)
    try:
        entries = os.listdir(directory)
        if entries:
            if entries != ['agent.iso']:
                return 'refused'
            image = os.stat('agent.iso', dir_fd=directory, follow_symlinks=False)
            if not stat.S_ISREG(image.st_mode) or (image.st_dev, image.st_ino) != expected:
                return 'refused'
            os.unlink('agent.iso', dir_fd=directory)
    finally:
        os.close(directory)
    try:
        os.rmdir(token, dir_fd=root)
    except OSError as error:
        if error.errno in (errno.ENOTEMPTY, errno.EEXIST):
            return 'refused'
        raise
    return 'removed'


def components(image):
    try:
        return set(urlsplit(image).path.split('/'))
    except ValueError:
        return set()


def sweep(root, expected, presented):
    held = set().union(*(components(image) for image in presented))
    counts = {'removed': 0, 'kept': 0, 'refused': 0}
    for token in sorted(os.listdir(root)):
        if not token.startswith('insert-'):
            continue
        if token in held:
            counts['kept'] += 1
            continue
        counts[retire(root, token, expected)] += 1
    return dict(counts, changed=counts['removed'] > 0)


def operate(request, source, image, operation, presented=()):
    publication = request['publicationRoot']
    source_token = relative(publication['url'], source)
    if not source_token or source_token.startswith('insert-'):
        raise ValueError('published source is unavailable')
    root = open_root(publication['path'])
    try:
        expected = source_identity(root, source_token, publication['path'])
        if operation == 'sweep':
            return sweep(root, expected, presented)
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
        'presented': {'type': 'list', 'elements': 'str', 'default': [], 'no_log': True},
        'operation': {'type': 'str', 'required': True, 'choices': ['publish', 'inspect', 'remove', 'sweep']},
    }, supports_check_mode=False)
    try:
        result = operate(**module.params)
    except LayoutError as error:
        module.fail_json(msg='the reserved cluster image publication could not be proved: %s; fix that directory '
                             'and repeat the operation' % error)
    except (OSError, ValueError, KeyError):
        module.fail_json(msg='the reserved cluster image publication could not be proved')
    if result.get('refused'):
        module.fail_json(
            msg='%d entries beneath the reserved cluster image root are named as insert links but are not '
                'directories holding only this cluster\'s published image; they were left in place for '
                'inspection and %d unpresented insert links were removed' % (result['refused'], result['removed']),
            **result)
    module.exit_json(**result)


if __name__ == '__main__':
    main()
