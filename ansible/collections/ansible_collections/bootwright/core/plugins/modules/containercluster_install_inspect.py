#!/usr/bin/python
"""Observe what one cluster's installation recorded, without changing any of it."""

from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_install_inspect
short_description: Observe one container cluster's own installation record
version_added: "0.1.0"
description:
  - Reports this build's own trust anchor, the SHA-256 of the certificate
    authority and client certificate in the administrator kubeconfig the
    installer wrote beside the image, and the address of the image its media
    block published.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen installation request.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the installation record
  bootwright.core.containercluster_install_inspect:
    request: '{{ bootwright_cluster_install_request }}'
"""

RETURN = r"""
observation:
  description: This build's trust anchor and the published image address.
  returned: always
  type: dict
"""

import base64
import binascii
import hashlib
import os
import re

from ansible.module_utils.basic import AnsibleModule

# KUBECONFIG is the administrator kubeconfig `openshift-install agent create
# image` writes into the work area together with the image, and IMAGE the name
# the media block published the image under inside the unguessable directory
# that attempt minted. The agent installer writes no metadata.json: its image
# target persists only the image, this kubeconfig and the administrator
# password (cmd/openshift-install/agent.go, agentImageTarget, release-4.21).
KUBECONFIG = os.path.join("auth", "kubeconfig")
IMAGE = "agent.iso"
MAX_KUBECONFIG = 65536

# The installer marshals one clientcmd v1 Config through sigs.k8s.io/yaml:
# block style, sorted keys, one cluster, one user and one context, each value a
# plain scalar unless it would read as another type, as a cluster named "on"
# would, when it is double-quoted (pkg/asset/kubeconfig/kubeconfig.go; the
# emitter's stringv in go.yaml.in/yaml/v2 encode.go). A key outside this set,
# such as insecure-skip-tls-verify, a token or an exec plugin, would let a read
# through the file succeed without the anchor it names, so a kubeconfig
# carrying one, or any shape the installer does not write, names no identity.
KEYS = frozenset((
    "certificate-authority-data", "client-certificate-data", "client-key-data", "cluster",
    "clusters", "context", "contexts", "current-context", "name", "preferences", "server",
    "user", "users",
))
ENTRY = re.compile(r'^( *)(- )?([a-z][a-z-]*):(?: ([A-Za-z0-9+/=:._-]+|"[A-Za-z0-9._-]+"|\{\}))?$')
SINGLE = ("clusters", "contexts", "users")
ANCHOR = ("certificate-authority-data", "client-certificate-data")
PEM = b"-----BEGIN CERTIFICATE-----"


def scalars(text):
    """Every value by key, or nothing when the file is not the installer's shape."""
    values, items, section = {}, {}, None
    for line in text.splitlines():
        entry = ENTRY.match(line)
        if entry is None or entry.group(3) not in KEYS:
            return None
        indent, item, key, value = entry.groups()
        if not indent and item:
            items[section] = items.get(section, 0) + 1
        elif not indent:
            section = key
        values.setdefault(key, []).append(value or "")
    if any(items.get(name) != 1 for name in SINGLE):
        return None
    return values


def certificate(values):
    """The PEM one base64 kubeconfig value carries, or nothing."""
    if len(values) != 1:
        return b""
    try:
        decoded = base64.b64decode(values[0], validate=True)
    except (binascii.Error, ValueError):
        return b""
    return decoded if decoded.startswith(PEM) else b""


def identity(path):
    """This build's own trust anchor, or nothing.

    The certificate authority the kubeconfig verifies the cluster's serving
    certificate against and the client certificate it authenticates with were
    both minted when the image was built, so their digest names the cluster
    that image installs and no other. Only certificates are hashed; the client
    key is read with the file and never leaves this function.
    """
    try:
        if os.path.getsize(path) > MAX_KUBECONFIG:
            return ""
        with open(path, "rb") as handle:
            text = handle.read().decode("utf-8")
    except (OSError, UnicodeDecodeError):
        return ""
    values = scalars(text)
    if values is None:
        return ""
    anchor = [certificate(values.get(key, [])) for key in ANCHOR]
    if not all(anchor):
        return ""
    return hashlib.sha256(anchor[0] + b"\0" + anchor[1]).hexdigest()


def published(root, base):
    """The address the image is published at, or nothing.

    The final segment is the unguessable one the publishing attempt minted, so
    this is the only place it is read, and every task that carries the result
    keeps it out of its own output.
    """
    try:
        entries = sorted(os.listdir(root))
    except OSError:
        return ""
    for entry in entries:
        if os.path.isfile(os.path.join(root, entry, IMAGE)):
            return base + "/" + entry + "/" + IMAGE
    return ""


def observe(request):
    """What this installation's work area and publication hold now."""
    image = request["image"]
    return {
        "identity": identity(os.path.join(request["workRoot"], KUBECONFIG)),
        "kubeconfig": os.path.isfile(os.path.join(request["workRoot"], KUBECONFIG)),
        "url": published(image["path"], image["url"]),
    }


def main():
    module = AnsibleModule(
        argument_spec={"request": {"type": "dict", "required": True}},
        supports_check_mode=True,
    )
    module.exit_json(changed=False, observation=observe(module.params["request"]))


if __name__ == "__main__":
    main()
